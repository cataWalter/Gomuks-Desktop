package matrix

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/gomuks/pkg/hicli"
	"go.mau.fi/gomuks/pkg/hicli/database"
	"go.mau.fi/gomuks/pkg/hicli/jsoncmd"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/attachment"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

const eventBufferSize = 256
const timelinePageSize = 100

// Room and Message are UI-facing snapshots that do not expose gomuks internals.
type Room struct {
	ID       string
	Name     string
	Topic    string
	Avatar   string
	Preview  string
	LastSeen time.Time
	Direct   bool
	Favorite bool
	Muted    bool
	Unread   int
	Mentions int
	Unanswered bool
	LastEventID string
	LastEventSender string
	LastEventAt time.Time
}

type Message struct {
	ID             string
	TransactionID  string
	Sender         string
	SenderName     string
	Avatar         string
	Body           string
	FormattedBody  string
	Timestamp      time.Time
	TimelineCursor int64
	Sending        bool
	SendError      string
	Own            bool
	Reactions      map[string]int
	MediaURI       string
	MediaMIME      string
	MediaEncrypted *event.EncryptedFileInfo
}

type Member struct {
	ID          string
	DisplayName string
}

type HistoryPage struct {
	Messages []Message
	Before   int64
	HasMore  bool
}

// Update is emitted when gomuks has persisted sync data to its local database.
type Update struct {
	Rooms                 []Room
	ChangedRoomID         []string
	TimelineChangedRoomID []string
	TypingUsers           map[string][]string
	Notifications         []Notification
	Error                 string
	InitialSyncComplete   bool
}

type Notification struct {
	RoomID  string
	Room    string
	Sender  string
	Body    string
	Mention bool
}

type Client struct {
	engine *hicli.HiClient
	dbPath string
	events chan Update
	done   chan struct{}

	mu                   sync.RWMutex
	rooms                map[string]Room
	local                map[string]map[string]Message
	typing               map[string][]string
	pendingNotifications []Notification
	notifiedEvents       map[string]struct{}
	pendingTimelineRooms map[string]struct{}
	dismissedEvents      map[string]string
	syncReady            bool

	closeOnce       sync.Once
	eventMu         sync.Mutex
	sendMu          sync.Mutex
	startMu         sync.Mutex
	started         bool
	lifecycleMu     sync.Mutex
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	operations      sync.WaitGroup
	closed          bool
}

// New opens gomuks' SQLite stores, restores the cached account if present, and
// loads cached room metadata. Network syncing is started separately by Start.
func New(databasePath string) (*Client, error) {
	databasePath, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve Matrix database path: %w", err)
	}
	dataDir := filepath.Dir(databasePath)
	if err = os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create Matrix data directory: %w", err)
	}
	if err = os.Chmod(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure Matrix data directory: %w", err)
	}
	pickleKey, err := loadOrCreatePickleKey(filepath.Join(dataDir, "crypto.key"))
	if err != nil {
		return nil, err
	}
	rawDB, err := dbutil.NewWithDialect(databasePath, "sqlite3-fk-wal")
	if err != nil {
		return nil, fmt.Errorf("open gomuks SQLite database: %w", err)
	}

	client := &Client{
		dbPath:               databasePath,
		events:               make(chan Update, eventBufferSize),
		done:                 make(chan struct{}),
		rooms:                make(map[string]Room),
		local:                make(map[string]map[string]Message),
		typing:               make(map[string][]string),
		notifiedEvents:       make(map[string]struct{}),
		pendingTimelineRooms: make(map[string]struct{}),
		dismissedEvents:      make(map[string]string),
	}
	client.lifecycleCtx, client.lifecycleCancel = context.WithCancel(context.Background())
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	// hicli is gomuks' embeddable Matrix engine; its callback is invoked from
	// sync and decryption goroutines, so it only publishes immutable snapshots.
	client.engine = hicli.New(rawDB, nil, logger, pickleKey, client.handleBackendEvent)

	ctx := context.Background()
	// Loading with an empty user ID upgrades the schema even when no account
	// has been saved yet, allowing the login form to use the same database.
	if err = client.engine.Load(ctx, ""); err != nil {
		client.Close()
		return nil, fmt.Errorf("initialize gomuks database: %w", err)
	}
	if err = client.engine.DB.Exec(ctx, `CREATE TABLE IF NOT EXISTS room_unanswered_status (
		room_id TEXT PRIMARY KEY,
		dismissed_event_id TEXT NOT NULL,
		dismissed_at INTEGER NOT NULL
	)`); err != nil {
		client.Close()
		return nil, fmt.Errorf("initialize unanswered chat state: %w", err)
	}
	dismissedRows, err := client.engine.DB.Query(ctx, `SELECT room_id, dismissed_event_id FROM room_unanswered_status`)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("load unanswered chat state: %w", err)
	}
	for dismissedRows.Next() {
		var roomID, eventID string
		if err = dismissedRows.Scan(&roomID, &eventID); err != nil {
			_ = dismissedRows.Close()
			client.Close()
			return nil, fmt.Errorf("decode unanswered chat state: %w", err)
		}
		client.dismissedEvents[roomID] = eventID
	}
	if err = dismissedRows.Err(); err != nil {
		_ = dismissedRows.Close()
		client.Close()
		return nil, fmt.Errorf("read unanswered chat state: %w", err)
	}
	_ = dismissedRows.Close()
	userID, err := client.engine.DB.Account.GetFirstUserID(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("read cached Matrix account: %w", err)
	}

	if userID != "" {
		if err = client.engine.Load(ctx, userID); err != nil {
			client.Close()
			return nil, fmt.Errorf("restore cached Matrix account: %w", err)
		}
		for initialSync := range client.engine.GetInitialSync(ctx, 100, 0) {
			client.applySync(initialSync, false)
		}
	}
	return client, nil
}

func (c *Client) SetRoomUnanswered(ctx context.Context, roomID string, unanswered bool) error {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()

	c.mu.RLock()
	room, exists := c.rooms[roomID]
	c.mu.RUnlock()
	if !exists {
		return fmt.Errorf("room %q is not available", roomID)
	}

	if unanswered {
		if err = c.engine.DB.Exec(ctx, `DELETE FROM room_unanswered_status WHERE room_id = ?`, roomID); err != nil {
			return fmt.Errorf("restore unanswered chat state: %w", err)
		}
		c.mu.Lock()
		delete(c.dismissedEvents, roomID)
		if current, ok := c.rooms[roomID]; ok {
			current.Unanswered = roomNeedsReply(current, c.currentUserID(), "")
			c.rooms[roomID] = current
		}
		c.mu.Unlock()
	} else {
		eventID := room.LastEventID
		if eventID == "" {
			eventID = room.ID
		}
		if err = c.engine.DB.Exec(ctx, `INSERT INTO room_unanswered_status (room_id, dismissed_event_id, dismissed_at)
			VALUES (?, ?, ?) ON CONFLICT(room_id) DO UPDATE SET
			dismissed_event_id = excluded.dismissed_event_id, dismissed_at = excluded.dismissed_at`,
			roomID, eventID, time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("save answered chat state: %w", err)
		}
		c.mu.Lock()
		c.dismissedEvents[roomID] = eventID
		if current, ok := c.rooms[roomID]; ok {
			current.Unanswered = false
			c.rooms[roomID] = current
		}
		c.mu.Unlock()
	}
	c.publish([]string{roomID}, "")
	return nil
}

func loadOrCreatePickleKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, fmt.Errorf("invalid gomuks crypto key at %q: expected 32 bytes", path)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read gomuks crypto key: %w", err)
	}

	key = make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate gomuks crypto key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		key, err = os.ReadFile(path)
		if err == nil && len(key) != 32 {
			return nil, fmt.Errorf("invalid gomuks crypto key at %q: expected 32 bytes", path)
		}
		return key, err
	}
	if err != nil {
		return nil, fmt.Errorf("create gomuks crypto key: %w", err)
	}
	written, err := file.Write(key)
	if err == nil && written != len(key) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write gomuks crypto key: %w", err)
	}
	if err = file.Close(); err != nil {
		return nil, fmt.Errorf("close gomuks crypto key: %w", err)
	}
	return key, nil
}

func (c *Client) Events() <-chan Update {
	return c.events
}

func (c *Client) IsLoggedIn() bool {
	return c.engine.IsLoggedIn()
}

func (c *Client) NeedsVerification() bool {
	return c.engine.IsLoggedIn() && !c.engine.VerificationState.IsVerified
}

func (c *Client) SyncReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.syncReady
}

func (c *Client) Rooms() []Room {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rooms := make([]Room, 0, len(c.rooms))
	for _, room := range c.rooms {
		rooms = append(rooms, room)
	}
	sort.Slice(rooms, func(i, j int) bool {
		if rooms[i].LastSeen.Equal(rooms[j].LastSeen) {
			left, right := strings.ToLower(rooms[i].Name), strings.ToLower(rooms[j].Name)
			if left == right {
				return rooms[i].ID < rooms[j].ID
			}
			return left < right
		}
		return rooms[i].LastSeen.After(rooms[j].LastSeen)
	})
	return rooms
}

func (c *Client) Messages(ctx context.Context, roomID string) ([]Message, error) {
	page, err := c.HistoryPage(ctx, roomID, 0)
	if err != nil {
		return nil, err
	}
	return page.Messages, nil
}

func (c *Client) HistoryPage(ctx context.Context, roomID string, before int64) (HistoryPage, error) {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return HistoryPage{}, err
	}
	defer finish()
	events, err := c.engine.DB.Timeline.Get(ctx, id.RoomID(roomID), timelinePageSize, database.TimelineRowID(before))
	if err != nil {
		return HistoryPage{}, fmt.Errorf("read room timeline: %w", err)
	}
	page := HistoryPage{HasMore: len(events) == timelinePageSize}
	if len(events) > 0 {
		page.Before = int64(events[len(events)-1].TimelineRowID)
	}
	messages := make([]Message, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		if message, ok := messageFromEvent(events[i]); ok {
			messages = append(messages, message)
		}
	}
	if before == 0 {
		c.mu.Lock()
		localMessages := c.local[roomID]
		for key, message := range localMessages {
			if containsMessage(messages, message) {
				delete(localMessages, key)
				continue
			}
			messages = append(messages, message)
		}
		if len(localMessages) == 0 {
			delete(c.local, roomID)
		}
		c.mu.Unlock()
	}
	c.populateMessageProfiles(ctx, roomID, messages)
	for i := range messages {
		messages[i].Own = c.isOwnMessage(messages[i])
	}
	sort.SliceStable(messages, func(i, j int) bool {
		if messages[i].Timestamp.Equal(messages[j].Timestamp) {
			return messages[i].ID < messages[j].ID
		}
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})
	page.Messages = messages
	return page, nil
}

func (c *Client) Login(ctx context.Context, homeserver, username, password string) error {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	// LoginPassword uses gomuks' credential storage and crypto initialization.
	if err = c.engine.LoginPassword(ctx, homeserver, username, password); err != nil {
		return fmt.Errorf("Matrix login: %w", err)
	}
	if err = c.startEngine(ctx); err != nil {
		return fmt.Errorf("start Matrix sync: %w", err)
	}
	return nil
}

func (c *Client) Verify(ctx context.Context, recoveryKeyOrPassphrase string) error {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if err = c.engine.Verify(ctx, recoveryKeyOrPassphrase); err != nil {
		return fmt.Errorf("verify Matrix encryption keys: %w", err)
	}
	return nil
}

func (c *Client) Start(ctx context.Context) error {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	return c.startEngine(ctx)
}

func (c *Client) startEngine(ctx context.Context) error {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	if c.started {
		return nil
	}
	// Start initializes the gomuks client and launches its Matrix sync loop.
	if err := c.engine.Start(ctx); err != nil {
		return err
	}
	c.started = true
	return nil
}

func (c *Client) SendMessage(ctx context.Context, roomID, text string) error {
	return c.sendMessage(ctx, roomID, text, nil, nil)
}

func (c *Client) SendMessageWithMentions(ctx context.Context, roomID, text string, userIDs []string) error {
	mentions := &event.Mentions{}
	for _, userID := range userIDs {
		matrixUserID := id.UserID(userID)
		if _, _, err := matrixUserID.ParseAndValidateStrict(); err == nil {
			mentions.UserIDs = append(mentions.UserIDs, matrixUserID)
		}
	}
	if len(mentions.UserIDs) == 0 {
		mentions = nil
	}
	return c.sendMessage(ctx, roomID, text, nil, mentions)
}

func (c *Client) SendReply(ctx context.Context, roomID, text, replyTo string) error {
	if replyTo == "" {
		return errors.New("reply target event ID is empty")
	}
	return c.sendMessage(ctx, roomID, text, &event.RelatesTo{
		InReplyTo: &event.InReplyTo{EventID: id.EventID(replyTo)},
	}, nil)
}

func (c *Client) EditMessage(ctx context.Context, roomID, text, editID string) error {
	if editID == "" {
		return errors.New("edit target event ID is empty")
	}
	return c.sendMessage(ctx, roomID, text, &event.RelatesTo{
		Type:    event.RelReplace,
		EventID: id.EventID(editID),
	}, nil)
}

func (c *Client) SendAttachment(ctx context.Context, roomID, filename string, data []byte) error {
	if len(data) == 0 {
		return errors.New("attachment is empty")
	}
	const maxAttachmentBytes = 50 << 20
	if len(data) > maxAttachmentBytes {
		return fmt.Errorf("attachment exceeds %d MiB", maxAttachmentBytes>>20)
	}
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "" {
		return errors.New("attachment filename is empty")
	}
	mimeType := http.DetectContentType(data)
	if detected, _, err := mime.ParseMediaType(mimeType); err == nil {
		mimeType = detected
	}
	msgType := event.MsgFile
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		msgType = event.MsgImage
	case strings.HasPrefix(mimeType, "video/"):
		msgType = event.MsgVideo
	case strings.HasPrefix(mimeType, "audio/"):
		msgType = event.MsgAudio
	}
	info := &event.FileInfo{MimeType: mimeType, Size: len(data)}
	if msgType == event.MsgImage {
		if config, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			info.Width, info.Height = config.Width, config.Height
		}
	}
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	encrypted, err := c.engine.ClientStore.IsEncrypted(ctx, id.RoomID(roomID))
	if err != nil {
		return fmt.Errorf("check room encryption before uploading attachment: %w", err)
	}
	content := &event.MessageEventContent{
		MsgType:  msgType,
		Body:     filename,
		FileName: filename,
		Info:     info,
	}
	uploadData := data
	var encryptedFile *attachment.EncryptedFile
	if encrypted {
		encryptedFile = attachment.NewEncryptedFile()
		uploadData = encryptedFile.Encrypt(data)
	}
	uploadType := mimeType
	if encrypted {
		uploadType = "application/octet-stream"
	}
	upload, err := c.engine.Client.UploadMedia(ctx, mautrix.ReqUploadMedia{
		ContentBytes: uploadData,
		ContentType:  uploadType,
		FileName:     filename,
	})
	if err != nil {
		return fmt.Errorf("upload Matrix attachment: %w", err)
	}
	if encrypted {
		content.File = &event.EncryptedFileInfo{
			EncryptedFile: *encryptedFile,
			URL:           upload.ContentURI.CUString(),
		}
	} else {
		content.URL = upload.ContentURI.CUString()
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	evt, err := c.engine.Send(ctx, id.RoomID(roomID), event.EventMessage, content, false, false)
	if err != nil {
		return fmt.Errorf("send Matrix attachment event: %w", err)
	}
	if message, ok := messageFromEvent(evt); ok {
		message.Sending = true
		message.Own = c.isOwnMessage(message)
		c.storeLocalMessage(roomID, message)
	}
	c.publishTimeline([]string{roomID}, "")
	return nil
}

func (c *Client) sendMessage(ctx context.Context, roomID, text string, relation *event.RelatesTo, mentions *event.Mentions) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	// gomuks persists the event and sends it asynchronously through its engine.
	// Serialize registration with SendComplete so a very fast response cannot
	// race ahead of the optimistic local timeline entry.
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	evt, err := c.engine.SendMessage(ctx, id.RoomID(roomID), nil, nil, text, relation, mentions, nil)
	if err != nil {
		return fmt.Errorf("send Matrix message: %w", err)
	}
	if message, ok := messageFromEvent(evt); ok {
		message.Sending = true
		message.Own = c.isOwnMessage(message)
		c.storeLocalMessage(roomID, message)
	}
	c.publishTimeline([]string{roomID}, "")
	return nil
}

func (c *Client) React(ctx context.Context, roomID, eventID, emoji string) error {
	if eventID == "" || strings.TrimSpace(emoji) == "" {
		return errors.New("reaction requires a target event and emoji")
	}
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if _, err = c.engine.Client.SendReaction(ctx, id.RoomID(roomID), id.EventID(eventID), emoji); err != nil {
		return fmt.Errorf("send Matrix reaction: %w", err)
	}
	c.publishTimeline([]string{roomID}, "")
	return nil
}

func (c *Client) RedactMessage(ctx context.Context, roomID, eventID string) error {
	if eventID == "" || strings.HasPrefix(eventID, "~") {
		return errors.New("cannot redact a message without a server event ID")
	}
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if _, err = c.engine.Client.RedactEvent(ctx, id.RoomID(roomID), id.EventID(eventID)); err != nil {
		return fmt.Errorf("redact Matrix message: %w", err)
	}
	c.publish([]string{roomID}, "")
	return nil
}

func (c *Client) isOwnMessage(message Message) bool {
	return c.engine.Account != nil && message.Sender == c.engine.Account.UserID.String()
}

func (c *Client) RetryMessage(ctx context.Context, transactionID string) error {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if _, err = c.engine.Resend(ctx, transactionID); err != nil {
		return fmt.Errorf("retry Matrix message: %w", err)
	}
	return nil
}

func (c *Client) MarkRead(ctx context.Context, roomID, eventID string) error {
	if eventID == "" {
		return nil
	}
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if err = c.engine.MarkRead(ctx, id.RoomID(roomID), id.EventID(eventID), event.ReceiptTypeRead); err != nil {
		return fmt.Errorf("mark Matrix room as read: %w", err)
	}
	c.mu.Lock()
	room, ok := c.rooms[roomID]
	if ok {
		room.Unread = 0
		room.Mentions = 0
		c.rooms[roomID] = room
	}
	c.mu.Unlock()
	if ok {
		c.publish(nil, "")
	}
	return nil
}

func (c *Client) SetTyping(ctx context.Context, roomID string, timeout time.Duration) error {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if err = c.engine.SetTyping(ctx, id.RoomID(roomID), timeout); err != nil {
		return fmt.Errorf("update Matrix typing status: %w", err)
	}
	return nil
}

func (c *Client) SearchMessages(ctx context.Context, roomID, query string, limit int) ([]Message, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("message search query is empty")
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	response, err := c.engine.SearchLocal(ctx, &jsoncmd.SearchParams{
		SearchTerm: query,
		RoomIDs:    []id.RoomID{id.RoomID(roomID)},
		Limit:      limit,
		SortByTime: true,
	})
	if err != nil {
		return nil, fmt.Errorf("search local Matrix history: %w", err)
	}
	messages := make([]Message, 0, len(response.Events))
	for _, evt := range response.Events {
		if message, ok := messageFromEvent(evt); ok {
			messages = append(messages, message)
		}
	}
	c.populateMessageProfiles(ctx, roomID, messages)
	return messages, nil
}

func (c *Client) RoomMembers(ctx context.Context, roomID string) ([]Member, error) {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	events, err := c.engine.DB.CurrentState.GetMembers(ctx, id.RoomID(roomID))
	if err != nil {
		return nil, fmt.Errorf("read Matrix room members: %w", err)
	}
	members := make([]Member, 0, len(events))
	for _, evt := range events {
		if evt == nil || evt.StateKey == nil {
			continue
		}
		var profile event.MemberEventContent
		if err = json.Unmarshal(evt.GetContent(), &profile); err != nil {
			log.Printf("decode member profile in room %s: %v", roomID, err)
			continue
		}
		if profile.Membership != event.MembershipJoin && profile.Membership != event.MembershipInvite {
			continue
		}
		displayName := strings.TrimSpace(profile.Displayname)
		if displayName == "" {
			displayName = *evt.StateKey
		}
		members = append(members, Member{ID: *evt.StateKey, DisplayName: displayName})
	}
	sort.Slice(members, func(i, j int) bool {
		return strings.ToLower(members[i].DisplayName) < strings.ToLower(members[j].DisplayName)
	})
	return members, nil
}

func (c *Client) LeaveRoom(ctx context.Context, roomID string) error {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if _, err = c.engine.Client.LeaveRoom(ctx, id.RoomID(roomID)); err != nil {
		return fmt.Errorf("leave Matrix room: %w", err)
	}
	c.mu.Lock()
	delete(c.rooms, roomID)
	delete(c.local, roomID)
	c.mu.Unlock()
	c.publishTimeline([]string{roomID}, "")
	return nil
}

func (c *Client) Logout(ctx context.Context) error {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	if c.engine.IsLoggedIn() {
		_, err = c.engine.Client.Logout(ctx)
	}
	finish()
	if err != nil {
		return fmt.Errorf("terminate Matrix session: %w", err)
	}
	c.Close()
	for _, path := range []string{
		c.dbPath, c.dbPath + "-wal", c.dbPath + "-shm", filepath.Join(filepath.Dir(c.dbPath), "crypto.key"),
	} {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("clear local Matrix session data at %q: %w", path, removeErr)
		}
	}
	c.mu.Lock()
	clear(c.rooms)
	clear(c.local)
	c.mu.Unlock()
	return nil
}

func (c *Client) DownloadAvatar(ctx context.Context, mxc string) ([]byte, error) {
	if strings.TrimSpace(mxc) == "" {
		return nil, nil
	}
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	uri, err := id.ContentURIString(mxc).Parse()
	if err != nil {
		return nil, fmt.Errorf("parse Matrix avatar URI: %w", err)
	}
	response, err := c.engine.Client.Download(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("download Matrix avatar: %w", err)
	}
	defer response.Body.Close()
	const maxAvatarBytes = 4 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxAvatarBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Matrix avatar: %w", err)
	}
	if len(data) > maxAvatarBytes {
		return nil, fmt.Errorf("Matrix avatar exceeds %d bytes", maxAvatarBytes)
	}
	return data, nil
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.lifecycleMu.Lock()
		c.closed = true
		c.lifecycleCancel()
		close(c.done)
		c.lifecycleMu.Unlock()
		c.operations.Wait()
		c.engine.Stop()
	})
}

func (c *Client) beginOperation(parent context.Context) (context.Context, func(), error) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.closed {
		return nil, nil, errors.New("Matrix client is closed")
	}
	c.operations.Add(1)
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(c.lifecycleCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
		c.operations.Done()
	}, nil
}

func (c *Client) handleBackendEvent(raw any) {
	switch evt := raw.(type) {
	case *jsoncmd.SyncComplete:
		c.applySync(evt, true)
	case *jsoncmd.EventsDecrypted:
		c.publishTimeline([]string{evt.RoomID.String()}, "")
	case *jsoncmd.Typing:
		roomID := evt.RoomID.String()
		users := make([]string, 0, len(evt.UserIDs))
		for _, userID := range evt.UserIDs {
			if c.engine.Account == nil || userID != c.engine.Account.UserID {
				users = append(users, userID.String())
			}
		}
		c.mu.Lock()
		if len(users) == 0 {
			delete(c.typing, roomID)
		} else {
			c.typing[roomID] = users
		}
		c.mu.Unlock()
		c.publish(nil, "")
	case *jsoncmd.SendComplete:
		if evt.Event != nil {
			c.sendMu.Lock()
			if message, ok := messageFromEvent(evt.Event); ok {
				message.Sending = false
				message.SendError = ""
				message.Own = c.isOwnMessage(message)
				if evt.Error != nil {
					message.SendError = evt.Error.Error()
				}
				c.storeLocalMessage(evt.Event.RoomID.String(), message)
			}
			c.sendMu.Unlock()
			errorMessage := ""
			if evt.Error != nil {
				errorMessage = evt.Error.Error()
			}
			c.publishTimeline([]string{evt.Event.RoomID.String()}, errorMessage)
		} else if evt.Error != nil {
			c.publish(nil, evt.Error.Error())
		}
	case *jsoncmd.SyncStatus:
		message := ""
		if evt.Type == jsoncmd.SyncStatusErroring || evt.Type == jsoncmd.SyncStatusFailed {
			message = evt.Error
		} else if evt.Type == jsoncmd.SyncStatusOK {
			c.mu.Lock()
			c.syncReady = true
			c.mu.Unlock()
		}
		c.publish(nil, message)
	case *jsoncmd.InitComplete:
		c.mu.Lock()
		c.syncReady = true
		c.mu.Unlock()
		c.publish(nil, "")
	}
}

func (c *Client) applySync(syncEvent *jsoncmd.SyncComplete, notify bool) {
	changed := make([]string, 0, len(syncEvent.Rooms)+len(syncEvent.LeftRooms))
	c.mu.Lock()
	for roomID, syncRoom := range syncEvent.Rooms {
		if syncRoom == nil {
			continue
		}
		key := roomID.String()
		room, exists := c.rooms[key]
		if !exists {
			room = Room{ID: key, Name: key}
		}
		if meta := syncRoom.Meta; meta != nil {
			room.Name = roomDisplayName(meta)
			if meta.Topic != nil {
				room.Topic = *meta.Topic
			}
			if meta.Avatar != nil {
				room.Avatar = meta.Avatar.String()
			}
			room.Direct = meta.DMUserID != nil
			room.Unread = meta.UnreadMessages + meta.UnreadNotifications
			room.Mentions = meta.UnreadHighlights
			if !meta.SortingTimestamp.Time.IsZero() {
				room.LastSeen = meta.SortingTimestamp.Time
			}
		}
		if notify && (syncRoom.Reset || len(syncRoom.Timeline) > 0) {
			c.pendingTimelineRooms[key] = struct{}{}
		}
		if notify {
			for _, notification := range syncRoom.Notifications {
				if notification.Event == nil || (!notification.Highlight && !room.Direct) {
					continue
				}
				if c.engine.Account != nil && notification.Event.Sender == c.engine.Account.UserID {
					continue
				}
				eventID := notification.Event.ID.String()
				if eventID == "" {
					continue
				}
				if _, seen := c.notifiedEvents[eventID]; seen {
					continue
				}
				c.notifiedEvents[eventID] = struct{}{}
				if message, ok := messageFromEvent(notification.Event); ok {
					sender := message.SenderName
					if sender == "" {
						sender = message.Sender
					}
					c.pendingNotifications = append(c.pendingNotifications, Notification{
						RoomID:  key,
						Room:    room.Name,
						Sender:  sender,
						Body:    message.Body,
						Mention: notification.Highlight,
					})
				}
			}
		}
		for _, evt := range syncRoom.Events {
			c.removeLocalMessage(key, evt)
			if evt != nil && !evt.Timestamp.Time.Before(room.LastEventAt) {
				room.LastEventAt = evt.Timestamp.Time
				room.LastEventID = evt.ID.String()
				room.LastEventSender = evt.Sender.String()
			}
			message, ok := messageFromEvent(evt)
			if !ok {
				continue
			}
			if room.Preview == "" || message.Timestamp.After(room.LastSeen) {
				room.LastSeen = message.Timestamp
				room.Preview = message.Body
			}
		}
		room.Unanswered = roomNeedsReply(room, c.currentUserID(), c.dismissedEvents[key])
		c.rooms[key] = room
		changed = append(changed, key)
	}
	for _, roomID := range syncEvent.LeftRooms {
		key := roomID.String()
		delete(c.rooms, key)
		changed = append(changed, key)
	}
	c.mu.Unlock()
	if notify && len(changed) > 0 {
		c.publish(changed, "")
	}
}

func (c *Client) populateMessageProfiles(ctx context.Context, roomID string, messages []Message) {
	users := make(map[string]struct{})
	for i := range messages {
		if messages[i].Sender != "" {
			users[messages[i].Sender] = struct{}{}
		}
	}
	if len(users) == 0 {
		return
	}
	keys := make([]database.RoomStateGUID, 0, len(users))
	for userID := range users {
		keys = append(keys, database.RoomStateGUID{
			RoomID:   id.RoomID(roomID),
			Type:     event.StateMember,
			StateKey: userID,
		})
	}
	members, err := c.engine.DB.CurrentState.GetMany(ctx, keys)
	if err != nil {
		log.Printf("read Matrix member profiles for room %s: %v", roomID, err)
		return
	}
	profiles := make(map[string]event.MemberEventContent, len(members))
	for _, member := range members {
		if member.StateKey == nil {
			continue
		}
		var profile event.MemberEventContent
		if err = json.Unmarshal(member.GetContent(), &profile); err != nil {
			log.Printf("decode Matrix member profile in room %s: %v", roomID, err)
			continue
		}
		profiles[*member.StateKey] = profile
	}
	for i := range messages {
		if profile, ok := profiles[messages[i].Sender]; ok {
			if profile.Displayname != "" {
				messages[i].SenderName = profile.Displayname
			}
			messages[i].Avatar = string(profile.AvatarURL)
		}
	}
}

func roomDisplayName(room *database.Room) string {
	if room.Name != nil && strings.TrimSpace(*room.Name) != "" {
		return *room.Name
	}
	if room.CanonicalAlias != nil && *room.CanonicalAlias != "" {
		return room.CanonicalAlias.String()
	}
	return room.ID.String()
}

func messageFromEvent(evt *database.Event) (Message, bool) {
	if evt == nil {
		return Message{}, false
	}
	evtType := evt.GetType().Type
	if evtType != event.EventMessage.Type && evtType != event.EventSticker.Type {
		return Message{}, false
	}
	var content struct {
		Body          string                   `json:"body"`
		FormattedBody string                   `json:"formatted_body"`
		URL           string                   `json:"url"`
		File          *event.EncryptedFileInfo `json:"file"`
		Info          *event.FileInfo          `json:"info"`
	}
	if err := json.Unmarshal(evt.GetContent(), &content); err != nil || content.Body == "" {
		return Message{}, false
	}
	return Message{
		ID:             evt.ID.String(),
		TransactionID:  evt.TransactionID,
		Sender:         evt.Sender.String(),
		Body:           content.Body,
		FormattedBody:  content.FormattedBody,
		Timestamp:      evt.Timestamp.Time,
		TimelineCursor: int64(evt.TimelineRowID),
		Sending:        evt.SendError == "not sent",
		SendError:      evt.SendError,
		Reactions:      evt.Reactions,
		MediaURI:       content.URL,
		MediaEncrypted: content.File,
		MediaMIME:      mediaMIME(content.Info),
	}, true
}

func mediaMIME(info *event.FileInfo) string {
	if info == nil {
		return ""
	}
	return info.MimeType
}

func (c *Client) storeLocalMessage(roomID string, message Message) {
	key := message.TransactionID
	if key == "" {
		key = message.ID
	}
	c.mu.Lock()
	if c.local[roomID] == nil {
		c.local[roomID] = make(map[string]Message)
	}
	c.local[roomID][key] = message
	room, exists := c.rooms[roomID]
	if exists {
		if message.Timestamp.After(room.LastSeen) {
			room.LastSeen = message.Timestamp
			room.Preview = message.Body
		}
		if !message.Timestamp.Before(room.LastEventAt) {
			room.LastEventAt = message.Timestamp
		room.LastEventID = message.ID
		room.LastEventSender = message.Sender
		room.Unanswered = roomNeedsReply(room, c.currentUserID(), c.dismissedEvents[roomID])
		}
		c.rooms[roomID] = room
	}
	c.mu.Unlock()
}

func roomNeedsReply(room Room, userID, dismissedEventID string) bool {
	if dismissedEventID != "" && room.LastEventID == dismissedEventID {
		return false
	}
	return room.Unread > 0 || (room.LastEventSender != "" && room.LastEventSender != userID)
}

func (c *Client) currentUserID() string {
	if c.engine == nil || c.engine.Account == nil {
		return ""
	}
	return c.engine.Account.UserID
}

func (c *Client) removeLocalMessage(roomID string, evt *database.Event) {
	if evt == nil {
		return
	}
	messages := c.local[roomID]
	for key, message := range messages {
		if message.ID == evt.ID.String() ||
			(message.TransactionID != "" && message.TransactionID == evt.TransactionID) {
			delete(messages, key)
		}
	}
	if len(messages) == 0 {
		delete(c.local, roomID)
	}
}

func containsMessage(messages []Message, candidate Message) bool {
	for _, message := range messages {
		if message.ID == candidate.ID ||
			(candidate.TransactionID != "" && message.TransactionID == candidate.TransactionID) {
			return true
		}
	}
	return false
}

func (c *Client) publish(changed []string, errMessage string) {
	update := Update{
		Rooms:               c.Rooms(),
		ChangedRoomID:       append([]string(nil), changed...),
		Error:               errMessage,
		InitialSyncComplete: c.SyncReady(),
	}
	c.mu.Lock()
	if c.pendingTimelineRooms == nil {
		c.pendingTimelineRooms = make(map[string]struct{})
	}
	update.TimelineChangedRoomID = make([]string, 0, len(c.pendingTimelineRooms))
	for roomID := range c.pendingTimelineRooms {
		update.TimelineChangedRoomID = append(update.TimelineChangedRoomID, roomID)
	}
	clear(c.pendingTimelineRooms)
	update.TypingUsers = make(map[string][]string, len(c.typing))
	for roomID, users := range c.typing {
		update.TypingUsers[roomID] = append([]string(nil), users...)
	}
	update.Notifications = append([]Notification(nil), c.pendingNotifications...)
	c.pendingNotifications = c.pendingNotifications[:0]
	c.mu.Unlock()
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	select {
	case <-c.done:
		return
	default:
	}
	select {
	case c.events <- update:
		return
	default:
	}

	// Keep the callback non-blocking even if Fyne is busy rendering. The next
	// delivered update contains the newest room snapshot and all pending IDs.
	changedSet := make(map[string]struct{}, len(update.ChangedRoomID))
	timelineChangedSet := make(map[string]struct{}, len(update.TimelineChangedRoomID))
	for _, roomID := range update.TimelineChangedRoomID {
		timelineChangedSet[roomID] = struct{}{}
	}
	pendingError := ""
	initialSyncComplete := update.InitialSyncComplete
	notifications := append([]Notification(nil), update.Notifications...)
drain:
	for {
		select {
		case pending := <-c.events:
			for _, roomID := range pending.ChangedRoomID {
				changedSet[roomID] = struct{}{}
			}
			for _, roomID := range pending.TimelineChangedRoomID {
				timelineChangedSet[roomID] = struct{}{}
			}
			if pending.Error != "" {
				pendingError = pending.Error
			}
			initialSyncComplete = initialSyncComplete || pending.InitialSyncComplete
			notifications = append(notifications, pending.Notifications...)
		default:
			break drain
		}
	}
	for _, roomID := range update.ChangedRoomID {
		changedSet[roomID] = struct{}{}
	}
	if update.Error == "" {
		update.Error = pendingError
	}
	update.InitialSyncComplete = initialSyncComplete
	update.Notifications = notifications
	update.ChangedRoomID = update.ChangedRoomID[:0]
	for roomID := range changedSet {
		update.ChangedRoomID = append(update.ChangedRoomID, roomID)
	}
	sort.Strings(update.ChangedRoomID)
	update.TimelineChangedRoomID = update.TimelineChangedRoomID[:0]
	for roomID := range timelineChangedSet {
		update.TimelineChangedRoomID = append(update.TimelineChangedRoomID, roomID)
	}
	sort.Strings(update.TimelineChangedRoomID)
	select {
	case c.events <- update:
	case <-c.done:
	}
}

func (c *Client) publishTimeline(roomIDs []string, errMessage string) {
	c.mu.Lock()
	if c.pendingTimelineRooms == nil {
		c.pendingTimelineRooms = make(map[string]struct{})
	}
	for _, roomID := range roomIDs {
		c.pendingTimelineRooms[roomID] = struct{}{}
	}
	c.mu.Unlock()
	c.publish(nil, errMessage)
}

package matrix

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	Preview  string
	LastSeen time.Time
}

type Message struct {
	ID             string
	TransactionID  string
	Sender         string
	Body           string
	Timestamp      time.Time
	TimelineCursor int64
	Sending        bool
	SendError      string
}

type HistoryPage struct {
	Messages []Message
	Before   int64
	HasMore  bool
}

// Update is emitted when gomuks has persisted sync data to its local database.
type Update struct {
	Rooms               []Room
	ChangedRoomID       []string
	Error               string
	InitialSyncComplete bool
}

type Client struct {
	engine *hicli.HiClient
	events chan Update
	done   chan struct{}

	mu        sync.RWMutex
	rooms     map[string]Room
	local     map[string]map[string]Message
	syncReady bool

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
		events: make(chan Update, eventBufferSize),
		done:   make(chan struct{}),
		rooms:  make(map[string]Room),
		local:  make(map[string]map[string]Message),
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
	evt, err := c.engine.SendMessage(ctx, id.RoomID(roomID), nil, nil, text, nil, nil, nil)
	if err != nil {
		return fmt.Errorf("send Matrix message: %w", err)
	}
	if message, ok := messageFromEvent(evt); ok {
		message.Sending = true
		c.storeLocalMessage(roomID, message)
	}
	c.publish([]string{roomID}, "")
	return nil
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
		c.publish([]string{evt.RoomID.String()}, "")
	case *jsoncmd.SendComplete:
		if evt.Event != nil {
			c.sendMu.Lock()
			if message, ok := messageFromEvent(evt.Event); ok {
				message.Sending = false
				message.SendError = ""
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
			c.publish([]string{evt.Event.RoomID.String()}, errorMessage)
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
		}
		for _, evt := range syncRoom.Events {
			c.removeLocalMessage(key, evt)
			message, ok := messageFromEvent(evt)
			if !ok {
				continue
			}
			if message.Timestamp.After(room.LastSeen) {
				room.LastSeen = message.Timestamp
				room.Preview = message.Body
			}
		}
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
		Body string `json:"body"`
	}
	if err := json.Unmarshal(evt.GetContent(), &content); err != nil || content.Body == "" {
		return Message{}, false
	}
	return Message{
		ID:             evt.ID.String(),
		TransactionID:  evt.TransactionID,
		Sender:         evt.Sender.String(),
		Body:           content.Body,
		Timestamp:      evt.Timestamp.Time,
		TimelineCursor: int64(evt.TimelineRowID),
		Sending:        evt.SendError == "not sent",
		SendError:      evt.SendError,
	}, true
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
	if exists && message.Timestamp.After(room.LastSeen) {
		room.LastSeen = message.Timestamp
		room.Preview = message.Body
		c.rooms[roomID] = room
	}
	c.mu.Unlock()
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
	pendingError := ""
	initialSyncComplete := update.InitialSyncComplete
drain:
	for {
		select {
		case pending := <-c.events:
			for _, roomID := range pending.ChangedRoomID {
				changedSet[roomID] = struct{}{}
			}
			if pending.Error != "" {
				pendingError = pending.Error
			}
			initialSyncComplete = initialSyncComplete || pending.InitialSyncComplete
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
	update.ChangedRoomID = update.ChangedRoomID[:0]
	for roomID := range changedSet {
		update.ChangedRoomID = append(update.ChangedRoomID, roomID)
	}
	sort.Strings(update.ChangedRoomID)
	select {
	case c.events <- update:
	case <-c.done:
	}
}

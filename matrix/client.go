package matrix

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	"go.mau.fi/util/jsontime"
	"maunium.net/go/mautrix"
	mautrixcrypto "maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/crypto/attachment"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type SearchRequest struct {
	Query        string
	RoomID       string
	Sender       string
	MinTimestamp int64
	MaxTimestamp int64
	Limit        int
	NextBatch    string
}

type SearchResult struct {
	RoomID  string
	Message Message
}

type SearchPage struct {
	Results   []SearchResult
	NextBatch string
}

const eventBufferSize = 256
const timelinePageSize = 100
const defaultInitialHistoryLimit = 100
const maxReactedEvents = 200

// Reconnect backoff bounds used when (re)starting Matrix sync after a failure.
const (
	reconnectInitialDelay = time.Second
	reconnectMaxDelay     = 30 * time.Second
)

// reconnectDelay returns the exponential backoff delay for a 1-based attempt
// number, capped at reconnectMaxDelay. It doubles the initial delay on every
// attempt: 1s, 2s, 4s, … 30s.
func reconnectDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := reconnectInitialDelay << (attempt - 1)
	if delay <= 0 || delay > reconnectMaxDelay {
		return reconnectMaxDelay
	}
	return delay
}

// Room and Message are UI-facing snapshots that do not expose gomuks internals.
type Room struct {
	ID              string
	Name            string
	Topic           string
	Avatar          string
	Preview         string
	LastSeen        time.Time
	Direct          bool
	Favorite        bool
	Muted           bool
	Unread          int
	Mentions        int
	Unanswered      bool
	LastEventID     string
	LastEventSender string
	LastEventAt     time.Time
	Bridge          string
}

type Message struct {
	ID              string
	TransactionID   string
	Sender          string
	SenderName      string
	Avatar          string
	Body            string
	FormattedBody   string
	ReplyToEventID  string
	ReplyToSender   string
	ReplyToPreview  string
	Timestamp       time.Time
	TimelineCursor  int64
	Sending         bool
	SendError       string
	Own             bool
	Reactions       map[string]int
	Reacted         map[string]bool
	MediaURI        string
	MediaMIME       string
	MediaSize       int
	MediaWidth      int
	MediaHeight     int
	MediaEncrypted  *event.EncryptedFileInfo
	MediaDurationMs int
	Encrypted       bool
	DecryptionError string
	Edited          bool
	EditHistory     []string
	Redacted        bool

	MediaType               string
	MediaThumbnailURI       string
	MediaThumbnailEncrypted *event.EncryptedFileInfo
}

type Member struct {
	ID          string
	DisplayName string
}

type Device struct {
	ID          string
	DisplayName string
	LastSeenIP  string
	LastSeenAt  time.Time
	Current     bool
}

type AccountProfile struct {
	UserID      string
	DisplayName string
	Homeserver  string
	DeviceID    string
	AvatarURI   string
	Verified    bool
}

type HistoryPage struct {
	Messages []Message
	Before   int64
	HasMore  bool
}

type ConnectionState string

const (
	StateConnecting ConnectionState = "connecting"
	StateSyncing    ConnectionState = "syncing"
	StateConnected  ConnectionState = "connected"
	StateOffline    ConnectionState = "offline"
)

// Update is emitted when gomuks has persisted sync data to its local database.
type Update struct {
	Rooms                 []Room
	ChangedRoomID         []string
	TimelineChangedRoomID []string
	TypingUsers           map[string][]string
	Notifications         []Notification
	Error                 string
	InitialSyncComplete   bool
	NeedsVerification     bool
	ConnectionState       ConnectionState
}

type Notification struct {
	RoomID  string
	Room    string
	Sender  string
	Body    string
	Mention bool
}

type Client struct {
	engine     *hicli.HiClient
	dbPath     string
	events     chan Update
	done       chan struct{}
	cacheReady chan struct{}

	mu                   sync.RWMutex
	rooms                map[string]Room
	local                map[string]map[string]Message
	mediaEncrypted       map[string]*event.EncryptedFileInfo
	typing               map[string][]string
	pendingNotifications []Notification
	notifiedEvents       map[string]struct{}
	pendingTimelineRooms map[string]struct{}
	dismissedEvents      map[string]string
	reactedEvents        map[string]map[string]struct{}
	syncReady            bool
	connectionState      ConnectionState
	mediaCacheDir        string

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

// New opens gomuks' SQLite stores and restores the cached account if present.
// Cached room metadata is loaded asynchronously; network syncing is started by Start.
func New(databasePath string) (*Client, error) {
	return NewWithLogOutput(databasePath, os.Stderr)
}

// NewWithLogOutput is New with gomuks engine logs written to logOutput.
func NewWithLogOutput(databasePath string, logOutput io.Writer) (*Client, error) {
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
	if err = ensureSecureFilePermissions(filepath.Join(dataDir, "crypto.key"), 0o600); err != nil {
		return nil, err
	}
	// Harden permissions on existing session material: the SQLite database
	// holds decrypted message contents, access tokens, and Megolm sessions.
	for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		if err = ensureSecureFilePermissions(path, 0o600); err != nil {
			return nil, err
		}
	}
	rawDB, err := dbutil.NewWithDialect(databasePath, "sqlite3-fk-wal")
	if err != nil {
		return nil, fmt.Errorf("open gomuks SQLite database: %w", err)
	}

	client := &Client{
		dbPath:               databasePath,
		events:               make(chan Update, eventBufferSize),
		done:                 make(chan struct{}),
		cacheReady:           make(chan struct{}),
		rooms:                make(map[string]Room),
		local:                make(map[string]map[string]Message),
		mediaEncrypted:       make(map[string]*event.EncryptedFileInfo),
		typing:               make(map[string][]string),
		notifiedEvents:       make(map[string]struct{}),
		pendingTimelineRooms: make(map[string]struct{}),
		dismissedEvents:      make(map[string]string),
		reactedEvents:        make(map[string]map[string]struct{}),
		mediaCacheDir:        defaultMediaCacheDir(dataDir),
	}
	client.lifecycleCtx, client.lifecycleCancel = context.WithCancel(context.Background())
	logger := zerolog.New(logOutput).With().Timestamp().Logger()
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
	if _, err = client.engine.DB.Exec(ctx, `CREATE TABLE IF NOT EXISTS room_unanswered_status (
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
		restoreCtx, finishRestore, restoreErr := client.beginOperation(ctx)
		if restoreErr != nil {
			client.Close()
			return nil, fmt.Errorf("start cached Matrix restore: %w", restoreErr)
		}
		go func() {
			defer func() {
				close(client.cacheReady)
				finishRestore()
			}()
			for initialSync := range client.engine.GetInitialSync(restoreCtx, 100, 0) {
				client.applySync(initialSync, false)
			}
			if restoreCtx.Err() == nil {
				client.publish(nil, "")
			}
		}()
	} else {
		close(client.cacheReady)
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
		if _, err = c.engine.DB.Exec(ctx, `DELETE FROM room_unanswered_status WHERE room_id = ?`, roomID); err != nil {
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
		if _, err = c.engine.DB.Exec(ctx, `INSERT INTO room_unanswered_status (room_id, dismissed_event_id, dismissed_at)
			VALUES (?, ?, ?) ON CONFLICT(room_id) DO UPDATE SET
			dismissed_event_id = excluded.dismissed_event_id, dismissed_at = excluded.dismissed_at`,
			roomID, eventID, time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("save answered chat state: %w", err)
		}
		c.mu.Lock()
		c.dismissedEvents[roomID] = eventID
		if current, ok := c.rooms[roomID]; ok {
			current.Unanswered = roomNeedsReply(current, c.currentUserID(), eventID)
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

func ensureSecureFilePermissions(path string, mode os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat secured file at %q: %w", path, err)
	}
	if info.Mode().Perm() == mode.Perm() {
		return nil
	}
	if err = os.Chmod(path, mode); err != nil {
		return fmt.Errorf("secure file permissions at %q: %w", path, err)
	}
	return nil
}

// defaultMediaCacheDir prefers the OS cache directory and falls back to the
// config data directory so portable installs keep working.
func defaultMediaCacheDir(dataDir string) string {
	if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
		return filepath.Join(cacheDir, "gomuks-desktop", "media")
	}
	return filepath.Join(dataDir, "media")
}

// SetMediaCacheDir overrides the on-disk media cache directory (tests, portable installs).
func (c *Client) SetMediaCacheDir(dir string) {
	c.mu.Lock()
	c.mediaCacheDir = dir
	c.mu.Unlock()
}

// MediaCacheDir reports the directory used for decrypted media files.
func (c *Client) MediaCacheDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mediaCacheDir
}

// mediaCachePath maps an mxc:// URI to a stable cache file name.
func (c *Client) mediaCachePath(mxc string) (string, error) {
	server, mediaID, err := parseMXC(mxc)
	if err != nil {
		return "", err
	}
	name := server + "_" + mediaID
	sum := sha256.Sum256([]byte(mxc))
	return filepath.Join(c.MediaCacheDir(), name+"-"+hex.EncodeToString(sum[:8])), nil
}

func (c *Client) mediaCachePathFor(mxc string, encrypted bool) (string, error) {
	path, err := c.mediaCachePath(mxc)
	if err != nil || !encrypted {
		return path, err
	}
	return path + ".encrypted", nil
}

func parseMXC(mxc string) (server, mediaID string, err error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(mxc), "mxc://")
	if !ok || rest == "" {
		return "", "", fmt.Errorf("invalid Matrix media URI %q", mxc)
	}
	server, mediaID, ok = strings.Cut(rest, "/")
	if !ok || server == "" || mediaID == "" || strings.Contains(mediaID, "/") {
		return "", "", fmt.Errorf("invalid Matrix media URI %q", mxc)
	}
	return server, mediaID, nil
}

// MediaCacheUsage walks the media cache directory and reports its size.
func (c *Client) MediaCacheUsage() (bytes int64, files int, err error) {
	dir := c.MediaCacheDir()
	if dir == "" {
		return 0, 0, nil
	}
	err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		bytes += info.Size()
		files++
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("measure media cache: %w", err)
	}
	return bytes, files, nil
}

// ClearMediaCache removes cached media files without touching message history.
func (c *Client) ClearMediaCache() error {
	dir := c.MediaCacheDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("list media cache: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if err = os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove cached media at %q: %w", path, err)
		}
	}
	return nil
}

func (c *Client) Events() <-chan Update {
	return c.events
}

func (c *Client) WaitForCache(ctx context.Context) error {
	if c.cacheReady == nil {
		return nil
	}
	select {
	case <-c.cacheReady:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errors.New("Matrix client is closed")
	}
}

func (c *Client) IsLoggedIn() bool {
	return c.engine.IsLoggedIn()
}

func (c *Client) NeedsVerification() bool {
	return c.engine.IsLoggedIn() && !c.engine.VerificationState.IsVerified
}

func (c *Client) Profile() AccountProfile {
	profile := AccountProfile{Verified: !c.NeedsVerification()}
	if account := c.engine.Account; account != nil {
		profile.UserID = string(account.UserID)
		profile.DisplayName = account.DisplayName
		profile.Homeserver = account.HomeserverURL
		profile.DeviceID = string(account.DeviceID)
		profile.AvatarURI = account.AvatarURL.String()
	}
	return profile
}

func (c *Client) StorageUsage() (databaseBytes, walBytes int64, err error) {
	databaseInfo, err := os.Stat(c.dbPath)
	if err != nil {
		return 0, 0, fmt.Errorf("stat Matrix database: %w", err)
	}
	databaseBytes = databaseInfo.Size()
	walInfo, err := os.Stat(c.dbPath + "-wal")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, 0, fmt.Errorf("stat Matrix database WAL: %w", err)
	}
	if err == nil {
		walBytes = walInfo.Size()
	}
	return databaseBytes, walBytes, nil
}

func (c *Client) OptimizeDatabase(ctx context.Context) error {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if _, err = c.engine.DB.Exec(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("optimize Matrix database: %w", err)
	}
	return nil
}

func (c *Client) SyncReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.syncReady
}

func (c *Client) ConnectionState() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connectionState
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

func timelineMessagePrefix(events []*database.Event, limit int) (int, int) {
	messageCount := 0
	for index, evt := range events {
		if _, ok := messageFromEvent(evt); !ok {
			continue
		}
		messageCount++
		if messageCount == limit {
			return index + 1, messageCount
		}
	}
	return len(events), messageCount
}

func isTimelineEvent(timeline []database.TimelineRowTuple, rowID database.EventRowID) bool {
	for _, tuple := range timeline {
		if tuple.Event == rowID {
			return true
		}
	}
	return false
}

func hasServerHistory(room *database.Room) bool {
	return room != nil && room.PrevBatch != "" && room.PrevBatch != database.PrevBatchPaginationComplete
}

func (c *Client) HistoryPage(ctx context.Context, roomID string, before int64) (HistoryPage, error) {
	return c.HistoryPageWithLimit(ctx, roomID, before, defaultInitialHistoryLimit)
}

func (c *Client) HistoryPageWithLimit(ctx context.Context, roomID string, before int64, initialLimit int) (HistoryPage, error) {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return HistoryPage{}, err
	}
	defer finish()
	page := HistoryPage{}
	var events []*database.Event
	if before == 0 {
		if initialLimit < 1 {
			initialLimit = defaultInitialHistoryLimit
		}
		cursor := database.TimelineRowID(0)
		remaining := initialLimit
		for {
			batch, getErr := c.engine.DB.Timeline.Get(ctx, id.RoomID(roomID), timelinePageSize, cursor)
			if getErr != nil {
				return HistoryPage{}, fmt.Errorf("read room timeline: %w", getErr)
			}
			if len(batch) == 0 {
				break
			}

			prefixLength, messageCount := timelineMessagePrefix(batch, remaining)
			events = append(events, batch[:prefixLength]...)
			if messageCount == remaining {
				page.Before = int64(batch[prefixLength-1].TimelineRowID)
				page.HasMore = prefixLength < len(batch) || len(batch) == timelinePageSize
				break
			}

			page.Before = int64(batch[len(batch)-1].TimelineRowID)
			remaining -= messageCount
			if len(batch) < timelinePageSize {
				break
			}
			cursor = database.TimelineRowID(page.Before)
		}
	} else {
		var getErr error
		events, getErr = c.engine.DB.Timeline.Get(ctx, id.RoomID(roomID), timelinePageSize, database.TimelineRowID(before))
		if getErr != nil {
			return HistoryPage{}, fmt.Errorf("read room timeline: %w", getErr)
		}
		page.HasMore = len(events) == timelinePageSize
		if len(events) > 0 {
			page.Before = int64(events[len(events)-1].TimelineRowID)
		}
	}
	room, getErr := c.engine.DB.Room.Get(ctx, id.RoomID(roomID))
	if getErr != nil {
		return HistoryPage{}, fmt.Errorf("read room history pagination state: %w", getErr)
	}
	if before != 0 && len(events) == 0 && hasServerHistory(room) {
		pagination, paginateErr := c.engine.Paginate(ctx, id.RoomID(roomID), database.TimelineRowID(before), timelinePageSize, false)
		if paginateErr != nil {
			return HistoryPage{}, fmt.Errorf("paginate older room history: %w", paginateErr)
		}
		events = pagination.Events
		page.HasMore = pagination.HasMore
		if len(events) > 0 {
			page.Before = int64(events[len(events)-1].TimelineRowID)
		}
	} else {
		page.HasMore = page.HasMore || hasServerHistory(room)
	}
	messages := make([]Message, 0, len(events))
	parsedTimelineEvents := 0
	skippedEventTypes := make(map[string]int)
	for i := len(events) - 1; i >= 0; i-- {
		if message, ok := messageFromEvent(events[i]); ok {
			messages = append(messages, message)
			parsedTimelineEvents++
		} else if events[i] != nil {
			skippedEventTypes[events[i].GetType().Type]++
		}
	}
	if parsedTimelineEvents == 0 {
		log.Printf("Matrix history page has no parsed timeline messages: rows=%d skipped_event_types=%v", len(events), skippedEventTypes)
	}
	if before == 0 {
		c.mu.Lock()
		localMessages := c.local[roomID]
		for key, message := range localMessages {
			if containsMessage(messages, message) || shadowedByServerRow(messages, message) {
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
	c.rememberMediaEncryption(messages)
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

func (c *Client) ExportRoomKeys(ctx context.Context, passphrase string) ([]byte, error) {
	if strings.TrimSpace(passphrase) == "" {
		return nil, errors.New("key export passphrase is empty")
	}
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	export, err := mautrixcrypto.ExportKeysIter(passphrase, c.engine.CryptoStore.GetAllGroupSessions(ctx))
	if err != nil {
		if errors.Is(err, mautrixcrypto.ErrNoSessionsForExport) {
			return nil, errors.New("there are no Megolm room keys to export")
		}
		return nil, fmt.Errorf("export Megolm room keys: %w", err)
	}
	return export, nil
}

func (c *Client) ImportRoomKeys(ctx context.Context, passphrase string, data []byte) (int, int, error) {
	if strings.TrimSpace(passphrase) == "" {
		return 0, 0, errors.New("key export passphrase is empty")
	}
	if len(data) == 0 {
		return 0, 0, errors.New("key export file is empty")
	}
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer finish()
	imported, total, err := c.engine.Crypto.ImportKeys(ctx, passphrase, data)
	if err != nil {
		return imported, total, fmt.Errorf("import Megolm room keys: %w", err)
	}
	return imported, total, nil
}

func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	response, err := c.engine.Client.GetDevicesInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Matrix devices: %w", err)
	}
	profile := c.Profile()
	devices := make([]Device, 0, len(response.Devices))
	for _, device := range response.Devices {
		deviceInfo := Device{
			ID:          string(device.DeviceID),
			DisplayName: device.DisplayName,
			LastSeenIP:  device.LastSeenIP,
			Current:     device.DeviceID.String() == profile.DeviceID,
		}
		if device.LastSeenTS > 0 {
			deviceInfo.LastSeenAt = time.UnixMilli(device.LastSeenTS)
		}
		devices = append(devices, deviceInfo)
	}
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Current != devices[j].Current {
			return devices[i].Current
		}
		return devices[i].LastSeenAt.After(devices[j].LastSeenAt)
	})
	return devices, nil
}

func (c *Client) RevokeDevice(ctx context.Context, deviceID string) error {
	if strings.TrimSpace(deviceID) == "" {
		return errors.New("Matrix device ID is empty")
	}
	if deviceID == c.Profile().DeviceID {
		return errors.New("cannot revoke the current device from this session")
	}
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if err = c.engine.Client.DeleteDevice(ctx, id.DeviceID(deviceID), &mautrix.ReqDeleteDevice[any]{}); err != nil {
		return fmt.Errorf("revoke Matrix device %q: %w", deviceID, err)
	}
	return nil
}

func (c *Client) Start(ctx context.Context) error {
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if err = c.WaitForCache(ctx); err != nil {
		return err
	}
	return c.startEngine(ctx)
}

// StartWithRetry starts sync and retries transient failures with exponential
// backoff, publishing Offline state between attempts. It returns nil once
// sync has started or when ctx is cancelled.
func (c *Client) StartWithRetry(ctx context.Context) error {
	for attempt := 1; ; attempt++ {
		if err := c.Start(ctx); err == nil {
			return nil
		} else if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		} else if attempt > 8 {
			return fmt.Errorf("start Matrix sync after %d attempts: %w", attempt-1, err)
		}
		delay := reconnectDelay(attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-c.done:
			timer.Stop()
			return errors.New("Matrix client is closed")
		case <-timer.C:
		}
	}
}

// LastReconnectDelay exposes the backoff delay for tests/UI diagnostics.
func LastReconnectDelay(attempt int) time.Duration { return reconnectDelay(attempt) }

func (c *Client) startEngine(ctx context.Context) error {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	if c.started {
		return nil
	}
	c.mu.Lock()
	c.connectionState = StateConnecting
	c.mu.Unlock()
	// Start initializes the gomuks client and launches its Matrix sync loop.
	if err := c.engine.Start(ctx); err != nil {
		c.mu.Lock()
		c.connectionState = StateOffline
		c.mu.Unlock()
		return err
	}
	c.started = true
	c.publish(nil, "")
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
	emoji = strings.TrimSpace(emoji)
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if _, err = c.engine.Client.SendReaction(ctx, id.RoomID(roomID), id.EventID(eventID), emoji); err != nil {
		return fmt.Errorf("send Matrix reaction: %w", err)
	}
	c.mu.Lock()
	if c.reactedEvents == nil {
		c.reactedEvents = make(map[string]map[string]struct{})
	}
	roomReactions := c.reactedEvents[roomID]
	if roomReactions == nil {
		roomReactions = make(map[string]struct{})
		c.reactedEvents[roomID] = roomReactions
	}
	roomReactions[eventID+"\x00"+emoji] = struct{}{}
	// Bound memory use: reactions are hints only, refreshed from sync data.
	if len(roomReactions) > maxReactedEvents {
		for key := range roomReactions {
			delete(roomReactions, key)
			if len(roomReactions) <= maxReactedEvents {
				break
			}
		}
	}
	if messages, ok := c.local[roomID]; ok {
		for key, message := range messages {
			if message.ID != eventID {
				continue
			}
			if message.Reacted == nil {
				message.Reacted = make(map[string]bool)
			}
			message.Reacted[emoji] = true
			messages[key] = message
		}
	}
	c.mu.Unlock()
	c.publishTimeline([]string{roomID}, "")
	return nil
}

// hasReacted reports whether the local user reacted with emoji to an event (best effort).
func (c *Client) hasReacted(roomID, eventID, emoji string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hasReactedLocked(roomID, eventID, emoji)
}

func (c *Client) hasReactedLocked(roomID, eventID, emoji string) bool {
	if roomReactions, ok := c.reactedEvents[roomID]; ok {
		if _, reacted := roomReactions[eventID+"\x00"+emoji]; reacted {
			return true
		}
	}
	if messages, ok := c.local[roomID]; ok {
		for _, message := range messages {
			if message.ID == eventID && message.Reacted[emoji] {
				return true
			}
		}
	}
	return false
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

func localSearchParams(request SearchRequest) (*jsoncmd.SearchParams, error) {
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return nil, errors.New("message search query is empty")
	}
	limit := request.Limit
	if limit < 1 || limit > 100 {
		limit = 50
	}
	params := &jsoncmd.SearchParams{
		SearchTerm: query,
		Limit:      limit,
		SortByTime: true,
		NextBatch:  request.NextBatch,
	}
	if request.RoomID != "" {
		params.RoomIDs = []id.RoomID{id.RoomID(request.RoomID)}
	}
	if request.Sender != "" {
		params.Senders = []id.UserID{id.UserID(request.Sender)}
	}
	if request.MinTimestamp > 0 {
		params.MinTimestamp = jsontime.UnixMilli{Time: time.UnixMilli(request.MinTimestamp)}
	}
	if request.MaxTimestamp > 0 {
		params.MaxTimestamp = jsontime.UnixMilli{Time: time.UnixMilli(request.MaxTimestamp)}
	}
	return params, nil
}

func (c *Client) SearchMessages(ctx context.Context, request SearchRequest) (SearchPage, error) {
	params, err := localSearchParams(request)
	if err != nil {
		return SearchPage{}, err
	}
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return SearchPage{}, err
	}
	defer finish()
	response, err := c.engine.SearchLocal(ctx, params)
	if err != nil {
		return SearchPage{}, fmt.Errorf("search local Matrix history: %w", err)
	}
	results := make([]SearchResult, 0, len(response.Events))
	profileMessages := make(map[string][]Message)
	profileResultIndexes := make(map[string][]int)
	for _, eventResult := range response.Events {
		if message, ok := messageFromEvent(eventResult); ok {
			roomID := eventResult.RoomID.String()
			var timelineCursor database.TimelineRowID
			cursorErr := c.engine.DB.Timeline.GetDB().QueryRow(ctx,
				"SELECT rowid FROM timeline WHERE room_id = ? AND event_rowid = ?",
				eventResult.RoomID, eventResult.RowID,
			).Scan(&timelineCursor)
			if cursorErr != nil && !errors.Is(cursorErr, sql.ErrNoRows) {
				return SearchPage{}, fmt.Errorf("find searched message in room timeline: %w", cursorErr)
			}
			message.TimelineCursor = int64(timelineCursor)
			profileMessages[roomID] = append(profileMessages[roomID], message)
			profileResultIndexes[roomID] = append(profileResultIndexes[roomID], len(results))
			results = append(results, SearchResult{RoomID: roomID, Message: message})
		}
	}
	for roomID, messages := range profileMessages {
		c.populateMessageProfiles(ctx, roomID, messages)
		for index, message := range messages {
			results[profileResultIndexes[roomID][index]].Message = message
		}
	}
	return SearchPage{Results: results, NextBatch: response.NextBatch}, nil
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
	clear(c.mediaEncrypted)
	c.mu.Unlock()
	return nil
}

func (c *Client) DownloadAvatar(ctx context.Context, mxc string) ([]byte, error) {
	if strings.TrimSpace(mxc) == "" {
		return nil, nil
	}
	data, err := c.DownloadMedia(ctx, mxc, nil)
	if isMissingMatrixMediaError(err) {
		return nil, nil
	}
	return data, err
}

func isMissingMatrixMediaError(err error) bool {
	return errors.Is(err, mautrix.MNotFound)
}

// CachedMediaBytes downloads media through the encrypted disk cache.
func (c *Client) CachedMediaBytes(ctx context.Context, mxc string) ([]byte, error) {
	return c.DownloadMedia(ctx, mxc, c.mediaEncryptionFor(mxc))
}

func (c *Client) mediaEncryptionFor(mxc string) *event.EncryptedFileInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mediaEncrypted[mxc]
}

func (c *Client) rememberMediaEncryption(messages []Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mediaEncrypted == nil {
		c.mediaEncrypted = make(map[string]*event.EncryptedFileInfo)
	}
	for _, message := range messages {
		if message.MediaURI != "" && message.MediaEncrypted != nil {
			c.mediaEncrypted[message.MediaURI] = message.MediaEncrypted
		}
		if message.MediaThumbnailURI != "" && message.MediaThumbnailEncrypted != nil {
			c.mediaEncrypted[message.MediaThumbnailURI] = message.MediaThumbnailEncrypted
		}
	}
}

func (c *Client) DownloadMedia(ctx context.Context, mxc string, encrypted *event.EncryptedFileInfo) ([]byte, error) {
	if strings.TrimSpace(mxc) == "" {
		return nil, errors.New("Matrix media URI is empty")
	}
	if cached, cacheErr := c.readCachedMedia(mxc, encrypted != nil); cacheErr == nil && cached != nil {
		return cached, nil
	}
	ctx, finish, err := c.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	uri, err := id.ContentURIString(mxc).Parse()
	if err != nil {
		return nil, fmt.Errorf("parse Matrix media URI: %w", err)
	}
	response, err := c.engine.Client.Download(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("download Matrix media: %w", err)
	}
	defer response.Body.Close()
	const maxMediaBytes = 50 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxMediaBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Matrix media: %w", err)
	}
	if len(data) > maxMediaBytes {
		return nil, fmt.Errorf("Matrix media exceeds %d MiB", maxMediaBytes>>20)
	}
	if encrypted != nil {
		if err = encrypted.DecryptInPlace(data); err != nil {
			return nil, fmt.Errorf("decrypt Matrix media: %w", err)
		}
	}
	c.writeCachedMedia(mxc, data, encrypted != nil)
	return data, nil
}

// CachedMediaPath returns the disk cache path for mxc when the file exists.
func (c *Client) CachedMediaPath(mxc string) (string, bool) {
	path, err := c.mediaCachePathFor(mxc, c.mediaEncryptionFor(mxc) != nil)
	if err != nil {
		return "", false
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

func (c *Client) readCachedMedia(mxc string, encrypted bool) ([]byte, error) {
	// Encrypted payloads are cached after decryption, so validate size before use.
	path, err := c.mediaCachePathFor(mxc, encrypted)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	const maxMediaBytes = 50 << 20
	if len(data) == 0 || len(data) > maxMediaBytes {
		_ = os.Remove(path)
		return nil, errors.New("cached Matrix media is invalid")
	}
	return data, nil
}

func (c *Client) writeCachedMedia(mxc string, data []byte, encrypted bool) {
	if len(data) == 0 {
		return
	}
	path, err := c.mediaCachePathFor(mxc, encrypted)
	if err != nil {
		return
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Printf("create Matrix media cache: %v", err)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".media-*")
	if err != nil {
		log.Printf("create Matrix media cache entry: %v", err)
		return
	}
	tmpName := tmp.Name()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		log.Printf("write Matrix media cache entry: %v", err)
		return
	}
	if err = tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		log.Printf("close Matrix media cache entry: %v", err)
		return
	}
	_ = os.Chmod(tmpName, 0o600)
	if err = os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		log.Printf("store Matrix media cache entry: %v", err)
	}
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
		c.refreshRoomFromTimeline(evt.RoomID.String())
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
					c.storeFailedMessage(evt.Event.RoomID.String(), message)
				} else if evt.Event.SendError == "" || evt.Event.SendError == "not sent" {
					// Send succeeded (or is still in flight with only the
					// transient marker): drop the optimistic pending entry so
					// the server-confirmed row takes over on next sync, but keep
					// the sidebar preview on the sent text.
					c.mu.Lock()
					roomID := evt.Event.RoomID.String()
					c.removeLocalMessageLocked(roomID, evt.Event)
					if room, ok := c.rooms[roomID]; ok {
						if message.Body != "" && !message.Timestamp.Before(room.LastSeen) {
							room.LastSeen = message.Timestamp
							room.Preview = message.Body
						}
						if !message.Timestamp.Before(room.LastEventAt) {
							room.LastEventAt = message.Timestamp
							room.LastEventID = evt.Event.ID.String()
							if message.Sender != "" {
								room.LastEventSender = message.Sender
							}
							room.Unanswered = roomNeedsReply(room, c.currentUserID(), c.dismissedEvents[roomID])
						}
						c.rooms[roomID] = room
					}
					c.mu.Unlock()
				} else {
					message.SendError = evt.Event.SendError
					c.storeFailedMessage(evt.Event.RoomID.String(), message)
				}
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
		c.mu.Lock()
		switch evt.Type {
		case jsoncmd.SyncStatusErroring, jsoncmd.SyncStatusFailed:
			c.connectionState = StateOffline
			message = evt.Error
			if message == "" {
				message = "Matrix sync is offline"
			}
		case jsoncmd.SyncStatusOK:
			c.syncReady = true
			c.connectionState = StateConnected
		case jsoncmd.SyncStatusWaiting:
			if c.syncReady {
				c.connectionState = StateConnected
			} else {
				c.connectionState = StateSyncing
			}
		}
		c.mu.Unlock()
		c.publish(nil, message)
	case *jsoncmd.InitComplete:
		c.mu.Lock()
		c.syncReady = true
		c.connectionState = StateConnected
		c.mu.Unlock()
		c.publish(nil, "")
	}
}

func (c *Client) applySync(syncEvent *jsoncmd.SyncComplete, notify bool) {
	roomAvatars := c.directRoomAvatars(syncEvent)
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
			if avatar, ok := roomAvatars[key]; ok && (meta.Avatar == nil || room.Avatar == "") {
				room.Avatar = avatar
			}
			room.Direct = isDirectRoom(meta)
			room.Unread = meta.UnreadMessages + meta.UnreadNotifications
			room.Mentions = meta.UnreadHighlights
			room.Bridge = bridgeLabel(meta)
			if !meta.SortingTimestamp.Time.IsZero() {
				room.LastSeen = meta.SortingTimestamp.Time
			}
		}
		if notify && (syncRoom.Reset || len(syncRoom.Timeline) > 0) {
			if c.pendingTimelineRooms == nil {
				c.pendingTimelineRooms = make(map[string]struct{})
			}
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
		// gomuks decides which event is a room's preview server-side
		// (room.preview_event_rowid, using the same eligibility rules the
		// timeline uses). Prefer that answer so the sidebar text is always the
		// event the open chat shows, rather than re-deriving it from whatever
		// happens to arrive in this payload.
		previewResolved := false
		if meta := syncRoom.Meta; meta != nil && meta.PreviewEventRowID != 0 {
			for _, evt := range syncRoom.Events {
				if evt == nil || evt.RowID != meta.PreviewEventRowID {
					continue
				}
				if preview, ok := messageFromEvent(evt); ok && preview.Body != "" {
					room.Preview = preview.Body
					previewResolved = true
				}
				break
			}
		}
		for _, evt := range syncRoom.Events {
			if evt == nil {
				continue
			}
			c.removeLocalMessageLocked(key, evt)
			if !evt.Timestamp.Time.Before(room.LastEventAt) {
				room.LastEventAt = evt.Timestamp.Time
				room.LastEventID = evt.ID.String()
				room.LastEventSender = evt.Sender.String()
			}
			inTimeline := isTimelineEvent(syncRoom.Timeline, evt.RowID)
			if message, ok := messageFromEvent(evt); ok && !previewResolved && evt.CanUseForPreview() {
				// Timeline membership keeps stale rows resent in `events` from
				// moving the preview backwards, but a freshly sent own message
				// may not have a timeline row yet, so let it seed an empty
				// preview instead of leaving the topic fallback in place.
				if room.Preview == "" || (inTimeline && !message.Timestamp.Before(room.LastSeen)) {
					room.LastSeen = message.Timestamp
					room.Preview = message.Body
				}
			}
			if notify && inTimeline {
				if c.pendingTimelineRooms == nil {
					c.pendingTimelineRooms = make(map[string]struct{})
				}
				c.pendingTimelineRooms[key] = struct{}{}
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

func (c *Client) directRoomAvatars(syncEvent *jsoncmd.SyncComplete) map[string]string {
	if c.engine == nil {
		return nil
	}

	keys := make([]database.RoomStateGUID, 0, len(syncEvent.Rooms))
	roomIDs := make(map[string]struct{}, len(syncEvent.Rooms))
	for roomID, syncRoom := range syncEvent.Rooms {
		if syncRoom == nil || syncRoom.Meta == nil || syncRoom.Meta.DMUserID == nil ||
			(syncRoom.Meta.Avatar != nil && syncRoom.Meta.Avatar.String() != "") {
			continue
		}
		keys = append(keys, database.RoomStateGUID{
			RoomID:   roomID,
			Type:     event.StateMember,
			StateKey: syncRoom.Meta.DMUserID.String(),
		})
		roomIDs[roomID.String()] = struct{}{}
	}
	if len(keys) == 0 {
		return nil
	}

	ctx, finish, err := c.beginOperation(context.Background())
	if err != nil {
		return nil
	}
	defer finish()

	members, err := c.engine.DB.CurrentState.GetMany(ctx, keys)
	if err != nil {
		log.Printf("read Matrix direct-message avatars: %v", err)
		return nil
	}
	avatars := make(map[string]string, len(roomIDs))
	for roomID := range roomIDs {
		avatars[roomID] = ""
	}
	for _, member := range members {
		if member.StateKey == nil {
			continue
		}
		var profile event.MemberEventContent
		if err = json.Unmarshal(member.GetContent(), &profile); err != nil {
			log.Printf("decode Matrix member profile in room %s: %v", member.RoomID, err)
			continue
		}
		avatars[member.RoomID.String()] = string(profile.AvatarURL)
	}
	return avatars
}

func isDirectRoom(room *database.Room) bool {
	if room == nil {
		return false
	}
	if room.DMUserID != nil {
		return true
	}
	if room.LazyLoadSummary == nil || room.LazyLoadSummary.JoinedMemberCount == nil {
		return false
	}
	memberCount := *room.LazyLoadSummary.JoinedMemberCount
	if room.LazyLoadSummary.InvitedMemberCount != nil {
		memberCount += *room.LazyLoadSummary.InvitedMemberCount
	}
	return memberCount == 2
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

// bridgeLabel derives a Beeper-network style badge label from room metadata.
// gomuks does not expose bridge protocol directly, so this uses the DM user
// and alias heuristics as a best-effort label (WhatsApp/Telegram/Signal/...).
func bridgeLabel(room *database.Room) string {
	if room == nil {
		return ""
	}
	candidates := make([]string, 0, 3)
	if room.DMUserID != nil {
		candidates = append(candidates, strings.ToLower(room.DMUserID.String()))
	}
	if room.CanonicalAlias != nil {
		candidates = append(candidates, strings.ToLower(room.CanonicalAlias.String()))
	}
	if room.Name != nil {
		candidates = append(candidates, strings.ToLower(*room.Name))
	}
	for _, candidate := range candidates {
		switch {
		case strings.Contains(candidate, "whatsapp"):
			return "WA"
		case strings.Contains(candidate, "telegram"):
			return "TG"
		case strings.Contains(candidate, "signal"):
			return "SG"
		case strings.Contains(candidate, "imessage"):
			return "iM"
		case strings.Contains(candidate, "instagram"):
			return "IG"
		case strings.Contains(candidate, "discord"):
			return "DC"
		case strings.Contains(candidate, "slack"):
			return "SL"
		case strings.Contains(candidate, "sms"):
			return "SMS"
		}
	}
	if room.DMUserID != nil && !strings.Contains(strings.ToLower(room.ID.String()), "matrix") {
		return "BR"
	}
	return ""
}

func messageFromEvent(evt *database.Event) (Message, bool) {
	if evt == nil {
		return Message{}, false
	}
	evtType := evt.GetType().Type
	if evtType != event.EventMessage.Type && evtType != event.EventSticker.Type && evtType != event.EventEncrypted.Type {
		return Message{}, false
	}
	if evt.RedactedBy != "" {
		return Message{
			ID:        evt.ID.String(),
			Sender:    evt.Sender.String(),
			Timestamp: evt.Timestamp.Time,
			Edited:    evt.LastEditRowID != nil,
			Redacted:  true,
		}, true
	}
	if evt.DecryptionError != "" {
		return Message{
			ID:              evt.ID.String(),
			Sender:          evt.Sender.String(),
			Timestamp:       evt.Timestamp.Time,
			Encrypted:       true,
			DecryptionError: evt.DecryptionError,
		}, true
	}
	if evtType == event.EventEncrypted.Type {
		return Message{}, false
	}
	var content struct {
		MsgType string `json:"msgtype"`
		Body    string `json:"body"`
		Text    []struct {
			Body     string `json:"body"`
			MIMEType string `json:"mimetype"`
		} `json:"m.text"`
		FormattedBody        string                       `json:"formatted_body"`
		URL                  string                       `json:"url"`
		File                 *event.EncryptedFileInfo     `json:"file"`
		Info                 *event.FileInfo              `json:"info"`
		FileName             string                       `json:"filename"`
		BeeperGalleryImages  []*event.MessageEventContent `json:"com.beeper.gallery.images"`
		BeeperGalleryCaption string                       `json:"com.beeper.gallery.caption"`
		RelatesTo            struct {
			EventID   string `json:"event_id"`
			InReplyTo struct {
				EventID string `json:"event_id"`
			} `json:"m.in_reply_to"`
		} `json:"m.relates_to"`
	}
	if err := json.Unmarshal(evt.GetContent(), &content); err != nil {
		return Message{}, false
	}
	mediaURI := content.URL
	mediaEncrypted := content.File
	mediaInfo := content.Info
	mediaType := content.MsgType
	if mediaURI == "" && content.File != nil {
		mediaURI = string(content.File.URL)
	}
	if content.MsgType == string(event.MsgBeeperGallery) {
		// The timeline row has one inline media slot, so display the first image in an album.
		for _, galleryImage := range content.BeeperGalleryImages {
			if galleryImage == nil {
				continue
			}
			galleryMIME := mediaMIME(galleryImage.Info)
			if galleryImage.MsgType != event.MsgImage && !strings.HasPrefix(galleryMIME, "image/") {
				continue
			}
			galleryURI := string(galleryImage.URL)
			if galleryURI == "" && galleryImage.File != nil {
				galleryURI = string(galleryImage.File.URL)
			}
			if galleryURI == "" {
				continue
			}
			mediaURI = galleryURI
			mediaEncrypted = galleryImage.File
			mediaInfo = galleryImage.Info
			mediaType = string(galleryImage.MsgType)
			break
		}
	}
	mediaMIMEType := mediaMIME(mediaInfo)
	if !strings.HasPrefix(mediaMIMEType, "image/") &&
		(mediaType == string(event.MsgImage) || evtType == event.EventSticker.Type) {
		mediaMIMEType = "image/unknown"
	}
	body := content.Body
	if body == "" {
		body = content.BeeperGalleryCaption
	}
	if body == "" {
		var fallbackText string
		for _, representation := range content.Text {
			if representation.Body == "" {
				continue
			}
			if fallbackText == "" {
				fallbackText = representation.Body
			}
			if representation.MIMEType == "" || representation.MIMEType == "text/plain" {
				body = representation.Body
				break
			}
		}
		if body == "" {
			body = fallbackText
		}
	}
	if body == "" {
		body = content.FileName
	}
	if body == "" {
		switch mediaType {
		case string(event.MsgImage):
			body = "Image"
		case string(event.MsgVideo):
			body = "Video"
		case string(event.MsgAudio):
			body = "Audio"
		case string(event.MsgFile):
			body = "File"
		case string(event.MsgBeeperGallery):
			body = "Media album"
		}
	}
	if body == "" && mediaURI != "" {
		body = "Attachment"
	}
	if body == "" {
		return Message{}, false
	}
	mediaMessage := mediaURI != "" || mediaType == string(event.MsgImage) ||
		mediaType == string(event.MsgVideo) || mediaType == string(event.MsgAudio) ||
		mediaType == string(event.MsgFile) || mediaType == string(event.MsgBeeperGallery) ||
		evtType == event.EventSticker.Type
	if mediaMessage {
		log.Printf("Matrix media mapped: msgtype=%q uri_present=%t mime=%q encrypted=%t inline_candidate=%t dimensions=%dx%d",
			mediaType, mediaURI != "", mediaMIMEType, mediaEncrypted != nil,
			mediaURI != "" && strings.HasPrefix(mediaMIMEType, "image/"), mediaWidth(mediaInfo), mediaHeight(mediaInfo))
	}
	replyTo := content.RelatesTo.EventID
	if replyTo == "" {
		replyTo = content.RelatesTo.InReplyTo.EventID
	}
	if replyTo == "" {
		replyTo = evt.RelatesTo.String()
		switch evt.RelationType {
		case "", event.RelThread, event.RelReference:
		default:
			// Edits (m.replace), reactions (m.annotation) etc. are not replies.
			if evt.RelationType != "m.in_reply_to" {
				replyTo = ""
			}
		}
		if replyTo != "" && strings.HasPrefix(replyTo, "$") == false && !strings.Contains(replyTo, ":") {
			replyTo = ""
		}
	}
	return Message{
		ID:              evt.ID.String(),
		TransactionID:   evt.TransactionID,
		Sender:          evt.Sender.String(),
		Body:            body,
		FormattedBody:   content.FormattedBody,
		ReplyToEventID:  replyTo,
		Timestamp:       evt.Timestamp.Time,
		TimelineCursor:  int64(evt.TimelineRowID),
		Sending:         isPendingEvent(evt),
		SendError:       mapSendError(evt.SendError),
		Reactions:       evt.Reactions,
		Reacted:         map[string]bool{},
		MediaURI:        mediaURI,
		MediaEncrypted:  mediaEncrypted,
		MediaMIME:       mediaMIMEType,
		MediaSize:       mediaSize(mediaInfo),
		MediaWidth:      mediaWidth(mediaInfo),
		MediaHeight:     mediaHeight(mediaInfo),
		MediaDurationMs: mediaDuration(mediaInfo),
		Encrypted:       evt.MegolmSessionID != "",
		Edited:          isEditedEvent(evt),
		EditHistory:     editHistory(evt),

		MediaType:               mediaType,
		MediaThumbnailURI:       mediaThumbnailURI(mediaInfo),
		MediaThumbnailEncrypted: mediaThumbnailEncrypted(mediaInfo),
	}, true
}

// refreshRoomFromTimeline re-derives the sidebar preview from the newest
// timeline message persisted in the DB. It is used after decryption completes:
// the sync-time Events payload often carries the still-encrypted stub (which
// messageFromEvent skips), so the preview would otherwise stay behind the
// open timeline until the next sync.
func (c *Client) refreshRoomFromTimeline(roomID string) {
	ctx, finish, err := c.beginOperation(context.Background())
	if err != nil {
		return
	}
	defer finish()
	events, err := c.engine.DB.Timeline.Get(ctx, id.RoomID(roomID), 20, 0)
	if err != nil || len(events) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	room, ok := c.rooms[roomID]
	if !ok {
		return
	}
	for i := len(events) - 1; i >= 0; i-- {
		message, ok := messageFromEvent(events[i])
		if !ok {
			continue
		}
		if room.Preview == "" || !message.Timestamp.Before(room.LastSeen) {
			room.LastSeen = message.Timestamp
			room.Preview = message.Body
		}
		if events[i] != nil && !events[i].Timestamp.Time.Before(room.LastEventAt) {
			room.LastEventAt = events[i].Timestamp.Time
			room.LastEventID = events[i].ID.String()
			room.LastEventSender = events[i].Sender.String()
		}
		break
	}
	room.Unanswered = roomNeedsReply(room, c.currentUserID(), c.dismissedEvents[roomID])
	c.rooms[roomID] = room
}

func mediaMIME(info *event.FileInfo) string {
	if info == nil {
		return ""
	}
	return info.MimeType
}

func mediaThumbnailURI(info *event.FileInfo) string {
	if info == nil {
		return ""
	}
	if info.ThumbnailURL != "" {
		return string(info.ThumbnailURL)
	}
	if info.ThumbnailFile != nil {
		return string(info.ThumbnailFile.URL)
	}
	return ""
}

func mediaThumbnailEncrypted(info *event.FileInfo) *event.EncryptedFileInfo {
	if info == nil {
		return nil
	}
	return info.ThumbnailFile
}

func mediaSize(info *event.FileInfo) int {
	if info == nil {
		return 0
	}
	return info.Size
}

func mediaWidth(info *event.FileInfo) int {
	if info == nil {
		return 0
	}
	return info.Width
}

func mediaHeight(info *event.FileInfo) int {
	if info == nil {
		return 0
	}
	return info.Height
}

func mediaDuration(info *event.FileInfo) int {
	if info == nil || info.Duration <= 0 {
		return 0
	}
	return int(info.Duration)
}

// editHistory reports prior edit sources when available. gomuks only stores
// the current edit source, so this is a placeholder that surfaces "(edited)"
// detail when the SDK exposes more history in the future.
// mapSendError converts the database send marker into a user-facing error.
// gomuks stores the transient "not sent" marker on every fresh local event and
// (upstream) never clears it on success, so it must not surface as an error.
// Only genuine failures (encrypt/send errors) are reported; the pending state
// is conveyed through Message.Sending instead.
func mapSendError(sendError string) string {
	if sendError == "" || sendError == "not sent" {
		return ""
	}
	return sendError
}

// isPendingEvent reports whether an event is still an unsent local echo.
//
// send_error alone cannot answer this: gomuks writes the transient "not sent"
// marker into every fresh local event and its deferred cleanup in
// actuallySend() re-writes it after a successful send too (UpdateID clears the
// column, then the defer persists the still-set in-memory "not sent" value).
// Upstream knows this and its Resend() checks for the "~" local placeholder
// prefix on the event ID instead, so that is the reliable pending signal here.
// Without it every confirmed message stays labelled "Sending…".
func isPendingEvent(evt *database.Event) bool {
	if evt == nil || evt.SendError == "" {
		return false
	}
	return evt.ID == "" || strings.HasPrefix(evt.ID.String(), "~")
}

// isEditedEvent reports whether the event was actually edited. gomuks
// initializes LastEditRowID to a non-nil zero pointer on fresh sends, so a
// nil check alone flags brand-new messages as edited.
func isEditedEvent(evt *database.Event) bool {
	if evt == nil || evt.LastEditRowID == nil {
		return false
	}
	return *evt.LastEditRowID != 0
}

func editHistory(evt *database.Event) []string {
	if !isEditedEvent(evt) {
		return nil
	}
	if local := evt.LocalContent; local != nil && strings.TrimSpace(local.EditSource) != "" {
		return []string{strings.TrimSpace(local.EditSource)}
	}
	return nil
}

func (c *Client) storeLocalMessage(roomID string, message Message) {
	key := message.TransactionID
	if key == "" {
		key = message.ID
	}
	c.mu.Lock()
	if message.MediaURI != "" && message.MediaEncrypted != nil {
		if c.mediaEncrypted == nil {
			c.mediaEncrypted = make(map[string]*event.EncryptedFileInfo)
		}
		c.mediaEncrypted[message.MediaURI] = message.MediaEncrypted
	}
	if c.local[roomID] == nil {
		c.local[roomID] = make(map[string]Message)
	}
	c.local[roomID][key] = message
	room, exists := c.rooms[roomID]
	if exists {
		// Own optimistic sends often share the same millisecond as the previous
		// event, so use >= here. Otherwise the sidebar preview/time advances
		// but the text stays on the old bridge topic ("WhatsApp private chat")
		// while the open timeline already shows the sent message.
		if !message.Timestamp.Before(room.LastSeen) {
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
	if dismissedEventID != "" &&
		(room.LastEventID == dismissedEventID || (room.LastEventID == "" && dismissedEventID == room.ID)) {
		return false
	}
	// Answering the room clears it immediately. The server's unread count only
	// drops on a later sync, so a stale Unread > 0 must not keep a room in the
	// needs-reply view after the user has already replied.
	if userID != "" && room.LastEventSender == userID {
		return false
	}
	return room.Unread > 0 || (room.LastEventSender != "" && room.LastEventSender != userID)
}

func (c *Client) currentUserID() string {
	if c.engine == nil || c.engine.Account == nil {
		return ""
	}
	return string(c.engine.Account.UserID)
}

func (c *Client) removeLocalMessage(roomID string, evt *database.Event) {
	if evt == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removeLocalMessageLocked(roomID, evt)
}

func (c *Client) removeLocalMessageLocked(roomID string, evt *database.Event) {
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

// storeFailedMessage keeps a genuinely failed send visible (with its error)
// under both the transaction-ID and event-ID keys so the timeline and the
// retry lookup can find it regardless of which identifier the UI holds.
func (c *Client) storeFailedMessage(roomID string, message Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.local[roomID] == nil {
		c.local[roomID] = make(map[string]Message)
	}
	if message.TransactionID != "" {
		c.local[roomID][message.TransactionID] = message
	}
	if message.ID != "" {
		c.local[roomID][message.ID] = message
	}
}

// shadowedByServerRow reports whether a server-confirmed row already covers a
// local optimistic entry whose IDs no longer line up (e.g. the server echo
// arrived with a real event ID while the pending entry still carries the
// local ~txn ID, or vice versa). Matching on sender+body+timestamp proximity
// avoids leaving a stale "Sending..." duplicate next to the confirmed copy.
func shadowedByServerRow(messages []Message, candidate Message) bool {
	if candidate.ID == "" || !candidate.Sending {
		return false
	}
	for _, message := range messages {
		if message.Sending || message.Sender != candidate.Sender || message.Body != candidate.Body {
			continue
		}
		if message.ID != "" && !strings.HasPrefix(message.ID, "~") {
			return true
		}
	}
	return false
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
		NeedsVerification:   c.engine != nil && c.NeedsVerification(),
		ConnectionState:     c.ConnectionState(),
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

	// Keep the callback non-blocking even if the UI is busy rendering. The next
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

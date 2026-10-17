package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mychat/matrix"
	"mychat/notifications"
)

type AppService struct {
	client               *matrix.Client
	clientMu             sync.RWMutex
	accountMu            sync.RWMutex
	accountRegistry      *accountRegistry
	accountClients       map[string]*matrix.Client
	accountEventPumps    map[string]bool
	accountSyncStarted   map[string]bool
	accountDataDir       string
	accountMediaCacheDir string
	activeAccountID      string
	preferences          *PreferenceStore
	notifier             notifications.Notifier
	ctx                  context.Context
	cancel               context.CancelFunc
	quit                 func()
	setAutostart         func(bool) error
	setMenuBarIcon       func(bool)
	chooseSavePath       func(directory, filename string) (string, error)
	chooseDownloadFolder func() (string, error)
	activeRoomMu         sync.RWMutex
	activeRoomID         string
	workers              sync.WaitGroup
	scheduled            *scheduledJobStore

	emitUpdate   func(Update)
	emitReminder func(ScheduledJob)
}

type Snapshot struct {
	LoggedIn          bool             `json:"loggedIn"`
	NeedsVerification bool             `json:"needsVerification"`
	SyncReady         bool             `json:"syncReady"`
	ConnectionState   string           `json:"connectionState"`
	Profile           AccountProfile   `json:"profile"`
	Accounts          []AccountSummary `json:"accounts"`
	ActiveAccountID   string           `json:"activeAccountId"`
	Rooms             []Room           `json:"rooms"`
	Preferences       map[string]any   `json:"preferences"`
}

type Room struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Topic           string `json:"topic"`
	Avatar          string `json:"avatar"`
	Preview         string `json:"preview"`
	LastSeen        int64  `json:"lastSeen"`
	Direct          bool   `json:"direct"`
	Favorite        bool   `json:"favorite"`
	Muted           bool   `json:"muted"`
	Unread          int    `json:"unread"`
	MarkedUnread    bool   `json:"markedUnread"`
	Mentions        int    `json:"mentions"`
	Unanswered      bool   `json:"unanswered"`
	LastEventID     string `json:"lastEventId"`
	LastEventSender string `json:"lastEventSender"`
	LastEventAt     int64  `json:"lastEventAt"`
	Bridge          string `json:"bridge"`
	AccountID       string `json:"accountId"`
}

type AccountProfile struct {
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
	Homeserver  string `json:"homeserver"`
	DeviceID    string `json:"deviceId"`
	AvatarURI   string `json:"avatarUri"`
	Verified    bool   `json:"verified"`
}

type Message struct {
	ID              string          `json:"id"`
	TransactionID   string          `json:"transactionId"`
	Sender          string          `json:"sender"`
	SenderName      string          `json:"senderName"`
	Avatar          string          `json:"avatar"`
	Body            string          `json:"body"`
	FormattedBody   string          `json:"formattedBody"`
	ReplyToEventID  string          `json:"replyToEventId"`
	ReplyToSender   string          `json:"replyToSender"`
	ReplyToPreview  string          `json:"replyToPreview"`
	Timestamp       int64           `json:"timestamp"`
	TimelineCursor  int64           `json:"timelineCursor"`
	Sending         bool            `json:"sending"`
	SendError       string          `json:"sendError"`
	Own             bool            `json:"own"`
	Reactions       map[string]int  `json:"reactions"`
	Reacted         map[string]bool `json:"reacted"`
	MediaURI        string          `json:"mediaUri"`
	MediaType       string          `json:"mediaType"`
	MediaMIME       string          `json:"mediaMime"`
	MediaSize       int             `json:"mediaSize"`
	MediaWidth      int             `json:"mediaWidth"`
	MediaHeight     int             `json:"mediaHeight"`
	MediaDurationMs int             `json:"mediaDurationMs"`
	Encrypted       bool            `json:"encrypted"`
	DecryptionError string          `json:"decryptionError"`
	Edited          bool            `json:"edited"`
	EditHistory     []string        `json:"editHistory"`
	Redacted        bool            `json:"redacted"`
}

type HistoryPage struct {
	Messages []Message `json:"messages"`
	Before   int64     `json:"before"`
	HasMore  bool      `json:"hasMore"`
}

type Member struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type Device struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	LastSeenIP  string `json:"lastSeenIp"`
	LastSeenAt  int64  `json:"lastSeenAt"`
	Current     bool   `json:"current"`
}

type RoomKeyImportResult struct {
	Imported int `json:"imported"`
	Total    int `json:"total"`
}

type StorageUsage struct {
	DatabaseBytes int64 `json:"databaseBytes"`
	WALBytes      int64 `json:"walBytes"`
}

type MediaCacheUsage struct {
	Bytes int64 `json:"bytes"`
	Files int   `json:"files"`
}

// RoomStorage is an approximate per-chat storage attribution.
type RoomStorage struct {
	RoomID     string `json:"roomId"`
	Name       string `json:"name"`
	Messages   int    `json:"messages"`
	TextBytes  int64  `json:"textBytes"`
	MediaBytes int64  `json:"mediaBytes"`
	MediaFiles int    `json:"mediaFiles"`
}

// MediaCleanupRequest selects cached media to delete. Empty or zero fields match everything.
type MediaCleanupRequest struct {
	RoomID        string `json:"roomId"`
	OlderThanDays int    `json:"olderThanDays"`
	MediaType     string `json:"mediaType"`
}

type MediaCleanupResult struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

type Notification struct {
	RoomID  string `json:"roomId"`
	Room    string `json:"room"`
	Sender  string `json:"sender"`
	Body    string `json:"body"`
	Mention bool   `json:"mention"`
}

type Update struct {
	Rooms                 []Room              `json:"rooms"`
	ChangedRoomID         []string            `json:"changedRoomId"`
	TimelineChangedRoomID []string            `json:"timelineChangedRoomId"`
	TypingUsers           map[string][]string `json:"typingUsers"`
	Notifications         []Notification      `json:"notifications"`
	Error                 string              `json:"error"`
	InitialSyncComplete   bool                `json:"initialSyncComplete"`
	NeedsVerification     bool                `json:"needsVerification"`
	ConnectionState       string              `json:"connectionState"`
}

func (s *AppService) Snapshot() (Snapshot, error) {
	client := s.currentClient()
	if err := client.WaitForCache(s.ctx); err != nil {
		return Snapshot{}, err
	}
	profile := client.Profile()
	preferences := s.preferences.All()
	rooms := mapRoomsWithPreferences(client.Rooms(), preferences)
	tagRooms(rooms, s.currentAccountID())
	return Snapshot{
		LoggedIn:          client.IsLoggedIn(),
		NeedsVerification: client.NeedsVerification(),
		SyncReady:         client.SyncReady(),
		ConnectionState:   string(client.ConnectionState()),
		Profile: AccountProfile{
			UserID:      profile.UserID,
			DisplayName: profile.DisplayName,
			Homeserver:  profile.Homeserver,
			DeviceID:    profile.DeviceID,
			AvatarURI:   profile.AvatarURI,
			Verified:    profile.Verified,
		},
		Accounts:        s.Accounts(),
		ActiveAccountID: s.currentAccountID(),
		Rooms:           rooms,
		Preferences:     preferences,
	}, nil
}

func (s *AppService) Login(homeserver, username, password string) error {
	client := s.currentClient()
	if err := client.Login(s.ctx, homeserver, username, password); err != nil {
		return err
	}
	if s.accountRegistry != nil {
		account := accountSummary(s.currentAccountID(), client)
		if err := s.accountRegistry.Upsert(account); err != nil {
			return err
		}
		return s.accountRegistry.SetActive(account.ID)
	}
	return nil
}

func (s *AppService) Verify(recoveryKey string) error {
	client := s.currentClient()
	if err := client.Verify(s.ctx, recoveryKey); err != nil {
		return err
	}
	if s.accountRegistry != nil {
		return s.accountRegistry.Upsert(accountSummary(s.currentAccountID(), client))
	}
	return nil
}

func (s *AppService) History(roomID string, before int64) (HistoryPage, error) {
	initialLimit := 100
	if before == 0 {
		if configured, ok := s.preferences.All()["history.initial_messages"].(string); ok {
			if value, parseErr := strconv.Atoi(configured); parseErr == nil {
				initialLimit = value
			}
		}
	}
	page, err := s.currentClient().HistoryPageWithLimit(s.ctx, roomID, before, initialLimit)
	if err != nil {
		return HistoryPage{}, err
	}
	return HistoryPage{
		Messages: mapMessages(page.Messages),
		Before:   page.Before,
		HasMore:  page.HasMore,
	}, nil
}

func (s *AppService) SendMessage(roomID, text string) error {
	return s.currentClient().SendMessage(s.ctx, roomID, text)
}

func (s *AppService) SendMessageWithMentions(roomID, text string, userIDs []string) error {
	return s.currentClient().SendMessageWithMentions(s.ctx, roomID, text, userIDs)
}

func (s *AppService) SendReply(roomID, text, replyTo string) error {
	return s.currentClient().SendReply(s.ctx, roomID, text, replyTo)
}

func (s *AppService) EditMessage(roomID, text, eventID string) error {
	return s.currentClient().EditMessage(s.ctx, roomID, text, eventID)
}

func (s *AppService) React(roomID, eventID, emoji string) error {
	return s.currentClient().React(s.ctx, roomID, eventID, emoji)
}

func (s *AppService) RedactMessage(roomID, eventID string) error {
	return s.currentClient().RedactMessage(s.ctx, roomID, eventID)
}

func (s *AppService) RetryMessage(transactionID string) error {
	return s.currentClient().RetryMessage(s.ctx, transactionID)
}

func (s *AppService) MarkRead(roomID, eventID string) error {
	return s.currentClient().MarkRead(s.ctx, roomID, eventID)
}

func (s *AppService) MarkUnread(roomID string) error {
	return s.currentClient().MarkUnread(s.ctx, roomID)
}

func (s *AppService) SetTyping(roomID string, timeoutMilliseconds int64) error {
	return s.currentClient().SetTyping(s.ctx, roomID, time.Duration(timeoutMilliseconds)*time.Millisecond)
}

type MessageSearchRequest struct {
	Query        string `json:"query"`
	RoomID       string `json:"roomID"`
	Sender       string `json:"sender"`
	MinTimestamp int64  `json:"minTimestamp"`
	MaxTimestamp int64  `json:"maxTimestamp"`
	Limit        int    `json:"limit"`
	NextBatch    string `json:"nextBatch"`
}

type MessageSearchResult struct {
	RoomID  string  `json:"roomID"`
	Message Message `json:"message"`
}

type MessageSearchPage struct {
	Results   []MessageSearchResult `json:"results"`
	NextBatch string                `json:"nextBatch"`
}

func (s *AppService) SearchMessages(request MessageSearchRequest) (MessageSearchPage, error) {
	page, err := s.currentClient().SearchMessages(s.ctx, matrix.SearchRequest{
		Query:        request.Query,
		RoomID:       request.RoomID,
		Sender:       request.Sender,
		MinTimestamp: request.MinTimestamp,
		MaxTimestamp: request.MaxTimestamp,
		Limit:        request.Limit,
		NextBatch:    request.NextBatch,
	})
	if err != nil {
		return MessageSearchPage{}, err
	}
	results := make([]MessageSearchResult, 0, len(page.Results))
	for _, result := range page.Results {
		mapped := mapMessages([]matrix.Message{result.Message})
		if len(mapped) > 0 {
			results = append(results, MessageSearchResult{RoomID: result.RoomID, Message: mapped[0]})
		}
	}
	return MessageSearchPage{Results: results, NextBatch: page.NextBatch}, nil
}

func (s *AppService) ScheduleMessage(roomID, text string, dueAt int64) (ScheduledJob, error) {
	if err := s.validateScheduledRoom(roomID); err != nil {
		return ScheduledJob{}, err
	}
	return s.scheduled.scheduleMessage(s.currentAccountID(), roomID, text, dueAt)
}

func (s *AppService) ScheduleReply(roomID, text, replyToEventID string, dueAt int64) (ScheduledJob, error) {
	if err := s.validateScheduledRoom(roomID); err != nil {
		return ScheduledJob{}, err
	}
	return s.scheduled.scheduleReply(s.currentAccountID(), roomID, text, replyToEventID, dueAt)
}

func (s *AppService) ScheduleReminder(roomID, eventID, text string, dueAt int64) (ScheduledJob, error) {
	if err := s.validateScheduledRoom(roomID); err != nil {
		return ScheduledJob{}, err
	}
	return s.scheduled.scheduleReminder(s.currentAccountID(), roomID, eventID, text, dueAt)
}

func (s *AppService) ScheduledJobs() []ScheduledJob {
	if s.scheduled == nil {
		return []ScheduledJob{}
	}
	return s.scheduled.listForAccount(s.currentAccountID())
}

func (s *AppService) UpdateScheduledJob(id string, dueAt int64) error {
	if s.scheduled == nil {
		return errors.New("scheduled jobs are unavailable")
	}
	return s.scheduled.update(id, dueAt)
}

func (s *AppService) CancelScheduledJob(id string) error {
	if s.scheduled == nil {
		return errors.New("scheduled jobs are unavailable")
	}
	return s.scheduled.cancel(id)
}

func (s *AppService) RetryScheduledMessage(id string) error {
	if s.scheduled == nil {
		return errors.New("scheduled jobs are unavailable")
	}
	return s.scheduled.retryMessage(id, time.Now().UnixMilli())
}

func (s *AppService) DismissScheduledJob(id string) error {
	if s.scheduled == nil {
		return errors.New("scheduled jobs are unavailable")
	}
	return s.scheduled.dismiss(id)
}

func (s *AppService) validateScheduledRoom(roomID string) error {
	if s.scheduled == nil {
		return errors.New("scheduled jobs are unavailable")
	}
	if roomID == "" {
		return errors.New("select a room first")
	}
	for _, room := range s.currentClient().Rooms() {
		if room.ID == roomID {
			return nil
		}
	}
	return errors.New("scheduled job room was not found")
}

func (s *AppService) RoomMembers(roomID string) ([]Member, error) {
	members, err := s.currentClient().RoomMembers(s.ctx, roomID)
	if err != nil {
		return nil, err
	}
	mapped := make([]Member, 0, len(members))
	for _, member := range members {
		mapped = append(mapped, Member{ID: member.ID, DisplayName: member.DisplayName})
	}
	return mapped, nil
}

func (s *AppService) LeaveRoom(roomID string) error {
	return s.currentClient().LeaveRoom(s.ctx, roomID)
}

func (s *AppService) Devices() ([]Device, error) {
	devices, err := s.currentClient().Devices(s.ctx)
	if err != nil {
		return nil, err
	}
	mapped := make([]Device, 0, len(devices))
	for _, device := range devices {
		lastSeenAt := int64(0)
		if !device.LastSeenAt.IsZero() {
			lastSeenAt = device.LastSeenAt.UnixMilli()
		}
		mapped = append(mapped, Device{
			ID:          device.ID,
			DisplayName: device.DisplayName,
			LastSeenIP:  device.LastSeenIP,
			LastSeenAt:  lastSeenAt,
			Current:     device.Current,
		})
	}
	return mapped, nil
}

func (s *AppService) RevokeDevice(deviceID string) error {
	return s.currentClient().RevokeDevice(s.ctx, deviceID)
}

func (s *AppService) ExportRoomKeys(passphrase string) ([]byte, error) {
	return s.currentClient().ExportRoomKeys(s.ctx, passphrase)
}

func (s *AppService) ImportRoomKeys(passphrase string, data []byte) (RoomKeyImportResult, error) {
	imported, total, err := s.currentClient().ImportRoomKeys(s.ctx, passphrase, data)
	if err != nil {
		return RoomKeyImportResult{}, err
	}
	return RoomKeyImportResult{Imported: imported, Total: total}, nil
}

func (s *AppService) SendAttachment(roomID, filename string, data []byte, voiceNote bool, durationMilliseconds int64) error {
	if int64(len(data)) > maxAttachmentSize {
		return fmt.Errorf("attachment exceeds the 50 MiB limit")
	}
	if voiceNote {
		return s.currentClient().SendVoiceNote(s.ctx, roomID, filename, data, durationMilliseconds)
	}
	return s.currentClient().SendAttachment(s.ctx, roomID, filename, data)
}

func (s *AppService) DownloadMedia(mxc string) ([]byte, error) {
	return s.currentClient().CachedMediaBytes(s.ctx, mxc)
}

// SaveMedia writes a cached attachment to the download folder, or to a path chosen in a save dialog when ask-each-time is enabled. An empty path with no error means the user cancelled.
func (s *AppService) SaveMedia(mxc, filename string) (string, error) {
	data, err := s.currentClient().CachedMediaBytes(s.ctx, mxc)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", errors.New("no media data to save")
	}
	name := safeDownloadName(filename)
	preferences := s.preferences.All()
	directory, _ := preferences["downloads.folder"].(string)
	if directory == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", homeErr
		}
		directory = filepath.Join(home, "Downloads")
	}
	askEach, _ := preferences["downloads.ask_each_time"].(bool)
	target := ""
	if askEach {
		if s.chooseSavePath == nil {
			return "", errors.New("save dialog is unavailable")
		}
		target, err = s.chooseSavePath(directory, name)
		if err != nil {
			return "", err
		}
		if target == "" {
			return "", nil
		}
	} else {
		target = uniqueDownloadPath(directory, name)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("create download folder: %w", err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", fmt.Errorf("save attachment: %w", err)
	}
	return target, nil
}

// ChooseDownloadFolder opens a native folder picker and returns the selected directory, or an empty string when cancelled.
func (s *AppService) ChooseDownloadFolder() (string, error) {
	if s.chooseDownloadFolder == nil {
		return "", errors.New("folder picker is unavailable")
	}
	return s.chooseDownloadFolder()
}

func safeDownloadName(filename string) string {
	name := filepath.Base(strings.TrimSpace(filename))
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return "attachment"
	}
	return name
}

// uniqueDownloadPath returns a path in directory that does not overwrite an existing file, appending " (n)" before the extension when needed.
func uniqueDownloadPath(directory, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	candidate := filepath.Join(directory, name)
	for i := 1; i < 1000; i++ {
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
		candidate = filepath.Join(directory, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
	return candidate
}

func (s *AppService) DownloadAvatar(mxc string) (string, error) {
	data, err := s.currentClient().DownloadAvatar(s.ctx, mxc)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return "", fmt.Errorf("Matrix avatar is not an image: %s", mimeType)
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (s *AppService) StorageUsage() (StorageUsage, error) {
	databaseBytes, walBytes, err := s.currentClient().StorageUsage()
	if err != nil {
		return StorageUsage{}, err
	}
	return StorageUsage{DatabaseBytes: databaseBytes, WALBytes: walBytes}, nil
}

func (s *AppService) MediaCacheUsage() (MediaCacheUsage, error) {
	bytes, files, err := s.currentClient().MediaCacheUsage()
	if err != nil {
		return MediaCacheUsage{}, err
	}
	return MediaCacheUsage{Bytes: bytes, Files: files}, nil
}

func (s *AppService) ClearMediaCache() error {
	return s.currentClient().ClearMediaCache()
}

// RoomStorageUsage reports cached media and approximate message storage per chat.
func (s *AppService) RoomStorageUsage() ([]RoomStorage, error) {
	usage, err := s.currentClient().RoomStorageUsage(s.ctx)
	if err != nil {
		return nil, err
	}
	result := make([]RoomStorage, 0, len(usage))
	for _, entry := range usage {
		result = append(result, RoomStorage{
			RoomID:     entry.RoomID,
			Name:       entry.Name,
			Messages:   entry.Messages,
			TextBytes:  entry.TextBytes,
			MediaBytes: entry.MediaBytes,
			MediaFiles: entry.MediaFiles,
		})
	}
	return result, nil
}

// ClearCachedMedia deletes cached media matching the request. Messages are kept.
func (s *AppService) ClearCachedMedia(request MediaCleanupRequest) (MediaCleanupResult, error) {
	filter, err := mediaCleanupFilter(request, time.Now())
	if err != nil {
		return MediaCleanupResult{}, err
	}
	result, err := s.currentClient().ClearCachedMedia(s.ctx, filter)
	if err != nil {
		return MediaCleanupResult{}, err
	}
	return MediaCleanupResult{Files: result.Files, Bytes: result.Bytes}, nil
}

func mediaCleanupFilter(request MediaCleanupRequest, now time.Time) (matrix.MediaCleanupFilter, error) {
	if request.OlderThanDays < 0 {
		return matrix.MediaCleanupFilter{}, errors.New("age must be zero or more days")
	}
	filter := matrix.MediaCleanupFilter{
		RoomID:    strings.TrimSpace(request.RoomID),
		MediaType: strings.TrimSpace(request.MediaType),
	}
	if request.OlderThanDays > 0 {
		filter.OlderThan = now.AddDate(0, 0, -request.OlderThanDays)
	}
	if err := filter.Validate(); err != nil {
		return matrix.MediaCleanupFilter{}, err
	}
	return filter, nil
}

func (s *AppService) OptimizeDatabase() error {
	return s.currentClient().OptimizeDatabase(s.ctx)
}

func (s *AppService) SetRoomUnanswered(roomID string, unanswered bool) error {
	return s.currentClient().SetRoomUnanswered(s.ctx, roomID, unanswered)
}

func (s *AppService) SetBooleanPreference(key string, value bool) error {
	if key == "system.launch_at_startup" && s.setAutostart != nil {
		previous, _ := s.preferences.All()[key].(bool)
		if err := s.setAutostart(value); err != nil {
			return fmt.Errorf("update launch at startup: %w", err)
		}
		if err := s.preferences.Set(key, value); err != nil {
			if rollbackErr := s.setAutostart(previous); rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("restore launch at startup: %w", rollbackErr))
			}
			return err
		}
		return nil
	}
	if err := s.preferences.Set(key, value); err != nil {
		return err
	}
	if key == "window.menu_bar_icon" && s.setMenuBarIcon != nil {
		s.setMenuBarIcon(value)
	}
	return nil
}

func (s *AppService) SetActiveRoom(roomID string) {
	s.activeRoomMu.Lock()
	s.activeRoomID = roomID
	s.activeRoomMu.Unlock()
}

func (s *AppService) sendDroppedFiles(paths []string) (string, bool, error) {
	if len(paths) == 0 {
		return "", false, nil
	}
	s.activeRoomMu.RLock()
	roomID := s.activeRoomID
	s.activeRoomMu.RUnlock()
	if roomID == "" {
		return "", false, errors.New("select a room before dropping attachments")
	}

	var combinedErr error
	sent := false
	for _, path := range paths {
		filename, data, err := readDroppedAttachment(path)
		if err != nil {
			combinedErr = errors.Join(combinedErr, err)
			continue
		}
		if err = s.SendAttachment(roomID, filename, data, false, 0); err != nil {
			combinedErr = errors.Join(combinedErr, fmt.Errorf("send dropped file %q: %w", filename, err))
			continue
		}
		sent = true
	}
	return roomID, sent, combinedErr
}

func (s *AppService) markAllRead() error {
	var combinedErr error
	client := s.currentClient()
	for _, room := range client.Rooms() {
		page, err := client.HistoryPage(s.ctx, room.ID, 0)
		if err != nil {
			combinedErr = errors.Join(combinedErr, fmt.Errorf("load history for %s: %w", room.ID, err))
			continue
		}
		for i := len(page.Messages) - 1; i >= 0; i-- {
			message := page.Messages[i]
			if message.ID == "" || strings.HasPrefix(message.ID, "~") {
				continue
			}
			if err = client.MarkRead(s.ctx, room.ID, message.ID); err != nil {
				combinedErr = errors.Join(combinedErr, fmt.Errorf("mark room %s as read: %w", room.ID, err))
			}
			break
		}
	}
	return combinedErr
}

func (s *AppService) SetStringPreference(key, value string) error {
	return s.preferences.Set(key, value)
}

func (s *AppService) NativeNotificationsSupported() bool {
	return s.notifier != nil && s.notifier.Supported()
}

func (s *AppService) NotificationPermission() string {
	if s.notifier == nil {
		return "unsupported"
	}
	return s.notifier.Permission()
}

func (s *AppService) RequestNotificationPermission() (string, error) {
	if s.notifier == nil {
		return "unsupported", nil
	}
	return s.notifier.RequestPermission()
}

func (s *AppService) SendNotification(title, body, roomID string) (bool, error) {
	if s.notifier == nil || !s.notifier.Supported() || s.notifier.Permission() != "granted" {
		return false, nil
	}
	if err := s.notifier.Send(title, body, roomID, false); err != nil {
		return false, err
	}
	return true, nil
}

func (s *AppService) Logout() error {
	accountID := s.currentAccountID()
	client := s.currentClient()
	if err := client.Logout(s.ctx); err != nil {
		return err
	}
	clients := s.accountClientsSnapshot()
	s.accountMu.RLock()
	registry := s.accountRegistry
	s.accountMu.RUnlock()
	if registry != nil {
		for _, account := range registry.All() {
			nextClient := clients[account.ID]
			if account.ID == accountID || nextClient == nil || !nextClient.IsLoggedIn() {
				continue
			}
			if err := registry.SetActive(account.ID); err != nil {
				return err
			}
			s.accountMu.Lock()
			s.activeAccountID = account.ID
			s.accountMu.Unlock()
			s.clientMu.Lock()
			s.client = nextClient
			s.clientMu.Unlock()
			return nil
		}
	}
	if s.quit != nil {
		s.quit()
	}
	return nil
}

func (s *AppService) startEventPump(emit func(Update)) {
	s.accountMu.Lock()
	s.emitUpdate = emit
	s.accountMu.Unlock()
	for accountID, client := range s.accountClientsSnapshot() {
		s.startAccountEventPump(accountID, client)
	}
}

func (s *AppService) startSync() {
	for accountID, client := range s.accountClientsSnapshot() {
		s.startAccountSync(accountID, client)
	}
}

func (s *AppService) startAccountSync(accountID string, client *matrix.Client) {
	s.accountMu.Lock()
	if s.accountSyncStarted == nil {
		s.accountSyncStarted = make(map[string]bool)
	}
	if s.accountSyncStarted[accountID] || !client.IsLoggedIn() || s.ctx.Err() != nil {
		s.accountMu.Unlock()
		return
	}
	s.accountSyncStarted[accountID] = true
	s.workers.Add(1)
	s.accountMu.Unlock()
	go func() {
		defer s.workers.Done()
		if err := client.StartWithRetry(s.ctx); err != nil && s.ctx.Err() == nil && s.currentAccountID() == accountID && s.emitUpdate != nil {
			s.emitUpdate(Update{Error: err.Error(), ConnectionState: string(matrix.StateOffline)})
		}
	}()
}

func (s *AppService) startScheduledJobWorker() {
	if s.scheduled == nil || s.currentClient() == nil {
		return
	}
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		s.processDueScheduledJobs()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.processDueScheduledJobs()
			}
		}
	}()
}

type scheduledMessageSender interface {
	SendMessage(context.Context, string, string) error
	SendReply(context.Context, string, string, string) error
}

func dispatchScheduledMessage(ctx context.Context, sender scheduledMessageSender, job ScheduledJob) error {
	if job.ReplyToEventID != "" {
		return sender.SendReply(ctx, job.RoomID, job.Text, job.ReplyToEventID)
	}
	return sender.SendMessage(ctx, job.RoomID, job.Text)
}

func dispatchDueScheduledMessagesForAccount(ctx context.Context, store *scheduledJobStore, accountID string, sender scheduledMessageSender, now int64) error {
	jobs, err := store.claimDueForAccount(scheduledMessageKind, now, accountID)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		startedAt := time.Now().UnixMilli()
		sendErr := dispatchScheduledMessage(ctx, sender, job)
		if finishErr := store.finishMessage(job.ID, sendErr, startedAt); finishErr != nil {
			log.Printf("finish scheduled message %s: %v", job.ID, finishErr)
		}
	}
	return nil
}

func (s *AppService) processDueScheduledJobs() {
	if s.scheduled == nil || s.ctx.Err() != nil {
		return
	}
	now := time.Now().UnixMilli()
	for accountID, client := range s.accountClientsSnapshot() {
		if !client.IsLoggedIn() || client.ConnectionState() != matrix.StateConnected {
			continue
		}
		if err := dispatchDueScheduledMessagesForAccount(s.ctx, s.scheduled, accountID, client, now); err != nil {
			log.Printf("claim scheduled messages for account %s: %v", accountID, err)
		}
	}
	reminders, err := s.scheduled.claimDue(scheduledReminderKind, now)
	if err != nil {
		log.Printf("claim scheduled reminders: %v", err)
		return
	}
	for _, reminder := range reminders {
		if s.emitReminder != nil {
			s.emitReminder(reminder)
		}
	}
}

func (s *AppService) close() {
	s.cancel()
	s.workers.Wait()
}

func mapRooms(rooms []matrix.Room) []Room {
	mapped := make([]Room, 0, len(rooms))
	for _, room := range rooms {
		mapped = append(mapped, Room{
			ID:              room.ID,
			Name:            room.Name,
			Topic:           room.Topic,
			Avatar:          room.Avatar,
			Preview:         room.Preview,
			LastSeen:        room.LastSeen.UnixMilli(),
			Direct:          room.Direct,
			Favorite:        room.Favorite,
			Muted:           room.Muted,
			Unread:          room.Unread,
			MarkedUnread:    room.MarkedUnread,
			Mentions:        room.Mentions,
			Unanswered:      room.Unanswered,
			LastEventID:     room.LastEventID,
			LastEventSender: room.LastEventSender,
			LastEventAt:     room.LastEventAt.UnixMilli(),
			Bridge:          room.Bridge,
		})
	}
	return mapped
}

// tagRooms sets the owning account on each room in place.
func tagRooms(rooms []Room, accountID string) {
	for i := range rooms {
		rooms[i].AccountID = accountID
	}
}

func mapRoomsWithPreferences(rooms []matrix.Room, preferences map[string]any) []Room {
	mapped := mapRooms(rooms)
	for i := range mapped {
		mapped[i].Favorite, _ = preferences["room.favorite."+mapped[i].ID].(bool)
		mapped[i].Muted, _ = preferences["room.muted."+mapped[i].ID].(bool)
	}
	return mapped
}

func mapMessages(messages []matrix.Message) []Message {
	mapped := make([]Message, 0, len(messages))
	for _, message := range messages {
		mapped = append(mapped, Message{
			ID:              message.ID,
			TransactionID:   message.TransactionID,
			Sender:          message.Sender,
			SenderName:      message.SenderName,
			Avatar:          message.Avatar,
			Body:            message.Body,
			FormattedBody:   message.FormattedBody,
			ReplyToEventID:  message.ReplyToEventID,
			ReplyToSender:   message.ReplyToSender,
			ReplyToPreview:  message.ReplyToPreview,
			Timestamp:       message.Timestamp.UnixMilli(),
			TimelineCursor:  message.TimelineCursor,
			Sending:         message.Sending,
			SendError:       message.SendError,
			Own:             message.Own,
			Reactions:       message.Reactions,
			Reacted:         message.Reacted,
			MediaURI:        message.MediaURI,
			MediaType:       message.MediaType,
			MediaMIME:       message.MediaMIME,
			MediaSize:       message.MediaSize,
			MediaWidth:      message.MediaWidth,
			MediaHeight:     message.MediaHeight,
			MediaDurationMs: message.MediaDurationMs,
			Encrypted:       message.Encrypted,
			DecryptionError: message.DecryptionError,
			Edited:          message.Edited,
			EditHistory:     message.EditHistory,
			Redacted:        message.Redacted,
		})
	}
	return mapped
}

func mapUpdate(update matrix.Update, preferences map[string]any) Update {
	notifications := make([]Notification, 0, len(update.Notifications))
	for _, notification := range update.Notifications {
		notifications = append(notifications, Notification{
			RoomID:  notification.RoomID,
			Room:    notification.Room,
			Sender:  notification.Sender,
			Body:    notification.Body,
			Mention: notification.Mention,
		})
	}
	return Update{
		Rooms:                 mapRoomsWithPreferences(update.Rooms, preferences),
		ChangedRoomID:         update.ChangedRoomID,
		TimelineChangedRoomID: update.TimelineChangedRoomID,
		TypingUsers:           update.TypingUsers,
		Notifications:         notifications,
		Error:                 update.Error,
		InitialSyncComplete:   update.InitialSyncComplete,
		NeedsVerification:     update.NeedsVerification,
		ConnectionState:       string(update.ConnectionState),
	}
}

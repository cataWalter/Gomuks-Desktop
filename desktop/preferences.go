package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type PreferenceStore struct {
	mu     sync.RWMutex
	path   string
	values map[string]any
}

func NewPreferenceStore(path string) (*PreferenceStore, error) {
	if path == "" {
		return nil, errors.New("preferences path is empty")
	}
	store := &PreferenceStore{path: path, values: make(map[string]any)}
	contents, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read preferences: %w", err)
	}
	if err = json.Unmarshal(contents, &store.values); err != nil {
		return nil, fmt.Errorf("decode preferences: %w", err)
	}
	if store.values == nil {
		return nil, errors.New("preferences must contain a JSON object")
	}
	return store, nil
}

func (s *PreferenceStore) All() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return maps.Clone(s.values)
}

func (s *PreferenceStore) Set(key string, value any) error {
	if err := validatePreference(key, value); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.values[key]
	s.values[key] = value
	if err := s.saveLocked(); err != nil {
		if existed {
			s.values[key] = previous
		} else {
			delete(s.values, key)
		}
		return err
	}
	return nil
}

func (s *PreferenceStore) saveLocked() error {
	contents, err := json.Marshal(s.values)
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	contents = append(contents, '\n')
	directory := filepath.Dir(s.path)
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create preferences directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".gomuks-preferences-*")
	if err != nil {
		return fmt.Errorf("create temporary preferences file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o600); err == nil {
		var written int
		written, err = temporary.Write(contents)
		if err == nil && written != len(contents) {
			err = errors.New("short write")
		}
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write temporary preferences file: %w", err)
	}
	if err = os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace preferences file: %w", err)
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open preferences directory: %w", err)
	}
	if err = directoryFile.Sync(); err != nil {
		_ = directoryFile.Close()
		return fmt.Errorf("sync preferences directory: %w", err)
	}
	if err = directoryFile.Close(); err != nil {
		return fmt.Errorf("close preferences directory: %w", err)
	}
	return nil
}

func validatePreference(key string, value any) error {
	for _, prefix := range []string{"room.archived.", "room.favorite.", "room.muted."} {
		if strings.HasPrefix(key, prefix) {
			if strings.TrimPrefix(key, prefix) == "" {
				return errors.New("room preference requires a room ID")
			}
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("room preference %q must be a boolean", key)
			}
			return nil
		}
	}

	switch key {
	case "appearance.compact", "appearance.hide_deleted_messages", "appearance.message_animations", "appearance.pointer_cursor", "appearance.show_avatars", "appearance.show_timestamps", "appearance.time_24_hour", "appearance.relative_timestamps",
		"clipboard.copy_timestamp", "composer.arrow_up_focus_timeline", "composer.ctrl_enter", "composer.spellcheck", "notifications.enabled", "notifications.only_when_unfocused", "notifications.sound",
		"sidebar.compact_chat_list", "sidebar.show_previews", "sidebar.show_unread_counts", "window.minimize_to_tray", "window.room_details_visible", "window.sidebar_visible", "system.launch_at_startup", "voice.autoplay_next":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("preference %q must be a boolean", key)
		}
	case "notifications.direct", "notifications.mentions", "notifications.group", "notifications.otp_immediate", "notifications.otp_copy", "sidebar.transparent", "sidebar.network_icons", "reactions.quick_enabled", "window.start_minimized", "window.quit_on_close", "window.menu_bar_icon", "downloads.ask_each_time", "appearance.balanced_text", "composer.formatting_toolbar", "emoji.autocomplete", "emoji.replace_emoticons":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("preference %q must be a boolean", key)
		}
	case "downloads.folder":
		folder, ok := value.(string)
		if !ok {
			return fmt.Errorf("preference %q must be a string", key)
		}
		if folder != "" && (!filepath.IsAbs(folder) || strings.ContainsRune(folder, 0)) {
			return errors.New("download folder must be an absolute path")
		}
		return nil
	case "ai.endpoint":
		endpoint, ok := value.(string)
		if !ok {
			return fmt.Errorf("preference %q must be a string", key)
		}
		if endpoint == "" {
			return nil
		}
		return validateAIEndpoint(endpoint)
	case "ai.model":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("preference %q must be a string", key)
		}
	case "ai.translate_language":
		language, ok := value.(string)
		if !ok {
			return fmt.Errorf("preference %q must be a string", key)
		}
		if len(language) > 40 {
			return errors.New("AI translation language must be 40 characters or fewer")
		}
	case "notifications.sound_direct", "notifications.sound_mention", "notifications.sound_group":
		if !oneOf(value, "Chime", "Pulse", "Soft", "Silent") {
			return fmt.Errorf("%s has an unsupported value", key)
		}
	case "notifications.batch_seconds":
		if !oneOf(value, "0", "3", "10", "30") {
			return errors.New("notifications.batch_seconds has an unsupported value")
		}
	case "notifications.repeat_minutes":
		if !oneOf(value, "0", "5", "15", "60") {
			return errors.New("notifications.repeat_minutes has an unsupported value")
		}
	case "sidebar.pinned_style":
		if !oneOf(value, "Top of list", "Star only") {
			return errors.New("sidebar.pinned_style has an unsupported value")
		}
	case "reactions.quick_source":
		if !oneOf(value, "Frequent", "Custom") {
			return errors.New("reactions.quick_source has an unsupported value")
		}
	case "reactions.custom_emojis":
		if !validEmojiList(value) {
			return errors.New("reactions.custom_emojis must contain 1 to 6 emoji separated by spaces")
		}
	case "reactions.default_emoji":
		if !validEmojiList(value) || strings.Contains(value.(string), " ") {
			return errors.New("reactions.default_emoji must be a single emoji")
		}
	case "messages.double_click":
		if !oneOf(value, "None", "Reply", "React") {
			return errors.New("messages.double_click has an unsupported value")
		}
	case "sounds.effects":
		if !oneOf(value, "Off", "Sent messages") {
			return errors.New("sounds.effects has an unsupported value")
		}
	case "appearance.theme":
		if !oneOf(value, "System default", "Light", "Dark", "OLED black", "Bosco", "Rosa") {
			return errors.New("appearance.theme has an unsupported value")
		}
	case "appearance.accent_color":
		if !oneOf(value, "Forest", "Ocean", "Rose", "Gold") {
			return errors.New("appearance.accent_color has an unsupported value")
		}
	case "appearance.chat_wallpaper":
		if !oneOf(value, "None", "Meadow", "Tide", "Rose") {
			return errors.New("appearance.chat_wallpaper has an unsupported value")
		}
	case "appearance.font_size":
		if !oneOf(value, "Small", "Medium", "Large") {
			return errors.New("appearance.font_size has an unsupported value")
		}
	case "appearance.zoom":
		if !oneOf(value, "75", "80", "85", "90", "95", "100", "105", "110", "115", "120", "125") {
			return errors.New("appearance.zoom has an unsupported value")
		}
	case "notifications.privacy":
		if !oneOf(value, "Sender and message preview", "Sender only", "Hidden content") {
			return errors.New("notifications.privacy has an unsupported value")
		}
	case "composer.undo_send_seconds":
		if !oneOf(value, "0", "5", "10", "20", "30") {
			return errors.New("composer.undo_send_seconds has an unsupported value")
		}
	case "appearance.bubble_color":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("preference %q must be a string", key)
		}
	case "schedule.suggestions_minutes":
		if !validScheduleSuggestions(value) {
			return errors.New("schedule.suggestions_minutes must contain 1 to 6 unique minute values from 1 to 10080")
		}
	case "schedule.default_send_minutes", "schedule.default_reminder_minutes":
		if !validScheduleDelay(value) {
			return fmt.Errorf("preference %q must be a minute value from 1 to 10080", key)
		}
	case "archive.next_room":
		if !oneOf(value, "next_visible", "next_unread", "none") {
			return errors.New("archive.next_room has an unsupported value")
		}
	case "archive.older_than_days":
		if !oneOf(value, "30", "60", "90", "180", "365") {
			return errors.New("archive.older_than_days has an unsupported value")
		}
	case "inbox.after_read":
		if !oneOf(value, "stay", "next_visible", "next_unread", "none") {
			return errors.New("inbox.after_read has an unsupported value")
		}
	case "audio.input_device":
		if _, ok := value.(string); !ok {
			return errors.New("audio.input_device must be a string")
		}
	case "shortcuts.mark_answered":
		shortcut, ok := value.(string)
		if !ok || len([]rune(shortcut)) != 1 {
			return errors.New("shortcuts.mark_answered must contain one character")
		}
	case "history.initial_messages":
		if !oneOf(value, "25", "50", "100", "250", "500", "1000") {
			return errors.New("history.initial_messages has an unsupported value")
		}
	default:
		return fmt.Errorf("unknown preference %q", key)
	}
	return nil
}

// validEmojiList accepts 1 to 6 space-separated tokens, each up to 32 bytes.
func validEmojiList(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	fields := strings.Fields(text)
	if len(fields) < 1 || len(fields) > 6 {
		return false
	}
	for _, field := range fields {
		if len(field) > 32 {
			return false
		}
	}
	return true
}

func validScheduleSuggestions(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	parts := strings.Split(text, ",")
	if len(parts) < 1 || len(parts) > 6 {
		return false
	}
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		minutes, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || minutes < 1 || minutes > 10080 {
			return false
		}
		if _, duplicate := seen[minutes]; duplicate {
			return false
		}
		seen[minutes] = struct{}{}
	}
	return true
}

func validScheduleDelay(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	minutes, err := strconv.Atoi(text)
	return err == nil && minutes >= 1 && minutes <= 10080
}

func oneOf(value any, options ...string) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, option := range options {
		if text == option {
			return true
		}
	}
	return false
}

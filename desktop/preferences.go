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
	for _, prefix := range []string{"room.favorite.", "room.muted."} {
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
	case "appearance.compact", "appearance.show_avatars", "appearance.time_24_hour", "appearance.relative_timestamps",
		"composer.ctrl_enter", "notifications.enabled", "notifications.only_when_unfocused", "notifications.sound",
		"window.minimize_to_tray", "window.room_details_visible", "window.sidebar_visible", "system.launch_at_startup":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("preference %q must be a boolean", key)
		}
	case "appearance.theme":
		if !oneOf(value, "System default", "Light", "Dark", "OLED black", "Bosco", "Rosa") {
			return errors.New("appearance.theme has an unsupported value")
		}
	case "appearance.font_size":
		if !oneOf(value, "Small", "Medium", "Large") {
			return errors.New("appearance.font_size has an unsupported value")
		}
	case "notifications.privacy":
		if !oneOf(value, "Sender and message preview", "Sender only", "Hidden content") {
			return errors.New("notifications.privacy has an unsupported value")
		}
	case "appearance.bubble_color":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("preference %q must be a string", key)
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

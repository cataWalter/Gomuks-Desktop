package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPreferenceStoreLoadsAndPersistsValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "preferences.json")
	legacyValues := map[string]any{
		"appearance.theme":   "Dark",
		"appearance.compact": true,
		"future.preference":  "preserved",
	}
	contents, err := json.Marshal(legacyValues)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewPreferenceStore(path)
	if err != nil {
		t.Fatalf("NewPreferenceStore() error = %v", err)
	}
	if got := store.All(); !reflect.DeepEqual(got, map[string]any{
		"appearance.theme":   "Dark",
		"appearance.compact": true,
		"future.preference":  "preserved",
	}) {
		t.Fatalf("All() = %#v", got)
	}
	if err = store.Set("room.favorite.!room:example.org", true); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err = store.Set("room.archived.primary.!room:example.org", true); err != nil {
		t.Fatalf("Set() room archive error = %v", err)
	}
	if err = store.Set("appearance.theme", "Light"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err = store.Set("history.initial_messages", "250"); err != nil {
		t.Fatalf("Set() history.initial_messages error = %v", err)
	}

	reloaded, err := NewPreferenceStore(path)
	if err != nil {
		t.Fatalf("reload preferences: %v", err)
	}
	values := reloaded.All()
	if values["appearance.theme"] != "Light" || values["room.favorite.!room:example.org"] != true || values["room.archived.primary.!room:example.org"] != true || values["history.initial_messages"] != "250" || values["future.preference"] != "preserved" {
		t.Fatalf("persisted values = %#v", values)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("preferences permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestPreferenceStoreAcceptsAdditionalThemes(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, theme := range []string{"Bosco", "Rosa"} {
		if err = store.Set("appearance.theme", theme); err != nil {
			t.Errorf("Set(appearance.theme, %q) error = %v", theme, err)
		}
	}
}

func TestPreferenceStoreAcceptsSpellcheckSetting(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Set("composer.spellcheck", false); err != nil {
		t.Fatalf("Set(composer.spellcheck, false) error = %v", err)
	}
}

func TestPreferenceStoreAcceptsCopyTimestampSetting(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Set("clipboard.copy_timestamp", true); err != nil {
		t.Fatalf("Set(clipboard.copy_timestamp, true) error = %v", err)
	}
}

func TestPreferenceStoreAcceptsAccentPalette(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, palette := range []string{"Forest", "Ocean", "Rose", "Gold"} {
		if err = store.Set("appearance.accent_color", palette); err != nil {
			t.Errorf("Set(appearance.accent_color, %q) error = %v", palette, err)
		}
	}
}

func TestPreferenceStoreAcceptsChatWallpaper(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, wallpaper := range []string{"None", "Meadow", "Tide", "Rose"} {
		if err = store.Set("appearance.chat_wallpaper", wallpaper); err != nil {
			t.Errorf("Set(appearance.chat_wallpaper, %q) error = %v", wallpaper, err)
		}
	}
}

func TestPreferenceStoreAcceptsChatListSettings(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"sidebar.compact_chat_list", "sidebar.show_previews", "sidebar.show_unread_counts"} {
		if err = store.Set(key, false); err != nil {
			t.Errorf("Set(%s, false) error = %v", key, err)
		}
	}
}

func TestPreferenceStoreAcceptsScheduleTimeSuggestions(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Set("schedule.suggestions_minutes", "5, 15, 60, 240"); err != nil {
		t.Fatalf("Set(schedule.suggestions_minutes) error = %v", err)
	}
}

func TestPreferenceStoreAcceptsArchiveNavigationPreference(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"next_visible", "next_unread", "none"} {
		if err = store.Set("archive.next_room", value); err != nil {
			t.Errorf("Set(archive.next_room, %q) error = %v", value, err)
		}
	}
}

func TestPreferenceStoreAcceptsArchiveAgePreference(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"30", "60", "90", "180", "365"} {
		if err = store.Set("archive.older_than_days", value); err != nil {
			t.Errorf("Set(archive.older_than_days, %q) error = %v", value, err)
		}
	}
}

func TestPreferenceStoreAcceptsAfterReadNavigationPreference(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"stay", "next_visible", "next_unread", "none"} {
		if err = store.Set("inbox.after_read", value); err != nil {
			t.Errorf("Set(inbox.after_read, %q) error = %v", value, err)
		}
	}
}

func TestPreferenceStoreAcceptsUndoSendWindow(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"0", "5", "10", "20", "30"} {
		if err = store.Set("composer.undo_send_seconds", value); err != nil {
			t.Errorf("Set(composer.undo_send_seconds, %q) error = %v", value, err)
		}
	}
	if err = store.Set("composer.undo_send_seconds", "15"); err == nil {
		t.Fatal("Set(composer.undo_send_seconds, 15) unexpectedly succeeded")
	}
}

func TestPreferenceStoreValidatesScheduleDefaultDelays(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schedule.default_send_minutes", "schedule.default_reminder_minutes"} {
		for _, value := range []string{"1", "5", "60", "10080"} {
			if err = store.Set(key, value); err != nil {
				t.Errorf("Set(%s, %q) error = %v", key, value, err)
			}
		}
		for _, value := range []any{"0", "10081", "5,15", 5} {
			if err = store.Set(key, value); err == nil {
				t.Errorf("Set(%s, %v) unexpectedly succeeded", key, value)
			}
		}
	}
}

func TestPreferenceStoreAcceptsPointerCursorSetting(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Set("appearance.pointer_cursor", false); err != nil {
		t.Fatalf("Set(appearance.pointer_cursor, false) error = %v", err)
	}
}

func TestPreferenceStoreAcceptsAppZoom(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Set("appearance.zoom", "110"); err != nil {
		t.Fatalf("Set(appearance.zoom, 110) error = %v", err)
	}
}

func TestPreferenceStoreAcceptsAudioInputDevice(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Set("audio.input_device", "microphone-device-id"); err != nil {
		t.Fatalf("Set(audio.input_device) error = %v", err)
	}
}

func TestPreferenceStoreAcceptsNotificationAndReactionSettings(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	valid := []struct {
		key   string
		value any
	}{
		{key: "notifications.direct", value: true},
		{key: "notifications.mentions", value: false},
		{key: "notifications.group", value: true},
		{key: "notifications.otp_immediate", value: true},
		{key: "notifications.otp_copy", value: false},
		{key: "notifications.sound_direct", value: "Pulse"},
		{key: "notifications.sound_mention", value: "Silent"},
		{key: "notifications.sound_group", value: "Soft"},
		{key: "notifications.batch_seconds", value: "10"},
		{key: "notifications.repeat_minutes", value: "15"},
		{key: "sidebar.transparent", value: true},
		{key: "sidebar.network_icons", value: true},
		{key: "sidebar.pinned_style", value: "Star only"},
		{key: "reactions.quick_enabled", value: false},
		{key: "reactions.quick_source", value: "Custom"},
		{key: "reactions.custom_emojis", value: "\U0001F44D \U0001F389"},
		{key: "reactions.default_emoji", value: "\U0001F44D"},
		{key: "messages.double_click", value: "Reply"},
		{key: "window.start_minimized", value: true},
		{key: "window.quit_on_close", value: true},
		{key: "sounds.effects", value: "Sent messages"},
	}
	for _, item := range valid {
		if err = store.Set(item.key, item.value); err != nil {
			t.Errorf("Set(%s, %v) error = %v", item.key, item.value, err)
		}
	}
}

func TestPreferenceStoreRejectsInvalidNotificationAndReactionSettings(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := []struct {
		key   string
		value any
	}{
		{key: "notifications.batch_seconds", value: "7"},
		{key: "notifications.batch_seconds", value: 3},
		{key: "notifications.repeat_minutes", value: "1"},
		{key: "notifications.sound_direct", value: "Loud"},
		{key: "sidebar.pinned_style", value: "Bottom"},
		{key: "reactions.quick_source", value: "Random"},
		{key: "reactions.custom_emojis", value: ""},
		{key: "reactions.custom_emojis", value: "a b c d e f g"},
		{key: "reactions.default_emoji", value: ""},
		{key: "reactions.default_emoji", value: "a b"},
		{key: "messages.double_click", value: "Edit"},
		{key: "sounds.effects", value: "Always"},
	}
	for _, item := range invalid {
		if err = store.Set(item.key, item.value); err == nil {
			t.Errorf("Set(%s, %v) unexpectedly succeeded", item.key, item.value)
		}
	}
}

func TestPreferenceStoreRejectsInvalidWrites(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		key   string
		value any
	}{
		{key: "appearance.theme", value: "Neon"},
		{key: "appearance.zoom", value: "130"},
		{key: "appearance.compact", value: "yes"},
		{key: "room.favorite.", value: true},
		{key: "room.muted.!room:example.org", value: "yes"},
		{key: "history.initial_messages", value: "12"},
		{key: "schedule.suggestions_minutes", value: "5,5"},
		{key: "archive.next_room", value: "random"},
		{key: "archive.older_than_days", value: "14"},
		{key: "inbox.after_read", value: "random"},
		{key: "unknown.key", value: true},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			if err := store.Set(test.key, test.value); err == nil {
				t.Fatalf("Set(%q, %#v) succeeded", test.key, test.value)
			}
		})
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(store.path), filepath.Base(store.path))); !os.IsNotExist(err) {
		t.Fatalf("invalid writes created a preferences file, stat error = %v", err)
	}
}

func TestNewPreferenceStoreRejectsCorruptData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPreferenceStore(path); err == nil {
		t.Fatal("NewPreferenceStore() accepted corrupt JSON")
	}
}

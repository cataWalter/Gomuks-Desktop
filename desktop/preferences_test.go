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
	if err = store.Set("appearance.theme", "Light"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	reloaded, err := NewPreferenceStore(path)
	if err != nil {
		t.Fatalf("reload preferences: %v", err)
	}
	values := reloaded.All()
	if values["appearance.theme"] != "Light" || values["room.favorite.!room:example.org"] != true || values["future.preference"] != "preserved" {
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
		{key: "appearance.compact", value: "yes"},
		{key: "room.favorite.", value: true},
		{key: "room.muted.!room:example.org", value: "yes"},
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

package migration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPathsForUsesPlatformPreferenceDirectories(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "home")
	configDir := filepath.Join(t.TempDir(), "config")

	darwin := PathsFor("darwin", homeDir, configDir)
	wantDarwinPreferences := filepath.Join(homeDir, "Library", "Preferences", "fyne", legacyFyneAppID, preferencesFileName)
	if darwin.LegacyPreferencesFile != wantDarwinPreferences {
		t.Fatalf("macOS legacy preferences path = %q, want %q", darwin.LegacyPreferencesFile, wantDarwinPreferences)
	}

	linux := PathsFor("linux", homeDir, configDir)
	wantLinuxPreferences := filepath.Join(configDir, "fyne", legacyFyneAppID, preferencesFileName)
	if linux.LegacyPreferencesFile != wantLinuxPreferences {
		t.Fatalf("Linux legacy preferences path = %q, want %q", linux.LegacyPreferencesFile, wantLinuxPreferences)
	}
}

func TestMigrateLegacyCopiesDatabaseKeyAndPreferences(t *testing.T) {
	paths := testPaths(t)
	legacyDatabase := filepath.Join(paths.LegacyDataDir, databaseFileName)
	sourceDB := createMessageDatabase(t, legacyDatabase, "legacy message")
	defer sourceDB.Close()
	if _, err := os.Stat(legacyDatabase + "-wal"); err != nil {
		t.Fatalf("expected uncheckpointed legacy WAL: %v", err)
	}

	legacyKey := bytes.Repeat([]byte{0x42}, 32)
	if err := os.MkdirAll(paths.LegacyDataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.LegacyDataDir, cryptoKeyFileName), legacyKey, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyPreferences := []byte(`{"appearance.theme":"Dark","appearance.compact":true}`)
	if err := os.MkdirAll(filepath.Dir(paths.LegacyPreferencesFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LegacyPreferencesFile, legacyPreferences, 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := MigrateLegacy(paths)
	if err != nil {
		t.Fatalf("MigrateLegacy() error = %v", err)
	}
	if !report.DatabaseMigrated || !report.CryptoKeyMigrated || !report.PreferencesMigrated {
		t.Fatalf("migration report = %+v, expected all legacy data to migrate", report)
	}
	if report.PreferencesWarning != nil {
		t.Fatalf("preferences warning = %v", report.PreferencesWarning)
	}

	destinationDatabase := filepath.Join(paths.DataDir, databaseFileName)
	if got := readMessage(t, destinationDatabase); got != "legacy message" {
		t.Fatalf("migrated database message = %q, want legacy data", got)
	}
	if got := readMessage(t, legacyDatabase); got != "legacy message" {
		t.Fatalf("legacy database message = %q, source should remain intact", got)
	}
	if got, err := os.ReadFile(filepath.Join(paths.DataDir, cryptoKeyFileName)); err != nil || !bytes.Equal(got, legacyKey) {
		t.Fatalf("migrated crypto key = %x, error = %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(paths.LegacyDataDir, cryptoKeyFileName)); err != nil {
		t.Fatalf("legacy crypto key was not preserved: %v", err)
	}
	if _, err := os.Stat(paths.LegacyPreferencesFile); err != nil {
		t.Fatalf("legacy preferences were not preserved: %v", err)
	}
	var preferences map[string]json.RawMessage
	contents, err := os.ReadFile(paths.PreferencesFile)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(contents, &preferences); err != nil {
		t.Fatalf("decode migrated preferences: %v", err)
	}
	if string(preferences["appearance.theme"]) != `"Dark"` || string(preferences["appearance.compact"]) != "true" {
		t.Fatalf("migrated preferences = %s", contents)
	}

	report, err = MigrateLegacy(paths)
	if err != nil {
		t.Fatalf("second MigrateLegacy() error = %v", err)
	}
	if report.DatabaseMigrated || report.CryptoKeyMigrated || report.PreferencesMigrated {
		t.Fatalf("second migration should be a no-op, got %+v", report)
	}
}

func TestMigrateLegacyDoesNotOverwriteDestinationData(t *testing.T) {
	paths := testPaths(t)
	sourceDB := createMessageDatabase(t, filepath.Join(paths.LegacyDataDir, databaseFileName), "legacy message")
	if err := sourceDB.Close(); err != nil {
		t.Fatal(err)
	}
	destinationDB := createMessageDatabase(t, filepath.Join(paths.DataDir, databaseFileName), "destination message")
	if err := destinationDB.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(paths.LegacyDataDir, cryptoKeyFileName), bytes.Repeat([]byte{0x11}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	destinationKey := bytes.Repeat([]byte{0x22}, 32)
	if err := os.WriteFile(filepath.Join(paths.DataDir, cryptoKeyFileName), destinationKey, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.LegacyPreferencesFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LegacyPreferencesFile, []byte(`{"legacy":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	destinationPreferences := []byte(`{"existing":"keep"}`)
	if err := os.WriteFile(paths.PreferencesFile, destinationPreferences, 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := MigrateLegacy(paths)
	if err != nil {
		t.Fatalf("MigrateLegacy() error = %v", err)
	}
	if report.DatabaseMigrated || report.CryptoKeyMigrated || report.PreferencesMigrated {
		t.Fatalf("existing destination data should not be overwritten, got %+v", report)
	}
	if got := readMessage(t, filepath.Join(paths.DataDir, databaseFileName)); got != "destination message" {
		t.Fatalf("destination database message = %q, want existing data", got)
	}
	if got, err := os.ReadFile(filepath.Join(paths.DataDir, cryptoKeyFileName)); err != nil || !bytes.Equal(got, destinationKey) {
		t.Fatalf("destination crypto key = %x, error = %v", got, err)
	}
	if got, err := os.ReadFile(paths.PreferencesFile); err != nil || !bytes.Equal(got, destinationPreferences) {
		t.Fatalf("destination preferences = %s, error = %v", got, err)
	}
}

func TestMigrateLegacyCanRetryAfterDatabaseBackupFailure(t *testing.T) {
	paths := testPaths(t)
	legacyDatabase := filepath.Join(paths.LegacyDataDir, databaseFileName)
	if err := os.MkdirAll(paths.LegacyDataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyDatabase, []byte("not a SQLite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyKey := bytes.Repeat([]byte{0x33}, 32)
	if err := os.WriteFile(filepath.Join(paths.LegacyDataDir, cryptoKeyFileName), legacyKey, 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := MigrateLegacy(paths)
	if err == nil {
		t.Fatal("MigrateLegacy() succeeded with an invalid SQLite source")
	}
	if !report.CryptoKeyMigrated || report.DatabaseMigrated {
		t.Fatalf("failed migration report = %+v", report)
	}
	if exists, err := pathExists(filepath.Join(paths.DataDir, databaseFileName)); err != nil || exists {
		t.Fatalf("partial destination database exists = %t, error = %v", exists, err)
	}

	if err := os.Remove(legacyDatabase); err != nil {
		t.Fatal(err)
	}
	sourceDB := createMessageDatabase(t, legacyDatabase, "legacy message")
	if err := sourceDB.Close(); err != nil {
		t.Fatal(err)
	}
	report, err = MigrateLegacy(paths)
	if err != nil {
		t.Fatalf("retry MigrateLegacy() error = %v", err)
	}
	if !report.DatabaseMigrated || report.CryptoKeyMigrated {
		t.Fatalf("retry migration report = %+v", report)
	}
	if got := readMessage(t, filepath.Join(paths.DataDir, databaseFileName)); got != "legacy message" {
		t.Fatalf("retried database message = %q", got)
	}
}

func TestMigrateLegacyTreatsInvalidPreferencesAsWarning(t *testing.T) {
	paths := testPaths(t)
	if err := os.MkdirAll(filepath.Dir(paths.LegacyPreferencesFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LegacyPreferencesFile, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := MigrateLegacy(paths)
	if err != nil {
		t.Fatalf("preference error should not block startup migration: %v", err)
	}
	if report.PreferencesWarning == nil {
		t.Fatal("expected invalid legacy preferences to produce a warning")
	}
	if exists, err := pathExists(paths.PreferencesFile); err != nil || exists {
		t.Fatalf("invalid preferences destination exists = %t, error = %v", exists, err)
	}
}

func testPaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, dataDirectoryName)
	return Paths{
		DataDir:               dataDir,
		LegacyDataDir:         filepath.Join(root, legacyDataDirectoryName),
		PreferencesFile:       filepath.Join(dataDir, preferencesFileName),
		LegacyPreferencesFile: filepath.Join(root, "fyne", legacyFyneAppID, preferencesFileName),
	}
}

func createMessageDatabase(t *testing.T, path, body string) *sql.DB {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA wal_autocheckpoint=0",
		"CREATE TABLE messages (body TEXT NOT NULL)",
		"INSERT INTO messages (body) VALUES (?)",
	} {
		if statement == "INSERT INTO messages (body) VALUES (?)" {
			if _, err = db.Exec(statement, body); err != nil {
				db.Close()
				t.Fatal(err)
			}
		} else if _, err = db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	return db
}

func readMessage(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var body string
	if err = db.QueryRow("SELECT body FROM messages").Scan(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

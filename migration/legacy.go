package migration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
)

const (
	dataDirectoryName       = "gomuks-desktop"
	legacyDataDirectoryName = "fyne-matrix"
	legacyFyneAppID         = "fi.mau.fyne-matrix"
	databaseFileName        = "matrix.db"
	preferencesFileName     = "preferences.json"
	cryptoKeyFileName       = "crypto.key"
)

type Paths struct {
	DataDir               string
	LegacyDataDir         string
	PreferencesFile       string
	LegacyPreferencesFile string
}

type Report struct {
	DatabaseMigrated    bool
	CryptoKeyMigrated   bool
	PreferencesMigrated bool
	PreferencesWarning  error
}

func CurrentPaths() (Paths, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user config directory: %w", err)
	}

	homeDir := ""
	if runtime.GOOS == "darwin" {
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve user home directory: %w", err)
		}
	}
	return PathsFor(runtime.GOOS, homeDir, configDir), nil
}

func PathsFor(goos, homeDir, configDir string) Paths {
	dataDir := filepath.Join(configDir, dataDirectoryName)
	fyneConfigDir := filepath.Join(configDir, "fyne")
	if goos == "darwin" {
		fyneConfigDir = filepath.Join(homeDir, "Library", "Preferences", "fyne")
	}

	return Paths{
		DataDir:               dataDir,
		LegacyDataDir:         filepath.Join(configDir, legacyDataDirectoryName),
		PreferencesFile:       filepath.Join(dataDir, preferencesFileName),
		LegacyPreferencesFile: filepath.Join(fyneConfigDir, legacyFyneAppID, preferencesFileName),
	}
}

func MigrateLegacy(paths Paths) (Report, error) {
	var report Report
	if paths.DataDir == "" || paths.LegacyDataDir == "" {
		return report, errors.New("migration requires new and legacy data directories")
	}
	if filepath.Clean(paths.DataDir) == filepath.Clean(paths.LegacyDataDir) {
		return report, errors.New("new and legacy data directories must differ")
	}

	destinationDatabase := filepath.Join(paths.DataDir, databaseFileName)
	legacyDatabase := filepath.Join(paths.LegacyDataDir, databaseFileName)
	destinationExists, sidecarExists, err := databaseDestinationState(destinationDatabase)
	if err != nil {
		return report, err
	}
	if sidecarExists && !destinationExists {
		return report, fmt.Errorf("SQLite sidecar exists without destination database at %q", destinationDatabase)
	}
	if !destinationExists {
		legacyExists, err := regularFileExists(legacyDatabase)
		if err != nil {
			return report, fmt.Errorf("inspect legacy Matrix database: %w", err)
		}
		if legacyExists {
			keyMigrated, err := migrateCryptoKey(
				filepath.Join(paths.LegacyDataDir, cryptoKeyFileName),
				filepath.Join(paths.DataDir, cryptoKeyFileName),
			)
			if err != nil {
				return report, err
			}
			report.CryptoKeyMigrated = keyMigrated

			report.DatabaseMigrated, err = migrateSQLiteDatabase(legacyDatabase, destinationDatabase)
			if err != nil {
				return report, fmt.Errorf("migrate legacy Matrix database: %w", err)
			}
		}
	}

	if paths.PreferencesFile != "" && paths.LegacyPreferencesFile != "" {
		report.PreferencesMigrated, report.PreferencesWarning = migratePreferences(
			paths.LegacyPreferencesFile,
			paths.PreferencesFile,
		)
	}
	return report, nil
}

func migrateCryptoKey(source, destination string) (bool, error) {
	key, err := os.ReadFile(source)
	if errors.Is(err, fs.ErrNotExist) {
		destinationExists, destinationErr := pathExists(destination)
		if destinationErr != nil {
			return false, fmt.Errorf("inspect destination crypto key: %w", destinationErr)
		}
		if destinationExists {
			return false, errors.New("legacy Matrix database has no crypto key, but a destination key already exists")
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read legacy crypto key: %w", err)
	}
	if len(key) != 32 {
		return false, fmt.Errorf("legacy crypto key at %q has invalid length %d", source, len(key))
	}

	existingKey, err := os.ReadFile(destination)
	if err == nil {
		if !bytes.Equal(existingKey, key) {
			return false, errors.New("destination crypto key differs from the legacy key; refusing to migrate the database")
		}
		return false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("read destination crypto key: %w", err)
	}

	migrated, err := writeExclusive(destination, key, 0o600)
	if err != nil {
		return false, fmt.Errorf("migrate legacy crypto key: %w", err)
	}
	if !migrated {
		existingKey, err = os.ReadFile(destination)
		if err != nil {
			return false, fmt.Errorf("read concurrent destination crypto key: %w", err)
		}
		if !bytes.Equal(existingKey, key) {
			return false, errors.New("destination crypto key differs from the legacy key; refusing to migrate the database")
		}
	}
	return migrated, nil
}

func migrateSQLiteDatabase(source, destination string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return false, fmt.Errorf("create destination data directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".gomuks-migration-*.db")
	if err != nil {
		return false, fmt.Errorf("create temporary database: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Close(); err != nil {
		return false, fmt.Errorf("close temporary database: %w", err)
	}

	if err := backupSQLite(source, temporaryPath); err != nil {
		return false, err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return false, fmt.Errorf("secure migrated database: %w", err)
	}
	if err := syncFile(temporaryPath); err != nil {
		return false, fmt.Errorf("sync migrated database: %w", err)
	}

	linked, err := linkExclusive(temporaryPath, destination)
	if err != nil {
		return false, fmt.Errorf("install migrated database: %w", err)
	}
	if !linked {
		return false, nil
	}
	if err := syncDirectory(filepath.Dir(destination)); err != nil {
		return true, fmt.Errorf("sync destination data directory: %w", err)
	}
	return true, nil
}

func backupSQLite(sourcePath, destinationPath string) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sourceURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(sourcePath)}).String()
	sourceDB, err := sql.Open("sqlite3", sourceURI+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("open legacy database: %w", err)
	}
	defer func() {
		if closeErr := sourceDB.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	sourceDB.SetMaxOpenConns(1)
	if err = sourceDB.PingContext(ctx); err != nil {
		return fmt.Errorf("read legacy database: %w", err)
	}

	destinationDB, err := sql.Open("sqlite3", destinationPath)
	if err != nil {
		return fmt.Errorf("open temporary database: %w", err)
	}
	defer func() {
		if closeErr := destinationDB.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	destinationDB.SetMaxOpenConns(1)
	if err = destinationDB.PingContext(ctx); err != nil {
		return fmt.Errorf("initialize temporary database: %w", err)
	}

	sourceConn, err := sourceDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire legacy database connection: %w", err)
	}
	defer func() {
		if closeErr := sourceConn.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	destinationConn, err := destinationDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire temporary database connection: %w", err)
	}
	defer func() {
		if closeErr := destinationConn.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	err = destinationConn.Raw(func(destinationDriverConn any) error {
		destinationSQLite, ok := destinationDriverConn.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("unexpected destination SQLite connection type %T", destinationDriverConn)
		}
		return sourceConn.Raw(func(sourceDriverConn any) error {
			sourceSQLite, ok := sourceDriverConn.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("unexpected source SQLite connection type %T", sourceDriverConn)
			}
			return runSQLiteBackup(destinationSQLite, sourceSQLite)
		})
	})
	return err
}

func runSQLiteBackup(destination, source *sqlite3.SQLiteConn) (err error) {
	backup, err := destination.Backup("main", source, "main")
	if err != nil {
		return err
	}
	defer func() {
		if finishErr := backup.Finish(); err == nil && finishErr != nil {
			err = finishErr
		}
	}()

	deadline := time.Now().Add(30 * time.Second)
	for {
		done, stepErr := backup.Step(-1)
		if stepErr != nil {
			return stepErr
		}
		if done {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("SQLite online backup timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func migratePreferences(source, destination string) (bool, error) {
	destinationExists, err := pathExists(destination)
	if err != nil {
		return false, fmt.Errorf("inspect destination preferences: %w", err)
	}
	if destinationExists {
		return false, nil
	}
	contents, err := os.ReadFile(source)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read legacy preferences: %w", err)
	}
	var values map[string]json.RawMessage
	if err = json.Unmarshal(contents, &values); err != nil {
		return false, fmt.Errorf("decode legacy preferences: %w", err)
	}
	if values == nil {
		return false, errors.New("legacy preferences must contain a JSON object")
	}
	contents, err = json.Marshal(values)
	if err != nil {
		return false, fmt.Errorf("encode migrated preferences: %w", err)
	}
	return writeExclusive(destination, append(contents, '\n'), 0o600)
}

func writeExclusive(destination string, contents []byte, mode os.FileMode) (bool, error) {
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return false, fmt.Errorf("create destination directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".gomuks-migration-*")
	if err != nil {
		return false, fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(mode); err == nil {
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
		return false, fmt.Errorf("write temporary file: %w", err)
	}

	linked, err := linkExclusive(temporaryPath, destination)
	if err != nil || !linked {
		return linked, err
	}
	if err = syncDirectory(directory); err != nil {
		return true, err
	}
	return true, nil
}

func linkExclusive(source, destination string) (bool, error) {
	if err := os.Link(source, destination); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func databaseDestinationState(path string) (databaseExists, sidecarExists bool, err error) {
	databaseExists, err = pathExists(path)
	if err != nil || databaseExists {
		return databaseExists, false, err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		exists, err := pathExists(path + suffix)
		if err != nil {
			return false, false, err
		}
		sidecarExists = sidecarExists || exists
	}
	return databaseExists, sidecarExists, nil
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%q is not a regular file", path)
	}
	return true, nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func syncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	if err = directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	return directory.Close()
}

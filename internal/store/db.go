// Package store owns the SQLite database: connections, migrations and backups.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // CGo-free SQLite driver (ADR-0003)
)

//go:embed migrations/*.sql
var migrations embed.FS

// TimeFormat is the stored timestamp format. It sorts lexically.
const TimeFormat = "2006-01-02T15:04:05.000Z"

// Now returns the current UTC time in TimeFormat.
func Now() string { return FormatTime(time.Now()) }

// FormatTime renders t in TimeFormat.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeFormat) }

// DB holds a single-connection writer pool and a read-only reader pool.
type DB struct {
	W    *sql.DB
	R    *sql.DB
	Path string
}

// pragmas uses modernc's _pragma=name(value) syntax; mattn-style params are ignored.
const pragmas = "_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"

// Open opens (creating if needed) the database in dataDir and runs migrations.
func Open(ctx context.Context, dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, "blair.db")
	return OpenPath(ctx, path)
}

// OpenPath opens the database at an explicit path and runs migrations.
func OpenPath(ctx context.Context, path string) (*DB, error) {
	w, err := sql.Open("sqlite", "file:"+path+"?"+pragmas+"&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if err := w.PingContext(ctx); err != nil {
		w.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	_ = os.Chmod(path, 0o600)

	db := &DB{W: w, Path: path}
	if err := db.migrate(ctx); err != nil {
		w.Close()
		return nil, err
	}

	r, err := sql.Open("sqlite", "file:"+path+"?"+pragmas+"&_pragma=query_only(1)")
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(8)
	db.R = r
	return db, nil
}

func (db *DB) Close() error {
	return errors.Join(db.R.Close(), db.W.Close())
}

func (db *DB) migrate(ctx context.Context) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	current, err := goose.GetDBVersionContext(ctx, db.W)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	all, err := goose.CollectMigrations("migrations", 0, goose.MaxVersion)
	if err != nil {
		return err
	}
	last, _ := all.Last()
	if current > 0 && last != nil && last.Version > current {
		// Safety copy before changing an existing schema.
		dst := filepath.Join(filepath.Dir(db.Path), "backups", fmt.Sprintf("pre-migrate-v%d-%s.db", current, time.Now().UTC().Format("20060102T150405Z")))
		if err := db.BackupTo(ctx, dst); err != nil {
			return fmt.Errorf("pre-migration backup: %w", err)
		}
		slog.Info("pre-migration backup written", "path", dst)
	}
	if err := goose.UpContext(ctx, db.W, "migrations"); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// BackupTo writes a consistent copy of the database to dst with VACUUM INTO.
func (db *DB) BackupTo(ctx context.Context, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("backup target %s already exists", dst)
	}
	if _, err := db.W.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return err
	}
	return os.Chmod(dst, 0o600)
}

// ScheduledBackup writes backups/blair-<timestamp>.db and keeps the newest keep files.
func (db *DB) ScheduledBackup(ctx context.Context, keep int) (string, error) {
	dir := filepath.Join(filepath.Dir(db.Path), "backups")
	dst := filepath.Join(dir, "blair-"+time.Now().UTC().Format("20060102T150405Z")+".db")
	if err := db.BackupTo(ctx, dst); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dst, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "blair-") && strings.HasSuffix(e.Name(), ".db") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
	return dst, nil
}

// Tx runs fn in a write transaction.
func (db *DB) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

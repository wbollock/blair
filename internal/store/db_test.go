package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenEnablesWALAndForeignKeys(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var mode string
	if err := db.W.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal (DSN pragmas ignored?)", mode)
	}
	var fk int
	if err := db.R.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d on reader, want 1", fk)
	}
}

func TestReaderIsReadOnly(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.R.Exec("CREATE TABLE x (a INT)"); err == nil {
		t.Fatal("reader pool accepted a write")
	}
}

func TestDatabaseFilePermissions(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st, err := os.Stat(filepath.Join(dir, "blair.db"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("db perms = %v, want 0600", st.Mode().Perm())
	}
}

func TestScheduledBackupRetention(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dir := filepath.Join(filepath.Dir(db.Path), "backups")
	// Seed older backups with sortable names.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"blair-20200101T000000Z.db", "blair-20200102T000000Z.db", "blair-20200103T000000Z.db"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ScheduledBackup(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("kept %d backups, want 2", len(entries))
	}
	if entries[0].Name() != "blair-20200103T000000Z.db" {
		t.Fatalf("oldest kept = %s", entries[0].Name())
	}
}

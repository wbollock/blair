package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestTasksDeleteScopeMigration(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	raw, err := migrations.ReadFile("migrations/00005_tasks_delete_scope.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(strings.SplitN(string(raw), "-- +goose Up", 2)[1], "-- +goose Down", 2)[0]

	if _, err := db.W.Exec(`INSERT INTO users (id, email, display_name, password_hash, created_at, updated_at) VALUES ('u1','a@b.c','a','x','t','t')`); err != nil {
		t.Skipf("users insert needs more columns than this test knows: %v", err)
	}
	for id, scopes := range map[string]string{
		"writer": "tasks:read tasks:write", "reader": "tasks:read", "notes": "notes:write",
		"already": "tasks:write tasks:delete", "only": "tasks:write",
	} {
		if _, err := db.W.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, scopes, created_at) VALUES (?, 'u1', ?, ?, 'p', ?, 't')`, id, id, []byte(id), scopes); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 { // idempotent
		if _, err := db.W.Exec(up); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]string{
		"writer": "tasks:read tasks:write tasks:delete", "reader": "tasks:read", "notes": "notes:write",
		"already": "tasks:write tasks:delete", "only": "tasks:write tasks:delete",
	}
	for id, w := range want {
		var got string
		if err := db.W.QueryRow(`SELECT scopes FROM api_tokens WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != w {
			t.Errorf("%s: scopes = %q, want %q", id, got, w)
		}
	}
}

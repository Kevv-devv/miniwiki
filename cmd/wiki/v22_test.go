package main

import (
	"os"
	"path/filepath"
	"testing"

	"miniwiki/internal/storage"
)

func TestV22BackupWorksWhileWikiLockHeld(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "wiki.db")
	s, err := storage.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	unlock, err := acquireWikiLock(db)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	conf := filepath.Join(dir, "config.toml")
	uploads := filepath.Join(dir, "uploads")
	if err := os.MkdirAll(uploads, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf, []byte("upload_dir=\""+uploads+"\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(dir, "backup")
	if err := backup([]string{"-config", conf, "-db", db, "-out", snapshot}); err != nil {
		t.Fatalf("online backup blocked: %v", err)
	}
	if err := checkSQLite(filepath.Join(snapshot, "wiki.db")); err != nil {
		t.Fatal(err)
	}
}

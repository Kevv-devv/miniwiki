package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"miniwiki/internal/storage"
)

func TestLockRejectsConcurrentWriter(t *testing.T) {
	db := filepath.Join(t.TempDir(), "wiki.db")
	unlock, err := acquireWikiLock(db)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := acquireWikiLock(db); err == nil {
		second()
		t.Fatal("second server acquired same lock")
	}
	unlock()
	third, err := acquireWikiLock(db)
	if err != nil {
		t.Fatal(err)
	}
	third()
}
func TestOfflineWALSnapshotAndRestoreIncludesUploads(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "wiki.db")
	uploads := filepath.Join(dir, "uploads")
	if err := os.MkdirAll(uploads, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := storage.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser("editor", "password12345", "editor"); err != nil {
		t.Fatal(err)
	}
	id, _ := s.UserIDByName("editor")
	if _, err := s.SavePage("recent-wal", "Recent WAL", "committed changes", id, "", false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploads, "picture.png"), []byte("ORIGINAL"), 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "snapshot")
	// An open WAL connection is safe when the backup uses VACUUM INTO.
	if err := snapshotTo(db, uploads, dest); err != nil {
		t.Fatal(err)
	}
	snap, err := storage.Open(filepath.Join(dest, "wiki.db"))
	if err != nil {
		t.Fatal(err)
	}
	if p, err := snap.PageBySlug("recent-wal"); err != nil || p.Content != "committed changes" {
		t.Fatalf("snapshot missed WAL data: %v", err)
	}
	if err := snap.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "uploads", "picture.png"))
	if err != nil || string(data) != "ORIGINAL" {
		t.Fatal("upload missing from backup", err)
	}
	if err := os.WriteFile(filepath.Join(uploads, "picture.png"), []byte("CHANGED"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreSnapshot(db, uploads, dest); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(uploads, "picture.png"))
	if err != nil || string(data) != "ORIGINAL" {
		t.Fatal("upload was not restored", err)
	}
	restored, err := storage.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if p, err := restored.PageBySlug("recent-wal"); err != nil || p.Content != "committed changes" {
		t.Fatal("DB restore was incorrect", err)
	}
	// A restored session from an older backup is revoked on its v21 migration.
	if err := restored.CreateSession("temporary", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}

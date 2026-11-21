package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestV23RetentionCannotDeleteUnownedDirectories(t *testing.T) {
	dir := t.TempDir()
	names := []string{"wiki-notes", "wiki-20261119-010203.000000001", "wiki-20261119-010204.000000002", "wiki-20261119-010205.000000003"}
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(n, "wiki-2026") {
			os.WriteFile(filepath.Join(p, "wiki.db"), []byte("test"), 0600)
			os.WriteFile(filepath.Join(p, "README.txt"), []byte("test"), 0600)
		}
	}
	for _, n := range names[2:] {
		os.WriteFile(filepath.Join(dir, n, ".miniwiki-automatic"), []byte("MiniWiki v23 managed automatic snapshot\n"), 0600)
	}
	if err := rotateSnapshots(dir, 1); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{names[0], names[1], names[3]} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("deleted unowned/recent folder %q: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, names[2])); !os.IsNotExist(err) {
		t.Fatal("old managed snapshot not pruned")
	}
}
func TestV23BackupLocationGuard(t *testing.T) {
	base := t.TempDir()
	db := filepath.Join(base, "wiki.db")
	up := filepath.Join(base, "uploads")
	for _, target := range []string{base, filepath.Join(base, "uploads", "backups"), filepath.Dir(base)} {
		if err := validateAutomaticBackupDir(target, db, up); err == nil {
			t.Errorf("unsafe backup directory accepted: %s", target)
		}
	}
	if err := validateAutomaticBackupDir(filepath.Join(base, "backups", "automatic"), db, up); err != nil {
		t.Fatal(err)
	}
}
func TestV23ScheduledBackupDueAfterRestart(t *testing.T) {
	dir := t.TempDir()
	if !scheduledBackupOverdue(dir, time.Hour) {
		t.Fatal("missing snapshot should trigger backup")
	}
	name := "wiki-" + time.Now().Format("20060102-150405.000000000")
	p := filepath.Join(dir, name)
	os.Mkdir(p, 0700)
	os.WriteFile(filepath.Join(p, "wiki.db"), []byte("db"), 0600)
	os.WriteFile(filepath.Join(p, "README.txt"), []byte("readme"), 0600)
	os.WriteFile(filepath.Join(p, ".miniwiki-automatic"), []byte("MiniWiki v23 managed automatic snapshot\n"), 0600)
	if scheduledBackupOverdue(dir, time.Hour) {
		t.Fatal("recent backup incorrectly overdue")
	}
}

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestV24BackupSymlinkParentAllowedButDestinationRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	base := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(base, alias); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	db := filepath.Join(alias, "wiki.db")
	up := filepath.Join(alias, "uploads")
	target := filepath.Join(alias, "backups", "automatic")
	if err := validateAutomaticBackupDir(target, db, up); err != nil {
		t.Fatalf("symlinked ancestor is normal on BSD/macOS: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "backups"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "uploads"), filepath.Join(base, "backups", "automatic")); err != nil {
		t.Fatal(err)
	}
	if err := validateAutomaticBackupDir(target, db, up); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("direct destination symlink accepted: %v", err)
	}
}
func TestV24BackupStatusSeparatesSnapshotAndRetention(t *testing.T) {
	root := t.TempDir()
	recordBackupResults(root, nil, os.ErrPermission)
	raw, err := os.ReadFile(filepath.Join(root, ".miniwiki-backup-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	var status BackupStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		t.Fatal(err)
	}
	if status.OK || !status.SnapshotOK || status.RetentionOK || !strings.Contains(status.Message, "retention") {
		t.Fatalf("bad status: %+v", status)
	}
}

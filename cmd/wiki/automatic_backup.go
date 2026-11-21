package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// within reports whether child and parent are equal or the first is inside the second.
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))))
}

// canonicalPath resolves existing ancestor symlinks while allowing a backup
// destination which has not been created yet (e.g. backups/automatic).
func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	probe := abs
	var pending []string
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			for i := len(pending) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, pending[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", err
		}
		pending = append(pending, filepath.Base(probe))
		probe = parent
	}
}

func validateAutomaticBackupDir(dir, db, uploads string) error {
	// The deletion boundary may not itself be a symlink, but normal system
	// symlinked parents (/home on FreeBSD, /var on macOS) are permitted.
	targetAbs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	if fi, err := os.Lstat(targetAbs); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup destination must not be a symlink: %s", targetAbs)
		}
		if !fi.IsDir() {
			return fmt.Errorf("backup destination is not a directory: %s", targetAbs)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	target, err := canonicalPath(dir)
	if err != nil {
		return err
	}
	dbp, err := canonicalPath(db)
	if err != nil {
		return err
	}
	up, err := canonicalPath(uploads)
	if err != nil {
		return err
	}
	wd, err := canonicalPath(".")
	if err != nil {
		return err
	}
	if within(target, dbp) || within(target, up) || within(target, wd) || within(up, target) || target == filepath.Dir(dbp) {
		return errors.New("backup_dir must be a separate child directory, outside the database, uploads and working directories")
	}
	return nil
}

func validSnapshotMarker(path string) bool {
	fi, e := os.Lstat(filepath.Join(path, "wiki.db"))
	if e != nil || !fi.Mode().IsRegular() {
		return false
	}
	fi, e = os.Lstat(filepath.Join(path, "README.txt"))
	if e != nil || !fi.Mode().IsRegular() {
		return false
	}
	fi, e = os.Lstat(filepath.Join(path, ".miniwiki-automatic"))
	if e != nil || !fi.Mode().IsRegular() {
		return false
	}
	marker, e := os.ReadFile(filepath.Join(path, ".miniwiki-automatic"))
	return e == nil && string(marker) == "MiniWiki v23 managed automatic snapshot\n"
}
func newestManagedSnapshot(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && automaticSnapshotName.MatchString(e.Name()) && validSnapshotMarker(filepath.Join(dir, e.Name())) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return filepath.Join(dir, names[len(names)-1], "uploads")
}
func scheduledBackupOverdue(dir string, every time.Duration) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	newest := time.Time{}
	for _, e := range entries {
		if e.IsDir() && automaticSnapshotName.MatchString(e.Name()) && validSnapshotMarker(filepath.Join(dir, e.Name())) {
			stamp := strings.TrimPrefix(e.Name(), "wiki-")
			t, err := time.ParseInLocation("20060102-150405.000000000", stamp, time.Local)
			if err == nil && t.After(newest) {
				newest = t
			}
		}
	}
	return newest.IsZero() || time.Since(newest) >= every
}
func cleanupAbandonedSnapshots(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), ".miniwiki-backup-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if time.Since(info.ModTime()) > 24*time.Hour {
			marker, err := os.ReadFile(filepath.Join(dir, e.Name(), ".miniwiki-work"))
			if err != nil || string(marker) != "MiniWiki v23 temporary snapshot\n" {
				continue
			}
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

type BackupStatus struct {
	At          time.Time `json:"at"`
	OK          bool      `json:"ok"`
	Message     string    `json:"message"`
	SnapshotOK  bool      `json:"snapshot_ok"`
	RetentionOK bool      `json:"retention_ok"`
}

func recordBackupResults(root string, snapshotErr, retentionErr error) {
	status := BackupStatus{At: time.Now(), OK: snapshotErr == nil && retentionErr == nil, SnapshotOK: snapshotErr == nil, RetentionOK: retentionErr == nil, Message: "Snapshot and retention successful"}
	if snapshotErr != nil {
		status.Message = "Snapshot failed: " + snapshotErr.Error()
	}
	if snapshotErr == nil && retentionErr != nil {
		status.Message = "Snapshot saved, but retention failed: " + retentionErr.Error()
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return
	}
	bytes, err := json.Marshal(status)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(root, ".miniwiki-backup-status.json"), bytes, 0600)
}

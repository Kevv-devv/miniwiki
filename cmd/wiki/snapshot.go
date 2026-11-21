package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"miniwiki/internal/config"
	"miniwiki/internal/storage"
)

func copyFile(src, dst string) error {
	from, err := os.Open(src)
	if err != nil {
		return err
	}
	defer from.Close()
	st, err := from.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	to, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer to.Close()
	if _, err := io.Copy(to, from); err != nil {
		return err
	}
	if err := to.Sync(); err != nil {
		return err
	}
	// Preserve source mtime so subsequent immutable uploads can be hard-linked.
	return os.Chtimes(dst, st.ModTime(), st.ModTime())
}
func copyUploadTree(src, dst string) error { return copyUploadTreeWithPrevious(src, dst, "") }
func copyUploadTreeWithPrevious(src, dst, previous string) error {
	if err := os.MkdirAll(dst, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("upload directory is not a normal directory")
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(dst, relative)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not permitted in upload backups: %s", path)
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("not a regular upload file: %s", path)
		}
		if previous != "" {
			previousPath := filepath.Join(previous, relative)
			a, ea := os.Stat(path)
			b, eb := os.Stat(previousPath)
			if ea == nil && eb == nil && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) {
				// An uploaded file is immutable. Verify content, not merely file size,
				// before sharing immutable snapshot data via a hard link.
				if sameDigest(path, previousPath) {
					if err := os.Link(previousPath, target); err == nil {
						return nil
					}
				}
			}
		}
		return copyFile(path, target)
	})
}
func sameDigest(a, b string) bool {
	digest := func(path string) (string, error) {
		f, e := os.Open(path)
		if e != nil {
			return "", e
		}
		defer f.Close()
		h := sha256.New()
		if _, e = io.Copy(h, f); e != nil {
			return "", e
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	ha, e := digest(a)
	if e != nil {
		return false
	}
	hb, e := digest(b)
	return e == nil && ha == hb
}
func snapshotTo(dbPath, uploadDir, dest string) error {
	return makeSnapshot(dbPath, uploadDir, dest, "", false)
}
func snapshotToWithPrevious(dbPath, uploadDir, dest, previous string) error {
	return makeSnapshot(dbPath, uploadDir, dest, previous, true)
}
func makeSnapshot(dbPath, uploadDir, dest, previous string, automatic bool) error {
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("database does not exist: %w", err)
	}
	if _, err := os.Stat(dest); err == nil {
		return errors.New("backup destination already exists")
	}
	absDest, _ := filepath.Abs(dest)
	absUploads, _ := filepath.Abs(uploadDir)
	if absDest == absUploads || strings.HasPrefix(absDest, absUploads+string(os.PathSeparator)) {
		return errors.New("backups may not be stored inside the uploads directory")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".miniwiki-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if automatic {
		if err := os.WriteFile(filepath.Join(tmp, ".miniwiki-work"), []byte("MiniWiki v23 temporary snapshot\n"), 0600); err != nil {
			return err
		}
	}
	store, err := storage.Open(dbPath)
	if err != nil {
		return err
	}
	// SQLite makes a consistent image of the database, including committed WAL.
	_, vacuumErr := store.DB.Exec(`VACUUM INTO ?`, filepath.Join(tmp, "wiki.db"))
	closeErr := store.Close()
	if vacuumErr != nil {
		return fmt.Errorf("SQLite snapshot failed: %w", vacuumErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Chmod(filepath.Join(tmp, "wiki.db"), 0600); err != nil {
		return err
	}
	if err := copyUploadTreeWithPrevious(uploadDir, filepath.Join(tmp, "uploads"), previous); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, "README.txt"), []byte("MiniWiki online-safe snapshot: wiki.db plus uploads/. Restore only while MiniWiki is stopped, using miniwiki restore -from <this-directory>.\n"), 0600); err != nil {
		return err
	}
	if automatic {
		if err := os.WriteFile(filepath.Join(tmp, ".miniwiki-automatic"), []byte("MiniWiki v23 managed automatic snapshot\n"), 0600); err != nil {
			return err
		}
	}
	return os.Rename(tmp, dest)
}
func backup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	configPath := fs.String("config", "config.toml", "config path")
	dbPath := fs.String("db", "", "database path")
	out := fs.String("out", "", "destination DIRECTORY (SQLite + uploaded files)")
	_ = fs.Parse(args)
	cfg, err := config.LoadChecked(*configPath)
	if err != nil {
		return err
	}
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	}
	dest := *out
	if dest == "" {
		dest = filepath.Join("backups", "wiki-"+time.Now().Format("20060102-150405"))
	}
	// VACUUM INTO reads a consistent WAL snapshot while the server runs.
	// Uploads are immutable and are copied after the database snapshot.
	if err := snapshotTo(cfg.DBPath, cfg.UploadDir, dest); err != nil {
		return err
	}
	fmt.Println("Snapshot saved to", dest)
	return nil
}
func removeIfExists(p string) error {
	err := os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func checkSQLite(path string) error {
	s, err := storage.Open(path)
	if err != nil {
		return err
	}
	var answer string
	err = s.DB.QueryRow(`PRAGMA integrity_check`).Scan(&answer)
	closeErr := s.Close()
	if err != nil {
		return err
	}
	if answer != "ok" {
		return errors.New("backup database failed SQLite integrity_check")
	}
	return closeErr
}
func restoreSnapshot(dbPath, uploadDir, source string) error {
	sourceDB := filepath.Join(source, "wiki.db")
	if _, err := os.Stat(sourceDB); err != nil {
		return fmt.Errorf("restore requires a v21 snapshot directory containing wiki.db: %w", err)
	}
	if _, err := os.Stat(filepath.Join(source, "uploads")); err != nil {
		return fmt.Errorf("restore requires uploads/ in the snapshot: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return err
	}
	// Preserve a coherent pre-restore safety snapshot for rollback and audit.
	if _, err := os.Stat(dbPath); err == nil {
		before := dbPath + ".before-restore-" + time.Now().Format("20060102-150405.000000000")
		if err := snapshotTo(dbPath, uploadDir, before); err != nil {
			return fmt.Errorf("could not make pre-restore safety snapshot: %w", err)
		}
		fmt.Println("Current installation saved as", before)
	}
	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)
	newDB := dbPath + ".restoring-" + nonce
	newUploads := uploadDir + ".restoring-" + nonce
	defer os.Remove(newDB)
	defer os.RemoveAll(newUploads)
	if err := copyFile(sourceDB, newDB); err != nil {
		return err
	}
	if err := copyUploadTree(filepath.Join(source, "uploads"), newUploads); err != nil {
		return err
	}
	if err := checkSQLite(newDB); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := removeIfExists(newDB + suffix); err != nil {
			return err
		}
	}
	// Never replay a pre-restore WAL over the restored database.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := removeIfExists(dbPath + suffix); err != nil {
			return err
		}
	}
	oldDB := dbPath + ".old-" + nonce
	oldUploads := uploadDir + ".old-" + nonce
	hadDB, hadUploads := false, false
	if _, err := os.Stat(dbPath); err == nil {
		if err := os.Rename(dbPath, oldDB); err != nil {
			return err
		}
		hadDB = true
	}
	if _, err := os.Stat(uploadDir); err == nil {
		if err := os.Rename(uploadDir, oldUploads); err != nil {
			if hadDB {
				_ = os.Rename(oldDB, dbPath)
			}
			return err
		}
		hadUploads = true
	}
	if err := os.Rename(newDB, dbPath); err != nil {
		if hadUploads {
			_ = os.Rename(oldUploads, uploadDir)
		}
		if hadDB {
			_ = os.Rename(oldDB, dbPath)
		}
		return err
	}
	if err := os.Rename(newUploads, uploadDir); err != nil {
		_ = os.Remove(dbPath)
		if hadDB {
			_ = os.Rename(oldDB, dbPath)
		}
		if hadUploads {
			_ = os.Rename(oldUploads, uploadDir)
		}
		return err
	}
	if hadDB {
		_ = os.Remove(oldDB)
	}
	if hadUploads {
		_ = os.RemoveAll(oldUploads)
	}
	return nil
}
func restore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	configPath := fs.String("config", "config.toml", "config path")
	dbPath := fs.String("db", "", "database path")
	from := fs.String("from", "", "v21 snapshot directory")
	_ = fs.Parse(args)
	if *from == "" {
		return errors.New("restore requires -from SNAPSHOT_DIRECTORY")
	}
	cfg, err := config.LoadChecked(*configPath)
	if err != nil {
		return err
	}
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	}
	unlock, err := acquireWikiLock(cfg.DBPath)
	if err != nil {
		return err
	}
	defer unlock()
	if err := restoreSnapshot(cfg.DBPath, cfg.UploadDir, *from); err != nil {
		return err
	}
	fmt.Println("Restored database AND uploads from", *from)
	return nil
}

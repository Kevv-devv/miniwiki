package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"miniwiki/internal/assets"
	"miniwiki/internal/auth"
	"miniwiki/internal/config"
	"miniwiki/internal/handlers"
	"miniwiki/internal/storage"
)

var version = "0.24.3-beta"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "serve":
		if err := serve(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "backup":
		if err := backup(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "check":
		if err := checkDatabase(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "passwd":
		if err := resetLocalPassword(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "restore":
		if err := restore(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "version", "--version", "-v":
		fmt.Println("MiniWiki", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

// resetLocalPassword is an offline recovery path that does not depend on SMTP.
// The random temporary password is printed only to the local command line.
func resetLocalPassword(args []string) error {
	fs := flag.NewFlagSet("passwd", flag.ContinueOnError)
	configPath := fs.String("config", "config.toml", "configuration file")
	dbPath := fs.String("db", "", "database path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: miniwiki passwd [-config PATH] [-db PATH] USERNAME (stop the server first)")
	}
	cfg, err := config.LoadChecked(*configPath)
	if err != nil {
		return err
	}
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	}
	if _, err := os.Stat(cfg.DBPath); err != nil {
		return fmt.Errorf("existing wiki database is required: %w", err)
	}
	unlock, err := acquireWikiLock(cfg.DBPath)
	if err != nil {
		return err
	}
	defer unlock()
	s, err := storage.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer s.Close()
	// Password recovery must work even when a future migration is blocked.
	id, err := s.UserIDByName(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	password := base64.RawURLEncoding.EncodeToString(secret)
	if err := s.AdminResetPassword(id, password); err != nil {
		return err
	}
	fmt.Printf("One-time password for %s: %s\nSign in and change it immediately; all old sessions have been revoked.\n", fs.Arg(0), password)
	return nil
}

// Only snapshots created by MiniWiki's scheduled job may be rotated.
var automaticSnapshotName = regexp.MustCompile(`^wiki-\d{8}-\d{6}\.\d{9}$`)

func rotateSnapshots(dir string, keep int) error {
	if keep < 1 {
		return fmt.Errorf("keep_last must be positive")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || !automaticSnapshotName.MatchString(e.Name()) || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		candidate := filepath.Join(dir, e.Name())
		if !validSnapshotMarker(candidate) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for len(names) > keep {
		if err := os.RemoveAll(filepath.Join(dir, names[0])); err != nil {
			return err
		}
		names = names[1:]
	}
	return nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "config.toml", "path to config.toml")
	addr := fs.String("addr", "", "listen address, overriding config/env")
	dbPath := fs.String("db", "", "database path, overriding config/env")
	tlsCert := fs.String("tls-cert", "", "TLS certificate PEM path")
	tlsKey := fs.String("tls-key", "", "TLS private key PEM path")
	_ = fs.Parse(args)

	cfg, err := config.LoadChecked(*configPath)
	if err != nil {
		return err
	}
	if *addr != "" {
		cfg.Addr = *addr
	}
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	}
	if (*tlsCert == "") != (*tlsKey == "") {
		return errors.New("provide both -tls-cert and -tls-key")
	}
	if *tlsCert != "" {
		cfg.SecureCookies = true
	}
	auth.SetSecureCookies(cfg.SecureCookies)
	unlock, err := acquireWikiLock(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("cannot acquire wiki lock (a server may be running): %w", err)
	}
	defer unlock()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		return err
	}
	if err := store.Seed(); err != nil {
		return err
	}
	_ = store.CleanupExpiredSessions()
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := store.CleanupExpiredSessions(); err != nil {
					log.Printf("session cleanup failed: %v", err)
				}
			}
		}
	}()
	if cfg.SMTPHost != "" && strings.Contains(cfg.BaseURL, "localhost") {
		log.Printf("WARNING: SMTP is configured but base_url uses localhost; reset links will not work remotely")
	}
	// Validate paths before enabling unattended cleanup or deletion.
	autoDir := filepath.Join(cfg.BackupDir, "automatic")
	if cfg.BackupEvery != "" && cfg.BackupEvery != "0" {
		every, parseErr := time.ParseDuration(cfg.BackupEvery)
		if parseErr != nil || every < time.Minute {
			return fmt.Errorf("invalid backup_every %q (use e.g. 24h)", cfg.BackupEvery)
		}
		if err := validateAutomaticBackupDir(autoDir, cfg.DBPath, cfg.UploadDir); err != nil {
			return err
		}
		if err := os.MkdirAll(autoDir, 0700); err != nil {
			return err
		}
		// An overdue snapshot is created immediately even after frequent restarts.
		runScheduledBackup := func() {
			if err := cleanupAbandonedSnapshots(autoDir); err != nil {
				log.Printf("backup cleanup failed: %v", err)
			}
			dest := filepath.Join(autoDir, "wiki-"+time.Now().Format("20060102-150405.000000000"))
			snapshotErr := snapshotToWithPrevious(cfg.DBPath, cfg.UploadDir, dest, newestManagedSnapshot(autoDir))
			var retentionErr error
			if snapshotErr == nil {
				retentionErr = rotateSnapshots(autoDir, cfg.KeepLast)
			}
			recordBackupResults(cfg.BackupDir, snapshotErr, retentionErr)
			if snapshotErr != nil {
				log.Printf("scheduled backup failed: %v", snapshotErr)
			}
			if retentionErr != nil {
				log.Printf("scheduled backup saved a snapshot, but retention failed: %v", retentionErr)
			}
		}
		go func() {
			// Run overdue jobs asynchronously so a large upload archive does not
			// keep the HTTP server offline during startup.
			if scheduledBackupOverdue(autoDir, every) {
				runScheduledBackup()
			}
			tick := time.NewTicker(every)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					runScheduledBackup()
				}
			}
		}()
	}

	app, err := handlers.New(store, assets.Templates(), assets.Static(), cfg.SiteName, cfg.UploadDir, handlers.MailConfig{BaseURL: cfg.BaseURL, Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword, From: cfg.SMTPFrom, DevMode: cfg.DevMode, TrustedProxies: cfg.TrustedProxies, RequireLogin: cfg.RequireLogin, BackupDir: cfg.BackupDir})
	if err != nil {
		return err
	}

	log.Printf("MiniWiki %s", version)
	log.Printf("Database: %s", cfg.DBPath)
	log.Printf("Listening: %s (bind: %s)", displayListenURL(cfg.Addr, *tlsCert != ""), cfg.Addr)
	server := &http.Server{Addr: cfg.Addr, Handler: app.Routes(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	serverErrors := make(chan error, 1)
	go func() {
		if *tlsCert != "" {
			serverErrors <- server.ListenAndServeTLS(*tlsCert, *tlsKey)
		} else {
			serverErrors <- server.ListenAndServe()
		}
	}()
	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := server.Shutdown(shutdownCtx)
		<-serverErrors
		return err
	}
}

// displayListenURL returns a valid browser URL for an HTTP/TLS listener.
// Wildcard bind addresses are shown as localhost to provide a usable local URL.
// The real bind address is logged separately, since :8080 accepts external traffic.
func displayListenURL(addr string, tlsEnabled bool) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

func usage() {
	fmt.Println(`MiniWiki commands:
  miniwiki serve [-config config.toml] [-addr :8080] [-db wiki.db] [-tls-cert certificate.pem -tls-key privatekey.pem]
  miniwiki backup [-config config.toml] [-db wiki.db] [-out backups/wiki-TIMESTAMP]
  miniwiki restore [-config config.toml] [-db wiki.db] -from backups/wiki-TIMESTAMP
  miniwiki passwd [-config config.toml] [-db wiki.db] USERNAME  (offline recovery)
  miniwiki check [-config config.toml] [-db wiki.db] (read-only preflight audit)
  miniwiki version

Environment variables:
  MINIWIKI_ADDR=:8080
  MINIWIKI_DB=wiki.db
  MINIWIKI_SITE_NAME=MiniWiki
  MINIWIKI_REQUIRE_LOGIN=0|1
  MINIWIKI_ADMIN_PASSWORD=initial-password-once
  MINIWIKI_SECURE_COOKIES=0|1
  MINIWIKI_UPLOAD_DIR=uploads
  MINIWIKI_BASE_URL=http://localhost:8080
  MINIWIKI_SMTP_HOST=smtp.example.com
  MINIWIKI_SMTP_PORT=587
  MINIWIKI_SMTP_USERNAME=user
  MINIWIKI_SMTP_PASSWORD=secret
  MINIWIKI_SMTP_FROM=wiki@example.com
  MINIWIKI_DEV_MODE=0|1`)
}

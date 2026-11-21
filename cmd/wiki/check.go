package main

import (
	"database/sql"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
	"miniwiki/internal/config"
	"miniwiki/internal/slug"
	_ "modernc.org/sqlite"
)

// checkDatabase opens the existing database read-only and DOES NOT migrate it.
// Operators can use it before copying databases or restarting a wiki.
func checkDatabase(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	configPath := fs.String("config", "config.toml", "configuration file")
	dbPath := fs.String("db", "", "database path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.LoadChecked(*configPath)
	if err != nil {
		return err
	}
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	}
	abs, err := filepath.Abs(cfg.DBPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("wiki database required: %w", err)
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return err
	}
	issues := 0
	checkDuplicates := func(label, query string) error {
		rows, err := db.Query(query)
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		defer rows.Close()
		for rows.Next() {
			var val string
			var count int
			if err := rows.Scan(&val, &count); err != nil {
				return err
			}
			issues++
			fmt.Printf("CONFLICT: %s %q appears %d times\n", label, val, count)
		}
		return rows.Err()
	}
	checks := []struct{ label, query string }{
		{"case-insensitive username", `SELECT lower(username),COUNT(*) FROM users GROUP BY lower(username) HAVING COUNT(*)>1`},
		{"account email", `SELECT lower(email),COUNT(*) FROM users WHERE email<>'' GROUP BY lower(email) HAVING COUNT(*)>1`},
		{"active upload filename", `SELECT lower(filename),COUNT(*) FROM uploads WHERE deleted_at IS NULL GROUP BY lower(filename) HAVING COUNT(*)>1`},
	}
	for _, c := range checks {
		if err := checkDuplicates(c.label, c.query); err != nil {
			return err
		}
	}
	rows, err := db.Query(`SELECT id,slug,title,deleted_at IS NOT NULL FROM pages ORDER BY id`)
	if err != nil {
		return err
	}
	type candidate struct {
		id              int64
		old, new, title string
		deleted         bool
	}
	var pages []candidate
	for rows.Next() {
		var c candidate
		var title string
		if err := rows.Scan(&c.id, &c.old, &title, &c.deleted); err != nil {
			rows.Close()
			return err
		}
		c.title = title
		c.new = slug.Slugify(c.old)
		if c.old == legacyASCIIForCheck(title) || c.old == slug.LegacyV22(title) {
			c.new = slug.Slugify(title)
		}
		pages = append(pages, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	// Deleted pages remain recoverable but should not block active-title migrations.
	seen := map[string]candidate{}
	compatibleTitles := map[string]candidate{}
	for _, c := range pages {
		if c.deleted {
			continue
		}
		if prior, ok := seen[c.new]; ok {
			fmt.Printf("CONFLICT: live page slug %q (IDs %d and %d)\n", c.new, prior.id, c.id)
			issues++
		}
		seen[c.new] = c
		key := strings.ToLower(norm.NFKC.String(strings.TrimSpace(c.title)))
		if prior, found := compatibleTitles[key]; found && prior.id != c.id {
			fmt.Printf("CONFLICT: visually equivalent titles %q and %q (IDs %d and %d)\n", prior.title, c.title, prior.id, c.id)
			issues++
		} else {
			compatibleTitles[key] = c
		}
	}
	for _, c := range pages {
		if !c.deleted && c.old != c.new {
			fmt.Printf("MIGRATION: page ID %d %q => %q\n", c.id, c.old, c.new)
		}
	}
	if issues > 0 {
		return fmt.Errorf("%d conflicting group(s) require attention before migration", issues)
	}
	fmt.Println("Preflight passed: no detected username, email, upload-name, or active slug collisions. No changes were made.")
	return nil
}
func legacyASCIIForCheck(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == ':' {
			b.WriteRune(r)
		}
	}
	s = strings.Trim(b.String(), "-")
	if s == "" {
		return "untitled"
	}
	return s
}

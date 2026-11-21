package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/mail"
	"os"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
	"miniwiki/internal/slug"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Store struct{ DB *sql.DB }

type User struct {
	ID                    int64
	Username, Role, Email string
	Blocked               bool
	MustChangePassword    bool
}
type Page struct {
	ID                   int64
	Slug, Title, Content string
	Protected, Deleted   bool
	CreatedAt, UpdatedAt time.Time
	DeletedAt            sql.NullTime
	DeletedBy            sql.NullInt64
}
type Revision struct {
	ID, PageID                              int64
	Slug, Title, Content, Username, Summary string
	Minor                                   bool
	CreatedAt                               time.Time
}
type SearchResult struct {
	Slug, Title, Snippet string
	UpdatedAt            time.Time
}
type Category struct {
	ID         int64
	Slug, Name string
	PageCount  int
}

type Upload struct {
	ID                                int64
	Filename, StoredName, ContentType string
	Size                              int64
	Username                          string
	CreatedAt                         time.Time
	IsImage                           bool
	Protected, Deleted                bool
	DeletedAt                         sql.NullTime
}

type WatchItem struct {
	PageID      int64
	Slug, Title string
	UpdatedAt   time.Time
}

type Redirect struct {
	FromSlug, ToSlug string
	CreatedAt        time.Time
}

type AuditEvent struct {
	ID                                                       int64
	ActorID                                                  sql.NullInt64
	ActorUsername, EventType, TargetType, Target, Detail, IP string
	CreatedAt                                                time.Time
}

type Migration struct {
	ID        int64
	Name      string
	AppliedAt time.Time
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	pragmas := []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = NORMAL`,
		`PRAGMA busy_timeout = 5000`,
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	db.SetMaxOpenConns(1)
	return &Store{DB: db}, nil
}
func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) Migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (id INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
		`CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'editor', blocked INTEGER NOT NULL DEFAULT 0, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
		`CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, user_id INTEGER NOT NULL, expires_at DATETIME NOT NULL, FOREIGN KEY(user_id) REFERENCES users(id));`,
		`CREATE TABLE IF NOT EXISTS pages (id INTEGER PRIMARY KEY AUTOINCREMENT, slug TEXT NOT NULL UNIQUE, title TEXT NOT NULL, content TEXT NOT NULL, protected INTEGER NOT NULL DEFAULT 0, deleted_at DATETIME, deleted_by INTEGER, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
		`CREATE TABLE IF NOT EXISTS revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, page_id INTEGER NOT NULL, user_id INTEGER, title TEXT NOT NULL, content TEXT NOT NULL, summary TEXT NOT NULL DEFAULT '', minor INTEGER NOT NULL DEFAULT 0, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(page_id) REFERENCES pages(id), FOREIGN KEY(user_id) REFERENCES users(id));`,
		`CREATE INDEX IF NOT EXISTS idx_revisions_page_id ON revisions(page_id);`,
		`CREATE TABLE IF NOT EXISTS categories (id INTEGER PRIMARY KEY AUTOINCREMENT, slug TEXT NOT NULL UNIQUE, name TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS page_categories (page_id INTEGER NOT NULL, category_id INTEGER NOT NULL, PRIMARY KEY(page_id, category_id), FOREIGN KEY(page_id) REFERENCES pages(id) ON DELETE CASCADE, FOREIGN KEY(category_id) REFERENCES categories(id) ON DELETE CASCADE);`,
		`CREATE TABLE IF NOT EXISTS page_links (from_page_id INTEGER NOT NULL, to_slug TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY(from_page_id,to_slug), FOREIGN KEY(from_page_id) REFERENCES pages(id) ON DELETE CASCADE);`,
		`CREATE INDEX IF NOT EXISTS idx_page_links_to_slug ON page_links(to_slug);`,
		`CREATE TABLE IF NOT EXISTS redirects (from_slug TEXT PRIMARY KEY, to_slug TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
		`CREATE INDEX IF NOT EXISTS idx_redirects_to_slug ON redirects(to_slug);`,
		`CREATE TABLE IF NOT EXISTS uploads (id INTEGER PRIMARY KEY AUTOINCREMENT, filename TEXT NOT NULL, stored_name TEXT NOT NULL UNIQUE, content_type TEXT NOT NULL DEFAULT '', size INTEGER NOT NULL DEFAULT 0, uploaded_by INTEGER, protected INTEGER NOT NULL DEFAULT 0, deleted_at DATETIME, deleted_by INTEGER, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(uploaded_by) REFERENCES users(id));`,
		`CREATE TABLE IF NOT EXISTS watchlist (user_id INTEGER NOT NULL, page_id INTEGER NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY(user_id,page_id), FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE, FOREIGN KEY(page_id) REFERENCES pages(id) ON DELETE CASCADE);`,
		`CREATE TABLE IF NOT EXISTS password_reset_tokens (token TEXT PRIMARY KEY, user_id INTEGER NOT NULL, expires_at DATETIME NOT NULL, used_at DATETIME, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE);`,
		`CREATE TABLE IF NOT EXISTS audit_log (id INTEGER PRIMARY KEY AUTOINCREMENT, actor_id INTEGER, actor_username TEXT NOT NULL DEFAULT '', event_type TEXT NOT NULL, target_type TEXT NOT NULL DEFAULT '', target TEXT NOT NULL DEFAULT '', detail TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
	}
	for i, stmt := range stmts {
		if _, err := s.DB.Exec(stmt); err != nil {
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
	}
	// FTS5 is used when the local SQLite build supports it. If not, search falls back to LIKE.
	_, _ = s.DB.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(slug UNINDEXED, title, content);`)
	// Explicit, idempotent column upgrades rather than ignored ALTER failures.
	columns := map[string]map[string]string{
		"users":     {"role": "TEXT NOT NULL DEFAULT 'editor'", "email": "TEXT NOT NULL DEFAULT ''", "blocked": "INTEGER NOT NULL DEFAULT 0", "must_change_password": "INTEGER NOT NULL DEFAULT 0"},
		"pages":     {"protected": "INTEGER NOT NULL DEFAULT 0", "deleted_at": "DATETIME", "deleted_by": "INTEGER"},
		"uploads":   {"protected": "INTEGER NOT NULL DEFAULT 0", "deleted_at": "DATETIME", "deleted_by": "INTEGER"},
		"revisions": {"summary": "TEXT NOT NULL DEFAULT ''", "minor": "INTEGER NOT NULL DEFAULT 0"},
	}
	for table, fields := range columns {
		for name, definition := range fields {
			if err := s.ensureColumn(table, name, definition); err != nil {
				return err
			}
		}
	}
	// Repair legacy duplicate display names before enforcing immutable link targets.
	if err := s.deduplicateLegacyUploadNames(); err != nil {
		return err
	}
	// Active filename uniqueness prevents later uploads from replacing existing embeds.
	if _, err := s.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_uploads_filename_active ON uploads(lower(filename)) WHERE deleted_at IS NULL`); err != nil {
		return fmt.Errorf("duplicate active upload filenames found; rename duplicates before upgrading: %w", err)
	}
	// Legacy case-colliding usernames need resolution before adding a unique index.
	if _, err := s.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_ci ON users(lower(username))`); err != nil {
		return fmt.Errorf("case-insensitive duplicate usernames found; resolve them before upgrading: %w", err)
	}
	// Emails must identify one account, case-insensitively. Duplicate legacy
	// addresses must be resolved by an administrator before upgrading.
	if _, err := s.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_unique ON users(lower(email)) WHERE email<>''`); err != nil {
		return fmt.Errorf("duplicate account email addresses need resolving before migration: %w", err)
	}
	var migrated int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE id=21001`).Scan(&migrated); err != nil {
		return err
	}
	if migrated == 0 {
		if err := s.RebuildSearchIndex(); err != nil {
			return err
		}
		if err := s.RebuildPageLinks(); err != nil {
			return err
		}
		// Old sessions stored bearer tokens in plaintext. Revoke them once during
		// the upgrade, then persist only hashes for new sessions.
		if _, err := s.DB.Exec(`DELETE FROM sessions`); err != nil {
			return err
		}
		// Rotate the unsafe v20 default administrator credential on upgrade.
		var oldID int64
		var oldHash string
		if err := s.DB.QueryRow(`SELECT id,password_hash FROM users WHERE username='admin'`).Scan(&oldID, &oldHash); err == nil {
			if bcrypt.CompareHashAndPassword([]byte(oldHash), []byte("admin123")) == nil {
				secret := make([]byte, 24)
				if _, err := rand.Read(secret); err != nil {
					return err
				}
				password := base64.RawURLEncoding.EncodeToString(secret)
				h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
				if err != nil {
					return err
				}
				if _, err := s.DB.Exec(`UPDATE users SET password_hash=?,must_change_password=1 WHERE id=?`, string(h), oldID); err != nil {
					return err
				}
				log.Printf("SECURITY UPGRADE: default v20 administrator password rotated. New one-time password for admin: %s", password)
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		if _, err := s.DB.Exec(`INSERT INTO schema_migrations(id,name) VALUES(21001,'v21: private sessions, email index, one-time index rebuild')`); err != nil {
			return err
		}
	}
	if err := s.migrateV22(); err != nil {
		return err
	}
	if err := s.migrateV23(); err != nil {
		return err
	}
	if err := s.dedupeUnicodeUploadNames(); err != nil {
		return err
	}
	return nil
}

var legacyInvalidSlug = regexp.MustCompile(`[^a-z0-9\-:]+`)
var legacyDuplicateDashes = regexp.MustCompile(`-+`)

func legacyASCIISlugify(title string) string {
	v := strings.ToLower(strings.TrimSpace(title))
	v = strings.ReplaceAll(v, " ", "-")
	v = legacyInvalidSlug.ReplaceAllString(v, "")
	v = legacyDuplicateDashes.ReplaceAllString(v, "-")
	v = strings.Trim(v, "-")
	if v == "" {
		return "untitled"
	}
	return v
}
func (s *Store) migrateV22() error {
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE id=22001`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	rows, err := s.DB.Query(`SELECT id,slug,title,deleted_at IS NOT NULL FROM pages ORDER BY id`)
	if err != nil {
		return err
	}
	type named struct {
		id              int64
		old, new, title string
		deleted         bool
	}
	var pending []named
	for rows.Next() {
		var x named
		if err := rows.Scan(&x.id, &x.old, &x.title, &x.deleted); err != nil {
			rows.Close()
			return err
		}
		x.new = Slugify(x.old)
		// Older versions used ASCII-only slugs. If the stored slug was
		// generated from the title, recover the Unicode title spelling.
		// For deliberately custom slugs, preserve the user's chosen slug.
		if x.old == legacyASCIISlugify(x.title) {
			x.new = Slugify(x.title)
		}
		pending = append(pending, x)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	seen := map[string]int64{}
	// Active pages win. A deleted page must never block an upgrade.
	for _, p := range pending {
		if p.deleted {
			continue
		}
		if other, exists := seen[p.new]; exists && other != p.id {
			return fmt.Errorf("legacy active slug collision at %q (IDs %d and %d); run miniwiki check first", p.new, other, p.id)
		}
		seen[p.new] = p.id
	}
	for i := range pending {
		p := &pending[i]
		if !p.deleted {
			continue
		}
		if _, exists := seen[p.new]; exists {
			p.new = fmt.Sprintf("deleted-page-%d", p.id)
		}
		for {
			if _, exists := seen[p.new]; !exists {
				break
			}
			p.new += "-archived"
		}
		seen[p.new] = p.id
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Stage temporary slugs first so migrating case/spacing differences cannot conflict.
	for _, p := range pending {
		if p.new != p.old {
			if _, err := tx.Exec(`UPDATE pages SET slug=? WHERE id=?`, fmt.Sprintf("__v22_migrating_%d", p.id), p.id); err != nil {
				return err
			}
		}
	}
	for _, p := range pending {
		if p.new != p.old {
			if _, err := tx.Exec(`UPDATE pages SET slug=? WHERE id=?`, p.new, p.id); err != nil {
				return err
			}
			if !p.deleted {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO redirects(from_slug,to_slug) VALUES(?,?)`, p.old, p.new); err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE redirects SET to_slug=? WHERE to_slug=?`, p.new, p.old); err != nil {
					return err
				}
			}
		}
	}
	// Old reset links were plaintext at rest; invalidate them on the upgrade.
	if _, err := tx.Exec(`DELETE FROM password_reset_tokens`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Refresh search/backlink indexes only once for a changed schema.
	if err := s.RebuildSearchIndex(); err != nil {
		return err
	}
	if err := s.RebuildPageLinks(); err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO schema_migrations(id,name) VALUES(22001,'v22 Unicode slugs and hashed reset tokens')`)
	return err
}

// migrateV23 updates only punctuation-derived v22 slugs such as C++ and C#.
// It leaves custom slugs untouched and keeps redirects to old titles.
func (s *Store) migrateV23() error {
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE id=23001`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	rows, err := s.DB.Query(`SELECT id,slug,title,deleted_at IS NOT NULL FROM pages ORDER BY id`)
	if err != nil {
		return err
	}
	type row struct {
		id        int64
		old, next string
		deleted   bool
	}
	var pages []row
	for rows.Next() {
		var p row
		var title string
		if err := rows.Scan(&p.id, &p.old, &title, &p.deleted); err != nil {
			rows.Close()
			return err
		}
		p.next = p.old
		if p.old == slug.LegacyV22(title) {
			p.next = slug.Slugify(title)
		}
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	seen := make(map[string]int64)
	for _, p := range pages {
		if p.deleted {
			continue
		}
		if id, ok := seen[p.next]; ok {
			return fmt.Errorf("v23 slug collision %q (pages %d and %d)", p.next, id, p.id)
		}
		seen[p.next] = p.id
	}
	for i := range pages {
		if !pages[i].deleted {
			continue
		}
		if _, ok := seen[pages[i].next]; ok {
			pages[i].next = fmt.Sprintf("deleted-page-%d", pages[i].id)
		}
		seen[pages[i].next] = pages[i].id
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range pages {
		if p.old != p.next {
			if _, err := tx.Exec(`UPDATE pages SET slug=? WHERE id=?`, fmt.Sprintf("__v23_tmp_%d", p.id), p.id); err != nil {
				return err
			}
		}
	}
	for _, p := range pages {
		if p.old != p.next {
			if _, err := tx.Exec(`UPDATE pages SET slug=? WHERE id=?`, p.next, p.id); err != nil {
				return err
			}
			if !p.deleted {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO redirects(from_slug,to_slug) VALUES(?,?)`, p.old, p.next); err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE redirects SET to_slug=? WHERE to_slug=?`, p.next, p.old); err != nil {
					return err
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Migration is only marked complete after both derived indexes rebuild.
	// Rerunning these operations after an interrupted upgrade is safe.
	if err := s.RebuildPageLinks(); err != nil {
		return err
	}
	if err := s.RebuildSearchIndex(); err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT OR IGNORE INTO schema_migrations(id,name) VALUES(23001,'v23 punctuation-aware slugs')`)
	return err
}

// Only legacy duplicates are renamed, deterministically: protected uploads win;
// otherwise preserve the previously visible (newest) upload. No stored files move.
func (s *Store) deduplicateLegacyUploadNames() error {
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE id=22002`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	rows, err := s.DB.Query(`SELECT id,filename FROM uploads WHERE deleted_at IS NULL ORDER BY protected DESC,id DESC`)
	if err != nil {
		return err
	}
	type named struct {
		id   int64
		name string
	}
	var files []named
	used := map[string]bool{}
	for rows.Next() {
		var item named
		if err := rows.Scan(&item.id, &item.name); err != nil {
			rows.Close()
			return err
		}
		files = append(files, item)
		used[strings.ToLower(item.name)] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := map[string]bool{}
	for _, item := range files {
		key := strings.ToLower(item.name)
		if !seen[key] {
			seen[key] = true
			continue
		}
		ext := ""
		if dot := strings.LastIndex(item.name, "."); dot > 0 {
			ext = item.name[dot:]
		}
		base := strings.TrimSuffix(item.name, ext)
		proposal := fmt.Sprintf("%s-legacy-%d%s", base, item.id, ext)
		for used[strings.ToLower(proposal)] {
			proposal = fmt.Sprintf("%s-legacy-%d-%d%s", base, item.id, len(proposal), ext)
		}
		if _, err := tx.Exec(`UPDATE uploads SET filename=? WHERE id=?`, proposal, item.id); err != nil {
			return err
		}
		used[strings.ToLower(proposal)] = true
		log.Printf("v22 upload migration: renamed duplicate display name %q to %q (upload id %d)", item.name, proposal, item.id)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(id,name) VALUES(22002,'v22 duplicate image filename migration')`); err != nil {
		return err
	}
	return tx.Commit()
}

// Unicode filename comparisons must match Go across all platforms; SQLite lower()
// handles ASCII only. These methods use the single SQLite writer transaction.
func filenameEqual(a, b string) bool {
	return strings.EqualFold(norm.NFC.String(a), norm.NFC.String(b))
}

func activeFilenameTaken(tx *sql.Tx, name string, except int64) (bool, error) {
	rows, err := tx.Query(`SELECT id,filename FROM uploads WHERE deleted_at IS NULL`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var existing string
		if err := rows.Scan(&id, &existing); err != nil {
			return false, err
		}
		if id != except && filenameEqual(existing, name) {
			return true, nil
		}
	}
	return false, rows.Err()
}
func (s *Store) dedupeUnicodeUploadNames() error {
	var found int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE id=23002`).Scan(&found); err != nil {
		return err
	}
	if found != 0 {
		return nil
	}
	rows, err := s.DB.Query(`SELECT id,filename FROM uploads WHERE deleted_at IS NULL ORDER BY protected DESC,id DESC`)
	if err != nil {
		return err
	}
	type uploadName struct {
		id   int64
		name string
	}
	var candidates []uploadName
	for rows.Next() {
		var n uploadName
		if err := rows.Scan(&n.id, &n.name); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	names := []string{}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, x := range candidates {
		duplicate := false
		for _, name := range names {
			if filenameEqual(name, x.name) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			names = append(names, x.name)
			continue
		}
		ext := ""
		if dot := strings.LastIndex(x.name, "."); dot > 0 {
			ext = x.name[dot:]
		}
		base := strings.TrimSuffix(x.name, ext)
		proposed := fmt.Sprintf("%s-legacy-%d%s", base, x.id, ext)
		for {
			taken := false
			for _, n := range names {
				if filenameEqual(n, proposed) {
					taken = true
					break
				}
			}
			if !taken {
				break
			}
			proposed += "-copy"
		}
		if _, err := tx.Exec(`UPDATE uploads SET filename=? WHERE id=?`, proposed, x.id); err != nil {
			return err
		}
		names = append(names, proposed)
		log.Printf("v23 Unicode upload name normalization: renamed duplicate %q to %q", x.name, proposed)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(id,name) VALUES(23002,'v23 Unicode filename collision repair')`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ensureColumn(table, name, definition string) error {
	rows, err := s.DB.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var columnName, colType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &columnName, &colType, &notnull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if columnName == name {
			found = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || found {
		return err
	}
	_, err = s.DB.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + name + ` ` + definition)
	return err
}

func (s *Store) Seed() error {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		password := os.Getenv("MINIWIKI_ADMIN_PASSWORD")
		if password == "" {
			secret := make([]byte, 24)
			if _, err := rand.Read(secret); err != nil {
				return err
			}
			password = base64.RawURLEncoding.EncodeToString(secret)
		}
		if err := ValidatePassword(password); err != nil {
			return fmt.Errorf("MINIWIKI_ADMIN_PASSWORD: %w", err)
		}
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if _, err := s.DB.Exec(`INSERT INTO users(username,password_hash,role,must_change_password) VALUES(?,?,?,1)`, "admin", string(h), "admin"); err != nil {
			return err
		}
		log.Printf("FIRST RUN administrator username: admin ; temporary password: %s ; change it in Account settings after login", password)
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM pages WHERE slug='main-page'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		var uid int64
		if err := s.DB.QueryRow(`SELECT id FROM users ORDER BY CASE WHEN role='admin' THEN 0 ELSE 1 END, id LIMIT 1`).Scan(&uid); err != nil {
			return err
		}
		p, err := s.SavePage("main-page", "Main Page", "# Main Page\n\nWelcome to MiniWiki.\n\nTry creating [[Statistics]], [[Go Programming]], or [[Bayesian IRT|Bayesian item response theory]].\n", uid, "Seed main page", false)
		if err != nil {
			return err
		}
		return s.SetPageCategories(p.ID, nil)
	}
	return nil
}

func (s *Store) UserIDByName(username string) (int64, error) {
	var id int64
	err := s.DB.QueryRow(`SELECT id FROM users WHERE username=? COLLATE NOCASE`, username).Scan(&id)
	return id, err
}

// Use the same expensive bcrypt check for nonexistent and blocked accounts.
var dummyPasswordHash = func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("dummy_password_never_accepted"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return h
}()

func (s *Store) Authenticate(username, password string) (*User, error) {
	var u User
	var hash string
	err := s.DB.QueryRow(`SELECT id,username,role,email,blocked,must_change_password,password_hash FROM users WHERE username=? COLLATE NOCASE`, username).Scan(&u.ID, &u.Username, &u.Role, &u.Email, &u.Blocked, &u.MustChangePassword, &hash)
	if err != nil || u.Blocked {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		return nil, errors.New("invalid credentials")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, errors.New("invalid credentials")
	}
	return &u, nil
}

func sessionKey(token string) string {
	h := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(h[:])
}
func (s *Store) UserBySession(sessionID string) (*User, error) {
	var u User
	err := s.DB.QueryRow(`SELECT u.id,u.username,u.role,u.email,u.blocked,u.must_change_password FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id=? AND s.expires_at > CURRENT_TIMESTAMP AND u.blocked=0`, sessionKey(sessionID)).Scan(&u.ID, &u.Username, &u.Role, &u.Email, &u.Blocked, &u.MustChangePassword)
	if err != nil {
		return nil, err
	}
	return &u, nil
}
func (s *Store) CreateSession(id string, userID int64, expires time.Time) error {
	_, err := s.DB.Exec(`INSERT INTO sessions(id,user_id,expires_at) VALUES(?,?,?)`, sessionKey(id), userID, expires.UTC())
	return err
}
func (s *Store) DeleteSession(id string) {
	_, _ = s.DB.Exec(`DELETE FROM sessions WHERE id=?`, sessionKey(id))
}

func (s *Store) CleanupExpiredSessions() error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE expires_at < CURRENT_TIMESTAMP`)
	return err
}
func (s *Store) IsAdmin(u *User) bool { return u != nil && u.Role == "admin" }
func (s *Store) CanEdit(u *User, p *Page) bool {
	if u == nil {
		return false
	}
	if p != nil && p.Protected && u.Role != "admin" {
		return false
	}
	return u.Role == "admin" || u.Role == "editor"
}
func (s *Store) AllUsers() ([]User, error) {
	rows, err := s.DB.Query(`SELECT id,username,role,email,blocked FROM users ORDER BY lower(username)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.Email, &u.Blocked); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Store) CreateUser(username, password, role string) error {
	return s.CreateUserWithEmail(username, password, role, "")
}

// ValidatePassword centralizes password policy across admin, account and token resets.
// bcrypt refuses inputs exceeding 72 bytes; return a meaningful error first.
func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < 8 || strings.TrimSpace(password) == "" {
		return errors.New("password must be at least 8 characters")
	}
	if len([]byte(password)) > 72 {
		return errors.New("password must be no more than 72 UTF-8 bytes")
	}
	return nil
}

func ValidateUsername(username string) error {
	if len([]rune(username)) < 2 || len([]rune(username)) > 48 {
		return errors.New("username must be 2–48 characters")
	}
	if username != strings.TrimSpace(username) {
		return errors.New("username must not contain leading/trailing spaces")
	}
	for _, ch := range username {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return errors.New("username may use only ASCII letters, numbers, underscore and hyphen")
		}
	}
	return nil
}
func (s *Store) CreateUserWithEmail(username, password, role, email string) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}
	if role != "admin" && role != "editor" && role != "reader" {
		role = "editor"
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	email, err = normalizedEmail(email)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO users(username,password_hash,role,email,must_change_password) VALUES(?,?,?,?,1)`, username, string(h), role, email)
	return err
}
func (s *Store) ChangePassword(userID int64, password string) error {
	return s.setPassword(userID, password, false)
}
func (s *Store) AdminResetPassword(userID int64, password string) error {
	return s.setPassword(userID, password, true)
}
func (s *Store) setPassword(userID int64, password string, requireChange bool) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.Exec(`UPDATE users SET password_hash=?, must_change_password=? WHERE id=?`, string(h), requireChange, userID)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM password_reset_tokens WHERE user_id=?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) UpdateUserRole(userID int64, role string) error {
	if role != "admin" && role != "editor" && role != "reader" {
		return errors.New("invalid role")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	var blocked bool
	if err := tx.QueryRow(`SELECT role,blocked FROM users WHERE id=?`, userID).Scan(&current, &blocked); err != nil {
		return err
	}
	if current == "admin" && role != "admin" && !blocked {
		var count int
		if err := tx.QueryRow(`SELECT count(*) FROM users WHERE role='admin' AND blocked=0`).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return errors.New("cannot demote the last active administrator")
		}
	}
	if _, err := tx.Exec(`UPDATE users SET role=? WHERE id=?`, role, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetUserBlocked(userID int64, blocked bool) error {
	v := 0
	if blocked {
		v = 1
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if blocked {
		var role string
		var wasBlocked bool
		if err := tx.QueryRow(`SELECT role,blocked FROM users WHERE id=?`, userID).Scan(&role, &wasBlocked); err != nil {
			return err
		}
		if role == "admin" && !wasBlocked {
			var count int
			if err := tx.QueryRow(`SELECT count(*) FROM users WHERE role='admin' AND blocked=0`).Scan(&count); err != nil {
				return err
			}
			if count <= 1 {
				return errors.New("cannot block the last active administrator")
			}
		}
	}
	result, err := tx.Exec(`UPDATE users SET blocked=? WHERE id=?`, v, userID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	if blocked {
		if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) AuditForUser(username string, limit int) ([]AuditEvent, error) {
	rows, err := s.DB.Query(`SELECT id,actor_id,actor_username,event_type,target_type,target,detail,ip,created_at FROM audit_log WHERE actor_username=? OR target=? ORDER BY id DESC LIMIT ?`, username, username, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorUsername, &e.EventType, &e.TargetType, &e.Target, &e.Detail, &e.IP, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) PageExists(slug string) bool {
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM pages WHERE slug=? AND deleted_at IS NULL`, slug).Scan(&n)
	return n > 0
}
func (s *Store) PageBySlug(slug string) (*Page, error) {
	return s.pageBySlug(slug, false)
}
func (s *Store) PageBySlugAny(slug string) (*Page, error) {
	return s.pageBySlug(slug, true)
}
func (s *Store) pageBySlug(slug string, includeDeleted bool) (*Page, error) {
	var p Page
	var prot int
	q := `SELECT id,slug,title,content,protected,deleted_at,deleted_by,created_at,updated_at FROM pages WHERE slug=?`
	if !includeDeleted {
		q += ` AND deleted_at IS NULL`
	}
	err := s.DB.QueryRow(q, slug).Scan(&p.ID, &p.Slug, &p.Title, &p.Content, &prot, &p.DeletedAt, &p.DeletedBy, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.Protected = prot == 1
	p.Deleted = p.DeletedAt.Valid
	return &p, nil
}
func (s *Store) DeletePage(slug string, userID int64) error {
	_, err := s.DB.Exec(`UPDATE pages SET deleted_at=CURRENT_TIMESTAMP, deleted_by=?, updated_at=CURRENT_TIMESTAMP WHERE slug=? AND deleted_at IS NULL`, userID, slug)
	_, _ = s.DB.Exec(`DELETE FROM pages_fts WHERE slug=?`, slug)
	return err
}
func (s *Store) UndeletePage(slug string, userID int64, summary string) (*Page, error) {
	p, err := s.PageBySlugAny(slug)
	if err != nil {
		return nil, err
	}
	if !p.Deleted {
		return p, nil
	}
	if _, err := s.DB.Exec(`UPDATE pages SET deleted_at=NULL, deleted_by=NULL, updated_at=CURRENT_TIMESTAMP WHERE id=?`, p.ID); err != nil {
		return nil, err
	}
	if summary == "" {
		summary = "Undeleted page"
	}
	_, _ = s.DB.Exec(`INSERT INTO revisions(page_id,user_id,title,content,summary,minor) VALUES(?,?,?,?,?,0)`, p.ID, userID, p.Title, p.Content, summary)
	p, err = s.PageBySlug(slug)
	if err == nil {
		_ = s.UpsertSearch(*p)
	}
	return p, err
}
func (s *Store) DeletedPages() ([]Page, error) {
	rows, err := s.DB.Query(`SELECT id,slug,title,content,protected,deleted_at,deleted_by,created_at,updated_at FROM pages WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Page
	for rows.Next() {
		var p Page
		var prot int
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Content, &prot, &p.DeletedAt, &p.DeletedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Protected = prot == 1
		p.Deleted = true
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) RedirectTarget(fromSlug string) (string, error) {
	var to string
	err := s.DB.QueryRow(`SELECT to_slug FROM redirects WHERE from_slug=?`, fromSlug).Scan(&to)
	return to, err
}

// Compatibility-normalized titles (e.g., fullwidth Ａｄｍｉｎ vs Admin)
// must not impersonate an existing page. This does not solve every Unicode
// cross-script confusable, which requires a full UTS #39 skeleton table.
func rejectCompatibilityTitle(tx *sql.Tx, title, candidateSlug string) error {
	rows, err := tx.Query(`SELECT slug,title FROM pages WHERE deleted_at IS NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	normalized := strings.ToLower(norm.NFKC.String(strings.TrimSpace(title)))
	for rows.Next() {
		var slug, title string
		if err := rows.Scan(&slug, &title); err != nil {
			return err
		}
		if slug != candidateSlug && normalized == strings.ToLower(norm.NFKC.String(strings.TrimSpace(title))) {
			return ErrTitleConflict
		}
	}
	return rows.Err()
}

func (s *Store) RenamePage(oldSlug, newSlug, newTitle string, userID int64, summary string) (*Page, error) {
	oldSlug = strings.TrimSpace(oldSlug)
	newSlug = strings.TrimSpace(newSlug)
	newTitle = strings.TrimSpace(newTitle)
	if oldSlug == "" || newSlug == "" || newTitle == "" {
		return nil, errors.New("old slug, new slug, and title are required")
	}
	if oldSlug == newSlug {
		return s.SavePage(oldSlug, newTitle, mustPageContent(s, oldSlug), userID, summary, false)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	var content string
	if err := tx.QueryRow(`SELECT id,content FROM pages WHERE slug=?`, oldSlug).Scan(&id, &content); err != nil {
		return nil, err
	}
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pages WHERE slug=?`, newSlug).Scan(&exists); err != nil {
		return nil, err
	}
	if exists > 0 {
		return nil, errors.New("target page already exists")
	}
	if err := rejectCompatibilityTitle(tx, newTitle, oldSlug); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE pages SET slug=?, title=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, newSlug, newTitle, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO redirects(from_slug,to_slug) VALUES(?,?)`, oldSlug, newSlug); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM redirects WHERE from_slug=?`, newSlug); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE redirects SET to_slug=? WHERE to_slug=?`, newSlug, oldSlug); err != nil {
		return nil, err
	}
	if summary == "" {
		summary = fmt.Sprintf("Renamed page from %s to %s", oldSlug, newSlug)
	}
	if _, err := tx.Exec(`INSERT INTO revisions(page_id,user_id,title,content,summary,minor) VALUES(?,?,?,?,?,0)`, id, userID, newTitle, content, summary); err != nil {
		return nil, err
	}
	_, _ = tx.Exec(`DELETE FROM pages_fts WHERE slug=?`, oldSlug)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	p, err := s.PageBySlug(newSlug)
	if err == nil {
		_ = s.UpsertSearch(*p)
	}
	return p, err
}

func mustPageContent(s *Store, slug string) string {
	p, err := s.PageBySlug(slug)
	if err != nil {
		return ""
	}
	return p.Content
}

// ProtectPageAndFiles updates the page and all selected uploaded files atomically.
func (s *Store) ProtectPageAndFiles(pageSlug string, uploadIDs []int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE pages SET protected=1 WHERE slug=? AND deleted_at IS NULL`, pageSlug)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	for _, id := range uploadIDs {
		if _, err := tx.Exec(`UPDATE uploads SET protected=1 WHERE id=? AND deleted_at IS NULL`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetPageProtected(slug string, protected bool) error {
	v := 0
	if protected {
		v = 1
	}
	_, err := s.DB.Exec(`UPDATE pages SET protected=? WHERE slug=?`, v, slug)
	return err
}

var (
	ErrEditConflict  = errors.New("edit conflict: page changed since you began editing")
	ErrTitleConflict = errors.New("a page with a visually equivalent title already exists")
	ErrPageTooLarge  = errors.New("page content exceeds the 2 MiB limit")
	ErrPageDeleted   = errors.New("page is deleted; an admin must undelete it first")
)

const MaxPageBytes = 2 << 20

func (s *Store) SavePage(slug, title, content string, userID int64, summary string, minor bool) (*Page, error) {
	return s.SavePageWithBase(slug, title, content, userID, summary, minor, -1)
}
func (s *Store) LatestRevisionID(slug string) (int64, error) {
	var id int64
	err := s.DB.QueryRow(`SELECT COALESCE(MAX(r.id),0) FROM revisions r JOIN pages p ON p.id=r.page_id WHERE p.slug=? AND p.deleted_at IS NULL`, slug).Scan(&id)
	return id, err
}
func (s *Store) SavePageWithBase(slug, title, content string, userID int64, summary string, minor bool, baseRev int64) (*Page, error) {
	if len(content) > MaxPageBytes {
		return nil, ErrPageTooLarge
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	var deletedAt sql.NullTime
	var oldTitle string
	err = tx.QueryRow(`SELECT id,deleted_at,title FROM pages WHERE slug=?`, slug).Scan(&id, &deletedAt, &oldTitle)
	if errors.Is(err, sql.ErrNoRows) {
		if baseRev > 0 {
			return nil, ErrEditConflict
		}
		if err := rejectCompatibilityTitle(tx, title, slug); err != nil {
			return nil, err
		}
		res, err := tx.Exec(`INSERT INTO pages(slug,title,content) VALUES(?,?,?)`, slug, title, content)
		if err != nil {
			return nil, err
		}
		id, _ = res.LastInsertId()
	} else if err == nil {
		if deletedAt.Valid {
			return nil, ErrPageDeleted
		}
		if oldTitle != title {
			if err := rejectCompatibilityTitle(tx, title, slug); err != nil {
				return nil, err
			}
		}
		if baseRev >= 0 {
			var latest int64
			if err := tx.QueryRow(`SELECT COALESCE(MAX(id),0) FROM revisions WHERE page_id=?`, id).Scan(&latest); err != nil {
				return nil, err
			}
			if latest != baseRev {
				return nil, ErrEditConflict
			}
		}
		if _, err := tx.Exec(`UPDATE pages SET title=?, content=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, title, content, id); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	mi := 0
	if minor {
		mi = 1
	}
	if _, err := tx.Exec(`INSERT INTO revisions(page_id,user_id,title,content,summary,minor) VALUES(?,?,?,?,?,?)`, id, userID, title, content, summary, mi); err != nil {
		return nil, err
	}
	if err := replacePageLinksTx(tx, id, content); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	p, err := s.PageBySlug(slug)
	if err == nil {
		_ = s.UpsertSearch(*p)
	}
	return p, err
}
func (s *Store) RestoreRevision(revID, userID int64, summary string) (*Page, error) {
	r, err := s.Revision(revID)
	if err != nil {
		return nil, err
	}
	if summary == "" {
		summary = fmt.Sprintf("Restored revision #%d", revID)
	}
	return s.SavePage(r.Slug, r.Title, r.Content, userID, summary, false)
}

func (s *Store) Revision(id int64) (*Revision, error) {
	var r Revision
	err := s.DB.QueryRow(`SELECT r.id,r.page_id,p.slug,r.title,r.content,COALESCE(u.username,'anonymous'),r.summary,r.minor,r.created_at FROM revisions r JOIN pages p ON p.id=r.page_id LEFT JOIN users u ON u.id=r.user_id WHERE r.id=? AND p.deleted_at IS NULL`, id).Scan(&r.ID, &r.PageID, &r.Slug, &r.Title, &r.Content, &r.Username, &r.Summary, &r.Minor, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}
func (s *Store) Revisions(slug string) ([]Revision, error) {
	rows, err := s.DB.Query(`SELECT r.id,r.page_id,p.slug,r.title,'',COALESCE(u.username,'anonymous'),r.summary,r.minor,r.created_at FROM revisions r JOIN pages p ON p.id=r.page_id LEFT JOIN users u ON u.id=r.user_id WHERE p.slug=? AND p.deleted_at IS NULL ORDER BY r.id DESC`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRevisions(rows)
}
func (s *Store) PrevRevision(r Revision) (*Revision, error) {
	var p Revision
	err := s.DB.QueryRow(`SELECT r.id,r.page_id,pg.slug,r.title,r.content,COALESCE(u.username,'anonymous'),r.summary,r.minor,r.created_at FROM revisions r JOIN pages pg ON pg.id=r.page_id LEFT JOIN users u ON u.id=r.user_id WHERE r.page_id=? AND r.id < ? AND pg.deleted_at IS NULL ORDER BY r.id DESC LIMIT 1`, r.PageID, r.ID).Scan(&p.ID, &p.PageID, &p.Slug, &p.Title, &p.Content, &p.Username, &p.Summary, &p.Minor, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
func (s *Store) Recent(limit int) ([]Revision, error) {
	rows, err := s.DB.Query(`SELECT r.id,r.page_id,p.slug,r.title,'',COALESCE(u.username,'anonymous'),r.summary,r.minor,r.created_at FROM revisions r JOIN pages p ON p.id=r.page_id LEFT JOIN users u ON u.id=r.user_id WHERE p.deleted_at IS NULL ORDER BY r.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRevisions(rows)
}
func scanRevisions(rows *sql.Rows) ([]Revision, error) {
	var out []Revision
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.ID, &r.PageID, &r.Slug, &r.Title, &r.Content, &r.Username, &r.Summary, &r.Minor, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpsertSearch(p Page) error {
	_, _ = s.DB.Exec(`DELETE FROM pages_fts WHERE slug=?`, p.Slug)
	_, err := s.DB.Exec(`INSERT INTO pages_fts(slug,title,content) VALUES(?,?,?)`, p.Slug, p.Title, p.Content)
	return err
}
func (s *Store) RebuildSearchIndex() error {
	_, err := s.DB.Exec(`DELETE FROM pages_fts`)
	if err != nil {
		return nil
	}
	rows, err := s.DB.Query(`SELECT id,slug,title,content,protected,created_at,updated_at FROM pages WHERE deleted_at IS NULL`)
	if err != nil {
		return err
	}
	var pages []Page
	for rows.Next() {
		var p Page
		var prot int
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Content, &prot, &p.CreatedAt, &p.UpdatedAt); err != nil {
			_ = rows.Close()
			return err
		}
		p.Protected = prot == 1
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, p := range pages {
		_ = s.UpsertSearch(p)
	}
	return nil
}
func (s *Store) Search(q string) ([]SearchResult, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	terms := strings.Fields(q)
	if len(terms) > 16 {
		terms = terms[:16]
	}
	fts := make([]string, 0, len(terms))
	for i, term := range terms {
		if len(term) > 128 {
			term = term[:128]
		}
		term = strings.ReplaceAll(term, `"`, `""`)
		if term == "" {
			continue
		}
		fragment := `"` + term + `"`
		if i == len(terms)-1 {
			fragment += `*`
		}
		fts = append(fts, fragment)
	}
	rows, err := s.DB.Query(`SELECT p.slug,p.title,snippet(pages_fts,2,'<mark>','</mark>','...',16),p.updated_at FROM pages_fts JOIN pages p ON p.slug=pages_fts.slug WHERE p.deleted_at IS NULL AND pages_fts MATCH ? ORDER BY bm25(pages_fts) LIMIT 50`, strings.Join(fts, " AND "))
	if err != nil {
		like := "%" + q + "%"
		rows, err = s.DB.Query(`SELECT slug,title,substr(content,1,220),updated_at FROM pages WHERE deleted_at IS NULL AND (title LIKE ? OR content LIKE ?) ORDER BY updated_at DESC LIMIT 50`, like, like)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var x SearchResult
		if err := rows.Scan(&x.Slug, &x.Title, &x.Snippet, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) SetPageCategories(pageID int64, names []string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM page_categories WHERE page_id=?`, pageID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		slug := Slugify(name)
		if seen[slug] {
			continue
		}
		seen[slug] = true
		if _, err := tx.Exec(`INSERT OR IGNORE INTO categories(slug,name) VALUES(?,?)`, slug, name); err != nil {
			return err
		}
		var cid int64
		if err := tx.QueryRow(`SELECT id FROM categories WHERE slug=?`, slug).Scan(&cid); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO page_categories(page_id,category_id) VALUES(?,?)`, pageID, cid); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) CategoriesForPage(pageID int64) ([]Category, error) {
	rows, err := s.DB.Query(`SELECT c.id,c.slug,c.name FROM categories c JOIN page_categories pc ON pc.category_id=c.id WHERE pc.page_id=? ORDER BY c.name`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) AllCategories() ([]Category, error) {
	rows, err := s.DB.Query(`SELECT c.id,c.slug,c.name,COUNT(p.id) FROM categories c LEFT JOIN page_categories pc ON pc.category_id=c.id LEFT JOIN pages p ON p.id=pc.page_id AND p.deleted_at IS NULL GROUP BY c.id,c.slug,c.name HAVING COUNT(p.id)>0 ORDER BY lower(c.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name, &c.PageCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) PagesByCategory(slug string) (*Category, []Page, error) {
	var c Category
	if err := s.DB.QueryRow(`SELECT id,slug,name FROM categories WHERE slug=?`, slug).Scan(&c.ID, &c.Slug, &c.Name); err != nil {
		return nil, nil, err
	}
	rows, err := s.DB.Query(`SELECT p.id,p.slug,p.title,'',p.protected,p.created_at,p.updated_at FROM pages p JOIN page_categories pc ON pc.page_id=p.id WHERE pc.category_id=? AND p.deleted_at IS NULL ORDER BY lower(p.title)`, c.ID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	pages, err := scanPages(rows)
	if err != nil {
		return nil, nil, err
	}
	if len(pages) == 0 {
		return nil, nil, sql.ErrNoRows
	} // Hide categories known only from deleted pages.
	return &c, pages, nil
}
func (s *Store) AllPages() ([]Page, error) {
	rows, err := s.DB.Query(`SELECT id,slug,title,'',protected,created_at,updated_at FROM pages WHERE deleted_at IS NULL ORDER BY lower(title)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPages(rows)
}
func scanPages(rows *sql.Rows) ([]Page, error) {
	var out []Page
	for rows.Next() {
		var p Page
		var prot int
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Content, &prot, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Protected = prot == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) RebuildPageLinks() error {
	type pageContent struct {
		ID      int64
		Content string
	}
	rows, err := s.DB.Query(`SELECT id,content FROM pages WHERE deleted_at IS NULL`)
	if err != nil {
		return err
	}
	var pages []pageContent
	for rows.Next() {
		var p pageContent
		if err := rows.Scan(&p.ID, &p.Content); err != nil {
			_ = rows.Close()
			return err
		}
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM page_links`); err != nil {
		return err
	}
	for _, p := range pages {
		if err := replacePageLinksTx(tx, p.ID, p.Content); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Backlinks(slug string) ([]Page, error) {
	p, err := s.PageBySlug(slug)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(`SELECT p.id,p.slug,p.title,'',p.protected,p.created_at,p.updated_at
		FROM pages p
		JOIN page_links l ON l.from_page_id=p.id
		WHERE (l.to_slug=? OR l.to_slug IN (SELECT from_slug FROM redirects WHERE to_slug=?)) AND p.id<>? AND p.deleted_at IS NULL
		ORDER BY lower(p.title)`, slug, slug, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPages(rows)
}

func (s *Store) Audit(actor *User, eventType, targetType, target, detail, ip string) {
	var actorID any = nil
	actorName := ""
	if actor != nil {
		actorID = actor.ID
		actorName = actor.Username
	}
	_, _ = s.DB.Exec(`INSERT INTO audit_log(actor_id,actor_username,event_type,target_type,target,detail,ip) VALUES(?,?,?,?,?,?,?)`, actorID, actorName, eventType, targetType, target, detail, ip)
}

func (s *Store) RecentAudit(limit int) ([]AuditEvent, error) {
	rows, err := s.DB.Query(`SELECT id,actor_id,actor_username,event_type,target_type,target,detail,ip,created_at FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorUsername, &e.EventType, &e.TargetType, &e.Target, &e.Detail, &e.IP, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) UserByID(id int64) (*User, error) {
	var u User
	err := s.DB.QueryRow(`SELECT id,username,role,email,blocked FROM users WHERE id=?`, id).Scan(&u.ID, &u.Username, &u.Role, &u.Email, &u.Blocked)
	if err != nil {
		return nil, err
	}
	return &u, nil
}
func (s *Store) UserByEmail(email string) (*User, error) {
	var u User
	err := s.DB.QueryRow(`SELECT id,username,role,email,blocked FROM users WHERE lower(email)=lower(?) AND email<>'' AND blocked=0`, strings.TrimSpace(email)).Scan(&u.ID, &u.Username, &u.Role, &u.Email, &u.Blocked)
	if err != nil {
		return nil, err
	}
	return &u, nil
}
func normalizedEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", nil
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || strings.ContainsAny(email, "\r\n") {
		return "", errors.New("invalid email address")
	}
	return strings.ToLower(email), nil
}
func (s *Store) UpdateUserEmail(userID int64, email string) error {
	email, err := normalizedEmail(email)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE users SET email=? WHERE id=?`, email, userID)
	return err
}

// UpdateAccount atomically verifies the current password for email or password changes.
// Password changes revoke all active sessions in the same transaction.
func (s *Store) UpdateAccount(userID int64, email, currentPassword, newPassword string) (bool, error) {
	email, err := normalizedEmail(email)
	if err != nil {
		return false, err
	}
	if newPassword != "" {
		if err := ValidatePassword(newPassword); err != nil {
			return false, err
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var savedEmail, hash string
	if err := tx.QueryRow(`SELECT email,password_hash FROM users WHERE id=?`, userID).Scan(&savedEmail, &hash); err != nil {
		return false, err
	}
	changedEmail := email != savedEmail
	if !changedEmail && newPassword == "" {
		return false, nil
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPassword)) != nil {
		return false, errors.New("current password is incorrect")
	}
	if newPassword != "" {
		next, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			return false, err
		}
		if _, err := tx.Exec(`UPDATE users SET email=?,password_hash=?,must_change_password=0 WHERE id=?`, email, string(next), userID); err != nil {
			return false, err
		}
		if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
			return false, err
		}
		if _, err := tx.Exec(`DELETE FROM password_reset_tokens WHERE user_id=?`, userID); err != nil {
			return false, err
		}
	} else if _, err := tx.Exec(`UPDATE users SET email=? WHERE id=?`, email, userID); err != nil {
		return false, err
	}
	return newPassword != "", tx.Commit()
}

func (s *Store) CreatePasswordResetToken(token string, userID int64, expires time.Time) error {
	_, err := s.DB.Exec(`INSERT INTO password_reset_tokens(token,user_id,expires_at) VALUES(?,?,?)`, sessionKey(token), userID, expires.UTC())
	return err
}
func (s *Store) ConsumePasswordResetToken(token string, newPassword string) (*User, error) {
	if err := ValidatePassword(newPassword); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var userID int64
	err = tx.QueryRow(`SELECT user_id FROM password_reset_tokens WHERE token=? AND used_at IS NULL AND expires_at > CURRENT_TIMESTAMP`, sessionKey(token)).Scan(&userID)
	if err != nil {
		return nil, err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE users SET password_hash=?,must_change_password=0 WHERE id=?`, string(h), userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE password_reset_tokens SET used_at=CURRENT_TIMESTAMP WHERE token=?`, sessionKey(token)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM password_reset_tokens WHERE user_id=? AND token<>?`, userID, sessionKey(token)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.UserByID(userID)
}
func (s *Store) AddUpload(filename, storedName, contentType string, size int64, userID int64) error {
	if strings.TrimSpace(filename) == "" {
		return errors.New("filename required")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	taken, err := activeFilenameTaken(tx, filename, 0)
	if err != nil {
		return err
	}
	if taken {
		return errors.New("filename already exists")
	}
	if _, err = tx.Exec(`INSERT INTO uploads(filename,stored_name,content_type,size,uploaded_by) VALUES(?,?,?,?,?)`, filename, storedName, contentType, size, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Uploads(limit int) ([]Upload, error) {
	rows, err := s.DB.Query(`SELECT up.id,up.filename,up.stored_name,up.content_type,up.size,COALESCE(u.username,'anonymous'),up.created_at,up.protected,up.deleted_at FROM uploads up LEFT JOIN users u ON u.id=up.uploaded_by WHERE up.deleted_at IS NULL ORDER BY up.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUploads(rows)
}

func scanUploads(rows *sql.Rows) ([]Upload, error) {
	var out []Upload
	for rows.Next() {
		var x Upload
		var prot int
		if err := rows.Scan(&x.ID, &x.Filename, &x.StoredName, &x.ContentType, &x.Size, &x.Username, &x.CreatedAt, &prot, &x.DeletedAt); err != nil {
			return nil, err
		}
		x.IsImage = strings.HasPrefix(x.ContentType, "image/")
		x.Protected = prot == 1
		x.Deleted = x.DeletedAt.Valid
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) DeletedUploads() ([]Upload, error) {
	rows, err := s.DB.Query(`SELECT up.id,up.filename,up.stored_name,up.content_type,up.size,COALESCE(u.username,'anonymous'),up.created_at,up.protected,up.deleted_at FROM uploads up LEFT JOIN users u ON u.id=up.uploaded_by WHERE up.deleted_at IS NOT NULL ORDER BY up.deleted_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUploads(rows)
}

// UploadByStoredName is used for serving files. Unlike UploadByName, it cannot
// confuse a user-supplied display filename with another upload's stored key.
func (s *Store) UploadByStoredName(storedName string, includeDeleted bool) (*Upload, error) {
	q := `SELECT up.id,up.filename,up.stored_name,up.content_type,up.size,COALESCE(u.username,'anonymous'),up.created_at,up.protected,up.deleted_at FROM uploads up LEFT JOIN users u ON u.id=up.uploaded_by WHERE up.stored_name=?`
	if !includeDeleted {
		q += ` AND up.deleted_at IS NULL`
	}
	rows, err := s.DB.Query(q, storedName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ups, err := scanUploads(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ups) == 0 {
		return nil, sql.ErrNoRows
	}
	return &ups[0], nil
}

func (s *Store) UploadByName(name string, includeDeleted bool) (*Upload, error) {
	q := `SELECT up.id,up.filename,up.stored_name,up.content_type,up.size,COALESCE(u.username,'anonymous'),up.created_at,up.protected,up.deleted_at FROM uploads up LEFT JOIN users u ON u.id=up.uploaded_by WHERE (lower(up.filename)=lower(?) OR lower(up.stored_name)=lower(?))`
	if !includeDeleted {
		q += ` AND up.deleted_at IS NULL`
	}
	q += ` ORDER BY CASE WHEN lower(up.filename)=lower(?) THEN 0 ELSE 1 END,up.id DESC LIMIT 1`
	rows, err := s.DB.Query(q, name, name, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ups, err := scanUploads(rows)
	if err != nil {
		return nil, err
	}
	if len(ups) == 0 {
		// SQLite lower() is ASCII-only; look up Unicode names using Go case folding.
		q2 := `SELECT up.id,up.filename,up.stored_name,up.content_type,up.size,COALESCE(u.username,'anonymous'),up.created_at,up.protected,up.deleted_at FROM uploads up LEFT JOIN users u ON u.id=up.uploaded_by`
		if !includeDeleted {
			q2 += ` WHERE up.deleted_at IS NULL`
		}
		q2 += ` ORDER BY up.id DESC`
		candidates, err := s.DB.Query(q2)
		if err != nil {
			return nil, err
		}
		all, err := scanUploads(candidates)
		candidates.Close()
		if err != nil {
			return nil, err
		}
		for _, candidate := range all {
			if filenameEqual(candidate.Filename, name) {
				return &candidate, nil
			}
		}
		return nil, sql.ErrNoRows
	}
	return &ups[0], nil
}

func (s *Store) UploadByID(id int64, includeDeleted bool) (*Upload, error) {
	q := `SELECT up.id,up.filename,up.stored_name,up.content_type,up.size,COALESCE(u.username,'anonymous'),up.created_at,up.protected,up.deleted_at FROM uploads up LEFT JOIN users u ON u.id=up.uploaded_by WHERE up.id=?`
	if !includeDeleted {
		q += ` AND up.deleted_at IS NULL`
	}
	rows, err := s.DB.Query(q, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ups, err := scanUploads(rows)
	if err != nil {
		return nil, err
	}
	if len(ups) == 0 {
		return nil, sql.ErrNoRows
	}
	return &ups[0], nil
}

func (s *Store) RenameUpload(id int64, newName string, allowProtected bool) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("filename required")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	taken, err := activeFilenameTaken(tx, newName, id)
	if err != nil {
		return err
	}
	if taken {
		return errors.New("filename already exists")
	}
	query := `UPDATE uploads SET filename=? WHERE id=? AND deleted_at IS NULL AND protected=0`
	if allowProtected {
		query = `UPDATE uploads SET filename=? WHERE id=? AND deleted_at IS NULL`
	}
	result, err := tx.Exec(query, newName, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("protected/deleted file cannot be renamed")
	}
	return tx.Commit()
}

func (s *Store) SetUploadProtected(id int64, protected bool) error {
	v := 0
	if protected {
		v = 1
	}
	_, err := s.DB.Exec(`UPDATE uploads SET protected=? WHERE id=?`, v, id)
	return err
}
func (s *Store) DeleteUpload(id, userID int64) error {
	u, err := s.UploadByID(id, false)
	if err != nil {
		return err
	}
	if u.Protected {
		return errors.New("file is protected")
	}
	_, err = s.DB.Exec(`UPDATE uploads SET deleted_at=CURRENT_TIMESTAMP, deleted_by=? WHERE id=?`, userID, id)
	return err
}
func (s *Store) UndeleteUpload(id int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var name string
	if err := tx.QueryRow(`SELECT filename FROM uploads WHERE id=? AND deleted_at IS NOT NULL`, id).Scan(&name); err != nil {
		return err
	}
	taken, err := activeFilenameTaken(tx, name, id)
	if err != nil {
		return err
	}
	if taken {
		return errors.New("filename already exists")
	}
	if _, err := tx.Exec(`UPDATE uploads SET deleted_at=NULL,deleted_by=NULL WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetWatched(userID, pageID int64, watched bool) error {
	if watched {
		_, err := s.DB.Exec(`INSERT OR IGNORE INTO watchlist(user_id,page_id) VALUES(?,?)`, userID, pageID)
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM watchlist WHERE user_id=? AND page_id=?`, userID, pageID)
	return err
}
func (s *Store) IsWatched(userID, pageID int64) bool {
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM watchlist WHERE user_id=? AND page_id=?`, userID, pageID).Scan(&n)
	return n > 0
}
func (s *Store) Watchlist(userID int64) ([]WatchItem, error) {
	rows, err := s.DB.Query(`SELECT p.id,p.slug,p.title,p.updated_at FROM watchlist w JOIN pages p ON p.id=w.page_id WHERE w.user_id=? AND p.deleted_at IS NULL ORDER BY p.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchItem
	for rows.Next() {
		var x WatchItem
		if err := rows.Scan(&x.PageID, &x.Slug, &x.Title, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

var wikiLinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

func replacePageLinksTx(tx *sql.Tx, pageID int64, content string) error {
	if _, err := tx.Exec(`DELETE FROM page_links WHERE from_page_id=?`, pageID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, m := range wikiLinkRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		target := strings.TrimSpace(m[1])
		if target == "" || strings.HasPrefix(strings.ToLower(target), "category:") || strings.HasPrefix(strings.ToLower(target), "image:") || strings.HasPrefix(strings.ToLower(target), "file:") {
			continue
		}
		slug := Slugify(target)
		if seen[slug] {
			continue
		}
		seen[slug] = true
		if _, err := tx.Exec(`INSERT OR IGNORE INTO page_links(from_page_id,to_slug) VALUES(?,?)`, pageID, slug); err != nil {
			return err
		}
	}
	return nil
}

func Slugify(title string) string { return slug.Slugify(title) }

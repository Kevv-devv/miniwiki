package storage

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func v22TestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "wiki.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestV22MigrateLegacyRawSlugs(t *testing.T) {
	s := v22TestStore(t)
	_, err := s.DB.Exec(`INSERT INTO pages(slug,title,content) VALUES ('My Page','My Page','Legacy'),('日本語','日本語','Unicode'),('Gödel','Gödel','Math'),('erds','Erdős','Old ASCII'),('untitled','한국어','Old non-Latin')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`DELETE FROM schema_migrations WHERE id=22001`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	for slug, body := range map[string]string{"my-page": "Legacy", "日本語": "Unicode", "gödel": "Math"} {
		got, err := s.PageBySlug(slug)
		if err != nil || got.Content != body {
			t.Fatalf("migration %s: %v %+v", slug, err, got)
		}
	}
	if _, err := s.PageBySlug("My Page"); err != sql.ErrNoRows {
		t.Fatalf("old raw slug should no longer be a page: %v", err)
	}
}
func TestV22ProtectedImageAndUniqueActiveName(t *testing.T) {
	s := v22TestStore(t)
	if err := s.CreateUser("editor", "editorpassword12", "editor"); err != nil {
		t.Fatal(err)
	}
	id, _ := s.UserIDByName("editor")
	if err := s.AddUpload("logo.png", "original-logo.png", "image/png", 3, id); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUpload("logo.png", "replacement.png", "image/png", 3, id); err == nil {
		t.Fatal("duplicate active display name was accepted")
	}
	up, err := s.UploadByName("logo.png", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetUploadProtected(up.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameUpload(up.ID, "changed.png", false); err == nil {
		t.Fatal("editor renamed protected image")
	}
	if err := s.RenameUpload(up.ID, "changed.png", true); err != nil {
		t.Fatalf("admin could not rename protected image: %v", err)
	}
}
func TestV22LegacyDuplicateImageRepair(t *testing.T) {
	s := v22TestStore(t)
	if _, err := s.DB.Exec(`DROP INDEX idx_uploads_filename_active`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO uploads(filename,stored_name) VALUES ('logo.png','one.png'),('logo.png','two.png')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`DELETE FROM schema_migrations WHERE id=22002`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	u, err := s.UploadByName("logo.png", false)
	if err != nil {
		t.Fatal(err)
	}
	if u.StoredName != "two.png" {
		t.Fatalf("expected formerly visible file: %q", u.StoredName)
	}
}
func TestV22AccountEmailRequiresPasswordAndUpdatesAtomically(t *testing.T) {
	s := v22TestStore(t)
	if err := s.CreateUserWithEmail("account", "originalsecret", "reader", "old@example.test"); err != nil {
		t.Fatal(err)
	}
	id, _ := s.UserIDByName("account")
	if err := s.CreateSession("session-secret", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateAccount(id, "new@example.test", "wrong-password", ""); err == nil {
		t.Fatal("email changed without current password")
	}
	u, err := s.UserByID(id)
	if err != nil || u.Email != "old@example.test" {
		t.Fatalf("email partially changed: %+v %v", u, err)
	}
	if _, err := s.UpdateAccount(id, "new@example.test", "originalsecret", "short"); err == nil {
		t.Fatal("invalid password accepted")
	}
	u, _ = s.UserByID(id)
	if u.Email != "old@example.test" {
		t.Fatal("partial email changed after password failure")
	}
	if changed, err := s.UpdateAccount(id, "new@example.test", "originalsecret", ""); err != nil || changed {
		t.Fatalf("email update failed: %v", err)
	}
	if changed, err := s.UpdateAccount(id, "new@example.test", "originalsecret", "updatedsecret123"); err != nil || !changed {
		t.Fatalf("password update failed: %v", err)
	}
	if _, err := s.UserBySession("session-secret"); err == nil {
		t.Fatal("session survived password change")
	}
	if _, err := s.Authenticate("account", "updatedsecret123"); err != nil {
		t.Fatal(err)
	}
}
func TestV22ResetTokensAreHashedAndNewAccountsForceChange(t *testing.T) {
	s := v22TestStore(t)
	if err := s.CreateUser("fresh", "validpassword123", "reader"); err != nil {
		t.Fatal(err)
	}
	u, err := s.Authenticate("fresh", "validpassword123")
	if err != nil || !u.MustChangePassword {
		t.Fatalf("new account not forced to change password: %+v %v", u, err)
	}
	token := strings.Repeat("a", 64)
	if err := s.CreatePasswordResetToken(token, u.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := s.DB.QueryRow(`SELECT token FROM password_reset_tokens WHERE user_id=?`, u.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == token || !strings.HasPrefix(stored, "sha256:") {
		t.Fatalf("reset token stored in plaintext: %q", stored)
	}
	if _, err := s.ConsumePasswordResetToken(token, "anotherpassword123"); err != nil {
		t.Fatal(err)
	}
}

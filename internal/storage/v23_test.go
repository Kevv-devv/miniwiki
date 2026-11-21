package storage

import (
	"testing"
)

func TestV23UnicodeUploadNamesCannotHijack(t *testing.T) {
	s := v22TestStore(t)
	if err := s.CreateUser("uploader", "password-value-123", "editor"); err != nil {
		t.Fatal(err)
	}
	id, err := s.UserIDByName("uploader")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddUpload("École.png", "file-1.png", "image/png", 3, id); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUpload("école.png", "file-2.png", "image/png", 3, id); err == nil {
		t.Fatal("Unicode collision allowed")
	}
	if err := s.AddUpload("Other.png", "file-3.png", "image/png", 3, id); err != nil {
		t.Fatal(err)
	}
	up, err := s.UploadByName("OTHER.PNG", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RenameUpload(up.ID, "école.png", false); err == nil {
		t.Fatal("rename hijacked existing image")
	}
	first, err := s.UploadByName("éCOLE.PNG", false)
	if err != nil {
		t.Fatal(err)
	}
	if first.StoredName != "file-1.png" {
		t.Fatalf("wrong Unicode image selected: %q", first.StoredName)
	}
	if err := s.DeleteUpload(first.ID, id); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUpload("école.png", "file-4.png", "image/png", 3, id); err != nil {
		t.Fatal(err)
	}
	if err := s.UndeleteUpload(first.ID); err == nil {
		t.Fatal("undelete reintroduced Unicode collision")
	}
}
func TestV23CaseInsensitiveUsernameLogin(t *testing.T) {
	s := v22TestStore(t)
	if err := s.CreateUser("Alice", "password-value-123", "editor"); err != nil {
		t.Fatal(err)
	}
	u, err := s.Authenticate("aLiCe", "password-value-123")
	if err != nil || u.Username != "Alice" {
		t.Fatalf("case insensitive login: %+v %v", u, err)
	}
	id, err := s.UserIDByName("ALICE")
	if err != nil || id != u.ID {
		t.Fatalf("case insensitive lookup: %v %d", err, id)
	}
}
func TestV23PunctuationSlugMigration(t *testing.T) {
	s := v22TestStore(t)
	if _, err := s.DB.Exec(`DELETE FROM schema_migrations WHERE id=23001`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO pages(slug,title,content) VALUES('c','C++','C++ guide')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	page, err := s.PageBySlug("c-plus-plus")
	if err != nil || page.Title != "C++" {
		t.Fatalf("punctuation migration failed: %+v %v", page, err)
	}
	target, err := s.RedirectTarget("c")
	if err != nil || target != "c-plus-plus" {
		t.Fatalf("redirect failed: %s %v", target, err)
	}
}

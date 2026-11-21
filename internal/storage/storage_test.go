package storage

import (
	"errors"
	"testing"
	"time"
)

func TestMigrateSeedAndPermissions(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINIWIKI_ADMIN_PASSWORD", "admin12345")
	if err := s.Seed(); err != nil {
		t.Fatal(err)
	}
	u, err := s.Authenticate("admin", "admin12345")
	if err != nil {
		t.Fatal(err)
	}
	if !s.IsAdmin(u) {
		t.Fatal("seeded admin should have admin role")
	}
	p, err := s.PageBySlug("main-page")
	if err != nil {
		t.Fatal(err)
	}
	if !s.CanEdit(u, p) {
		t.Fatal("admin should edit main page")
	}
}

func TestWatchlist(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINIWIKI_ADMIN_PASSWORD", "admin12345")
	if err := s.Seed(); err != nil {
		t.Fatal(err)
	}
	u, _ := s.Authenticate("admin", "admin12345")
	p, _ := s.PageBySlug("main-page")
	if err := s.SetWatched(u.ID, p.ID, true); err != nil {
		t.Fatal(err)
	}
	if !s.IsWatched(u.ID, p.ID) {
		t.Fatal("page should be watched")
	}
	items, err := s.Watchlist(u.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("watchlist got %d, err=%v", len(items), err)
	}
}

func TestPasswordPolicyForAdminCreatedAndResetUsers(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUserWithEmail("short", "x", "reader", ""); err == nil {
		t.Fatal("accepted short password")
	}
	if err := s.CreateUserWithEmail("long", string(make([]byte, 73)), "reader", ""); err == nil {
		t.Fatal("accepted >72-byte bcrypt password")
	}
	if err := s.CreateUserWithEmail("reader", "initialpassword", "reader", ""); err != nil {
		t.Fatal(err)
	}
	id, err := s.UserIDByName("reader")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePassword(id, "123"); err == nil {
		t.Fatal("accepted short admin reset")
	}
	if err := s.ChangePassword(id, "updatedpassword"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate("reader", "initialpassword"); err == nil {
		t.Fatal("old password still works")
	}
	if _, err := s.Authenticate("reader", "updatedpassword"); err != nil {
		t.Fatal(err)
	}
}

func TestFTSLiteralSearchAndUploadedStoredName(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINIWIKI_ADMIN_PASSWORD", "admin12345")
	if err := s.Seed(); err != nil {
		t.Fatal(err)
	}
	admin, _ := s.Authenticate("admin", "admin12345")
	if _, err := s.SavePage("search-operators", "AND OR NOT", "Expressions can use AND OR NOT operators", admin.ID, "", false); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"OR", "AND", "NOT", `"unclosed`, "NEAR(x,y)"} {
		if _, err := s.Search(query); err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
	}
	results, err := s.Search("AND OR NOT")
	if err != nil || len(results) == 0 {
		t.Fatalf("literal FTS phrase not found: count %d err %v", len(results), err)
	}
	if err := s.AddUpload("original.png", "123-original.png", "image/png", 10, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUpload("123-original.png", "456-second.png", "image/png", 11, admin.ID); err != nil {
		t.Fatal(err)
	}
	up, err := s.UploadByStoredName("123-original.png", false)
	if err != nil {
		t.Fatal(err)
	}
	if up.Filename != "original.png" {
		t.Fatalf("stored lookup picked an unrelated filename: %q", up.Filename)
	}
	if err := s.DeleteUpload(up.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadByStoredName("123-original.png", false); err == nil {
		t.Fatal("deleted stored file should not be public")
	}
	if deleted, err := s.UploadByStoredName("123-original.png", true); err != nil || !deleted.Deleted {
		t.Fatalf("admin recovery lookup failed: %v", err)
	}
}

func TestBlockClearsSessionsAndPreventsLogin(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser("member", "validpassword", "editor"); err != nil {
		t.Fatal(err)
	}
	id, _ := s.UserIDByName("member")
	if err := s.CreateSession("a-session", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserBlocked(id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserBySession("a-session"); err == nil {
		t.Fatal("blocked session is still valid")
	}
	if _, err := s.Authenticate("member", "validpassword"); err == nil {
		t.Fatal("blocked user can log in")
	}
	if err := s.SetUserBlocked(id, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserBySession("a-session"); err == nil {
		t.Fatal("old session should remain revoked")
	}
	if _, err := s.Authenticate("member", "validpassword"); err != nil {
		t.Fatal(err)
	}
}

func TestDeletedPagesDoNotLeakThroughRevisionAPIs(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser("author", "password12345", "editor"); err != nil {
		t.Fatal(err)
	}
	author, _ := s.UserIDByName("author")
	p, err := s.SavePage("secret", "Secret", "private secret content", author, "", false)
	if err != nil {
		t.Fatal(err)
	}
	r1, _ := s.LatestRevisionID(p.Slug)
	_, err = s.SavePage("secret", "Secret", "second secret", author, "", false)
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := s.LatestRevisionID(p.Slug)
	if err := s.DeletePage("secret", author); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revision(r1); err == nil {
		t.Fatal("deleted revision exposed")
	}
	if _, err := s.Revision(r2); err == nil {
		t.Fatal("deleted revision 2 exposed")
	}
	if revs, err := s.Revisions("secret"); err != nil || len(revs) != 0 {
		t.Fatalf("deleted history exposed: %v %d", err, len(revs))
	}
	if recent, err := s.Recent(100); err != nil {
		t.Fatal(err)
	} else {
		for _, r := range recent {
			if r.Slug == "secret" {
				t.Fatal("deleted page in recent changes")
			}
		}
	}
	if _, err := s.PrevRevision(Revision{ID: r2, PageID: p.ID}); err == nil {
		t.Fatal("deleted predecessor exposed")
	}
	if _, err := s.UndeletePage("secret", author, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revision(r1); err != nil {
		t.Fatal("undelete did not restore revision visibility", err)
	}
}
func TestChangeAndTokenResetRevokeSessions(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser("tester", "firstpassword12", "reader"); err != nil {
		t.Fatal(err)
	}
	id, _ := s.UserIDByName("tester")
	if err := s.CreateSession("token-1", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := s.DB.QueryRow(`SELECT id FROM sessions`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "token-1" {
		t.Fatal("bearer token stored in plaintext")
	}
	if err := s.ChangePassword(id, "nextpassword12"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserBySession("token-1"); err == nil {
		t.Fatal("session survived password change")
	}
	if err := s.CreateSession("token-2", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreatePasswordResetToken("reset-token", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumePasswordResetToken("reset-token", "thirdpassword12"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserBySession("token-2"); err == nil {
		t.Fatal("session survived token reset")
	}
	if err := s.AdminResetPassword(id, "fourthpassword12"); err != nil {
		t.Fatal(err)
	}
	user, err := s.Authenticate("tester", "fourthpassword12")
	if err != nil {
		t.Fatal(err)
	}
	if !user.MustChangePassword {
		t.Fatal("admin-set password didn't force change")
	}
}
func TestSeedNeverRestoresFixedAdmin(t *testing.T) {
	t.Setenv("MINIWIKI_ADMIN_PASSWORD", "superrandompassword123")
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := s.Seed(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate("admin", "admin123"); err == nil {
		t.Fatal("known default password accepted")
	}
	if _, err := s.Authenticate("admin", "superrandompassword123"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser("maintainer", "validpassword12", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE users SET username='former-admin' WHERE username='admin'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Seed(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserIDByName("admin"); err == nil {
		t.Fatal("removed admin was silently recreated")
	}
}
func TestEditConflictAndBacklinksAfterRename(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser("editor", "password12345", "editor"); err != nil {
		t.Fatal(err)
	}
	id, _ := s.UserIDByName("editor")
	p, _ := s.SavePage("target-old", "Target Old", "original", id, "", false)
	base, _ := s.LatestRevisionID(p.Slug)
	if _, err := s.SavePageWithBase(p.Slug, p.Title, "new content", id, "", false, base); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SavePageWithBase(p.Slug, p.Title, "stale content", id, "", false, base); !errors.Is(err, ErrEditConflict) {
		t.Fatalf("stale editor not rejected: %v", err)
	}
	s.SavePage("reference", "Reference", "See [[Target Old]]", id, "", false)
	if _, err := s.RenamePage("target-old", "target-new", "Target New", id, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RebuildPageLinks(); err != nil {
		t.Fatal(err)
	}
	backlinks, err := s.Backlinks("target-new")
	if err != nil {
		t.Fatal(err)
	}
	if len(backlinks) != 1 || backlinks[0].Slug != "reference" {
		t.Fatalf("backlinks lost across rename: %v", backlinks)
	}
}
func TestRoleGuardsAndDuplicateEmails(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUserWithEmail("boss", "password12345", "admin", "boss@example.com"); err != nil {
		t.Fatal(err)
	}
	id, _ := s.UserIDByName("boss")
	if err := s.UpdateUserRole(id, "editor"); err == nil {
		t.Fatal("last admin demoted")
	}
	if err := s.SetUserBlocked(id, true); err == nil {
		t.Fatal("last admin blocked")
	}
	if err := s.CreateUserWithEmail("duplicated", "password12345", "reader", "BOSS@example.com"); err == nil {
		t.Fatal("duplicate case-insensitive email accepted")
	}
	if err := s.CreateUserWithEmail("bad", "password12345", "reader", "bad\r\nemail@example.com"); err == nil {
		t.Fatal("invalid email accepted")
	}
}

package handlers

import (
	"miniwiki/internal/assets"
	"miniwiki/internal/auth"
	"miniwiki/internal/slug"
	"miniwiki/internal/storage"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestV24DownloadGatePerClientLimit(t *testing.T) {
	a := &App{downloadSlots: make(chan struct{}, downloadSlotsCapacity), downloadByIP: map[string]int{}}
	var release []func()
	for i := 0; i < downloadPerIPLimit; i++ {
		f, ok := a.acquireDownload("203.0.113.1")
		if !ok {
			t.Fatalf("request %d denied", i)
		}
		release = append(release, f)
	}
	if _, ok := a.acquireDownload("203.0.113.1"); ok {
		t.Fatal("download above per-IP ceiling was accepted")
	}
	other, ok := a.acquireDownload("203.0.113.2")
	if !ok {
		t.Fatal("different IP denied when total slots available")
	}
	for _, f := range release {
		f()
	}
	other()
	if len(a.downloadSlots) != 0 || len(a.downloadByIP) != 0 {
		t.Fatalf("download limit leaked state: %d %v", len(a.downloadSlots), a.downloadByIP)
	}
	if f, ok := a.acquireDownload("203.0.113.1"); !ok {
		t.Fatal("client never released")
	} else {
		f()
	}
}
func TestV24EditErrorsKeepUserDraft(t *testing.T) {
	s, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser("writer", "password12345", "editor"); err != nil {
		t.Fatal(err)
	}
	uid, err := s.UserIDByName("writer")
	if err != nil {
		t.Fatal(err)
	}
	// Newly created accounts are forced to change their initial password.
	// Complete that step before testing an authenticated editor session.
	if err := s.ChangePassword(uid, "password12345"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SavePage("admin", "Admin", "existing", uid, "", false); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("writer-session", uid, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	app, err := New(s, assets.Templates(), assets.Static(), "MiniWiki", t.TempDir(), MailConfig{})
	if err != nil {
		t.Fatal(err)
	}
	post := func(pageSlug, title, content string) *httptest.ResponseRecorder {
		v := url.Values{"title": {title}, "content": {content}, "summary": {"my summary"}, "base_revision_id": {"0"}, "csrf_token": {"token"}}
		req := httptest.NewRequest(http.MethodPost, "/wiki/"+url.PathEscape(pageSlug)+"/edit", strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "writer-session"})
		req.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: "token"})
		rec := httptest.NewRecorder()
		app.Routes().ServeHTTP(rec, req)
		return rec
	}
	got := post(slug.Slugify("Ａdmin"), "Ａdmin", "precious text draft")
	if got.Code != http.StatusConflict || !strings.Contains(got.Body.String(), "precious text draft") || !strings.Contains(got.Body.String(), "visually equivalent") {
		t.Fatalf("title conflict lost draft: %d: %s", got.Code, got.Body.String())
	}
	long := "beginning-of-large-draft-" + strings.Repeat("x", storage.MaxPageBytes)
	got = post("large-test", "Large test", long)
	if got.Code != http.StatusRequestEntityTooLarge || !strings.Contains(got.Body.String(), "beginning-of-large-draft-") {
		t.Fatalf("oversize draft not preserved: %d: %.400s", got.Code, got.Body.String())
	}
	if _, err := s.SavePage("disappearing", "Disappearing", "before deletion", uid, "", false); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePage("disappearing", uid); err != nil {
		t.Fatal(err)
	}
	got = post("disappearing", "Disappearing", "draft-after-delete")
	if got.Code != http.StatusConflict || !strings.Contains(got.Body.String(), "draft-after-delete") {
		t.Fatalf("deleted-page draft lost: %d: %.600s", got.Code, got.Body.String())
	}
}

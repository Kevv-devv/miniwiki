package handlers

import (
	"fmt"
	"miniwiki/internal/assets"
	"miniwiki/internal/auth"
	"miniwiki/internal/storage"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestV241DownloadGateBalancesNormalBrowserAndAbuse(t *testing.T) {
	a := &App{downloadSlots: make(chan struct{}, downloadSlotsCapacity), downloadByIP: make(map[string]int)}
	var releases []func()
	for i := 0; i < downloadPerIPLimit; i++ {
		release, ok := a.acquireDownload("192.0.2.1")
		if !ok {
			t.Fatalf("normal client request %d of %d was denied", i+1, downloadPerIPLimit)
		}
		releases = append(releases, release)
	}
	if release, ok := a.acquireDownload("192.0.2.1"); ok {
		release()
		t.Fatal("exceeded per-IP ceiling")
	}
	// The second client should still have space even after the first fills
	// its quota; fill all remaining slots with distinct clients.
	for i := downloadPerIPLimit; i < downloadSlotsCapacity; i++ {
		release, ok := a.acquireDownload(fmt.Sprintf("198.51.100.%d", i-downloadPerIPLimit+1))
		if !ok {
			t.Fatalf("failed to use global transfer slot %d", i)
		}
		releases = append(releases, release)
	}
	if release, ok := a.acquireDownload("203.0.113.5"); ok {
		release()
		t.Fatal("exceeded global transfer ceiling")
	}
	for _, release := range releases {
		release()
	}
	if len(a.downloadSlots) != 0 || len(a.downloadByIP) != 0 {
		t.Fatalf("transfer counter leaked after releases: %d, %v", len(a.downloadSlots), a.downloadByIP)
	}
}

func TestV241ExcessDownloadsReturn429WithRetryAfter(t *testing.T) {
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
	id, err := s.UserIDByName("writer")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	name := "100-test.png"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("not a real png"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUpload("test.png", name, "image/png", int64(len("not a real png")), id); err != nil {
		t.Fatal(err)
	}
	app, err := New(s, assets.Templates(), assets.Static(), "MiniWiki", dir, MailConfig{})
	if err != nil {
		t.Fatal(err)
	}
	var releases []func()
	for i := 0; i < downloadPerIPLimit; i++ {
		release, ok := app.acquireDownload("203.0.113.10")
		if !ok {
			t.Fatalf("download %d blocked early", i)
		}
		releases = append(releases, release)
	}
	request := func(remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/uploads/"+name, nil)
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		app.Routes().ServeHTTP(rec, req)
		return rec
	}
	over := request("203.0.113.10:45100")
	if over.Code != http.StatusTooManyRequests || over.Header().Get("Retry-After") != "2" {
		t.Fatalf("expected 429 with Retry-After for busy client, got %d: %q", over.Code, over.Header().Get("Retry-After"))
	}
	other := request("203.0.113.11:45100")
	if other.Code != http.StatusOK {
		t.Fatalf("other IP unexpectedly blocked: %d", other.Code)
	}
	for _, release := range releases {
		release()
	}
}

func TestV241UnicodeURLEncodedEditWithinDecodedLimit(t *testing.T) {
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
	if err := s.ChangePassword(uid, "password12345"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("unicode-session", uid, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	app, err := New(s, assets.Templates(), assets.Static(), "MiniWiki", t.TempDir(), MailConfig{})
	if err != nil {
		t.Fatal(err)
	}
	// UTF-8 source bytes remain below 2 MiB, but application/x-www-form-
	// urlencoded grows above the old 2 MiB + 32 KiB request-body ceiling.
	content := strings.Repeat("日本語", 110000)
	if len(content) >= storage.MaxPageBytes {
		t.Fatal("fixture exceeds decoded page limit")
	}
	form := url.Values{"title": {"Unicode Test"}, "content": {content}, "summary": {"Unicode"}, "base_revision_id": {"0"}, "csrf_token": {"csrf"}}
	encoded := form.Encode()
	if len(encoded) <= storage.MaxPageBytes+(32<<10) || len(encoded) >= int(editFormBodyLimit) {
		t.Fatalf("fixture did not straddle previous and new body caps: %d encoded bytes", len(encoded))
	}
	req := httptest.NewRequest(http.MethodPost, "/wiki/unicode-test/edit", strings.NewReader(encoded))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "unicode-session"})
	req.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: "csrf"})
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("valid Unicode edit rejected with HTTP %d: %.400s", rec.Code, rec.Body.String())
	}
	page, err := s.PageBySlug("unicode-test")
	if err != nil || page.Content != content {
		t.Fatalf("Unicode content not saved correctly: err=%v", err)
	}
}

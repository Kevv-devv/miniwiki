package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"
	"miniwiki/internal/assets"
	"miniwiki/internal/auth"
	"miniwiki/internal/storage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSafeFilename(t *testing.T) {
	got := safeFilename("../../hello world.png")
	if got != "hello-world.png" {
		t.Fatalf("got %q", got)
	}
}

func TestClientIPRejectsSpoofedForwardedHeaders(t *testing.T) {
	a := &App{}
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.9:4321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")
	if got := a.clientIP(req); got != "203.0.113.9" {
		t.Fatalf("trusted spoofed address: %q", got)
	}
	proxies, err := parseTrustedProxies("127.0.0.1/32, ::1/128")
	if err != nil {
		t.Fatal(err)
	}
	a.trustedProxies = proxies
	req.RemoteAddr = "127.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.9")
	if got := a.clientIP(req); got != "203.0.113.9" {
		t.Fatalf("didn't choose rightmost untrusted hop: %q", got)
	}
	req.Header.Set("X-Forwarded-For", "1.2.3.4, bogus")
	if got := a.clientIP(req); got != "127.0.0.1" {
		t.Fatalf("unparseable forwarded chain accepted: %q", got)
	}
	if _, err := parseTrustedProxies("random text"); err == nil {
		t.Fatal("invalid proxy network allowed")
	}
}

func TestRateLimiterPrunesInactiveIPKeys(t *testing.T) {
	a := &App{rateHits: map[string][]time.Time{"stale": {time.Now().Add(-2 * time.Minute)}}}
	handler := a.rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	req.RemoteAddr = "203.0.113.2:9999"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if _, ok := a.rateHits["stale"]; ok {
		t.Fatal("inactive IP was retained after sweep")
	}
}

func TestExpiredLoginFailureIsPruned(t *testing.T) {
	a := &App{loginFails: map[string]loginFail{}}
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "203.0.113.3:12345"
	for i := 0; i < 5; i++ {
		a.recordLoginFailure(req, "test")
	}
	if !a.loginBlocked(req, "test") {
		t.Fatal("five failed logins did not lock user")
	}
	k := a.throttleKey(req, "test")
	f := a.loginFails[k]
	f.LastAttempt = time.Now().Add(-20 * time.Minute)
	f.Until = time.Now().Add(-5 * time.Minute)
	a.loginFails[k] = f
	a.failSweepAt = time.Time{}
	if a.loginBlocked(req, "test") {
		t.Fatal("expired lock is still active")
	}
	if _, ok := a.loginFails[k]; ok {
		t.Fatal("expired login failure record not evicted")
	}
}

func TestAdminPageAndPasswordResetRequireAuthenticationAndConfirmation(t *testing.T) {
	s, err := storage.Open(":memory:")
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
	if err := s.CreateUser("reader", "validpassword", "reader"); err != nil {
		t.Fatal(err)
	}
	admin, err := s.Authenticate("admin", "admin12345")
	if err != nil {
		t.Fatal(err)
	}
	readerID, _ := s.UserIDByName("reader")
	if err := s.ChangePassword(readerID, "validpassword"); err != nil {
		t.Fatal(err)
	}
	app, err := New(s, assets.Templates(), assets.Static(), "MiniWiki", t.TempDir(), MailConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Routes()
	request := func(method, path, body, session string, addCSRF bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = "203.0.113.8:4567"
		if body != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if session != "" {
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session})
		}
		if addCSRF {
			req.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: "testtoken"})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if got := request("GET", "/admin", "", "", false).Code; got != http.StatusSeeOther {
		t.Fatalf("anonymous admin: %d", got)
	}
	if err := s.CreateSession("reader-session", readerID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := request("GET", "/admin", "", "reader-session", false).Code; got != http.StatusForbidden {
		t.Fatalf("reader admin: %d", got)
	}
	// First-run administrators must change their temporary passwords before
	// accessing administrative routes. Exercise the complete account workflow
	// rather than bypassing the security check in this test.
	if err := s.CreateSession("admin-session", admin.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	redirect := request("GET", "/admin", "", "admin-session", false)
	if redirect.Code != http.StatusSeeOther || redirect.Header().Get("Location") != "/account" {
		t.Fatalf("temporary-password admin must be redirected to account: %d %s", redirect.Code, redirect.Header().Get("Location"))
	}
	accountPage := request("GET", "/account", "", "admin-session", false)
	if accountPage.Code != http.StatusOK || !strings.Contains(accountPage.Body.String(), "must change") {
		t.Fatalf("mandatory password change screen missing: %d %s", accountPage.Code, accountPage.Body.String())
	}
	changed := request("POST", "/account", "current_password=admin12345&new_password=newsecureadminpassword&csrf_token=testtoken", "admin-session", true)
	if changed.Code != http.StatusSeeOther || !strings.HasPrefix(changed.Header().Get("Location"), "/login") {
		t.Fatalf("first-run password change failed: %d %s", changed.Code, changed.Header().Get("Location"))
	}
	if _, err := s.UserBySession("admin-session"); err == nil {
		t.Fatal("first-run password change did not revoke old administrator session")
	}
	if _, err := s.Authenticate("admin", "newsecureadminpassword"); err != nil {
		t.Fatalf("administrator cannot log in after changing temporary password: %v", err)
	}
	if err := s.CreateSession("admin-after-change", admin.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	adminSession := "admin-after-change"
	s.Audit(admin, "test_action", "user", "reader", "verified audit event", "203.0.113.8")
	r := request("GET", "/admin", "", adminSession, false)
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "test_action") {
		t.Fatalf("admin recent audit missing, status=%d: %s", r.Code, r.Body.String())
	}
	path := fmt.Sprintf("/user/%d/password", readerID)
	r = request("GET", path, "", adminSession, false)
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "Confirm administrator password reset") {
		t.Fatalf("confirmation page unavailable status=%d: %s", r.Code, r.Body.String())
	}
	r = request("POST", path, "password=newsecurepassword&csrf_token=testtoken", adminSession, true)
	if r.Code != http.StatusBadRequest {
		t.Fatalf("reset without explicit confirmation: %d", r.Code)
	}
	if _, err := s.Authenticate("reader", "validpassword"); err != nil {
		t.Fatalf("password changed without confirmation: %v", err)
	}
	r = request("POST", path, "confirm=1&password=newsecurepassword&csrf_token=bad", adminSession, true)
	if r.Code != http.StatusForbidden {
		t.Fatalf("invalid CSRF allowed reset: %d", r.Code)
	}
	r = request("POST", path, "confirm=1&password=short&csrf_token=testtoken", adminSession, true)
	if r.Code != http.StatusBadRequest {
		t.Fatalf("short reset accepted: %d", r.Code)
	}
	r = request("POST", path, "confirm=1&password=newsecurepassword&csrf_token=testtoken", adminSession, true)
	if r.Code != http.StatusSeeOther {
		t.Fatalf("confirmed password reset: %d %s", r.Code, r.Body.String())
	}
	if _, err := s.Authenticate("reader", "newsecurepassword"); err != nil {
		t.Fatal(err)
	}
}

func TestServeUploadUsesExactStoredFilename(t *testing.T) {
	s, err := storage.Open(":memory:")
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
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "123-first.png"), []byte("RIGHT"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "456-second.png"), []byte("WRONG"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUpload("first.png", "123-first.png", "image/png", 5, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUpload("123-first.png", "456-second.png", "image/png", 5, admin.ID); err != nil {
		t.Fatal(err)
	}
	app, err := New(s, assets.Templates(), assets.Static(), "MiniWiki", dir, MailConfig{})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/uploads/123-first.png", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "RIGHT" {
		t.Fatalf("wrong upload returned: %d %q", rec.Code, rec.Body.String())
	}
	up, _ := s.UploadByStoredName("123-first.png", false)
	if err := s.DeleteUpload(up.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/uploads/123-first.png", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("deleted file served: %d", rec.Code)
	}
}

func TestDeletedRevisionRoutesAndRedirectCycles(t *testing.T) {
	s, err := storage.Open(":memory:")
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
	page, err := s.SavePage("private-secret", "Private Secret", "sensitive", id, "", false)
	if err != nil {
		t.Fatal(err)
	}
	rev, _ := s.LatestRevisionID(page.Slug)
	_, _ = s.SavePage("loop-a", "Loop A", "[[Loop B]]", id, "", false)
	_, _ = s.SavePage("loop-b", "Loop B", "[[Loop A]]", id, "", false)
	app, err := New(s, assets.Templates(), assets.Static(), "MiniWiki", t.TempDir(), MailConfig{})
	if err != nil {
		t.Fatal(err)
	}
	h := app.Routes()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/wiki/loop-a", nil))
	if rec.Code != http.StatusLoopDetected {
		t.Fatalf("redirect cycle status=%d", rec.Code)
	}
	if err := s.DeletePage(page.Slug, id); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fmt.Sprintf("/revision/%d", rev), fmt.Sprintf("/diff/%d", rev), fmt.Sprintf("/compare?from=%d&to=%d", rev, rev)} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code == http.StatusOK {
			t.Fatalf("deleted revision exposed through %s", path)
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/recent", nil))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "private-secret") {
		t.Fatalf("recent changes leaked deleted content: %d", rec.Code)
	}
}
func TestPrivateWikiAndPostOnlyLogout(t *testing.T) {
	s, err := storage.Open(":memory:")
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
	if err := s.ChangePassword(id, "password12345"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SavePage("test-page", "Test Page", "Hi", id, "", false); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("my-session", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	app, err := New(s, assets.Templates(), assets.Static(), "MiniWiki", t.TempDir(), MailConfig{RequireLogin: true})
	if err != nil {
		t.Fatal(err)
	}
	h := app.Routes()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/wiki/test-page", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous read allowed: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/uploads/example.png", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous upload access: %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/wiki/test-page", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "my-session"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated read failed: %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "/logout", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "my-session"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("logout GET allowed: %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/logout", strings.NewReader("csrf_token=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "my-session"})
	req.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: "good"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("logout CSRF bypass: %d", rec.Code)
	}
	if _, err := s.UserBySession("my-session"); err != nil {
		t.Fatal("invalid CSRF revoked session", err)
	}
	req = httptest.NewRequest("POST", "/logout", strings.NewReader("csrf_token=good"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "my-session"})
	req.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: "good"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("valid logout: %d", rec.Code)
	}
	if _, err := s.UserBySession("my-session"); err == nil {
		t.Fatal("session was not revoked")
	}
}
func TestEditConflictsAndCanonicalURL(t *testing.T) {
	s, err := storage.Open(":memory:")
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
	if err := s.ChangePassword(id, "password12345"); err != nil {
		t.Fatal(err)
	}
	p, err := s.SavePage("my-page", "My Page", "Original content", id, "", false)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := s.LatestRevisionID(p.Slug)
	_, err = s.SavePage(p.Slug, p.Title, "Current content", id, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("s", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	app, err := New(s, assets.Templates(), assets.Static(), "MiniWiki", t.TempDir(), MailConfig{})
	if err != nil {
		t.Fatal(err)
	}
	h := app.Routes()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/wiki/My%20Page/edit", nil))
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/wiki/my-page/edit") {
		t.Fatalf("noncanonical URL not redirected: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	body := fmt.Sprintf("csrf_token=good&base_revision_id=%d&title=My+Page&content=Draft+content&summary=test", base)
	req := httptest.NewRequest("POST", "/wiki/my-page/edit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "s"})
	req.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: "good"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Draft content") {
		t.Fatalf("conflict/draft handling: %d", rec.Code)
	}
	p, err = s.PageBySlug("my-page")
	if err != nil {
		t.Fatal(err)
	}
	if p.Content != "Current content" {
		t.Fatal("stale editor overwrote content")
	}
}
func TestProxyMultiLineHeadersAndZeroNetworkRejected(t *testing.T) {
	if _, err := parseTrustedProxies("0.0.0.0/0"); err == nil {
		t.Fatal("IPv4 /0 accepted")
	}
	if _, err := parseTrustedProxies("::/0"); err == nil {
		t.Fatal("IPv6 /0 accepted")
	}
	proxies, err := parseTrustedProxies("127.0.0.1/32")
	if err != nil {
		t.Fatal(err)
	}
	app := &App{trustedProxies: proxies}
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Add("X-Forwarded-For", "198.51.100.5")
	req.Header.Add("X-Forwarded-For", "203.0.113.4")
	if got := app.clientIP(req); got != "203.0.113.4" {
		t.Fatalf("did not use all forwarded headers: %s", got)
	}
}

func TestEmbeddedTemplatesParse(t *testing.T) {
	app, err := New(nil, assets.Templates(), assets.Static(), "MiniWiki", t.TempDir(), MailConfig{})
	if err != nil {
		t.Fatalf("embedded templates failed to parse: %v", err)
	}
	if len(app.templates) < 25 {
		t.Fatalf("unexpectedly few embedded templates: %d", len(app.templates))
	}
}

func TestV23SecurityHeadersUseShortHSTS(t *testing.T) {
	a := &App{}
	req := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	rec := httptest.NewRecorder()
	a.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(rec, req)
	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=86400" {
		t.Fatalf("unexpected HSTS: %q", got)
	}
}

func TestV23StreamedUploadChecksCSRFBeforeFile(t *testing.T) {
	makeRequest := func(order []string) *http.Request {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for _, name := range order {
			if name == "csrf_token" {
				mw.WriteField("csrf_token", "valid-token")
			} else {
				part, _ := mw.CreateFormFile("file", "image.png")
				part.Write([]byte("fake image"))
			}
		}
		mw.Close()
		r := httptest.NewRequest(http.MethodPost, "/upload", &body)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		r.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: "valid-token"})
		return r
	}
	p, err := streamUploadPart(makeRequest([]string{"csrf_token", "file"}))
	if err != nil || p.FileName() != "image.png" {
		t.Fatalf("valid upload not streamed: %v", err)
	}
	p.Close()
	_, err = streamUploadPart(makeRequest([]string{"file", "csrf_token"}))
	if !errors.Is(err, errUploadCSRF) {
		t.Fatalf("file preceding CSRF must be rejected: %v", err)
	}
}

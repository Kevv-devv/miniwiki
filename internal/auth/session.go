package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"time"
)

const CookieName = "miniwiki_session"
const CSRFCookieName = "miniwiki_csrf"

var secureCookiesOverride *bool

func SetSecureCookies(enabled bool) { secureCookiesOverride = &enabled }

func NewSessionID() string { b := make([]byte, 32); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func NewCSRFToken() string { b := make([]byte, 32); _, _ = rand.Read(b); return hex.EncodeToString(b) }

func SecureCookiesEnabled() bool {
	if secureCookiesOverride != nil {
		return *secureCookiesOverride
	}
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MINIWIKI_SECURE_COOKIES")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

type secureContextKey struct{}

func WithSecureRequest(r *http.Request, enabled bool) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), secureContextKey{}, enabled))
}
func cookieSecure(req ...*http.Request) bool {
	if SecureCookiesEnabled() {
		return true
	}
	if len(req) == 0 || req[0] == nil {
		return false
	}
	r := req[0]
	if r.TLS != nil {
		return true
	}
	return r.Context().Value(secureContextKey{}) == true
}
func SetCookie(w http.ResponseWriter, id string, req ...*http.Request) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: id, Path: "/", HttpOnly: true, Secure: cookieSecure(req...), SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(7 * 24 * time.Hour)})
}
func ClearCookie(w http.ResponseWriter, req ...*http.Request) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: cookieSecure(req...), SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func SetCSRFCookie(w http.ResponseWriter, token string, req ...*http.Request) {
	http.SetCookie(w, &http.Cookie{Name: CSRFCookieName, Value: token, Path: "/", HttpOnly: true, Secure: cookieSecure(req...), SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(7 * 24 * time.Hour)})
}
func ClearCSRFCookie(w http.ResponseWriter, req ...*http.Request) {
	http.SetCookie(w, &http.Cookie{Name: CSRFCookieName, Value: "", Path: "/", HttpOnly: true, Secure: cookieSecure(req...), SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

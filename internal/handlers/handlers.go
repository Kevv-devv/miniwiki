package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"miniwiki/internal/auth"
	"miniwiki/internal/render"
	"miniwiki/internal/storage"
)

type MailConfig struct {
	BaseURL, Host, Port, Username, Password, From string
	DevMode                                       bool
	TrustedProxies                                string
	RequireLogin                                  bool
	BackupDir                                     string
}

type App struct {
	Store                 *storage.Store
	templateFS            fs.FS
	staticFS              fs.FS
	siteName              string
	uploadDir             string
	mail                  MailConfig
	templates             map[string]*template.Template
	throttleMu            sync.Mutex
	loginFails            map[string]loginFail
	rateMu                sync.Mutex
	rateHits              map[string][]time.Time
	rateSweepAt           time.Time
	failSweepAt           time.Time
	trustedProxies        []netip.Prefix
	mailQueue             chan resetMessage
	untrustedProxyWarning sync.Once
	downloadSlots         chan struct{}
	downloadMu            sync.Mutex
	downloadByIP          map[string]int
}

type resetMessage struct{ to, link string }

type loginFail struct {
	Count       int
	Until       time.Time
	LastAttempt time.Time
}

type Data struct {
	Title          string
	User           *storage.User
	Page           *storage.Page
	HTML           template.HTML
	Error          string
	Query          string
	Results        []storage.SearchResult
	CreateTitle    string
	CreateSlug     string
	PageExists     bool
	Pages          []storage.Page
	Categories     []storage.Category
	Category       *storage.Category
	Backlinks      []storage.Page
	Revisions      []storage.Revision
	Revision       *storage.Revision
	Prev           *storage.Revision
	Diff           []DiffLine
	ReturnTo       string
	Preview        bool
	Summary        string
	Minor          bool
	Users          []storage.User
	TargetUser     *storage.User
	Uploads        []storage.Upload
	Upload         *storage.Upload
	DeletedPages   []storage.Page
	AuditEvents    []storage.AuditEvent
	Watchlist      []storage.WatchItem
	Watched        bool
	Email          string
	Message        string
	NewTitle       string
	NewSlug        string
	ResetToken     string
	ResetLink      string
	CompareFrom    *storage.Revision
	CompareTo      *storage.Revision
	CSRFToken      string
	SiteName       string
	BaseRevisionID int64
	RedirectedFrom string
	BackupStatus   string
}
type DiffLine struct{ Kind, Text string }

func snippetHTML(s string) template.HTML {
	esc := template.HTMLEscapeString(s)
	esc = strings.ReplaceAll(esc, "&lt;mark&gt;", "<mark>")
	esc = strings.ReplaceAll(esc, "&lt;/mark&gt;", "</mark>")
	return template.HTML(esc)
}

func New(s *storage.Store, templateFS fs.FS, staticFS fs.FS, siteName, uploadDir string, mail MailConfig) (*App, error) {
	trustedProxies, err := parseTrustedProxies(mail.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("trusted_proxies: %w", err)
	}
	if siteName == "" {
		siteName = "MiniWiki"
	}
	if uploadDir == "" {
		uploadDir = "uploads"
	}
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return nil, err
	}
	tpls := map[string]*template.Template{}
	entries, err := fs.ReadDir(templateFS, "templates")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == "base.html" || !strings.HasSuffix(name, ".html") {
			continue
		}
		t, err := template.New("base.html").Funcs(template.FuncMap{"snippetHTML": snippetHTML}).ParseFS(templateFS, "templates/base.html", "templates/"+name)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		tpls[name] = t
	}
	app := &App{Store: s, templateFS: templateFS, staticFS: staticFS, siteName: siteName, uploadDir: uploadDir, mail: mail, templates: tpls, loginFails: map[string]loginFail{}, rateHits: map[string][]time.Time{}, trustedProxies: trustedProxies, mailQueue: make(chan resetMessage, 64), downloadSlots: make(chan struct{}, downloadSlotsCapacity), downloadByIP: make(map[string]int)}
	if mail.Host != "" && mail.From != "" {
		go func() {
			for msg := range app.mailQueue {
				if err := app.sendResetEmail(msg.to, msg.link); err != nil {
					log.Printf("password reset email delivery failed: %v", err)
				}
			}
		}()
	}
	return app, nil
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(a.staticFS))))
	mux.HandleFunc("/uploads/", a.serveUpload)
	mux.HandleFunc("/", a.home)
	mux.HandleFunc("/login", a.login)
	mux.HandleFunc("/logout", a.logout)
	mux.HandleFunc("/account", a.account)
	mux.HandleFunc("/forgot-password", a.forgotPassword)
	mux.HandleFunc("/reset-password", a.resetPassword)
	mux.HandleFunc("/recent", a.recent)
	mux.HandleFunc("/pages", a.pages)
	mux.HandleFunc("/categories", a.categories)
	mux.HandleFunc("/category/", a.category)
	mux.HandleFunc("/search", a.search)
	mux.HandleFunc("/revision/", a.revision)
	mux.HandleFunc("/diff/", a.diff)
	mux.HandleFunc("/admin", a.admin)
	mux.HandleFunc("/users", a.users)
	mux.HandleFunc("/checkuser", a.checkUser)
	mux.HandleFunc("/deleted/pages", a.deletedPages)
	mux.HandleFunc("/deleted-upload/", a.serveDeletedUpload)
	mux.HandleFunc("/upload/", a.uploadAdmin)
	mux.HandleFunc("/upload", a.upload)
	mux.HandleFunc("/watchlist", a.watchlist)
	mux.HandleFunc("/user/", a.userAdmin)
	mux.HandleFunc("/restore/", a.restore)
	mux.HandleFunc("/compare", a.compare)
	mux.HandleFunc("/wiki/", a.wiki)
	return a.securityHeaders(a.rateLimit(a.authGate(mux)))
}

type userContextKey struct{}

func (a *App) authGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/static/") || path == "/login" || path == "/forgot-password" || path == "/reset-password" {
			next.ServeHTTP(w, r)
			return
		}
		u := a.user(r)
		r = r.WithContext(context.WithValue(r.Context(), userContextKey{}, u))
		if u != nil && u.MustChangePassword && path != "/account" && path != "/logout" {
			http.Redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
		if a.mail.RequireLogin {
			if !strings.HasPrefix(path, "/uploads/") {
				w.Header().Set("Cache-Control", "private, no-store")
			}
			if u == nil {
				if strings.HasPrefix(path, "/uploads/") || strings.HasPrefix(path, "/deleted-upload/") {
					http.Error(w, "Login required", http.StatusForbidden)
					return
				}
				http.Redirect(w, r, "/login?return="+url.QueryEscape(safeReturnTo(r.URL.RequestURI())), http.StatusSeeOther)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) user(r *http.Request) *storage.User {
	if v := r.Context().Value(userContextKey{}); v != nil {
		u, _ := v.(*storage.User)
		return u
	}
	c, err := r.Cookie(auth.CookieName)
	if err != nil {
		return nil
	}
	u, err := a.Store.UserBySession(c.Value)
	if err != nil {
		return nil
	}
	return u
}

func (a *App) csrfToken(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(auth.CSRFCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	token := auth.NewCSRFToken()
	auth.SetCSRFCookie(w, token, r)
	return token
}

func (a *App) checkCSRF(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		return true
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				http.Error(w, "Upload exceeds the 25 MiB size limit.", http.StatusRequestEntityTooLarge)
				return false
			}
			http.Error(w, "Bad form submission.", http.StatusBadRequest)
			return false
		}
	} else if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form submission.", http.StatusBadRequest)
		return false
	}
	c, err := r.Cookie(auth.CSRFCookieName)
	if err != nil || c.Value == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.FormValue("csrf_token"))) != 1 {
		a.Store.Audit(a.user(r), "csrf_rejected", "request", r.URL.Path, "missing or invalid CSRF token", a.clientIP(r))
		http.Error(w, "Invalid security token. Go back, reload the page, and try again.", http.StatusForbidden)
		return false
	}
	return true
}

func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// Conservative CSP for the current server-rendered app. Inline styles/scripts are not needed.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		secure := r.TLS != nil || (a.trustedPeer(r) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"))
		if secure {
			w.Header().Set("Strict-Transport-Security", "max-age=86400")
		}
		next.ServeHTTP(w, auth.WithSecureRequest(r, secure))
	})
}

func (a *App) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") || strings.HasPrefix(r.URL.Path, "/uploads/") {
			next.ServeHTTP(w, r)
			return
		}
		key := a.clientIP(r)
		now := time.Now()
		window := now.Add(-time.Minute)
		a.rateMu.Lock()
		// Sweep all keys periodically, including addresses that never return.
		if !now.Before(a.rateSweepAt) {
			for ip, history := range a.rateHits {
				remaining := history[:0]
				for _, t := range history {
					if t.After(window) {
						remaining = append(remaining, t)
					}
				}
				if len(remaining) == 0 {
					delete(a.rateHits, ip)
				} else {
					a.rateHits[ip] = remaining
				}
			}
			a.rateSweepAt = now.Add(time.Minute)
		}
		hits := a.rateHits[key]
		keep := hits[:0]
		for _, t := range hits {
			if t.After(window) {
				keep = append(keep, t)
			}
		}
		if len(keep) >= 180 {
			a.rateHits[key] = keep
			a.rateMu.Unlock()
			http.Error(w, "Too many requests. Try again soon.", http.StatusTooManyRequests)
			return
		}
		keep = append(keep, now)
		a.rateHits[key] = keep
		a.rateMu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// parseTrustedProxies requires an explicit IP or CIDR allowlist. A boolean
// "trust forwarded headers" switch would allow accidental public spoofing.
func parseTrustedProxies(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, field := range strings.Split(raw, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if ip, err := netip.ParseAddr(field); err == nil {
			out = append(out, netip.PrefixFrom(ip.Unmap(), ip.Unmap().BitLen()))
			continue
		}
		prefix, err := netip.ParsePrefix(field)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy %q: %w", field, err)
		}
		if prefix.Bits() == 0 {
			return nil, errors.New("a /0 trusted proxy network would allow spoofed client IPs")
		}
		out = append(out, prefix.Masked())
	}
	return out, nil
}

func (a *App) trustedPeer(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && a.isTrustedProxy(ip.Unmap())
}
func (a *App) isTrustedProxy(ip netip.Addr) bool {
	for _, prefix := range a.trustedProxies {
		if prefix.Contains(ip.Unmap()) {
			return true
		}
	}
	return false
}

func (a *App) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote, err := netip.ParseAddr(host)
	if err != nil {
		return r.RemoteAddr
	}
	remote = remote.Unmap()
	if !a.isTrustedProxy(remote) {
		if len(r.Header.Values("X-Forwarded-For")) > 0 || r.Header.Get("X-Real-IP") != "" {
			a.untrustedProxyWarning.Do(func() {
				log.Printf("WARNING: forwarded IP headers received from an untrusted peer; ignoring them. If behind a reverse proxy, configure trusted_proxies or rate limiting will be shared by the proxy IP")
			})
		}
		return remote.String()
	}
	// Work back from the app's trusted peer to the first untrusted hop.
	// The leftmost value can be arbitrarily supplied by a malicious client.
	parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			return remote.String()
		}
		ip = ip.Unmap()
		if !a.isTrustedProxy(ip) {
			return ip.String()
		}
	}
	return remote.String()
}

func (a *App) throttleKey(r *http.Request, username string) string {
	return a.clientIP(r) + "|" + strings.ToLower(strings.TrimSpace(username))
}

const loginFailureWindow = 15 * time.Minute

func (a *App) pruneLoginFailures(now time.Time) {
	if now.Before(a.failSweepAt) {
		return
	}
	for key, f := range a.loginFails {
		if !f.Until.After(now) && (f.LastAttempt.IsZero() || !f.LastAttempt.Add(loginFailureWindow).After(now)) {
			delete(a.loginFails, key)
		}
	}
	a.failSweepAt = now.Add(time.Minute)
}

func (a *App) loginBlocked(r *http.Request, username string) bool {
	a.throttleMu.Lock()
	defer a.throttleMu.Unlock()
	now := time.Now()
	a.pruneLoginFailures(now)
	return a.loginFails[a.throttleKey(r, username)].Until.After(now) || a.loginFails[a.clientIP(r)+"|*"].Until.After(now)
}

func (a *App) recordLoginFailure(r *http.Request, username string) {
	a.throttleMu.Lock()
	defer a.throttleMu.Unlock()
	now := time.Now()
	a.pruneLoginFailures(now)
	key := a.throttleKey(r, username)
	f := a.loginFails[key]
	if f.LastAttempt.IsZero() || !f.LastAttempt.Add(loginFailureWindow).After(now) {
		f = loginFail{}
	}
	f.Count++
	f.LastAttempt = now
	if f.Count >= 5 {
		f.Until = now.Add(loginFailureWindow)
	}
	a.loginFails[key] = f
	// Additional per-IP limit prevents username spraying from one source.
	ipKey := a.clientIP(r) + "|*"
	if username == "*" {
		return
	}
	ipFail := a.loginFails[ipKey]
	if ipFail.LastAttempt.IsZero() || !ipFail.LastAttempt.Add(loginFailureWindow).After(now) {
		ipFail = loginFail{}
	}
	ipFail.Count++
	ipFail.LastAttempt = now
	if ipFail.Count >= 20 {
		ipFail.Until = now.Add(loginFailureWindow)
	}
	a.loginFails[ipKey] = ipFail
}

func (a *App) clearLoginFailures(r *http.Request, username string) {
	a.throttleMu.Lock()
	defer a.throttleMu.Unlock()
	delete(a.loginFails, a.throttleKey(r, username))
}
func (a *App) render(w http.ResponseWriter, r *http.Request, name string, d Data) {
	d.User = a.user(r)
	d.SiteName = a.siteName
	d.CSRFToken = a.csrfToken(w, r)
	if d.Title == "" {
		d.Title = "MiniWiki"
	}
	t, ok := a.templates[name]
	if !ok {
		http.Error(w, "template not found", 500)
		return
	}
	if err := t.ExecuteTemplate(w, "base", d); err != nil {
		http.Error(w, "Could not render page.", 500)
	}
}

func (a *App) imageResolver(name string) (string, string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", false
	}
	u, err := a.Store.UploadByName(name, false)
	if err != nil || u == nil {
		u, err = a.Store.UploadByName(safeFilename(name), false)
	}
	if err != nil || u == nil || !u.IsImage {
		return "", "", false
	}
	return "/uploads/" + u.StoredName, u.Filename, true
}

func (a *App) require(w http.ResponseWriter, r *http.Request) *storage.User {
	u := a.user(r)
	if u == nil {
		http.Redirect(w, r, "/login?return="+url.QueryEscape(safeReturnTo(r.URL.RequestURI())), http.StatusSeeOther)
		return nil
	}
	return u
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/wiki/main-page", http.StatusSeeOther)
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	ret := safeReturnTo(r.URL.Query().Get("return"))
	if ret == "" {
		ret = "/wiki/main-page"
	}
	if r.Method == http.MethodGet {
		a.render(w, r, "login.html", Data{Title: "Login", ReturnTo: ret})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	username := r.FormValue("username")
	if a.loginBlocked(r, username) {
		a.Store.Audit(nil, "login_blocked", "user", username, "too many failed login attempts", a.clientIP(r))
		a.render(w, r, "login.html", Data{Title: "Login", Error: "Too many failed login attempts. Try again later.", ReturnTo: ret})
		return
	}
	u, err := a.Store.Authenticate(username, r.FormValue("password"))
	if err != nil {
		a.recordLoginFailure(r, username)
		a.Store.Audit(nil, "login_failed", "user", username, "invalid credentials", a.clientIP(r))
		a.render(w, r, "login.html", Data{Title: "Login", Error: "Invalid username or password", ReturnTo: ret})
		return
	}
	a.clearLoginFailures(r, username)
	sid := auth.NewSessionID()
	if err := a.Store.CreateSession(sid, u.ID, time.Now().Add(7*24*time.Hour)); err != nil {
		http.Error(w, "Could not create session.", 500)
		return
	}
	a.Store.Audit(u, "login_success", "user", u.Username, "", a.clientIP(r))
	auth.SetCookie(w, sid, r)
	if u.MustChangePassword {
		ret = "/account"
	}
	http.Redirect(w, r, ret, http.StatusSeeOther)
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Logout requires POST", http.StatusMethodNotAllowed)
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	u := a.user(r)
	if c, err := r.Cookie(auth.CookieName); err == nil {
		a.Store.DeleteSession(c.Value)
	}
	a.Store.Audit(u, "logout", "session", "", "", a.clientIP(r))
	auth.ClearCookie(w, r)
	auth.ClearCSRFCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) wiki(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/wiki/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	slug := parts[0]
	if len(parts) > 2 {
		http.NotFound(w, r)
		return
	}
	if slug == "" {
		http.Redirect(w, r, "/wiki/main-page", 303)
		return
	}
	if canonical := render.Slugify(slug); slug != canonical {
		if r.Method != http.MethodGet {
			http.Error(w, "Use canonical page URL", http.StatusBadRequest)
			return
		}
		destination := "/wiki/" + url.PathEscape(canonical)
		if len(parts) > 1 {
			destination += "/" + parts[1]
		}
		if r.URL.RawQuery != "" {
			destination += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, destination, http.StatusSeeOther)
		return
	}
	action := "view"
	if len(parts) > 1 {
		action = parts[1]
	}
	switch action {
	case "view", "":
		a.viewPage(w, r, slug)
	case "edit":
		a.editPage(w, r, slug)
	case "history":
		a.history(w, r, slug)
	case "raw":
		a.raw(w, r, slug)
	case "backlinks":
		a.backlinks(w, r, slug)
	case "protect":
		a.protect(w, r, slug)
	case "watch":
		a.watch(w, r, slug)
	case "delete":
		a.deletePage(w, r, slug)
	case "deleted":
		a.viewDeletedPage(w, r, slug)
	case "undelete":
		a.undeletePage(w, r, slug)
	case "rename":
		a.renamePage(w, r, slug)
	default:
		http.NotFound(w, r)
	}
}

// resolveView follows both page-content redirects and rename redirects,
// rejecting redirect cycles instead of asking the browser to follow forever.
func (a *App) resolveView(slug string) (string, error) {
	seen := map[string]bool{}
	for i := 0; i < 16; i++ {
		if seen[slug] {
			return "", errors.New("redirect cycle detected")
		}
		seen[slug] = true
		p, err := a.Store.PageBySlug(slug)
		if err == nil {
			if target, ok := render.RedirectTarget(p.Content); ok && target != slug {
				slug = target
				continue
			}
			return slug, nil
		}
		if target, err := a.Store.RedirectTarget(slug); err == nil && target != "" {
			slug = target
			continue
		}
		return slug, nil
	}
	return "", errors.New("too many redirects")
}
func (a *App) viewPage(w http.ResponseWriter, r *http.Request, slug string) {
	if r.URL.Query().Get("redirect") != "no" {
		final, err := a.resolveView(slug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusLoopDetected)
			return
		}
		if final != slug {
			http.Redirect(w, r, "/wiki/"+final+"?from="+url.QueryEscape(slug), http.StatusFound)
			return
		}
	}
	p, err := a.Store.PageBySlug(slug)
	if err != nil {
		if r.URL.Query().Get("redirect") == "no" {
			if target, targetErr := a.Store.RedirectTarget(slug); targetErr == nil && target != "" {
				a.render(w, r, "redirect_source.html", Data{Title: "Redirect: " + titleFromSlug(slug), RedirectedFrom: slug, NewSlug: target})
				return
			}
		}
		a.render(w, r, "missing.html", Data{Title: "Page not found", Page: &storage.Page{Slug: slug, Title: titleFromSlug(slug)}})
		return
	}
	html, err := render.WikiMarkdownWithImages(p.Content, a.Store.PageExists, a.imageResolver)
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	cats, _ := a.Store.CategoriesForPage(p.ID)
	usr := a.user(r)
	redirected := r.URL.Query().Get("from")
	if redirected != render.Slugify(redirected) || redirected == p.Slug {
		redirected = ""
	}
	if redirected != "" {
		resolved, err := a.resolveView(redirected)
		if err != nil || resolved != slug {
			redirected = ""
		}
	}
	a.render(w, r, "view.html", Data{Title: p.Title, Page: p, HTML: html, Categories: cats, Watched: usr != nil && a.Store.IsWatched(usr.ID, p.ID), RedirectedFrom: redirected})
}

func (a *App) editPage(w http.ResponseWriter, r *http.Request, slug string) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	p, _ := a.Store.PageBySlug(slug)
	if p == nil {
		if deleted, derr := a.Store.PageBySlugAny(slug); derr == nil && deleted.Deleted {
			if r.Method == http.MethodPost {
				// Do not discard the POST body if another editor deleted the page.
				r.Body = http.MaxBytesReader(w, r.Body, editFormBodyLimit)
				if !a.checkCSRF(w, r) {
					return
				}
				draft := &storage.Page{Slug: slug, Title: r.FormValue("title"), Content: r.FormValue("content")}
				base, _ := strconv.ParseInt(r.FormValue("base_revision_id"), 10, 64)
				w.WriteHeader(http.StatusConflict)
				a.render(w, r, "edit.html", Data{Title: "Page deleted — draft preserved", Page: draft, Error: "This page was deleted while you were editing. Your draft is shown below; copy it before leaving and ask an admin to restore the page.", Summary: r.FormValue("summary"), Minor: r.FormValue("minor") == "on", BaseRevisionID: base})
				return
			}
			if !a.Store.IsAdmin(u) {
				http.Error(w, "This page is deleted. Ask an admin to undelete it.", http.StatusForbidden)
				return
			}
			http.Redirect(w, r, "/wiki/"+slug+"/deleted", http.StatusSeeOther)
			return
		}
		p = &storage.Page{Slug: slug, Title: titleFromSlug(slug)}
	}
	if !a.Store.CanEdit(u, p) {
		http.Error(w, "You do not have permission to edit this page.", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodGet {
		base, _ := a.Store.LatestRevisionID(slug)
		a.render(w, r, "edit.html", Data{Title: "Edit " + p.Title, Page: p, BaseRevisionID: base})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, editFormBodyLimit)
	if !a.checkCSRF(w, r) {
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = titleFromSlug(slug)
	}
	content := r.FormValue("content")
	baseRev, err := strconv.ParseInt(r.FormValue("base_revision_id"), 10, 64)
	summary := strings.TrimSpace(r.FormValue("summary"))
	minor := r.FormValue("minor") == "on"
	p.Title = title
	p.Content = content
	if len(content) > storage.MaxPageBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		a.render(w, r, "edit.html", Data{Title: "Page too large — draft preserved", Page: p, Error: "Page content exceeds 2 MiB. Your draft is still in the editor; reduce its size and save again.", Summary: summary, Minor: minor, BaseRevisionID: baseRev})
		return
	}
	if err != nil || baseRev < 0 {
		w.WriteHeader(http.StatusBadRequest)
		a.render(w, r, "edit.html", Data{Title: "Please reload — draft preserved", Page: p, Error: "Missing or invalid base revision. Your draft is preserved; copy it before reloading the editor.", Summary: summary, Minor: minor, BaseRevisionID: 0})
		return
	}
	if r.FormValue("action") == "preview" {
		html, err := render.WikiMarkdownWithImages(content, a.Store.PageExists, a.imageResolver)
		if err != nil {
			log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
			w.WriteHeader(http.StatusInternalServerError)
			a.render(w, r, "edit.html", Data{Title: "Preview failed — draft preserved", Page: p, Error: "Preview could not be generated. Your draft is preserved.", Summary: summary, Minor: minor, BaseRevisionID: baseRev})
			return
		}
		a.render(w, r, "edit.html", Data{Title: "Preview - " + title, Page: p, HTML: html, Preview: true, Summary: summary, Minor: minor, BaseRevisionID: baseRev})
		return
	}
	draft := *p
	p, err = a.Store.SavePageWithBase(slug, title, content, u.ID, summary, minor, baseRev)
	if errors.Is(err, storage.ErrEditConflict) {
		w.WriteHeader(http.StatusConflict)
		a.render(w, r, "edit.html", Data{Title: "Edit conflict - " + title, Page: &draft, Error: "Edit conflict: another user saved changes since you opened the editor. Copy your draft, reload the page, and merge the changes before saving.", Summary: summary, Minor: minor, BaseRevisionID: baseRev})
		return
	}
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrTitleConflict):
			w.WriteHeader(http.StatusConflict)
			a.render(w, r, "edit.html", Data{Title: "Title conflict — draft preserved", Page: &draft, Error: "A page with a visually equivalent title already exists. Your draft is preserved; choose a different title or edit the existing page.", Summary: summary, Minor: minor, BaseRevisionID: baseRev})
		case errors.Is(err, storage.ErrPageTooLarge):
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			a.render(w, r, "edit.html", Data{Title: "Page too large — draft preserved", Page: &draft, Error: "Page content exceeds 2 MiB. Your draft is preserved; shorten it before saving.", Summary: summary, Minor: minor, BaseRevisionID: baseRev})
		case errors.Is(err, storage.ErrPageDeleted):
			w.WriteHeader(http.StatusConflict)
			a.render(w, r, "edit.html", Data{Title: "Page deleted — draft preserved", Page: &draft, Error: "This page was deleted. Your draft is preserved; contact an administrator to restore the page.", Summary: summary, Minor: minor, BaseRevisionID: baseRev})
		default:
			log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
			// Unexpected failures should preserve the user's draft too.
			w.WriteHeader(http.StatusInternalServerError)
			a.render(w, r, "edit.html", Data{Title: "Save failed — draft preserved", Page: &draft, Error: "The page could not be saved. Your draft is still in the editor; please copy it before leaving and try again later.", Summary: summary, Minor: minor, BaseRevisionID: baseRev})
		}
		return
	}
	if err := a.Store.SetPageCategories(p.ID, render.ExtractCategories(content)); err != nil {
		http.Error(w, "Could not update page categories.", 500)
		return
	}
	a.Store.Audit(u, "page_save", "page", p.Slug, summary, a.clientIP(r))
	http.Redirect(w, r, "/wiki/"+p.Slug, http.StatusSeeOther)
}
func (a *App) history(w http.ResponseWriter, r *http.Request, slug string) {
	p, err := a.Store.PageBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	revs, err := a.Store.Revisions(slug)
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	a.render(w, r, "history.html", Data{Title: "History - " + p.Title, Page: p, Revisions: revs})
}
func (a *App) raw(w http.ResponseWriter, r *http.Request, slug string) {
	p, err := a.Store.PageBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, p.Content)
}
func (a *App) recent(w http.ResponseWriter, r *http.Request) {
	revs, err := a.Store.Recent(100)
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	a.render(w, r, "recent.html", Data{Title: "Recent Changes", Revisions: revs})
}

func (a *App) pages(w http.ResponseWriter, r *http.Request) {
	pages, err := a.Store.AllPages()
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	a.render(w, r, "pages.html", Data{Title: "All Pages", Pages: pages})
}

func (a *App) categories(w http.ResponseWriter, r *http.Request) {
	cats, err := a.Store.AllCategories()
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	a.render(w, r, "categories.html", Data{Title: "Categories", Categories: cats})
}

func (a *App) category(w http.ResponseWriter, r *http.Request) {
	slug := strings.Trim(strings.TrimPrefix(r.URL.Path, "/category/"), "/")
	cat, pages, err := a.Store.PagesByCategory(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.render(w, r, "category.html", Data{Title: "Category: " + cat.Name, Category: cat, Pages: pages})
}

func (a *App) backlinks(w http.ResponseWriter, r *http.Request, slug string) {
	p, err := a.Store.PageBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	links, err := a.Store.Backlinks(slug)
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	a.render(w, r, "backlinks.html", Data{Title: "Backlinks - " + p.Title, Page: p, Backlinks: links})
}

func (a *App) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var res []storage.SearchResult
	if q != "" {
		var err error
		res, err = a.Store.Search(q)
		if err != nil {
			log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
			http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
			return
		}
	}
	createTitle := q
	createSlug := ""
	pageExists := false
	if q != "" {
		createSlug = render.Slugify(q)
		pageExists = a.Store.PageExists(createSlug)
	}
	a.render(w, r, "search.html", Data{Title: "Search", Query: q, Results: res, CreateTitle: createTitle, CreateSlug: createSlug, PageExists: pageExists})
}

func (a *App) admin(w http.ResponseWriter, r *http.Request) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if !a.Store.IsAdmin(u) {
		http.Error(w, "Admin only.", http.StatusForbidden)
		return
	}
	users, _ := a.Store.AllUsers()
	deleted, _ := a.Store.DeletedPages()
	ups, _ := a.Store.DeletedUploads()
	audit, err := a.Store.RecentAudit(50)
	if err != nil {
		http.Error(w, "Could not load audit log.", http.StatusInternalServerError)
		return
	}
	backupStatus := "No scheduled backup result recorded."
	if a.mail.BackupDir != "" {
		if raw, err := os.ReadFile(filepath.Join(a.mail.BackupDir, ".miniwiki-backup-status.json")); err == nil {
			var info struct {
				At      time.Time `json:"at"`
				OK      bool      `json:"ok"`
				Message string    `json:"message"`
			}
			if json.Unmarshal(raw, &info) == nil {
				result := "Failed"
				if info.OK {
					result = "Successful"
				}
				backupStatus = fmt.Sprintf("%s at %s — %s", result, info.At.Format(time.RFC3339), info.Message)
			}
		}
	}
	a.render(w, r, "admin.html", Data{Title: "Admin", Users: users, DeletedPages: deleted, Uploads: ups, AuditEvents: audit, BackupStatus: backupStatus})
}

func (a *App) revision(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/revision/"), 10, 64)
	rev, err := a.Store.Revision(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	html, err := render.WikiMarkdownWithImages(rev.Content, a.Store.PageExists, a.imageResolver)
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	a.render(w, r, "revision.html", Data{Title: "Revision", Revision: rev, HTML: html})
}
func (a *App) diff(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/diff/"), 10, 64)
	rev, err := a.Store.Revision(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	prev, _ := a.Store.PrevRevision(*rev)
	lines := []DiffLine{{Kind: "same", Text: "No previous revision to compare."}}
	if prev != nil {
		lines = simpleDiff(prev.Content, rev.Content)
	}
	a.render(w, r, "diff.html", Data{Title: "Diff", Revision: rev, Prev: prev, Diff: lines})
}

func simpleDiff(a, b string) []DiffLine {
	oldLines := splitLines(a)
	newLines := splitLines(b)
	m, n := len(oldLines), len(newLines)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []DiffLine
	for i, j := 0, 0; i < m || j < n; {
		switch {
		case i < m && j < n && oldLines[i] == newLines[j]:
			out = append(out, DiffLine{Kind: "same", Text: oldLines[i]})
			i++
			j++
		case j < n && (i == m || dp[i][j+1] >= dp[i+1][j]):
			out = append(out, DiffLine{Kind: "add", Text: newLines[j]})
			j++
		case i < m:
			out = append(out, DiffLine{Kind: "del", Text: oldLines[i]})
			i++
		}
	}
	return out
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func (a *App) compare(w http.ResponseWriter, r *http.Request) {
	fromID, _ := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	toID, _ := strconv.ParseInt(r.URL.Query().Get("to"), 10, 64)
	from, err1 := a.Store.Revision(fromID)
	to, err2 := a.Store.Revision(toID)
	if err1 != nil || err2 != nil || from.PageID != to.PageID {
		http.Error(w, "Choose two revisions from the same page.", http.StatusBadRequest)
		return
	}
	a.render(w, r, "compare.html", Data{Title: "Compare revisions", CompareFrom: from, CompareTo: to, Diff: simpleDiff(from.Content, to.Content)})
}

func (a *App) restore(w http.ResponseWriter, r *http.Request) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/restore/"), 10, 64)
	rev, err := a.Store.Revision(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := a.Store.PageBySlug(rev.Slug)
	if err != nil || !a.Store.CanEdit(u, p) {
		http.Error(w, "You do not have permission to restore this page.", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		a.render(w, r, "restore.html", Data{Title: "Restore revision", Revision: rev, Page: p})
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	newp, err := a.Store.RestoreRevision(id, u.ID, strings.TrimSpace(r.FormValue("summary")))
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	_ = a.Store.SetPageCategories(newp.ID, render.ExtractCategories(newp.Content))
	a.Store.Audit(u, "revision_restore", "page", newp.Slug, fmt.Sprintf("restored revision %d", id), a.clientIP(r))
	http.Redirect(w, r, "/wiki/"+newp.Slug, http.StatusSeeOther)
}

func (a *App) protect(w http.ResponseWriter, r *http.Request, slug string) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if !a.Store.IsAdmin(u) {
		http.Error(w, "Admin only.", http.StatusForbidden)
		return
	}
	p, err := a.Store.PageBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/wiki/"+slug, http.StatusSeeOther)
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	protected := r.FormValue("protected") == "1"
	var protectErr error
	if protected && r.FormValue("protect_embedded_files") == "1" {
		var ids []int64
		for _, name := range render.EmbeddedImageNames(p.Content) {
			if file, err := a.Store.UploadByName(name, false); err == nil {
				ids = append(ids, file.ID)
			}
		}
		protectErr = a.Store.ProtectPageAndFiles(p.Slug, ids)
	} else {
		protectErr = a.Store.SetPageProtected(p.Slug, protected)
	}
	if protectErr != nil {
		http.Error(w, "Could not update page protection.", 500)
		return
	}
	a.Store.Audit(u, "page_protection", "page", p.Slug, fmt.Sprintf("protected=%t, protect_embedded_files=%t", protected, r.FormValue("protect_embedded_files") == "1"), a.clientIP(r))
	http.Redirect(w, r, "/wiki/"+p.Slug, http.StatusSeeOther)
}

func (a *App) renamePage(w http.ResponseWriter, r *http.Request, slug string) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if !a.Store.IsAdmin(u) {
		http.Error(w, "Admin only.", http.StatusForbidden)
		return
	}
	p, err := a.Store.PageBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		a.render(w, r, "rename.html", Data{Title: "Rename " + p.Title, Page: p, NewTitle: p.Title, NewSlug: p.Slug})
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	newTitle := strings.TrimSpace(r.FormValue("title"))
	newSlug := strings.TrimSpace(r.FormValue("slug"))
	if newTitle == "" {
		newTitle = p.Title
	}
	if newSlug == "" {
		newSlug = render.Slugify(newTitle)
	} else {
		newSlug = render.Slugify(newSlug)
	}
	summary := strings.TrimSpace(r.FormValue("summary"))
	np, err := a.Store.RenamePage(p.Slug, newSlug, newTitle, u.ID, summary)
	if err != nil {
		a.render(w, r, "rename.html", Data{Title: "Rename " + p.Title, Page: p, NewTitle: newTitle, NewSlug: newSlug, Summary: summary, Error: "This page could not be renamed. Check whether the title is already taken."})
		return
	}
	a.Store.Audit(u, "page_rename", "page", p.Slug, "renamed to "+np.Slug, a.clientIP(r))
	http.Redirect(w, r, "/wiki/"+np.Slug, http.StatusSeeOther)
}

func (a *App) deletePage(w http.ResponseWriter, r *http.Request, slug string) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if !a.Store.IsAdmin(u) {
		http.Error(w, "Admin only.", http.StatusForbidden)
		return
	}
	p, err := a.Store.PageBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		a.render(w, r, "delete.html", Data{Title: "Delete " + p.Title, Page: p})
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	if r.FormValue("confirm") != "delete" {
		a.render(w, r, "delete.html", Data{Title: "Delete " + p.Title, Page: p, Error: "Type delete to confirm."})
		return
	}
	if err := a.Store.DeletePage(slug, u.ID); err != nil {
		http.Error(w, "Could not delete page.", 500)
		return
	}
	a.Store.Audit(u, "page_delete", "page", slug, "deleted page", a.clientIP(r))
	http.Redirect(w, r, "/pages", http.StatusSeeOther)
}

func (a *App) users(w http.ResponseWriter, r *http.Request) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if !a.Store.IsAdmin(u) {
		http.Error(w, "Admin only.", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodPost {
		if !a.checkCSRF(w, r) {
			return
		}
		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		role := r.FormValue("role")
		email := strings.TrimSpace(r.FormValue("email"))
		if username == "" || password == "" {
			users, _ := a.Store.AllUsers()
			a.render(w, r, "users.html", Data{Title: "Users", Users: users, Error: "Username and password are required."})
			return
		}
		if err := a.Store.CreateUserWithEmail(username, password, role, email); err != nil {
			users, _ := a.Store.AllUsers()
			a.render(w, r, "users.html", Data{Title: "Users", Users: users, Error: "Could not create this account. Check the username, email, and password requirements."})
			return
		}
		a.Store.Audit(u, "user_create", "user", username, "role="+role, a.clientIP(r))
		http.Redirect(w, r, "/users", http.StatusSeeOther)
		return
	}
	users, err := a.Store.AllUsers()
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	a.render(w, r, "users.html", Data{Title: "Users", Users: users})
}

func (a *App) account(w http.ResponseWriter, r *http.Request) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if r.Method == http.MethodGet {
		msg := ""
		if u.MustChangePassword {
			msg = "You must change your temporary password before continuing."
		}
		a.render(w, r, "account.html", Data{Title: "Account", Email: u.Email, Message: msg})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	current, newpw := r.FormValue("current_password"), r.FormValue("new_password")
	if u.MustChangePassword && newpw == "" {
		a.render(w, r, "account.html", Data{Title: "Account", Email: email, Error: "You must choose a new password before continuing."})
		return
	}
	if (email != u.Email || newpw != "") && a.loginBlocked(r, u.Username) {
		http.Error(w, "Too many failed attempts. Try again later.", http.StatusTooManyRequests)
		return
	}
	changed, err := a.Store.UpdateAccount(u.ID, email, current, newpw)
	if err != nil {
		if strings.Contains(err.Error(), "current password") {
			a.recordLoginFailure(r, u.Username)
			a.Store.Audit(u, "account_password_rejected", "user", u.Username, "invalid current password", a.clientIP(r))
		}
		log.Printf("account update rejected for user %d: %v", u.ID, err)
		a.render(w, r, "account.html", Data{Title: "Account", Email: u.Email, Error: "Account update was not accepted. Check your current password, new password requirements, and email address."})
		return
	}
	a.clearLoginFailures(r, u.Username)
	if changed {
		a.Store.Audit(u, "password_change", "user", u.Username, "revoked sessions", a.clientIP(r))
		auth.ClearCookie(w, r)
		http.Redirect(w, r, "/login?changed=1", http.StatusSeeOther)
		return
	}
	a.Store.Audit(u, "account_update", "user", u.Username, "email/settings updated", a.clientIP(r))
	a.render(w, r, "account.html", Data{Title: "Account", Email: email, Message: "Account updated."})
}

func (a *App) forgotPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.render(w, r, "forgot_password.html", Data{Title: "Forgot password"})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	// Generic response for every address, and never wait for SMTP on the HTTP request.
	u, err := a.Store.UserByEmail(strings.TrimSpace(r.FormValue("email")))
	if err == nil && u != nil {
		token := randomHex(32)
		if err := a.Store.CreatePasswordResetToken(token, u.ID, time.Now().Add(time.Hour)); err == nil {
			link := strings.TrimRight(a.mail.BaseURL, "/") + "/reset-password?token=" + token
			if a.mail.Host != "" && a.mail.From != "" {
				select {
				case a.mailQueue <- resetMessage{u.Email, link}:
				default:
					log.Print("password reset queue full")
				}
			} else if a.mail.DevMode {
				log.Printf("DEV password reset link for %s: %s", u.Username, link)
			}
			a.Store.Audit(nil, "password_reset_requested", "user", u.Username, "reset requested", a.clientIP(r))
		}
	}
	a.render(w, r, "forgot_password.html", Data{Title: "Forgot password", Message: "If that email address belongs to an account, a reset link will be sent if email is configured."})
}

func (a *App) resetPassword(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if r.Method == http.MethodGet {
		a.render(w, r, "reset_password.html", Data{Title: "Reset password", ResetToken: token})
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	token = strings.TrimSpace(r.FormValue("token"))
	pw := r.FormValue("password")
	if token == "" || storage.ValidatePassword(pw) != nil {
		a.render(w, r, "reset_password.html", Data{Title: "Reset password", ResetToken: token, Error: "Invalid token or password shorter than 8 characters."})
		return
	}
	u, err := a.Store.ConsumePasswordResetToken(token, pw)
	if err != nil {
		a.render(w, r, "reset_password.html", Data{Title: "Reset password", Error: "Reset link is invalid, expired, or already used."})
		return
	}
	a.Store.Audit(u, "password_reset_completed", "user", u.Username, "password reset by token", a.clientIP(r))
	a.render(w, r, "login.html", Data{Title: "Login", Message: "Password reset. You can log in now.", ReturnTo: "/wiki/main-page"})
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (a *App) sendResetEmail(to, link string) error {
	if a.mail.Host == "" || a.mail.From == "" {
		return errors.New("SMTP is not configured")
	}
	address := net.JoinHostPort(a.mail.Host, a.mail.Port)
	dialer := net.Dialer{Timeout: 5 * time.Second}
	var conn net.Conn
	var err error
	if a.mail.Port == "465" {
		conn, err = tls.DialWithDialer(&dialer, "tcp", address, &tls.Config{ServerName: a.mail.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.Dial("tcp", address)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	c, err := smtp.NewClient(conn, a.mail.Host)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok && a.mail.Port != "465" {
		if err := c.StartTLS(&tls.Config{ServerName: a.mail.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	} else if a.mail.Port != "465" && (a.mail.Username != "" || a.mail.Password != "") {
		return errors.New("SMTP server does not support STARTTLS")
	}
	if a.mail.Username != "" || a.mail.Password != "" {
		if err := c.Auth(smtp.PlainAuth("", a.mail.Username, a.mail.Password, a.mail.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(a.mail.From); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	message := "From: " + (&mail.Address{Address: a.mail.From}).String() + "\r\n" +
		"To: " + (&mail.Address{Address: to}).String() + "\r\n" +
		"Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n" +
		"Message-ID: <" + randomHex(16) + "@" + a.mail.Host + ">\r\n" +
		"Subject: MiniWiki password reset\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\nReset your password using this link (expires in one hour):\r\n" + link + "\r\n"
	if _, err := io.WriteString(w, message); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

var errUploadCSRF = errors.New("invalid upload security token")

// streamUploadPart checks the first form field before reading upload bytes.
// This avoids ParseMultipartForm's second disk spool and limits unauthenticated
// multipart processing to a small token field.
func streamUploadPart(r *http.Request) (*multipart.Part, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, err
	}
	verified := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch part.FormName() {
		case "csrf_token":
			if verified {
				part.Close()
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(part, 513))
			part.Close()
			if readErr != nil {
				return nil, readErr
			}
			cookie, err := r.Cookie(auth.CSRFCookieName)
			if err != nil || cookie.Value == "" || len(data) > 512 || subtle.ConstantTimeCompare([]byte(cookie.Value), data) != 1 {
				return nil, errUploadCSRF
			}
			verified = true
		case "file":
			if !verified {
				part.Close()
				return nil, errUploadCSRF
			}
			if part.FileName() == "" {
				part.Close()
				return nil, errors.New("no filename provided")
			}
			return part, nil
		default:
			part.Close()
		}
	}
	return nil, errors.New("file part missing")
}

func (a *App) upload(w http.ResponseWriter, r *http.Request) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if u.Role != "admin" && u.Role != "editor" {
		http.Error(w, "Editors only", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodGet {
		ups, _ := a.Store.Uploads(100)
		a.render(w, r, "upload.html", Data{Title: "Uploads", Uploads: ups})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(10 * time.Minute))
	r.Body = http.MaxBytesReader(w, r.Body, 25<<20)
	part, err := streamUploadPart(r)
	if err != nil {
		if errors.Is(err, errUploadCSRF) {
			a.Store.Audit(a.user(r), "csrf_rejected", "request", r.URL.Path, "invalid streamed upload token", a.clientIP(r))
			http.Error(w, "Invalid security token. Reload the upload page.", http.StatusForbidden)
			return
		}
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			http.Error(w, "Upload exceeds the 25 MiB limit.", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Choose a file to upload.", http.StatusBadRequest)
		return
	}
	defer part.Close()
	tmp, err := os.CreateTemp(a.uploadDir, ".upload-*")
	if err != nil {
		http.Error(w, "Cannot create upload", 500)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	n, err := io.Copy(tmp, part)
	if err != nil || n == 0 {
		tmp.Close()
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			http.Error(w, "Upload exceeds the 25 MiB limit.", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Unable to save uploaded file.", http.StatusBadRequest)
		return
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		tmp.Close()
		http.Error(w, "Upload failed", 500)
		return
	}
	head := make([]byte, 512)
	headN, err := tmp.Read(head)
	if err != nil && err != io.EOF {
		tmp.Close()
		http.Error(w, "Upload failed", 500)
		return
	}
	ct := http.DetectContentType(head[:headN])
	name := safeFilename(part.FileName())
	if isBlockedUpload(name, ct) {
		tmp.Close()
		http.Error(w, "This file type is not allowed", 400)
		return
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		http.Error(w, "Upload failed", 500)
		return
	}
	if err := tmp.Close(); err != nil {
		http.Error(w, "Upload failed", 500)
		return
	}
	stored := fmt.Sprintf("%d-%s", time.Now().UnixNano(), name)
	path := filepath.Join(a.uploadDir, stored)
	if err := os.Chmod(tmpName, 0600); err != nil {
		http.Error(w, "Upload failed", 500)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		http.Error(w, "Upload failed", 500)
		return
	}
	if err := a.Store.AddUpload(name, stored, ct, n, u.ID); err != nil {
		os.Remove(path)
		log.Printf("upload DB insert failed: %v", err)
		http.Error(w, "Filename is already in use or the upload could not be saved.", http.StatusConflict)
		return
	}
	a.Store.Audit(u, "file_upload", "file", name, fmt.Sprintf("%d bytes", n), a.clientIP(r))
	http.Redirect(w, r, "/upload", http.StatusSeeOther)
}

func isBlockedUpload(name, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".html", ".htm", ".svg", ".js", ".mjs", ".css", ".xml", ".xhtml":
		return true
	}
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "image/svg") || strings.Contains(ct, "javascript")
}

// downloadSlotsCapacity bounds overall in-flight transfers. The per-IP
// ceiling leaves enough room for normal browsers and small offices sharing NAT,
// while preventing one client from exhausting the entire transfer budget.
const (
	downloadSlotsCapacity = 64
	downloadPerIPLimit    = 16
	// The page content limit applies to decoded UTF-8 text. URL encoding can
	// expand each byte to three bytes (%XX), so allow that wire-level overhead
	// while keeping storage.MaxPageBytes as the actual content limit.
	editFormBodyLimit = storage.MaxPageBytes*3 + (32 << 10)
)

// acquireDownload enforces both a global and per-client concurrent-download
// limit. The returned release function must be deferred on success.
func (a *App) acquireDownload(ip string) (func(), bool) {
	a.downloadMu.Lock()
	defer a.downloadMu.Unlock()
	if a.downloadByIP[ip] >= downloadPerIPLimit {
		return nil, false
	}
	select {
	case a.downloadSlots <- struct{}{}:
		a.downloadByIP[ip]++
		return func() {
			a.downloadMu.Lock()
			a.downloadByIP[ip]--
			if a.downloadByIP[ip] == 0 {
				delete(a.downloadByIP, ip)
			}
			<-a.downloadSlots
			a.downloadMu.Unlock()
		}, true
	default:
		return nil, false
	}
}

func (a *App) serveUpload(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/uploads/")
	if name == "" || name != safeFilename(name) {
		http.NotFound(w, r)
		return
	}
	if _, err := a.Store.UploadByStoredName(name, false); err != nil {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(a.uploadDir, name)
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	ct := http.DetectContentType(buf[:n])
	_, _ = f.Seek(0, 0)
	// Limit concurrent public readers and bound slow clients by size.
	release, ok := a.acquireDownload(a.clientIP(r))
	if !ok {
		w.Header().Set("Retry-After", "2")
		http.Error(w, "Too many simultaneous file downloads. Try again shortly.", http.StatusTooManyRequests)
		return
	}
	defer release()
	stat, statErr := f.Stat()
	if statErr != nil {
		http.NotFound(w, r)
		return
	}
	allowed := 60*time.Second + time.Duration(stat.Size()/(100*1024))*time.Second
	if allowed > 5*time.Minute {
		allowed = 5 * time.Minute
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(allowed))
	w.Header().Set("Content-Type", ct)
	w.Header().Set("ETag", `"`+name+`"`)
	if a.mail.RequireLogin {
		w.Header().Set("Cache-Control", "private, no-cache, must-revalidate")
	} else {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !strings.HasPrefix(ct, "image/png") && !strings.HasPrefix(ct, "image/jpeg") && !strings.HasPrefix(ct, "image/gif") && !strings.HasPrefix(ct, "image/webp") {
		w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	}
	http.ServeContent(w, r, name, time.Time{}, f)
}

func (a *App) serveDeletedUpload(w http.ResponseWriter, r *http.Request) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if !a.Store.IsAdmin(u) {
		http.Error(w, "Admin only.", http.StatusForbidden)
		return
	}
	id, _ := strconv.ParseInt(strings.Trim(strings.TrimPrefix(r.URL.Path, "/deleted-upload/"), "/"), 10, 64)
	up, err := a.Store.UploadByID(id, true)
	if err != nil || !up.Deleted {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex, noarchive")
	path := filepath.Join(a.uploadDir, up.StoredName)
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	ct := http.DetectContentType(buf[:n])
	_, _ = f.Seek(0, 0)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !strings.HasPrefix(ct, "image/png") && !strings.HasPrefix(ct, "image/jpeg") && !strings.HasPrefix(ct, "image/gif") && !strings.HasPrefix(ct, "image/webp") {
		w.Header().Set("Content-Disposition", "attachment; filename=\""+up.Filename+"\"")
	}
	http.ServeContent(w, r, up.Filename, time.Time{}, f)
}

func safeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
	if name == "" || name == "." {
		return "upload.bin"
	}
	return name
}

func (a *App) watch(w http.ResponseWriter, r *http.Request, slug string) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed.", 405)
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	p, err := a.Store.PageBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	watch := r.FormValue("watch") == "1"
	if err := a.Store.SetWatched(u.ID, p.ID, watch); err != nil {
		http.Error(w, "Could not update watchlist.", 500)
		return
	}
	a.Store.Audit(u, "watchlist_update", "page", p.Slug, fmt.Sprintf("watched=%t", watch), a.clientIP(r))
	http.Redirect(w, r, "/wiki/"+p.Slug, http.StatusSeeOther)
}

func (a *App) watchlist(w http.ResponseWriter, r *http.Request) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	items, err := a.Store.Watchlist(u.ID)
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	a.render(w, r, "watchlist.html", Data{Title: "Watchlist", Watchlist: items})
}

func (a *App) userAdmin(w http.ResponseWriter, r *http.Request) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if !a.Store.IsAdmin(u) {
		http.Error(w, "Admin only.", http.StatusForbidden)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/user/"), "/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if parts[1] == "password" && r.Method == http.MethodGet {
		target, err := a.Store.UserByID(id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		a.render(w, r, "admin_password_reset.html", Data{Title: "Confirm password reset", TargetUser: target})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	switch parts[1] {
	case "role":
		if id == u.ID && r.FormValue("role") != "admin" {
			http.Error(w, "You cannot demote your own administrator account", 400)
			return
		}
		if err := a.Store.UpdateUserRole(id, r.FormValue("role")); err != nil {
			http.Error(w, "Could not update role.", 400)
			return
		}
		a.Store.Audit(u, "user_role_update", "user", strconv.FormatInt(id, 10), "role="+r.FormValue("role"), a.clientIP(r))
	case "block":
		if id == u.ID {
			http.Error(w, "You cannot block your own account.", 400)
			return
		}
		blocked := r.FormValue("blocked") == "1"
		if err := a.Store.SetUserBlocked(id, blocked); err != nil {
			http.Error(w, "Could not update block status.", 500)
			return
		}
		a.Store.Audit(u, "user_block_update", "user", strconv.FormatInt(id, 10), fmt.Sprintf("blocked=%t", blocked), a.clientIP(r))
	case "password":
		if r.FormValue("confirm") != "1" {
			http.Error(w, "Confirm the password reset from its dedicated page.", http.StatusBadRequest)
			return
		}
		if err := a.Store.AdminResetPassword(id, r.FormValue("password")); err != nil {
			log.Printf("admin password reset rejected for account %d: %v", id, err)
			http.Error(w, "Password reset was not accepted. Verify the password requirements.", http.StatusBadRequest)
			return
		}
		a.Store.Audit(u, "user_password_reset", "user", strconv.FormatInt(id, 10), "", a.clientIP(r))
	default:
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func safeReturnTo(s string) string {
	if s == "" {
		return "/"
	}
	if !strings.HasPrefix(s, "/") {
		return "/"
	}
	if strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/\\") {
		return "/"
	}
	return s
}

func titleFromSlug(slug string) string {
	if slug == "" {
		return "Untitled"
	}
	slug = strings.Trim(slug, "-/ ")
	if slug == "" {
		return "Untitled"
	}
	namespace := ""
	rest := slug
	if i := strings.Index(rest, ":"); i >= 0 {
		namespace = rest[:i+1]
		rest = rest[i+1:]
	}
	parts := strings.Split(rest, "-")
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	title := strings.Join(parts, " ")
	if namespace != "" {
		switch strings.ToLower(strings.TrimSuffix(namespace, ":")) {
		case "user":
			namespace = "User:"
		case "user talk", "user-talk":
			namespace = "User talk:"
		}
	}
	return namespace + title
}

func (a *App) deletedPages(w http.ResponseWriter, r *http.Request) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if !a.Store.IsAdmin(u) {
		http.Error(w, "Admin only.", http.StatusForbidden)
		return
	}
	pages, err := a.Store.DeletedPages()
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	uploads, _ := a.Store.DeletedUploads()
	a.render(w, r, "deleted.html", Data{Title: "Deleted content", DeletedPages: pages, Uploads: uploads})
}

func (a *App) viewDeletedPage(w http.ResponseWriter, r *http.Request, slug string) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if !a.Store.IsAdmin(u) {
		http.Error(w, "Admin only.", http.StatusForbidden)
		return
	}
	p, err := a.Store.PageBySlugAny(slug)
	if err != nil || !p.Deleted {
		http.NotFound(w, r)
		return
	}
	html, err := render.WikiMarkdownWithImages(p.Content, a.Store.PageExists, a.imageResolver)
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	a.render(w, r, "deleted_view.html", Data{Title: "Deleted - " + p.Title, Page: p, HTML: html})
}

func (a *App) undeletePage(w http.ResponseWriter, r *http.Request, slug string) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if !a.Store.IsAdmin(u) {
		http.Error(w, "Admin only.", http.StatusForbidden)
		return
	}
	p, err := a.Store.PageBySlugAny(slug)
	if err != nil || !p.Deleted {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		a.render(w, r, "undelete.html", Data{Title: "Undelete " + p.Title, Page: p})
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	np, err := a.Store.UndeletePage(slug, u.ID, strings.TrimSpace(r.FormValue("summary")))
	if err != nil {
		log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
		http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		return
	}
	_ = a.Store.SetPageCategories(np.ID, render.ExtractCategories(np.Content))
	a.Store.Audit(u, "page_undelete", "page", slug, "undeleted page", a.clientIP(r))
	http.Redirect(w, r, "/wiki/"+np.Slug, http.StatusSeeOther)
}

func (a *App) checkUser(w http.ResponseWriter, r *http.Request) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	if !a.Store.IsAdmin(u) {
		http.Error(w, "Admin only.", http.StatusForbidden)
		return
	}
	var target *storage.User
	var events []storage.AuditEvent
	query := strings.TrimSpace(r.URL.Query().Get("username"))
	if query != "" {
		users, _ := a.Store.AllUsers()
		for i := range users {
			if strings.EqualFold(users[i].Username, query) {
				target = &users[i]
				break
			}
		}
		if target != nil {
			events, _ = a.Store.AuditForUser(target.Username, 200)
			a.Store.Audit(u, "checkuser_view", "user", target.Username, "admin viewed account activity", a.clientIP(r))
		}
	}
	a.render(w, r, "checkuser.html", Data{Title: "CheckUser", Query: query, TargetUser: target, AuditEvents: events})
}

func (a *App) uploadAdmin(w http.ResponseWriter, r *http.Request) {
	u := a.require(w, r)
	if u == nil {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/upload/"), "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	id, _ := strconv.ParseInt(parts[0], 10, 64)
	action := parts[1]
	up, err := a.Store.UploadByID(id, true)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		if action == "deleted" && r.Method == http.MethodGet {
			if !a.Store.IsAdmin(u) || !up.Deleted {
				http.NotFound(w, r)
				return
			}
			a.render(w, r, "deleted_file.html", Data{Title: "Deleted file - " + up.Filename, Upload: up})
			return
		}
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}
	if !a.checkCSRF(w, r) {
		return
	}
	switch action {
	case "rename":
		if u.Role != "admin" && u.Role != "editor" {
			http.Error(w, "Editors only.", http.StatusForbidden)
			return
		}
		name := safeFilename(r.FormValue("filename"))
		if err := a.Store.RenameUpload(id, name, a.Store.IsAdmin(u)); err != nil {
			log.Printf("file rename rejected: %v", err)
			http.Error(w, "File rename rejected. Check the name and whether the file is protected or already exists.", http.StatusConflict)
			return
		}
		a.Store.Audit(u, "file_rename", "file", up.Filename, "renamed to "+name, a.clientIP(r))
	case "delete":
		if !a.Store.IsAdmin(u) {
			http.Error(w, "Admin only.", http.StatusForbidden)
			return
		}
		if err := a.Store.DeleteUpload(id, u.ID); err != nil {
			log.Printf("file delete rejected: %v", err)
			http.Error(w, "File deletion was not allowed.", http.StatusConflict)
			return
		}
		a.Store.Audit(u, "file_delete", "file", up.Filename, "deleted file", a.clientIP(r))
	case "undelete":
		if !a.Store.IsAdmin(u) {
			http.Error(w, "Admin only.", http.StatusForbidden)
			return
		}
		if err := a.Store.UndeleteUpload(id); err != nil {
			log.Printf("file restoration rejected: %v", err)
			http.Error(w, "Cannot restore the file because the name is in use or its record is unavailable.", http.StatusConflict)
			return
		}
		a.Store.Audit(u, "file_undelete", "file", up.Filename, "undeleted file", a.clientIP(r))
	case "protect":
		if !a.Store.IsAdmin(u) {
			http.Error(w, "Admin only.", http.StatusForbidden)
			return
		}
		protected := r.FormValue("protected") == "1"
		if err := a.Store.SetUploadProtected(id, protected); err != nil {
			log.Printf("MiniWiki internal error on %s: %v", r.URL.Path, err)
			http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
			return
		}
		a.Store.Audit(u, "file_protection", "file", up.Filename, fmt.Sprintf("protected=%t", protected), a.clientIP(r))
	default:
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/upload", http.StatusSeeOther)
}

# MiniWiki v20 — Security & Reliability Remediation

This release is based on the complete v19 source archive and addresses the eight findings in `miniwiki_v19_audit.md` supplied for review. No existing SQLite schema change is required.

## Remediation matrix

| v19 audit finding | v20 remediation | Main code location |
|---|---|---|
| Unbounded `rateHits` IP map | Once-per-minute global sweep deletes expired hit histories, including IPs that never request the site again. | `internal/handlers/handlers.go`: `rateLimit` |
| Unbounded `loginFails` map | Last-attempt timestamp, time-window reset, and periodic stale-key cleanup on login requests. Locks expire instead of permanently escalating. | `handlers.go`: `pruneLoginFailures`, `loginBlocked`, `recordLoginFailure` |
| Raw FTS5 query text | User search is escaped and wrapped as a literal phrase in `MATCH` to avoid boolean/operator syntax failures; existing LIKE fallback is retained. | `internal/storage/storage.go`: `Search` |
| Spoofable forwarded IP address | Direct peer IP is used by default, ignoring `X-Forwarded-For` and `X-Real-IP`. Optional `trusted_proxies` is a strict IP/CIDR allowlist. Forwarded chains are walked right-to-left, choosing the first untrusted IP. | `internal/config/config.go`, `handlers.go`, `config.example.toml` |
| Admin password reset without confirmation | User management now links to a separate GET confirmation/form page; POST must include a CSRF token and explicit confirmation. | `templates/users.html`, `templates/admin_password_reset.html`, `handlers.go` |
| Ambiguous upload lookup | Public `/uploads/<stored-name>` uses exact stored-name lookups, not a filename OR stored-name query. Deleted records are not served. | `storage.go`: `UploadByStoredName`, `handlers.go`: `serveUpload` |
| Weak passwords created by admins | One storage-layer policy requires at least 8 Unicode characters and at most 72 UTF-8 bytes for account creation, admin password changes, and token resets. bcrypt errors are now returned rather than ignored. | `storage.go`: `ValidatePassword`, `CreateUserWithEmail`, `ChangePassword`, `ConsumePasswordResetToken` |
| Missing global audit log UI | Admin dashboard now shows the 50 most recent audit events including the actor, type, target, details, timestamp, and IP. | `handlers.go`: `admin`, `templates/admin.html` |
| Insufficient tests | Added tests for proxy spoofing and proxy chains, rate limiting and login map pruning, CSRF/permissions/admin reset flow, storage password policy, block and session invalidation, upload collisions/deletions, search and image rendering. | `internal/{handlers,storage,render}/*_test.go` |

In addition, blocking an account and removing its active sessions are now one SQLite transaction. Admin-only deleted-file responses have `Cache-Control: private, no-store` and `X-Robots-Tag: noindex, noarchive`.

## Configuration of trusted proxies

By default `trusted_proxies` is empty. MiniWiki uses the actual TCP peer IP for login throttling, global request limiting and audit logging. This is suitable when directly exposed to an untrusted network (although a properly configured HTTPS reverse proxy is still recommended for public use).

When using a local trusted reverse proxy that correctly appends the client IP to `X-Forwarded-For`:

```toml
# Only if a proxy running on localhost is the only allowed peer:
addr = "127.0.0.1:8080"
trusted_proxies = "127.0.0.1/32, ::1/128"
```

Or use `MINIWIKI_TRUSTED_PROXIES`. **Do not trust all sources** (`0.0.0.0/0`, `::/0`), or public client ranges. The proxy should replace untrusted forwarded headers and append its observed peer as appropriate. A single boolean `trusted_proxy=true` was *not* added because it would allow misconfiguration to silently reintroduce spoofing.

## Developer verification

On Ubuntu, after unzipping and `cd` into `miniwiki_v20`:

```bash
go mod tidy
go test ./...
go run ./cmd/wiki version
go run ./cmd/wiki serve
```

To compile an executable:

```bash
go build -o miniwiki ./cmd/wiki
./miniwiki serve
```

### Manual acceptance checklist

1. Admin sees the global recent activity table in `/admin`; a reader gets HTTP 403.
2. `/users` offers a reset-password **link**, not an inline one-click POST. The confirmation screen refuses passwords shorter than eight characters.
3. Five bad logins trigger temporary throttling. Blocks revoke existing user sessions, and unblocking permits new logins.
4. Uploaded images and files are retrievable by their exact public stored-name URLs; a deleted file returns 404 to non-admins; admins can use the deleted-file interface.
5. Search queries such as `AND`, `OR`, `NOT`, and unmatched quotes do not break the search page.
6. Existing page revisions, redirects, user pages, talk pages, watchlists, image embeds, delete/undelete workflows and wiki syntax still function.
7. If using a reverse proxy, configure `trusted_proxies` only for real proxy IPs.

## Verification and remaining deployment risks

Source syntax and embedded template parsing have been checked. The execution environment creating this archive could not download Go modules from `proxy.golang.org`, so a successful `go test ./...`, full compilation, and live HTTP validation remain to be performed on Ubuntu. Do not consider this a certified or exhaustive OWASP penetration test.

**Outstanding production-hardening items:** fresh installations still seed a known `admin`/`admin123` account; change this immediately and do not expose a fresh instance to the public internet first. HTTPS remains the operator's responsibility (`secure_cookies = true` behind TLS). Password-reset links can be logged if SMTP is not configured. Automated dependency vulnerability scanning, first-run admin setup, secure SQLite backup under WAL and broader access-control threat testing should follow in later releases.

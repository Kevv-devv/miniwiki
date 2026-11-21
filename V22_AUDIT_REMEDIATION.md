# MiniWiki v22 — v21.1 Audit Remediation

This release was modified from the **actual v21.1 ZIP source**. It keeps the existing project and formats.

## High-priority findings

- **2.1**: `backup` uses live SQLite `VACUUM INTO` and no longer tries to lock out the server. Server and `restore` retain exclusive advisory locks; a crash releases the OS lock automatically on Unix and Windows. Optional scheduled online snapshots: `backup_dir`, `backup_every`, `keep_last`.
- **2.2**: Shared Unicode-aware NFC slug generation (`internal/slug`) for storage and rendering. Accented and non-Latin titles survive; otherwise-empty titles get deterministic hash-derived slugs. Generated article links escape URL path segments.
- **2.3**: One-time legacy slug migration updates stored page slugs, records redirects, and rebuilds search and backlink indexes. Ambiguous title collisions abort startup instead of silently merging content. A previously interrupted migration can be repeated.
- **2.4**: Offline `miniwiki passwd [-db wiki.db] USERNAME` generates a one-time password, requires a change on next login, and revokes sessions. It requires stopping the server first.

## Medium findings

- **3.1**: SQLite enforces case-insensitive unique filenames for **active** uploads; migration renames duplicate legacy display names, preferring protected uploads, otherwise the previously visible newest upload. Stored files themselves never move. Protected file renaming is blocked for editors and permitted for admins. Undelete filename collisions return a conflict instead of silently changing links.
- **3.2**: Current password required for email changes; email and password changes run atomically via `UpdateAccount`, preventing partial writes. Failed current-password attempts are audited and throttled.
- **3.3**: Warn once if an untrusted peer sends forwarded IP headers. Session/CSRF cookies automatically become `Secure` for direct TLS or explicitly trusted HTTPS proxy connections; HSTS is sent over validated HTTPS.
- **3.4**: Upload handlers extend read deadlines and stream multipart file bodies to disk with bounded in-memory buffering. File response handlers extend write deadlines.
- **3.5**: Password-reset email adds From, Date, Message-ID, MIME headers, and supports implicit TLS on port 465. Startup warns if SMTP is configured with localhost base_url.
- **3.6**: Recent history, revision lists, all pages, category page lists, backlinks and deleted-page lists no longer load full article bodies when only list metadata is needed.
- **3.7**: Accounts created by admins must change the initial password on first login.

## Low/hygiene findings

- **4.1**: Restrict *new* usernames to 2–48 ASCII letters, digits, `_`, `-`; enforce case-insensitive uniqueness. Legacy names are not silently renamed.
- **4.2**: Escape the `return` parameter in the authentication-required redirect.
- **4.3**: Cache authenticated user for the duration of a request using the request context.
- **4.4**: SHA-256 hash reset tokens in SQLite; revoke all old plaintext reset tokens on the one-time upgrade.
- **4.5**: Audit and throttle wrong current-password attempts in Account settings.
- **4.6**: Unique stored-name uploads carry ETag and immutable cache directives (private in require-login mode).
- **4.7**: Watchlist excludes deleted pages; category listings suppress categories only referenced by deleted pages.
- **4.8**: Account email and password writes are atomic.
- **4.9**: GitHub Actions checksums/verification added and action versions pinned to commit SHA. **Outstanding:** `go.sum` cannot be generated in this sandbox because Go module DNS is blocked. Generate and commit it from Ubuntu (see below), then run `govulncheck` to guide safe dependency upgrades. Full dependency tests were not run here.
- **4.10**: Strengthened systemd sandbox options, preserving documented writable paths.
- **4.11**: Supplied `docs/UPGRADING_V21_1_TO_V22.md` with the correct archive and paths.

## Migration precautions

1. **Back up before upgrading.** The first v22 startup automatically updates the **copied** database. Keep your v21.1 original and a separate snapshot for rollback.
2. Legacy duplicate display filenames may be renamed once. The original file paths and bytes do not change.
3. Existing password-reset links are invalidated during migration. Issue new links after upgrading.
4. On legacy slug collisions, startup **intentionally stops**. Resolve ambiguous old slugs rather than guessing which page should win.
5. Admin account changes now require the initial-password workflow for newly created users.
6. The offline `passwd` command needs the server stopped; backups can run while it is live.

## Build verification

- `gofmt` succeeded.
- Source compiled with temporary substitute Go dependency modules on Linux, Windows, FreeBSD, macOS, and OpenBSD. This is only a **syntax/type check** for the project code, not proof of correct real dependencies.
- `go test ./internal/slug` passed against a minimal Unicode normalization substitute; this does not verify NFC normalization itself.
- Full `go test ./...`, `go test -race ./...`, `go mod verify`, and `govulncheck` require real modules on Ubuntu. Do not expose a deployment to the public until these pass.
- The substitute modules are **not** shipped in the archive.

## Generating go.sum on Ubuntu

```bash
cd ~/Downloads/miniwiki_go_v22_audit_remediation/miniwiki_v22
go mod tidy
go mod verify
go test ./...
go test -race ./...
go vet ./...
# To scan dependencies after installing the scanner:
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

**After running these**, retain `go.sum` in your source/Git repository; do not delete or ignore it. The complete Ubuntu upgrade guide is in `docs/UPGRADING_V21_1_TO_V22.md`.

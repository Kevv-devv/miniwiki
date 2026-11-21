> **Current security-patched beta candidate: MiniWiki v0.24.2-beta-rc.** This hotfix upgrades Goldmark and x/text after the first real Ubuntu vulnerability scan identified two reachable flaws. See [v24.2 security notes](V24_2_SECURITY_DEPENDENCY_NOTES.md), [exact Ubuntu upgrade commands](docs/UPGRADING_V24_TO_V24_2.md), and [beta acceptance](docs/BETA_ACCEPTANCE_V24_1.md). Do not publish a beta tag until Ubuntu tests and the vulnerability scan pass, and commit the generated `go.sum`.

> **MiniWiki v23:** v22 audit-hardening release. Safe scheduled-backup retention, fixed example config, preflight migration checks, native TLS, streaming uploads, Unicode filename protection, and regression tests. See [V23_AUDIT_REMEDIATION.md](V23_AUDIT_REMEDIATION.md) and [Ubuntu upgrade guide](docs/UPGRADING_V22_TO_V23.md).

# MiniWiki v21.1 — Security and data integrity release

See [`V21_AUDIT_REMEDIATION.md`](V21_AUDIT_REMEDIATION.md) for the v21 security change list and [`V21_1_TEST_FIX.md`](V21_1_TEST_FIX.md) for the v21.1 regression-test correction.

**Upgrade note:** Stop the old MiniWiki server before copying its database or uploads. Read `docs/UPGRADING_V20_TO_V21.md` before starting. On first-run or when rotating an unchanged v20 default admin password, the secure temporary password is printed in the terminal ONCE.

# MiniWiki

MiniWiki is a small self-hosted wiki written in Go with SQLite storage and server-rendered HTML.

MiniWiki focuses on easy deployment plus core wiki features:

- embedded templates/static assets
- one binary plus one SQLite database file
- CLI commands: `serve`, `check`, `backup`, `restore`, `passwd`, `version`
- `config.toml` support
- Makefile build/release targets
- Linux systemd example
- BSD rc.d example
- release targets for Linux, Windows, macOS, FreeBSD, OpenBSD, and NetBSD



## Previous: v20 security and reliability

This release remediates eight findings in the v19 audit, with additional regression tests:

- Trusted-proxy IP/CIDR allowlist (`trusted_proxies` / `MINIWIKI_TRUSTED_PROXIES`); forwarded headers are ignored unless the direct peer is trusted.
- Periodic cleanup of global rate-limit and login-throttle maps.
- Literal FTS5 search query escaping.
- Storage-enforced password length limits for administrator-created and reset accounts.
- Dedicated confirmation form for administrator password resets.
- Exact stored-filename lookup when serving uploads.
- Recent global audit events on the admin dashboard.
- Transactional account blocking/session revocation and no-cache deleted-file previews.

See [`V20_SECURITY_RELIABILITY_NOTES.md`](V20_SECURITY_RELIABILITY_NOTES.md) for details and the remaining security/deployment limitations.

## v17 governance and media highlights

- Admin soft-delete for pages with deleted-page review and undelete workflows.
- Admin-only deleted-page viewing without restoring the page.
- MediaWiki-style single-link redirects: a page containing only `[[Statistics]]` redirects to `Statistics`.
- Admin account blocking/unblocking with blocked sessions invalidated.
- Admin CheckUser-style audit review for user activity and recorded IPs.
- Wiki image embedding via `[[Image:filename.png]]`; plain `[[filename.png]]` remains a normal wiki link.
- Upload/image rename support for editors and admins.
- Admin delete, undelete, protect, unprotect support for uploads/images.

## v16 quality-of-life highlights

- page rename with automatic redirects
- improved upload gallery with image previews and copy-paste Markdown snippets
- drag-and-drop upload UI
- keyboard shortcuts: `E` to edit, `/` to search, `R` for recent changes
- safer highlighted search snippets
- additional dark-mode and layout polish

## Quick start from source

```bash
go mod tidy
go run ./cmd/wiki serve
```

Open:

```text
http://localhost:8080
```

First installation creates the `admin` username with a **random temporary password printed once in the server terminal**. Sign in and change it immediately. On upgrade from v20, an unchanged `admin/admin123` password is automatically rotated and printed once.

For a private wiki, set `require_login = true` in `config.toml`. See `docs/UPGRADING_V20_TO_V21.md`.

## Build

```bash
make build
./dist/miniwiki serve
```

## Commands

```bash
./miniwiki serve
./miniwiki serve -addr :9090 -db wiki.db
./miniwiki backup
./miniwiki restore -from backups/wiki-YYYYMMDD-HHMMSS
./miniwiki version
```

## Config

Copy:

```bash
cp config.example.toml config.toml
```

Example:

```toml
addr = ":8080"
db_path = "wiki.db"
site_name = "MiniWiki"
secure_cookies = false
```

Environment variables override the config file:

```bash
MINIWIKI_ADDR=:8080
MINIWIKI_DB=wiki.db
MINIWIKI_SITE_NAME=MiniWiki
MINIWIKI_SECURE_COOKIES=1
MINIWIKI_TRUSTED_PROXIES="127.0.0.1/32, ::1/128"  # ONLY if those IPs are actual reverse proxies
```

Set `secure_cookies = true` only when using HTTPS.

## Release builds

```bash
make release
```

Targets include Linux, Windows, macOS, FreeBSD, OpenBSD, and NetBSD on amd64/arm64 where supported.

Note: MiniWiki uses `modernc.org/sqlite`, a pure-Go SQLite driver. This keeps builds CGO-free and makes cross-compiling Linux, Windows, macOS, FreeBSD, OpenBSD, and NetBSD releases much easier.

## Deployment

See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).


## Pure-Go SQLite

MiniWiki uses `modernc.org/sqlite`, so the database remains a standard SQLite file while the Go build stays CGO-free. This makes cross-platform release builds much simpler than using the C SQLite binding.

## v12 additions

MiniWiki now includes basic production-readiness features:

- file uploads stored in `uploads/`
- page namespaces using colon-style titles such as `User:Kevin`, `Help:Editing`, and `Project:About`
- watchlists for logged-in users
- account page for email and self-service password changes
- optional email-based password resets
- additional tests around migrations, seeding, permissions, watchlists, and filename safety

Password resets require users to add an email address on `/account`. Configure SMTP with `config.toml` or environment variables. If SMTP is not configured, reset links are logged only when `dev_mode=true` and SMTP is disabled for local development. Set `dev_mode = true` or `MINIWIKI_DEV_MODE=1` to log reset links to the server console during **local development only**. They are not shown in the browser.

## v13 audit fixes

This release responds to the v0.12 product audit:

- SQLite WAL mode, `synchronous=NORMAL`, `busy_timeout`, and serialized writes
- lightweight global request rate limiting
- server-side upload MIME detection
- dangerous upload types blocked: HTML, SVG, JavaScript, CSS
- non-image uploads served as attachments with `X-Content-Type-Options: nosniff`
- LCS-based line diff instead of index-paired diff
- admin page deletion
- templates parsed once at startup and cached
- expired session cleanup at startup and daily thereafter
- search highlight CSS and small UI polish



## v14 UI/UX pass

This version adds a responsive navigation shell, dark mode, an editor toolbar, live draft preview, word/character counts, a sticky edit action bar, automatic table of contents, and broader visual polish.

## v16 quality-of-life pass

This version adds page rename with redirects, improved uploads with image previews and copy-paste Markdown snippets, drag-and-drop upload UI, keyboard shortcuts, safer highlighted search snippets, and additional dark-mode/layout polish.


## v18 highlights

MediaWiki-like image syntax, search redlinks, user pages / user talk pages, and a consolidated admin dashboard.


## v19 security and UI pass

See `V19_SECURITY_UI_NOTES.md` for the OWASP Top 10 review, security hardening notes, user settings cleanup, edit-summary suggestions, and moderation verification.


## MiniWiki v23 notes

* Configuration values support inline comments outside quotes. `config.example.toml` is tested as part of `go test ./...`.
* Scheduled backups are written under `backup_dir/automatic/` and retention only removes snapshots carrying a MiniWiki-managed marker. Manual backups are never selected for pruning.
* `miniwiki check -db wiki.db` reads an existing database without migrating it; use before upgrading.
* `miniwiki passwd admin` works offline even when a pending data migration needs repair.
* For HTTPS without a proxy, use `serve -tls-cert cert.pem -tls-key key.pem` and an appropriate listen address. Use publicly trusted certificates for internet-facing deployments.
* Upload files remain private (mode 0600). If a backup service needs file access, grant explicit permissions instead of making uploads world-readable.
* Dependencies require `go mod tidy` on first build; commit the generated `go.sum` to your Git repository. This archive does not claim to include verified third-party dependency checksums.
* See `V23_AUDIT_REMEDIATION.md` and `docs/UPGRADING_V22_TO_V23.md` for limitations and upgrade instructions.


## v24 stabilization notes

MiniWiki v24 adds fail-closed configuration validation, draft-preserving save errors, per-client file download limits, and additional backup safeguards. See [`V24_STABILIZATION_NOTES.md`](V24_STABILIZATION_NOTES.md) and [`docs/UPGRADING_V23_TO_V24.md`](docs/UPGRADING_V23_TO_V24.md). Before calling the release production-ready, run real tests, verify dependencies and perform the browser acceptance checklist.

## License

MiniWiki is free and open-source software distributed under the
GNU Affero General Public License, version 3 only (AGPL-3.0-only).

See [LICENSE](LICENSE) for the complete license and
[NOTICE](NOTICE) for copyright information.

MiniWiki welcomes contributions and improvements under the
terms of the GNU AGPLv3.

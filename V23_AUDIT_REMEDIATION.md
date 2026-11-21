# MiniWiki v23 — v22 Audit Remediation and Verification

**Source baseline:** `miniwiki_go_v22_audit_remediation.zip`; changes made in place to that source tree. Version: `0.23.0-dev`.

## High-priority issues

- **H1 — broken example configuration:** `config.example.toml` no longer contains an inline comment in `backup_every`. The configuration reader now handles inline comments **outside** quoted strings and matched double/literal single quotation marks without removing significant SMTP-password characters. `config.LoadChecked` reports malformed values with filename and line number. `TestShippedExampleParses` and `TestInlineCommentsAndSignificantQuoteCharacters` cover this.
- **H2 — unsafe retention:** automatic snapshots live under `backup_dir/automatic/`, separate from manual snapshots. Retention requires a strictly formatted timestamp directory **and** the `wiki.db`, `README.txt`, and `.miniwiki-automatic` marker files with the expected marker bytes. Misconfigured paths encompassing the database, uploads, or working directory (or passing through symlinks) are rejected. Stale temp folders are cleaned only if they carry the v23 `.miniwiki-work` ownership marker and are >24 hours old. After a restart, an overdue automatic snapshot runs without waiting for the full interval or delaying web startup. Admin shows the last backup status/time. Hard links to unchanged immutable uploaded files reduce successive backup storage after comparing hashes; fallback copies preserve timestamps. `v23_test.go` tests retention ownership/path guards/overdue detection. Backup files and account databases remain highly sensitive; use a private backup directory.

## Medium issues

- Public downloads: a 32-download concurrent gate and size-scaled (60 s–5 min) write deadlines, while avoiding the original overly permissive 10-minute window.
- Deleted-file caching: `no-cache, must-revalidate` with ETag, with a DB availability check *before* conditional responses. A deleted upload is no longer advertised as immutable for one day; already downloaded copies cannot be retroactively erased.
- Errors: internal handler/storage exceptions are logged server-side; account updates, uploads, file operations, user creation, and most HTTP 500 responses return neutral user-facing messages. No raw unique-index names are reflected in those responses.
- Usernames: `Authenticate` and `UserIDByName` use the case-insensitive collation already used by username uniqueness.
- Migrations: `miniwiki check [-db PATH]` opens the existing DB **read-only**, lists duplicate usernames/emails/active upload names and active slug collisions, and reports pending title updates. It performs no data migrations. `miniwiki passwd USER` no longer calls `Migrate()`, so admin recovery is independent of migration problems. Deleted pages no longer block the v22/v23 slug conversion; colliding deleted slugs move to recoverable `deleted-page-ID` tombstones.
- TLS: `serve -tls-cert /path/to/cert.pem -tls-key /path/to/key.pem` supports native HTTPS using Go's `http.Server.ListenAndServeTLS`. Both flags are required. Secure cookies activate for direct TLS automatically. TLS certificate management is the operator's responsibility; the reverse-proxy deployment remains supported.
- Tests: configuration, scheduled-backup safety, Unicode upload names, login, punctuation slugs, CSRF-streaming, embedded file extraction, and HTTPS headers have new regression tests.

## Hygiene improvements

- `C++` becomes `c-plus-plus`, `C#` becomes `c-sharp` rather than colliding with `C`. A one-time migration updates affected existing v22-derived slugs and maintains redirects; custom slugs remain unchanged.
- Active uploaded file names are compared with Unicode-aware `strings.EqualFold` after NFC normalization, in serial SQLite write transactions. Historical Unicode duplicates are resolved once with protected-first/newest-first precedence. Files can be renamed or restored only if the new active display name is unique.
- HSTS uses an initial one-day policy rather than immediately locking clients into a year.
- Upload streaming checks the CSRF field first and then streams the file to a single temporary destination, eliminating a second multipart disk-spool, while retaining the 25 MiB total request limit. Oversize responses use HTTP 413.
- An optional administrator checkbox protects files referenced by `[[Image:...]]` / `[[File:...]]` in the same transaction as protecting the page. It does **not** automatically unprotect files when the page is unprotected.
- `RenameUpload` now has an explicit `allowProtected bool` argument.
- CI build matrix now matches the 11 `make release` targets. `govulncheck` is version-pinned to `v1.10.0` for a modern Go toolchain.
- Uploaded files remain mode `0600` by design. Provide an explicit ACL to separate backup users rather than changing the secure default.

## What is **not** claimed fixed or verified

- **`go.sum` is not in this archive.** External Go module downloads are blocked in the construction environment. On Ubuntu, run `go mod tidy`, `go mod verify`, `go test -race ./...`, `go vet ./...` and `govulncheck ./...`; commit the resulting `go.sum` to the Git repository. Don't treat offline compilation against temporary stubs as a full integration test.
- **Pinned dependency versions remain as in v22.** Actual vulnerability scanning must determine safe, compatible updates before bumping them; blindly upgrading Go modules could break existing Ubuntu versions.
- New/renamed titles that collide under NFKC compatibility normalization are rejected and preflight reports existing NFKC collisions. Unicode homoglyph attacks (e.g. cross-script Cyrillic/Latin impersonation) cannot be solved reliably with NFC or NFKC alone. Administrators should review lookalike titles; a full Unicode confusables-skeleton implementation is future work. Punctuation-derived collisions now fail visibly rather than overwriting pages.
- An already-cached or saved file is outside the server's control, even after deletion. Downloads now revalidate on subsequent requests.
- No load test or adversarial penetration test has been conducted. Private wiki mode (`require_login=true`) is recommended for sensitive data.
- CI has not been executed against a remote GitHub repository here. The build matrix and static compilation checks do not prove runtime support on every BSD target.

## Local checks performed here

- `gofmt` and Go syntax/type compilation of the entire tree and all test files against **temporary dependency stand-ins** (not shipped).
- Real `go test -race ./internal/config` passed; that package uses only the standard library.
- Real executions of retention/path guard/overdue tests and HTTPS-header tests using the temporary stand-ins passed.
- Full Go module tests and `govulncheck` **remain unverified** until Ubuntu CI/testing is run.

See `docs/UPGRADING_V22_TO_V23.md` for safe, explicit Ubuntu commands. The recommended order is: stop v22, test v23 source, run a WAL-safe snapshot, `check` the old database, restore the snapshot into v23, then serve and run browser acceptance tests.

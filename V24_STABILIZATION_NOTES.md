# MiniWiki v24 — Beta stabilization / v23 audit remediation

This release is built directly on the v23 source tree. It addresses findings in `miniwiki_v23_audit.md` and keeps the established wiki features unchanged.

## Changes

- **Draft-preserving editor:** storage now reports expected title collisions, deleted-page saves and size-limit errors with typed sentinel errors. The editor returns an appropriate status while retaining the page title, content, edit summary and revision information. Unexpected save errors and a page deleted while editing also retain the submitted draft rather than replacing it with an error-only page.
- **Fail-closed configuration:** invalid booleans, unknown keys, malformed lines, unsupported sections and invalid `keep_last` values stop startup. The same checks apply to environment overrides (including `MINIWIKI_REQUIRE_LOGIN`). A typo must not silently turn a private wiki into a public wiki.
- **Downloads:** concurrent downloads remain globally limited to 32 and are additionally limited to 4 per originating client IP; release functions clean up both counters. Trusted proxy handling is shared with existing request throttling.
- **Automatic backup safeguards:** existing symlinks in parent paths are resolved (normal on FreeBSD/macOS); a symlink at the destination itself remains forbidden. Snapshot creation and retention result fields are recorded separately so a retention failure does not misreport the snapshot as unsaved.
- **Migration integrity:** v23's `schema_migrations` completion marker is recorded after rebuilding both derived indexes, so a rebuild failure cannot mark the migration complete.
- **Regression tests:** added `internal/config/v24_test.go`, `cmd/wiki/v24_test.go` and `internal/handlers/v24_test.go`; testing covers configuration typos/overrides, backup-path symlink behavior/status, download-cap exhaustion, and draft-preservation scenarios.

## Deliberate non-changes / outstanding verification

- Third-party Go dependencies are unchanged from v23. `go.sum` cannot be generated authentically in this offline build environment. **Run `go mod tidy`, `go mod verify`, `go test -race ./...`, and `go vet ./...` on Ubuntu** and commit the real generated `go.sum` to the Git repository. Run `govulncheck ./...` with an appropriate, trusted installed scanner before a production deployment. No clean vulnerability scan is claimed here.
- Dependency-independent and stubbed offline compile/test checks are not substitutes for tests against SQLite, Goldmark, bcrypt, and Unicode normalization. The new handler/database tests require real dependencies on Ubuntu.
- Unicode confusables (cross-script lookalikes) are *not* fully solved: a correct UTS #39-compatible confusable policy would need dedicated data and careful migration. Existing punctuation-derived slug collisions may still exist. Do not present MiniWiki as covering all Unicode spoofing cases.
- Download IP throttles assume a correctly configured `trusted_proxies` list. Verify that setup before internet exposure.
- Automatic snapshots share unchanged upload files via hard links; corruption of a shared inode affects all linked snapshots. Independent offsite/copy-only backups are recommended. Hashing both files for deduplication is intentionally retained to verify byte equivalence at the cost of backup time. Backup files contain user/session-related data and must be kept private.
- v22 stored scheduled backups in `backups/wiki-*`. v23/v24 rotate only managed snapshots under `backups/automatic/`. Inspect and manually archive/delete older v22 backups *after* verifying them; automated retention will not touch them.
- For native TLS, v24 still loads certificate/key material at server start. Reload renewed certs by restarting the MiniWiki service after renewal, or terminate TLS in a proxy that handles automatic reloads.
- Image lookup for uncommon Unicode filenames may scan the uploads table; a future indexed normalized name column could optimize very large file libraries, after careful migration planning.

## Ubuntu upgrade

See `docs/UPGRADING_V23_TO_V24.md` for the exact preservation, backup, testing and startup commands.

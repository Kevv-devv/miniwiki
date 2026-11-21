# MiniWiki v0.24.1-beta — final audit follow-up

This focused beta-candidate update is based on the v24 final audit. It does not
change the SQLite schema, file layout, user roles, or wiki syntax.

## Changes

1. Concurrent file serving: the global transfer budget increases from 32 to
   64 and the per-client-IP budget from 4 to 16, so a normal page with many
   images is far less likely to receive a spurious denial. Saturated requests
   respond with HTTP 429 and `Retry-After: 2`, not HTTP 503. Both limits
   continue to release slots after requests complete. A reverse proxy must
   be in `trusted_proxies` or its entire user population shares one client IP.
2. Empty environment overrides are treated as unset. This applies to
   `MINIWIKI_REQUIRE_LOGIN`, `MINIWIKI_SECURE_COOKIES`, `MINIWIKI_DEV_MODE`,
   and `MINIWIKI_KEEP_LAST`. Invalid **nonempty** values continue to fail
   startup rather than silently disabling a security setting. A value in
   `config.toml` remains authoritative when the corresponding environment
   variable is blank.
3. URL-encoded page edits may require up to three times as many bytes on the
   wire as the decoded UTF-8 content. The edit-form request ceiling is now
   three times `MaxPageBytes` plus 32 KiB for metadata; the storage layer still
   rejects decoded page bodies above 2 MiB. This avoids rejecting valid CJK
   content simply because of `%XX` form encoding.
4. Added tests for environment overrides, global/per-IP quotas, the HTTP
   429 response, and a Unicode edit that exceeded the old wire-body cap.
5. CI now **requires a committed `go.sum`** and checks that `go mod tidy`
   does not change committed dependency files; tests/builds use read-only
   module mode. This gate will intentionally fail until `go.sum` and any
   resulting changes to `go.mod` are committed from the verified Ubuntu build.

## Verification and release gates

The source has been formatted and compiled with temporary offline dependency
substitutes, and the real standard-library-only config tests pass. **This is
not equivalent to testing against SQLite, Goldmark, bcrypt, and x/text.** The
full `go test -race ./...`, `go vet ./...`, dependency checks, and a
`govulncheck ./...` scan must still run on a connected development machine.

This source archive **does not include a generated `go.sum`**. The v24
installation on Ubuntu should already contain a verified `go.sum`; copy that
file into v24.1, run `go mod tidy && go mod verify`, and commit `go.mod` and
`go.sum` together before publishing GitHub releases.

The known issues inherited from v24 are unchanged: hard-linked snapshots share
inodes, older v22 backup folders are not automatically retained/pruned by the
new manager, native TLS certificates require a server restart after renewal,
very large unusual-Unicode upload-name lookups can scan stored files, and
visually confusable Unicode titles beyond compatibility-equivalent forms are
not comprehensively detected. Test scheduled backup retention in a safe
staging instance and keep a separate offsite backup.

**Beta readiness is conditional on** the full Ubuntu test run, vulnerability
scan and browser acceptance checklist. See `docs/UPGRADING_V24_TO_V24_1.md`
and `docs/BETA_ACCEPTANCE_V24_1.md`.

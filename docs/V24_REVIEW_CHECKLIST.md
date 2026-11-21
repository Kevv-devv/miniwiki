# MiniWiki v24 — focused independent review / acceptance checklist

Audit the *actual Go source* (not only this document), and check the v23 audit regressions:

1. `internal/handlers/handlers.go`: Do rejected saves keep title/content/summary/base revision? Are 409, 413 and 500 statuses appropriate? Test concurrent deletion and missing base revision. Check response-template safety.
2. `internal/config/config.go`: Unknown keys, invalid booleans (`require_login = ture`, `secure_cookies = ture`), bad `MINIWIKI_*` overrides, bad `keep_last` and malformed config lines must terminate startup. Test example file compatibility.
3. Download gate: Test 4 parallel connections by the same true client IP (5th denied), a different client still permitted, and global 32 slot exhaustion. Confirm all paths release their counters even on errors.
4. Backups: Verify final symlink destination rejection but parent symlink acceptance (FreeBSD `/home`, macOS `/var`). Verify backup path overlaps and that only owned snapshot names are pruned. Check independent snapshot/retention success reporting.
5. Migration `migrateV23`: completion marker only after derived backlink/search rebuild. Consider interruptions and idempotent reruns.
6. New v24 tests and preexisting test suite: run `go mod tidy`, `go mod verify`, `go test -race ./...`, `go vet ./...` against real modules. Verify checksum file in Git; record a `govulncheck ./...` output.
7. Browser acceptance: accounts, redirects, Unicode slugs, image embed, deletion/recovery, edit-conflict draft preservation, HTTPS, watchlists, and restore.

**Outstanding known risks:** Unicode UTS #39 confusable detection, large-upload index performance, strong independent/offsite backup strategy, and TLS certificate reloads. No unverified scan is declared clean.

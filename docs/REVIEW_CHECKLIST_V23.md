# MiniWiki v23 — Independent review and Ubuntu acceptance checklist

This is a checklist for a fresh independent audit. Read the source and test failures; do not assume a status is passed merely because a note says it was addressed.

## Build and static checks (real dependencies)

```bash
go mod tidy
go mod verify
go test -race ./...
go vet ./...
go build -o miniwiki ./cmd/wiki
./miniwiki version
# Optional with a compatible current Go toolchain:
go install golang.org/x/vuln/cmd/govulncheck@v1.10.0
govulncheck ./...
```

Inspect the freshly generated `go.sum` and commit it with `go.mod` to Git. Run these checks before exposing the application. The zip does not include a verified `go.sum`.

## Database migration and recovery

1. On a **copy** of a v22 database, run `./miniwiki check -db /path/to/wiki.db`. Confirm it prints no destructive changes and that the database is unchanged. Insert a duplicate legacy username or simulated C++/C# slug collision into a separate test copy and confirm the preflight detects it.
2. Test `./miniwiki passwd -db /path/to/wiki.db admin` with the server stopped. Verify the temporary password is emitted once and existing sessions become invalid.
3. Start on a backup of a v22 database with `C++`, `C#`, accented titles and deleted pages. Verify redirects and histories survive. Ensure deleted titles cannot be accessed anonymously through revision/diff routes.
4. Create `Alice`, log in as `alice`, and verify a blocked account cannot log in. Confirm a new account must change its password.

## Backups

1. Make a manual backup while the server is running. Check both `wiki.db` and `uploads/` are present and the DB passes `PRAGMA integrity_check`.
2. Enable `backup_every="1h"`, verify auto backups appear only in `backups/automatic/`, and inspect Admin's last-run result.
3. In a **test** backup directory, put `wiki-notes/`, `wiki-20261119-010203.000000000/` without a valid marker, and managed snapshots. Trigger retention: only older owned snapshots may be pruned.
4. Restart a server with overdue backup schedule and verify it starts serving requests while an overdue backup runs; verify periodic backups are not indefinitely delayed by restarts.
5. Verify that repeated snapshots hard-link identical uploaded files on supported filesystems; on a filesystem without hard links the fallback copy should still work.
6. Try an unsafe `backup_dir` such as `.` or the upload directory on a test wiki. Startup must refuse to configure automatic deletion.

## Upload and HTTP security

1. Upload an image, embed with `[[Image:logo.png]]`, and link with `[[logo.png]]`; confirm behavior differs as intended.
2. Attempt another upload with an identical active filename, including case and Unicode variations (e.g., `École.png` vs `école.png`); it must be rejected. Rename/undelete must also reject collisions.
3. Protect a page with the embedded-file checkbox enabled. Confirm the image is protected and cannot be renamed by editors.
4. Upload more than 25 MiB and verify HTTP 413 without saving an orphaned file. Missing/reordered CSRF fields must be rejected.
5. Check file responses include ETag and revalidation cache headers. Delete an uploaded file, then use the same URL: it must return 404 even with `If-None-Match`.
6. Simulate slow simultaneous public downloads; the concurrency gate must reject requests beyond its limit and the read/write deadlines must eventually close slow requests.
7. Test native TLS using `serve -tls-cert/-tls-key` and a trusted certificate in a private test environment. Verify Secure cookies and a one-day HSTS header. Plain localhost HTTP must not emit HSTS.
8. Attempt intentionally malformed configuration quoting and the shipped example config. Quoted SMTP secrets must preserve trailing quote characters.

## Limitations to assess honestly

- Test full dependencies and actual SQL behavior; offline stand-ins do not replicate SQLite, Goldmark or bcrypt.
- Confirm the `modernc.org/sqlite` version works on all 11 release architectures; a successful cross-compile is not an operating-system integration test.
- Run `govulncheck` with a modern Go toolchain, determine required dependency upgrades, and ensure checksums are committed.
- Consider whether compatibility-based Unicode collisions and cross-script confusable titles require a full UTS #39 skeleton policy in a future release.
- Review TLS certificate renewal/deployment and backup key/material handling for production installations.

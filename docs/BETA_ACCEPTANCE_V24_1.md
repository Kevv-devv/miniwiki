# MiniWiki v0.24.1-beta — acceptance checklist

Run on a staging installation containing only non-sensitive test data before
exposing MiniWiki to the public. Record the results and any platform-specific
errors rather than marking unchecked items as passed.

1. **Build and security:** `go mod tidy`, `go mod verify`, `go test -race ./...`,
   `go vet ./...`, and `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`
   complete successfully. Commit the generated `go.sum`.
2. **Many images:** Load a page with 10–20 embedded images. Verify all images
   display, reload/revalidate correctly, and no spurious 429 occurs in normal
   use. Test behind your actual reverse proxy if applicable.
3. **Transfer saturation:** One source IP holding 16 concurrent transfers
   receives HTTP 429 with `Retry-After: 2` for additional transfers. A second
   client IP must retain capacity. Check that counters return to zero after
   disconnects. Do not perform a load test against a production host.
4. **Environment configuration:** Set `MINIWIKI_REQUIRE_LOGIN=` and
   `MINIWIKI_KEEP_LAST=` to empty; the config file should still apply. Set
   `MINIWIKI_REQUIRE_LOGIN=ture` in staging; startup must fail with a useful
   diagnostic. Unset it afterward.
5. **Unicode edits:** Save a moderately large page containing CJK text. Its
   decoded content may be below 2 MiB while the URL-encoded POST exceeds 2
   MiB; saving should succeed. Saving decoded content above 2 MiB should
   produce a size warning with the draft preserved in the editor.
6. **Edit conflicts and Unicode titles:** Submit a draft for a visually
   equivalent fullwidth title when the canonical title already exists;
   expect the editor back, draft intact, and a conflict message.
7. **Deleted uploads:** Fetch an image and note its ETag, delete it as admin,
   then fetch with `If-None-Match`. Expect 404, not cached 304.
8. **Scheduled backups:** In staging set `backup_every = "1m"`,
   `keep_last = 2`, and use a dedicated backup directory. Confirm a snapshot
   appears in `backups/automatic/`, appears on `/admin`, and retention does
   **not** remove unrelated `wiki-notes/` or manual snapshots.
9. **Restore and crash recovery:** Restore a staging backup and verify users,
   edits and images. Verify the server restarts after forced termination
   without a stale lock, but do not kill the live service during active edits.
10. **User workflow:** Confirm first login/change-password, block/unblock,
    watchlist, private mode, redirect, image embedding, admin file recovery,
    and admin-only access checks.
11. **Production prerequisites:** Ensure HTTPS, correct trusted proxy CIDRs,
    appropriately configured `require_login`, an offsite independent backup,
    and saved recovery credentials. Native TLS certificates need a server
    restart after renewal.

## Go dependency and browser gaps

Source tests are not a substitute for end-to-end testing. The v24.1 source
archive does not contain a generated `go.sum`; supply it from a connected
Ubuntu build and commit it. Vulnerability scanning depends on current
advisory data and must be repeated periodically.

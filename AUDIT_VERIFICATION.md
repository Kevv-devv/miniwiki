# MiniWiki v13.2 Audit Verification Notes

This release is a stabilization pass over v13.1, focused on Claude's MiniWiki v0.12 audit findings.

## Addressed in v13/v13.1/v13.2

- SQLite WAL mode: `storage.Open` enables `journal_mode=WAL`, `synchronous=NORMAL`, `busy_timeout`, and serialized writes.
- Global request rate limiting: `handlers.App.rateLimit` wraps the mux, excluding static/upload file serving.
- Upload MIME safety: upload handling uses `http.DetectContentType`; dangerous active web file types/extensions are blocked.
- Upload serving safety: non-image uploads are served with `Content-Disposition: attachment`.
- Diff quality: line-position diff was replaced with an LCS-style line diff.
- Page delete: admin-only delete route and confirmation template added.
- Backlink indexing: `page_links` table added and rebuilt on migration; page saves update links transactionally.
- Template caching: templates are parsed at application startup and stored in `App.templates`.
- Session cleanup: expired sessions are deleted at startup and periodically every 24 hours.
- Password-reset dev UX: `dev_mode = true` or `MINIWIKI_DEV_MODE=1` shows the reset link in the browser when SMTP is not configured, while still logging it.
- README/version cleanup: project docs now identify this as v13.2.

## Not changed intentionally

- Revision storage still stores full content snapshots. This is acceptable for the project's current personal/small-team niche and keeps rollback/simple history reliable.
- Backups remain SQLite-file based.
- Default admin remains `admin / admin123`; documentation tells users to change it after first login.

## Local verification commands

```bash
go mod tidy
go test ./...
go run ./cmd/wiki serve
```

Then manually test: login, edit/save, history/diff, search, upload, watchlist, delete, account password change, and forgot-password dev mode.

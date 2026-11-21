# MiniWiki v22 — Audit

**Method:** static read of the v22 source, diffed against v21.1 (storage, handlers, `cmd/wiki/*`, config, auth, slug package, CI, systemd unit, upgrade guide, tests). I could not run `go build` / `go test` here (no Go toolchain, module hosts blocked), and the v22 notes say the author's environment compiled against *stub* dependencies only. So **no real build or test run has happened for v20 → v22.** "✅" below means "the code does what the notes say", not "tested". Items I couldn't confirm are marked *(unverified)*. One finding (H1) I reproduced by re-implementing the parser logic.

---

## 0. Corrections to my v21.1 audit

- I said `Watchlist` still showed deleted pages' titles. **That was wrong**: the v21.1 query already had `p.deleted_at IS NULL` (v22 didn't change it). The only real residual was category *names* that existed solely for deleted pages, which v22 now hides.
- Everything else I reported against v21.1 stands.

---

## 1. Did v22 fix the v21.1 findings?

Mostly yes, and the design choices are sound. All 21 items have an implementation; the exceptions are noted.

| v21.1 item | Verdict | Notes |
|---|---|---|
| 2.1 backups need downtime / stale lock | ✅ | Backup is now lock-free (`VACUUM INTO`, uploads copied after the DB). Server/`restore`/`passwd` use `flock` (Unix) / `LockFileEx` (Windows), so a crash can't leave a stuck lock. Scheduled backups added. See H2 for hardening. |
| 2.2 non-ASCII slugs | ✅ | New `internal/slug`: NFC, `unicode.IsLetter/IsNumber/IsMark`, hash fallback (`untitled-<sha256[:4]>`). Shared by storage and render. |
| 2.3 legacy slug migration | ✅ | One-time, transactional, staged through temp slugs, adds redirects, rebuilds indexes, safe to repeat. Aborts on collisions (see 3.5). |
| 2.4 admin recovery | ✅ | `miniwiki passwd USER`: random one-time password, forced change, sessions revoked, holds the lock. |
| 3.1 upload hijack / protected rename | ✅ | Partial unique index on active `lower(filename)`; legacy duplicates renamed once (protected first, else newest); `RenameUpload` enforces `protected` for non-admins; lookup prefers display-name match. |
| 3.2 email change re-auth | ✅ | `UpdateAccount`: one transaction, current password required for email *or* password change, failures audited and counted by the throttle. |
| 3.3 proxy trap / cookies | ✅ | One-time warning for forwarded headers from untrusted peers; `Secure` cookies auto-enabled for TLS or `X-Forwarded-Proto` from a *trusted* proxy only; HSTS on validated HTTPS. |
| 3.4 timeouts vs uploads | ✅ / ⚠️ | Uploads stream to a temp file with extended read deadline. Download deadline extension creates a new exposure (3.1 below). |
| 3.5 reset email | ✅ | From/To (via `mail.Address`), Date, Message-ID, MIME headers; implicit TLS on 465; localhost-`base_url` warning. |
| 3.6 list views loading bodies | ✅ | `Recent`, `Revisions`, `AllPages`, `Backlinks`, category lists select `''`; no template reads `.Content` on those views. |
| 3.7 admin-created users | ✅ | `must_change_password=1` for every `CreateUserWithEmail`. |
| 4.1 username policy | ✅ | 2–48 ASCII `[A-Za-z0-9_-]` for new names; legacy names untouched; case-insensitive unique index. |
| 4.2–4.8 | ✅ | Escaped `return`; per-request cached user; reset tokens SHA-256 hashed (old ones deleted); atomic account update; immutable cache headers with ETag; category suppression. |
| 4.9 supply chain | ⚠️ | Actions pinned to commit SHAs, `go mod verify` added. **`go.sum` is still not in the repo**, so `verify` only checks what `go mod tidy` just fetched. |
| 4.10 systemd | ✅ *(unverified)* | `ProtectSystem=strict`, `PrivateDevices`, `RestrictAddressFamilies`, `MemoryDenyWriteExecute`. Run `systemd-analyze security` and do a real start; MDWE is the one most likely to surprise. |
| 4.11 docs | ✅ | New `UPGRADING_V21_1_TO_V22.md` with correct paths (but see 4.6). |

---

## 2. High priority

### H1. The shipped `config.example.toml` makes the server refuse to start
The example contains:

```toml
backup_every = ""  # e.g. "24h"; empty disables scheduled backups
```

`applyFile` has no inline-comment handling: it cuts at `=`, trims whitespace, then `strings.Trim(v, "\"'")`. I reproduced that logic on this exact line and the resulting value is `  # e.g. "24h"; empty disables scheduled backups`, which is non-empty and not `"0"`. `serve` then calls `time.ParseDuration` on it and exits with **`invalid backup_every`**. README and `docs/DEPLOYMENT.md` both tell people to `cp config.example.toml config.toml`, so **the documented install path fails.**

The same hand-rolled parser also strips quote characters from *both ends* of every value, so an SMTP password that ends in `'` or `"` is silently altered.

**Fix:** remove the comment from that line now; then either handle inline comments and matched quotes properly or use a real TOML parser. Add a test that loads `config.example.toml` and asserts the defaults. This is the kind of bug a single test would have caught.

### H2. Scheduled-backup retention can delete the wrong directories (and has smaller gaps)
`rotateSnapshots` `RemoveAll`s every **directory whose name starts with `wiki-`** in `backup_dir`, oldest first, once more than `keep_last` exist. Because the default `backup_dir` (`backups`) is also where manual `miniwiki backup` writes, manual snapshots are pruned too, and any misconfiguration (`backup_dir = "."`, `~`, a shared folder) can delete unrelated folders such as `wiki-notes`.

**Fix:** only delete entries matching `^wiki-\d{8}-\d{6}(\.\d{9})?$` that also contain the `wiki.db` + `README.txt` markers; refuse a `backup_dir` that equals or contains the DB, uploads dir or working directory; keep manual backups in a separate subfolder.

Related gaps in the same feature:
- **The timer restarts with the process.** The first backup happens `backup_every` after startup, so a server restarted more often than that (nightly updates, flaky host) never backs up. At startup, look at the newest snapshot's age and run immediately if it's overdue.
- **Failures are only logged.** Show "last backup: time / status" on `/admin`.
- **Each snapshot is a full copy of `uploads/`** (7 retained = 7× the upload size). Uploaded files are immutable, so hard-linking unchanged files from the previous snapshot would make this almost free.
- An interrupted backup leaves a `.miniwiki-backup-*` temp directory that nothing cleans.

---

## 3. Medium

1. **Slow-read exposure on public wikis.** `/uploads/` is exempt from the rate limiter, and `serveUpload` now sets a **10-minute** write deadline (was bounded by the 60 s global). In public mode, anyone can open many connections to a valid upload and read slowly, pinning goroutines/file descriptors. Scale the deadline to file size (e.g. `max(60 s, size / 100 KB/s)`) and/or cap concurrent upload downloads.
2. **"Deleted" uploads stay cached.** Public mode now sends `Cache-Control: public, max-age=86400, immutable`. After an admin deletes a file (e.g. something sensitive), shared caches and browsers can keep serving it for a day with no revalidation, and `immutable` forbids re-checking. Use `no-cache` + ETag (cheap revalidation) or a much shorter `max-age`, and document the trade-off.
3. **Raw database errors reach users.** Duplicate uploads return `"…could not be recorded: " + err.Error()`; renames return `err.Error()`; the Account page shows storage errors verbatim. These expose internal index names (`idx_uploads_filename_active`, `idx_users_email_unique`). The email one is also an **existence oracle**: a user who knows their own password can test whether any address is registered. Map to friendly, generic messages.
4. **Login is still case-sensitive** even though usernames are now case-insensitively unique. `Alice` and `alice` are the same account for uniqueness but only one logs in. Use `COLLATE NOCASE` in `Authenticate` / `UserIDByName`.
5. **Migration failure modes need a dry run.**
   - Slug collisions and legacy duplicate usernames abort `Migrate()`, which **also runs inside `passwd`**, so the recovery command refuses to work in exactly the situation where the operator is already stuck.
   - Operators only discover a collision *after* copying data and starting v22. Add `miniwiki check` (read-only) that lists slug collisions, duplicate usernames/emails and duplicate upload names, and make `passwd` open the DB without running data migrations.
   - The collision scan includes soft-deleted pages, so a deleted page can block an upgrade.
6. **No HTTPS in the binary.** `ListenAndServe` only; `r.TLS` is never set, so the new auto-`Secure`/HSTS logic depends entirely on a trusted proxy. For the "download one file and run it" story, `-tls-cert/-tls-key` flags or `autocert` would finish the job (`golang.org/x/crypto` is already a dependency).
7. **No new handler tests for v22.** The handler test names are identical to v21.1; the new storage/cmd/slug tests are good, but upload streaming, cookie/HSTS selection, the account flow, mail formatting, the proxy warning, `PathEscape` redirects and cache headers are untested. There's also no config-parsing test (H1) and, as the notes admit, no NFC test.

---

## 4. Low / hygiene

1. **Slug collisions by punctuation.** `Slugify` drops everything except letters/numbers/marks/`:`, so `C++`, `C#` and `C` all become `c`. Previously rare; more visible now that every script is supported. Consider keeping `+`/`#` as words (`c-plus-plus`) or detecting collisions with a friendly error on page creation.
2. **Look-alike titles.** NFC only; no NFKC or confusable handling, so fullwidth or Cyrillic-homoglyph titles make distinct slugs (impersonation of pages like `Admin`).
3. **`lower()` mismatch.** SQLite's `lower()` folds ASCII only, while the Go dedupe uses `strings.ToLower` (Unicode). Harmless today, but the two can disagree on `É.png` vs `é.png`.
4. **HSTS** is `max-age=31536000` from the first HTTPS response; ramp up (e.g. a day, then a month) so an operator can still undo a bad HTTPS setup.
5. **Oversize upload message.** `checkCSRF` parses the body first, so a too-large file reports "Bad form submission" instead of a size message; the multipart parser also spools to the OS temp dir before the second copy into `uploads/`.
6. **Protected images:** protecting a page doesn't protect the images it embeds; each upload must be protected separately. Consider a "protect embedded files" action.
7. **API smell:** `RenameUpload(id, name, allowProtected ...bool)`; use an explicit parameter.
8. **Docs drift.** README says unsent reset links are "printed to the server log", and that `dev_mode` shows the link in the browser; the code only logs it, and only when `dev_mode` is on. The upgrade guide's `-config ""` works only because the loader ignores missing files.
9. **Build matrix.** CI cross-builds 6 targets; `make release` builds 11 (openbsd, netbsd, freebsd/arm64 missing from CI). Verify the pure-Go SQLite driver actually supports each *(unverified)*. `govulncheck@latest` is unpinned, `go 1.22` and the mid-2024 dependency pins (now four, with `golang.org/x/text`) are well out of date, so let the scanner decide.
10. **Upload perms:** files are now `0600`; fine for the server, but a separate web server or backup user won't be able to read them.

---

## 5. Security checklist (v22)

| Check | Status |
|---|---|
| Documented install path works (`cp config.example.toml`) | ❌ (H1) |
| Safe online backup / restore with real file locks | ✅ |
| Backup retention only removes its own snapshots | ❌ (H2) |
| Admin password recovery without SMTP | ✅ |
| Protected uploads protected; unique active names | ✅ |
| Email change requires re-authentication | ✅ |
| Unicode titles; legacy slug migration | ✅ (dry-run missing, 3.5) |
| Reset tokens / session IDs hashed at rest | ✅ |
| Auto `Secure` cookies + HSTS only for trusted HTTPS | ✅ |
| Public downloads bounded in time and cache lifetime | ❌ (3.1, 3.2) |
| User-facing errors free of internal details | ❌ (3.3) |
| Handler-level tests for v22 changes | ❌ (3.7) |
| Real test run + committed `go.sum` | ❌ not yet |

---

## 6. Suggested order of work

1. **Fix H1 today** (delete the inline comment; add a "load the example config" test), then **harden backup retention (H2)**.
2. **Run it for real:** `go mod tidy && go test -race ./...` on a normal machine, commit `go.sum`, and watch the first CI run. Everything above is unverified until then.
3. Add `miniwiki check` (dry-run migration report) and decouple `passwd` from data migrations (3.5).
4. Tighten public exposure: size-scaled download deadlines, shorter/revalidating cache for uploads, generic error messages, case-insensitive login (3.1–3.4).
5. Add handler tests for the v22 changes, then consider built-in TLS (3.6).

**Overall:** v22 closes the substantive v21.1 findings with careful designs: lock-free online backups with crash-safe locks, a transactional slug migration, hashed tokens, atomic account changes. The remaining problems are mostly at the edges: one configuration-parsing bug that breaks the documented install, and new operational features (scheduled backups, longer deadlines, caching) that need guard rails. Fix H1 first; it's a one-line change that decides whether a new user's first run works.

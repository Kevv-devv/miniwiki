# MiniWiki v21 — Ubuntu upgrade and operational guide

## IMPORTANT: Preserve your existing wiki

MiniWiki's database path defaults to `wiki.db` relative to the *current working directory*. Starting v21 from an empty directory without copying your v20 database starts a new wiki, even though the old data remains in its v20 folder.

1. Stop the v20 server with **Ctrl+C** (or stop its systemd service). **Do not copy the live v20 SQLite database while v20 is running**, because WAL files may contain committed changes.
2. Download `miniwiki_go_v21_audit_fixes.zip` into `~/Downloads`.
3. Use these exact Ubuntu commands to copy your existing data into the new release, test, and start it:

```bash
cd ~/Downloads
unzip miniwiki_go_v21_audit_fixes.zip -d miniwiki_go_v21_audit_fixes

OLD="$HOME/Downloads/miniwiki_go_v20_security_reliability/miniwiki_v20"
NEW="$HOME/Downloads/miniwiki_go_v21_audit_fixes/miniwiki_v21"

# Copy only after v20 has stopped. The sidecars are optional but included if present.
if [ -f "$OLD/wiki.db" ]; then
  cp -p "$OLD/wiki.db" "$NEW/wiki.db"
  for suffix in -wal -shm; do
    if [ -f "$OLD/wiki.db$suffix" ]; then
      cp -p "$OLD/wiki.db$suffix" "$NEW/wiki.db$suffix"
    fi
  done
fi
if [ -d "$OLD/uploads" ]; then
  cp -a "$OLD/uploads" "$NEW/uploads"
fi

cd "$NEW"
go mod tidy
go test ./...
go run ./cmd/wiki serve
```

If your v20 wiki uses a **custom `config.toml` or paths**, first copy the configuration, then adjust the paths. On upgrading, v21 revokes existing sessions as a security migration, requiring everyone to log in again.

**Old default password migration:** If `admin` still has `admin123`, v21 rotates that password on the first start and prints the new temporary password once in the terminal. Change it in Account settings immediately; v21 requires a password change on the first login with a temporary password. Otherwise existing accounts and passwords are preserved.

### For a completely new installation

```bash
cd ~/Downloads
unzip miniwiki_go_v21_audit_fixes.zip -d miniwiki_go_v21_audit_fixes
cd miniwiki_go_v21_audit_fixes/miniwiki_v21
go mod tidy
go test ./...
go run ./cmd/wiki serve
```

The randomly generated initial administrator password is printed once in the terminal. There is **no longer** a permanent default `admin/admin123` credential. Alternatively, set `MINIWIKI_ADMIN_PASSWORD` (8–72 UTF-8 bytes) before first start.

## Privacy (new)

Set `require_login = true` in `config.toml`, or export `MINIWIKI_REQUIRE_LOGIN=1` before starting. In this mode, read pages, search, history, revisions, gallery, and uploaded files all require an authenticated session; static CSS/JS and login/password-reset screens remain public. The default remains `false` for compatibility with existing public wikis. For an office or private research wiki, enable it before exposing the service.

Use HTTPS and `secure_cookies = true` for non-local deployments. Set `trusted_proxies` only for the actual proxy IPs/CIDRs. The default ignores forwarded client IP headers, and `/0` proxy CIDRs are forbidden.

## Consistent backup and restore (new directory format)

**Stop MiniWiki first**; both commands refuse to run if a v21 server holds the DB's `.lock` file. They operate on **a directory containing `wiki.db` and `uploads/`**, not a standalone `.db` file.

```bash
cd ~/Downloads/miniwiki_go_v21_audit_fixes/miniwiki_v21
# after stopping server:
go run ./cmd/wiki backup -out backups/backup-november
# restore only when server is stopped:
go run ./cmd/wiki restore -from backups/backup-november
```

Backups use SQLite `VACUUM INTO`, capturing committed WAL data correctly, then copy `uploads/` under the same offline lock. The destination must not already exist. Before restoring, MiniWiki saves a rollback snapshot (unless the database is absent), validates the incoming SQLite database and safely clears stale WAL/SHM before replacing the DB and uploads. Avoid symlinks in the uploads folder.

Older v20 `backup` results consisting of **only one `.db` file** are **not** compatible with v21's `restore`; they may lack committed WAL transactions or uploaded files. Preserve them for manual recovery; make a new v21 snapshot after an orderly upgrade.

### Lock file recovery

If MiniWiki is forcibly killed, `wiki.db.lock` may remain. **Verify that NO MiniWiki process is running** before removing it; a lock must never be deleted to work around an active server. Run `ps aux | grep '[m]iniwiki'` and check the process list, then manually remove `wiki.db.lock` only when safe. For `go run` the binary may have a temporary path, so also check the listening port with `ss -ltnp | grep ':8080'`.

## Edit conflicts

The editor records the latest revision when you open it. If someone saves changes before you do, v21 preserves your submitted draft in the editor and returns HTTP 409 rather than silently overwriting the other person's changes. Copy your draft, reload, and merge.

## Password resets

Password changes, admin resets and email-token resets invalidate all sessions for that account. After changing your own password you must log in again. Admin resets require the target user to change their temporary password. Reset emails are sent asynchronously using SMTP connections with explicit network timeouts and STARTTLS when available; reset links appear only in logs when `dev_mode=true` and SMTP is not configured.

## CI, dependencies, and checksums

The source archive includes `.github/workflows/ci.yml`, running test/race/vet, `govulncheck`, and cross-builds on GitHub Actions. The sandbox could not fetch Go modules to generate and verify **go.sum**. The `go mod tidy` step above creates the genuine checksum file on your Ubuntu laptop; commit **both `go.mod` and `go.sum`** to your Git repository after it succeeds:

```bash
git add go.mod go.sum .github/workflows/ci.yml
git commit -m "MiniWiki v21 dependency checksums and CI"
```

These last two commands apply only if the project directory is already a Git repository. Otherwise first initialize Git with `git init`, or commit through your existing workflow. CI also generates a checksum artifact for convenience, but a checked-in `go.sum` is preferable.

# Ubuntu: Upgrade MiniWiki v22 to v23 without losing data

Use the exact commands below in your Ubuntu terminal. Run them only after saving `miniwiki_go_v23_audit_hardening.zip` to `~/Downloads`. Stop the v22 server first with **Ctrl+C** (or stop the systemd service if applicable). The old v22 folder is not modified.

```bash
(
set -e
cd ~/Downloads
unzip -q miniwiki_go_v23_audit_hardening.zip -d miniwiki_go_v23_audit_hardening
OLD="$HOME/Downloads/miniwiki_go_v22_audit_remediation/miniwiki_v22"
NEW="$HOME/Downloads/miniwiki_go_v23_audit_hardening/miniwiki_v23"
cd "$NEW"
go mod tidy
go mod verify
go test -race ./...
go vet ./...

# Snapshot v22 with SQLite's consistent VACUUM INTO. Includes uploads.
BACKUP="$HOME/Downloads/miniwiki-pre-v23-$(date +%Y%m%d-%H%M%S)"
MINIWIKI_UPLOAD_DIR="$OLD/uploads" go run ./cmd/wiki backup -config "" -db "$OLD/wiki.db" -out "$BACKUP"

# Diagnose conflicts without changing the original database.
go run ./cmd/wiki check -config "" -db "$OLD/wiki.db"

# Restore the coherent snapshot into the clean v23 directory.
MINIWIKI_UPLOAD_DIR="$NEW/uploads" go run ./cmd/wiki restore -config "" -db "$NEW/wiki.db" -from "$BACKUP"

# Preserve any existing deployment settings.
if [ -f "$OLD/config.toml" ]; then cp -p "$OLD/config.toml" "$NEW/config.toml"; fi

# Start v23 on its own database, with its own uploads.
MINIWIKI_UPLOAD_DIR="$NEW/uploads" go run ./cmd/wiki serve -db "$NEW/wiki.db"
)
```

These commands run inside a subshell, so Ctrl+C or a failed test cannot leave `set -e` active in your interactive terminal. If `go test -race ./...` or `miniwiki check` fails, the subshell stops the upgrade before data restoration. Correct the issue first. If the old database contains duplicate account names or active-title collisions, `check` reports them rather than repairing the wrong item automatically. Do not delete old v22 data until browser tests on v23 succeed.

After startup, visit `http://localhost:8080`. Confirm pages and uploaded images are present; log in and verify watchlists, protected pages, redirects, account administration, and the `/admin` backup status panel. To enable periodic snapshots, set `backup_every = "24h"` in `config.toml`. Automatic snapshots go under `backup_dir/automatic/`; existing v22 manual/scheduled folders are not automatically deleted.

For native TLS, use `-tls-cert` and `-tls-key` on `serve`, or retain your current trusted reverse proxy. For offline recovery, stop the server and run `go run ./cmd/wiki passwd -db "$NEW/wiki.db" admin` (flags must precede the username).

If a configuration uses absolute DB/upload paths, the explicit `-db` argument and `MINIWIKI_UPLOAD_DIR` environment override them in the above commands; review paths before deploying under systemd.

After first running `go mod tidy`, save `go.sum` along with source in version control. The archive could not include one due to network restrictions in the construction environment. For security review, run a version-appropriate `govulncheck` installation and scan against the actual dependencies.

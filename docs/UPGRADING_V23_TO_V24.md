# Upgrading MiniWiki v23 to v24 on Ubuntu

Stop the v23 `go run ... serve` process first with Ctrl+C. Do **not** delete your v23 directory. Download `miniwiki_go_v24_beta_stabilization.zip` into `~/Downloads`.

Run the following commands in a terminal. They stop on the first failed step. A full, consistent snapshot of your database **and** uploads is created before restoring into the new version. If the preflight or automated tests fail, the old installation remains untouched.

```bash
(
set -e
cd ~/Downloads

OLD="$HOME/Downloads/miniwiki_go_v23_audit_hardening/miniwiki_v23"
NEW="$HOME/Downloads/miniwiki_go_v24_beta_stabilization/miniwiki_v24"

if [ ! -f "$OLD/wiki.db" ]; then
    echo "No v23 database at $OLD/wiki.db. Stop; check the old path." >&2
    exit 1
fi
if [ -e "$NEW" ]; then
    echo "v24 target exists. Stop to avoid overwriting data: $NEW" >&2
    exit 1
fi

unzip -q miniwiki_go_v24_beta_stabilization.zip -d miniwiki_go_v24_beta_stabilization
cd "$NEW"

go mod tidy
go mod verify
go test -race ./...
go vet ./...

BACKUP="$HOME/Downloads/miniwiki-before-v24-$(date +%Y%m%d-%H%M%S)"
MINIWIKI_UPLOAD_DIR="$OLD/uploads"     go run ./cmd/wiki backup -config "" -db "$OLD/wiki.db" -out "$BACKUP"
go run ./cmd/wiki check -config "" -db "$OLD/wiki.db"

MINIWIKI_UPLOAD_DIR="$NEW/uploads"     go run ./cmd/wiki restore -config "" -db "$NEW/wiki.db" -from "$BACKUP"

if [ -f "$OLD/config.toml" ]; then
    cp -p "$OLD/config.toml" "$NEW/config.toml"
fi

MINIWIKI_UPLOAD_DIR="$NEW/uploads" go run ./cmd/wiki serve -db "$NEW/wiki.db"
)
```

Visit `http://localhost:8080` after startup (or your configured address). Confirm existing users, pages, history, watchlists, and uploads remain present. If `config.toml` has an unknown key or a misspelled boolean, v24 deliberately stops with a filename and line number; fix the typo, don't bypass validation.

## Security verification

1. Make a deliberately invalid test config with `require_login = ture` and confirm it fails to load; use a *separate* test file, not your main configuration.
2. In the browser, try creating a page titled `Ａdmin` after `Admin` exists. It should return the editor with a clear conflict message and preserve the draft.
3. Save a page over 2 MiB. The editor should display the limit error and retain its submitted content within the request limit.
4. Test a downloaded file's old ETag after admin deletion; expect HTTP 404, never 304.
5. If scheduled backups are used, inspect `backups/automatic/` and confirm `/admin` shows recent status. Remember that snapshot hard links are not independent physical copies.
6. For an exposed instance, review HTTPS, `trusted_proxies`, `require_login`, firewall rules, and `govulncheck` results.

**Important:** Newer source zips intentionally contain no wiki database or uploaded user files. The upgrade procedure restores them from the old installation. Preserve the pre-upgrade snapshot and old directory until the new instance is fully verified.

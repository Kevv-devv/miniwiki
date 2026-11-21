# Ubuntu: upgrade from MiniWiki v24 to the v24.2 beta release candidate

Stop the currently running MiniWiki server with **Ctrl+C**. Download `miniwiki_go_v24_2_security_hotfix.zip` to `~/Downloads`.

The previous v24.1 command exited when `govulncheck` detected reachable vulnerabilities, **before** restoring your data to v24.1. These instructions use the verified v24 installation as the source; verify that its `wiki.db` contains your real content before executing them.

```bash
(
set -e
cd ~/Downloads
OLD="$HOME/Downloads/miniwiki_go_v24_beta_stabilization/miniwiki_v24"
NEW="$HOME/Downloads/miniwiki_go_v24_2_security_hotfix/miniwiki_v24_2"
PREVIOUS_CANDIDATE="$HOME/Downloads/miniwiki_go_v24_1_beta_candidate/miniwiki_v24_1"

if [ ! -f "$OLD/wiki.db" ]; then echo "Missing original v24 database"; exit 1; fi
if [ -e "$NEW" ]; then echo "v24.2 directory already exists; aborting"; exit 1; fi

unzip -q miniwiki_go_v24_2_security_hotfix.zip -d miniwiki_go_v24_2_security_hotfix
cd "$NEW"

# Reuse the verified checksums you generated on Ubuntu, then extend with new pins.
if [ -f "$PREVIOUS_CANDIDATE/go.sum" ]; then
    cp -p "$PREVIOUS_CANDIDATE/go.sum" "$NEW/go.sum"
elif [ -f "$OLD/go.sum" ]; then
    cp -p "$OLD/go.sum" "$NEW/go.sum"
fi

go mod tidy
go mod verify
go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.10.0 ./...

# A successful scan is required before proceeding with the upgrade.
BACKUP="$HOME/Downloads/miniwiki-before-v24_2-$(date +%Y%m%d-%H%M%S)"
MINIWIKI_UPLOAD_DIR="$OLD/uploads" \
  go run ./cmd/wiki backup -config "" -db "$OLD/wiki.db" -out "$BACKUP"

go run ./cmd/wiki check -config "" -db "$OLD/wiki.db"

MINIWIKI_UPLOAD_DIR="$NEW/uploads" \
  go run ./cmd/wiki restore -config "" -db "$NEW/wiki.db" -from "$BACKUP"

if [ -f "$OLD/config.toml" ]; then
    cp -p "$OLD/config.toml" "$NEW/config.toml"
fi

# For a GitHub release, commit the newly updated go.sum with go.mod.
MINIWIKI_UPLOAD_DIR="$NEW/uploads" \
  go run ./cmd/wiki serve -db "$NEW/wiki.db"
)
```

The server should report `MiniWiki 0.24.2-beta-rc` and listen on `http://localhost:8080` with the default configuration. Check that your pages, images, and accounts are present. Also test a normal Markdown link and image embed and confirm the server log does not show startup errors.

**Release gate:** A successful scan is important but cannot prove complete security; browser acceptance, scheduled backup verification, and committing `go.sum` are still needed before publishing a final beta tag.

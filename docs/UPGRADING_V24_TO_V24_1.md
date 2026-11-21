# Ubuntu upgrade from v24 to v0.24.1-beta

Stop v24 with Ctrl+C (or stop the systemd unit) before upgrading. Do not
remove or overwrite the previous v24 installation. The commands below expect
both zip files/installations to use the names supplied in our releases.

```bash
(
set -e
cd "$HOME/Downloads"
OLD="$HOME/Downloads/miniwiki_go_v24_beta_stabilization/miniwiki_v24"
NEW="$HOME/Downloads/miniwiki_go_v24_1_beta_candidate/miniwiki_v24_1"

if [ ! -f "$OLD/wiki.db" ]; then
    echo "The existing v24 database is missing: $OLD/wiki.db" >&2
    exit 1
fi
if [ -e "$NEW" ]; then
    echo "Destination already exists; will not overwrite: $NEW" >&2
    exit 1
fi
unzip -q miniwiki_go_v24_1_beta_candidate.zip -d miniwiki_go_v24_1_beta_candidate
cd "$NEW"

# Reuse checksums generated and verified on the prior Ubuntu installation.
if [ -f "$OLD/go.sum" ]; then
    cp -p "$OLD/go.sum" "$NEW/go.sum"
fi

go mod tidy
go mod verify
go test -race ./...
go vet ./...

# A security scan is necessary before tagging/publishing beta.
# It needs network access to install the scanner and fetch advisories.
go run golang.org/x/vuln/cmd/govulncheck@latest ./...

BACKUP="$HOME/Downloads/miniwiki-before-v24_1-$(date +%Y%m%d-%H%M%S)"
MINIWIKI_UPLOAD_DIR="$OLD/uploads" \
    go run ./cmd/wiki backup -config "" -db "$OLD/wiki.db" -out "$BACKUP"
go run ./cmd/wiki check -config "" -db "$OLD/wiki.db"
MINIWIKI_UPLOAD_DIR="$NEW/uploads" \
    go run ./cmd/wiki restore -config "" -db "$NEW/wiki.db" -from "$BACKUP"
if [ -f "$OLD/config.toml" ]; then
    cp -p "$OLD/config.toml" "$NEW/config.toml"
fi

MINIWIKI_UPLOAD_DIR="$NEW/uploads" \
    go run ./cmd/wiki serve -db "$NEW/wiki.db"
)
```

If `govulncheck` identifies a reachable vulnerability, **do not publish the
beta** until you have evaluated and fixed it. If the scanner cannot run due
to network/toolchain restrictions, keep the release marked as an unverified
candidate. A passed vulnerability scan is a point-in-time check, not a
security certification.

The data layout is unchanged from v24. Do not copy a SQLite `wiki.db` file
while the old server is still running; the backup CLI safely snapshots a WAL
database and its uploaded files. Keep the original and the new backup until
you have confirmed that your pages, users, files, redirects and watchlists
are present in the new installation.

If you use Git, commit `go.mod`, the **real** `go.sum` generated on Ubuntu,
and the v24.1 sources before tagging a beta. A tag names a commit; it cannot
make a ZIP reproducible unless both dependency files are versioned.

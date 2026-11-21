# MiniWiki deployment guide

MiniWiki is designed to be easy to deploy: one Go binary plus one SQLite database file.
Templates and static assets are embedded into the binary.

## Fresh Ubuntu/Debian server

Install a build toolchain only if you are building from source:

```bash
sudo apt update
sudo apt install -y golang make sqlite3
```

Build:

```bash
make build
```

Install:

```bash
sudo useradd --system --home /opt/miniwiki --shell /usr/sbin/nologin miniwiki
sudo mkdir -p /opt/miniwiki /etc/miniwiki
sudo cp dist/miniwiki /opt/miniwiki/miniwiki
sudo cp config.example.toml /etc/miniwiki/config.toml
sudo chown -R miniwiki:miniwiki /opt/miniwiki
```

Edit `/etc/miniwiki/config.toml` if desired.

Install systemd service:

```bash
sudo cp deploy/systemd/miniwiki.service /etc/systemd/system/miniwiki.service
sudo systemctl daemon-reload
sudo systemctl enable --now miniwiki
sudo systemctl status miniwiki
```

Logs:

```bash
journalctl -u miniwiki -f
```

## Home/LAN quick start

```bash
make build
mkdir -p ~/miniwiki
cp dist/miniwiki ~/miniwiki/
cp config.example.toml ~/miniwiki/config.toml
cd ~/miniwiki
./miniwiki serve
```

Open `http://SERVER-IP:8080`.

A new installation creates `admin` with a randomly generated **temporary password printed once in the terminal**. Change it in Account settings at first login. Existing v20 default credentials are automatically rotated on first v21 migration.

## Backup

```bash
./miniwiki backup
```

This creates a timestamped backup in `backups/` by default.

Custom backup path:

```bash
./miniwiki backup -out /backup/miniwiki-snapshot
```

## Restore

Stop MiniWiki first, then:

```bash
./miniwiki restore -from backups/wiki-YYYYMMDD-HHMMSS
```

v21 backup/restore operate on **directories** containing a SQLite `wiki.db` backup plus `uploads/`. MiniWiki may remain running for `backup`, but **must be stopped for `restore`**. `VACUUM INTO` handles WAL safely and restore first makes a pre-restore safety snapshot. See `docs/UPGRADING_V20_TO_V21.md`.

## HTTPS / reverse proxy

For public or business use, put Caddy or Nginx in front of MiniWiki and set:

```toml
secure_cookies = true
```

or:

```bash
export MINIWIKI_SECURE_COOKIES=1
```

MiniWiki itself can listen on localhost:

```toml
addr = "127.0.0.1:8080"
```

If the reverse proxy and MiniWiki are on the same server, set this in MiniWiki's config **only if** the proxy overwrites/appends `X-Forwarded-For` correctly:

```toml
trusted_proxies = "127.0.0.1/32, ::1/128"
```

Or set `MINIWIKI_TRUSTED_PROXIES` in the environment. By default, MiniWiki **ignores** `X-Forwarded-For` and `X-Real-IP` and uses the direct TCP peer to prevent request spoofing. With configured trusted proxies, MiniWiki walks the forwarded chain from right to left and chooses the first untrusted IP. Do not trust arbitrary public addresses or `0.0.0.0/0`. The per-client
concurrent-download limit (16) and request throttling also use the detected
client IP: when a reverse proxy is **not** listed as trusted, all users behind
it share the same limit and might see HTTP 429 even if each browser behaves
normally. Only trust proxies under your control that correctly sanitize
forwarded headers.

## FreeBSD / OpenBSD / NetBSD

Build or download the matching binary, then place it in `/usr/local/miniwiki/miniwiki` and put `config.toml` in `/usr/local/etc/miniwiki/config.toml`.

A sample rc.d script is in `deploy/bsd/miniwiki.rc`.

FreeBSD example:

```bash
sudo mkdir -p /usr/local/miniwiki /usr/local/etc/miniwiki
sudo cp miniwiki /usr/local/miniwiki/miniwiki
sudo cp config.example.toml /usr/local/etc/miniwiki/config.toml
sudo cp deploy/bsd/miniwiki.rc /usr/local/etc/rc.d/miniwiki
sudo chmod +x /usr/local/etc/rc.d/miniwiki
sudo sysrc miniwiki_enable=YES
sudo service miniwiki start
```

## Cross-platform builds

`make release` includes targets for Linux, Windows, macOS, FreeBSD, OpenBSD, and NetBSD on amd64/arm64 where supported.

MiniWiki uses `modernc.org/sqlite`, a pure-Go SQLite driver. Cross-compiling the release targets should not require target C compilers or CGO toolchains.


## SQLite driver

MiniWiki uses `modernc.org/sqlite`, which is implemented in Go. The database is still a normal SQLite database file (`wiki.db`), but the build does not require CGO or a C compiler. This is important for the easy-deployment goal: one source tree can produce binaries for Linux, Windows, macOS, FreeBSD, OpenBSD, and NetBSD with standard `GOOS`/`GOARCH` builds.

## Uploads

Uploaded files are stored outside the binary in the directory configured by:

```toml
upload_dir = "uploads"
```

Back up this directory alongside `wiki.db`.

## Password reset email

Password reset emails are optional. Configure SMTP:

```toml
base_url = "https://wiki.example.com"
smtp_host = "smtp.example.com"
smtp_port = "587"
smtp_username = "user"
smtp_password = "secret"
smtp_from = "wiki@example.com"
```

Without SMTP, reset links are written to the server log only when `dev_mode=true` (local testing only). For offline administrator recovery, stop the server and run `miniwiki passwd USER`.


## v16 notes

Page renames create redirects automatically. Uploads are stored in the configured `upload_dir`; image uploads can be embedded in pages using the Markdown snippet shown on the Uploads page.

## v23 operations and hardening

Use `miniwiki check -db wiki.db` for a non-mutating database check before migrating. An explicit native-TLS certificate and key may be provided with `miniwiki serve -tls-cert cert.pem -tls-key key.pem`. Install trusted certificates or use a TLS reverse proxy; never expose the initial HTTP-only configuration directly to the public internet.

For scheduled backups, set `backup_every = "24h"`. The backup service writes only to `backup_dir/automatic/`, verifies its own snapshot ownership markers before pruning, and shows last-run status to administrators. Manual backups are separate. Backup data must be protected like the original wiki.

The example configuration is tested by `go test`. The first build needs `go mod tidy`, and the generated `go.sum` must be stored in source control. `go mod verify`, `go test -race ./...`, and `go vet ./...` are recommended before deployment.

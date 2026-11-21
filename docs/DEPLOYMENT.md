# Deploying MiniWiki

MiniWiki is a self-hosted Go wiki with SQLite storage. Its templates and static assets are embedded in the executable. The **precompiled archives require no Go toolchain**.

For desktop or home installations start with [Install from a release](INSTALL_FROM_RELEASE.md).

## Quick start from a downloaded archive

1. Download and verify the archive for your OS from the [GitHub Releases page](https://github.com/Kevv-devv/miniwiki/releases).
2. Extract it, and note the full path to the `miniwiki` executable (`miniwiki.exe` on Windows).
3. Create a persistent directory for your instance, database, and uploads.

```sh
mkdir -p "$HOME/miniwiki-instance"
cd "$HOME/miniwiki-instance"
/path/to/extracted/miniwiki serve -addr 127.0.0.1:8080 -db wiki.db
```

Open `http://127.0.0.1:8080`. The one-time `admin` password appears only in the server terminal at first run; sign in and change it. The path above is an example—replace it with your extracted executable's actual path.

**Important:** The working directory controls the default location of `wiki.db`, `uploads/`, and `backups/`. Keep it consistent across restarts and preserve these files when upgrading.

## Build from source (optional)

Only developers building MiniWiki from the repository need Go:

```sh
git clone https://github.com/Kevv-devv/miniwiki.git
cd miniwiki
go mod download
go mod verify
make build
```

This generates `dist/miniwiki` in the **source tree**. `dist/miniwiki` is **not** the filename in the downloadable release archive: the release executable is named simply `miniwiki`.

## Linux service installation (systemd)

The repository provides a sample service in `deploy/systemd/miniwiki.service`. Inspect its `ExecStart`, working directory, database path, service account, and permissions before enabling it. That sample service file is not bundled in the small binary downloads; retrieve it from the corresponding tagged source repository.

Example setup (adapt paths to your server):

```sh
sudo useradd --system --home /opt/miniwiki --shell /usr/sbin/nologin miniwiki
sudo mkdir -p /opt/miniwiki /etc/miniwiki
sudo cp /path/to/extracted/miniwiki /opt/miniwiki/miniwiki
sudo cp /path/to/extracted/config.example.toml /etc/miniwiki/config.toml
sudo chown -R miniwiki:miniwiki /opt/miniwiki
```

Edit `/etc/miniwiki/config.toml` for your deployment, and ensure the service account can write the configured data and upload directories. Download `deploy/systemd/miniwiki.service` from the matching Git tag, check it carefully, then:

```sh
sudo cp deploy/systemd/miniwiki.service /etc/systemd/system/miniwiki.service
sudo systemctl daemon-reload
sudo systemctl enable --now miniwiki
sudo systemctl status miniwiki
journalctl -u miniwiki -f
```

Do not run the service as root merely to work around file permissions.

## HTTPS / reverse proxy

For public access use HTTPS (for example, via Caddy or Nginx). With HTTPS enable secure cookies:

```toml
secure_cookies = true
```

If the reverse proxy is on the same host, MiniWiki can bind to `127.0.0.1:8080`. Only trust known reverse proxy addresses that correctly sanitize forwarded headers:

```toml
addr = "127.0.0.1:8080"
trusted_proxies = "127.0.0.1/32, ::1/128"
```

Never trust arbitrary client networks or `0.0.0.0/0`. Without configured trusted proxies, rate limiting may treat all visitors behind the proxy as one client. For native TLS without a proxy, use `miniwiki serve -tls-cert cert.pem -tls-key key.pem` with trusted certificates and an appropriate listen address.

**Never expose an HTTP-only fresh wiki to the public internet.** For private wikis consider `require_login = true`.

## Backup / restore / database checks

```sh
./miniwiki backup
./miniwiki check -db wiki.db
# Stop the running service before restoring:
./miniwiki restore -from backups/wiki-YYYYMMDD-HHMMSS
```

Backups contain sensitive database and upload content. A backup is a **directory** containing a SQLite-consistent database snapshot plus uploaded files. Restrict access. MiniWiki may remain running while creating backups, but must be stopped before restore or offline password recovery (`miniwiki passwd USERNAME`).

Scheduled backups can be enabled in `config.toml` with:

```toml
backup_dir = "backups"
backup_every = "24h"
keep_last = 7
```

Verify backup integrity before deleting or rotating older copies.

## BSD / macOS / Windows

Each release contains the platform-specific MiniWiki executable. A sample FreeBSD rc.d script is available in the tagged source tree at `deploy/bsd/miniwiki.rc`; inspect its paths and permissions before deployment. The first beta supports FreeBSD and OpenBSD on AMD64/ARM64, and NetBSD on AMD64. Runtime behavior on every platform is not guaranteed solely by cross-compilation.

See [Install from a release](INSTALL_FROM_RELEASE.md) for macOS and Windows steps.

## SMTP-based password reset (optional)

Configure `base_url`, `smtp_host`, `smtp_port`, `smtp_username`, `smtp_password`, and `smtp_from` in `config.toml` as needed. When SMTP is disabled, development-only reset links can be logged if `dev_mode=true`; **never enable development mode on a public wiki**. Offline recovery is available via `miniwiki passwd USERNAME` with the server stopped.

## Further reading

See `config.example.toml` and the version-specific `docs/UPGRADING_*.md` notes in the source repository for supported configuration and migration details.

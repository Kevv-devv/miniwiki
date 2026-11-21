# MiniWiki

[![Build and security checks](https://github.com/Kevv-devv/miniwiki/actions/workflows/ci.yml/badge.svg)](https://github.com/Kevv-devv/miniwiki/actions/workflows/ci.yml)
[![AGPL-3.0-only](https://img.shields.io/badge/license-AGPL--3.0--only-blue.svg)](LICENSE)

**A small, self-hosted wiki built with Go and SQLite.** MiniWiki embeds its HTML templates and static assets in a single executable. You can download a prebuilt binary and get started without installing Go, a database server, or a web framework.

**First public beta:** [v0.24.2-beta](https://github.com/Kevv-devv/miniwiki/releases/tag/v0.24.2-beta) · [All releases](https://github.com/Kevv-devv/miniwiki/releases) · [Report an issue](https://github.com/Kevv-devv/miniwiki/issues)

> **Beta software:** Back up your database and uploads before upgrades. Cross-platform compilation is checked in CI; not every operating system has received full runtime testing.

## Features

| Editing and navigation | Accounts and administration | Deployment and maintenance |
| --- | --- | --- |
| Markdown, wiki links, and redirects | Roles, authentication, and optional private-wiki mode | Single self-contained Go executable |
| Revision history and diffs | Page protection, deletion, and restoration | SQLite database; no separate database server |
| Categories, search, backlinks, and recent changes | User pages, talk pages, and watchlists | Config file and environment-variable overrides |
| Image uploads, previews, and embedding | Admin moderation and account management | Backups, restores, database checks, and optional TLS |
| Responsive layout and dark mode | Optional password-reset email | Linux, Windows, macOS, and supported BSD targets |

## Screenshots

<!-- SCREENSHOT-GALLERY-START -->
Real screenshots will appear here after you run the [screenshot capture instructions](docs/SCREENSHOTS.md) against a **disposable demo wiki**. No placeholder or fabricated UI images are included.
<!-- SCREENSHOT-GALLERY-END -->

## Download and run (no Go required)

Choose your package on the [Releases page](https://github.com/Kevv-devv/miniwiki/releases). The published v0.24.2-beta provides **10 builds**: Linux (AMD64/ARM64), Windows (AMD64), macOS (Intel/Apple Silicon), FreeBSD (AMD64/ARM64), OpenBSD (AMD64/ARM64), and NetBSD (AMD64). NetBSD ARM64 is not available due to a dependency limitation.

### Ubuntu / Linux AMD64 example (v0.24.2-beta)

```bash
# Download the matching archive and its checksum file.
curl -fLO https://github.com/Kevv-devv/miniwiki/releases/download/v0.24.2-beta/miniwiki-v0.24.2-beta-linux-amd64.tar.gz
curl -fLO https://github.com/Kevv-devv/miniwiki/releases/download/v0.24.2-beta/SHA256SUMS

# Verify only the archive you downloaded.
grep -F '  miniwiki-v0.24.2-beta-linux-amd64.tar.gz' SHA256SUMS | sha256sum -c -

# Extract and run in a clean directory.
tar -xzf miniwiki-v0.24.2-beta-linux-amd64.tar.gz
cd miniwiki-v0.24.2-beta-linux-amd64
./miniwiki version
./miniwiki serve -addr 127.0.0.1:8080
```

Open **http://127.0.0.1:8080**. On first launch, MiniWiki creates an `admin` account and prints its randomly generated temporary password **once in the server terminal**. Sign in and change it immediately. Do not share the initial password or expose a fresh HTTP-only wiki to the public internet.

The default database (`wiki.db`) and uploads directory are created relative to the process's working directory. For a persistent installation, run MiniWiki from a dedicated data directory and back up both `wiki.db` and `uploads/`.

**Other operating systems:** See [Install from a release archive](docs/INSTALL_FROM_RELEASE.md) for Windows, macOS, BSD, checksums, and upgrade precautions. Prebuilt packages include `LICENSE`, `NOTICE`, `config.example.toml`, and deployment instructions.

## Build from source

Requires Go as specified by `go.mod` and a working module download connection:

```bash
git clone https://github.com/Kevv-devv/miniwiki.git
cd miniwiki
go mod download
go mod verify
go test ./...
make build
./dist/miniwiki serve -addr 127.0.0.1:8080
```

For a development run, use `go run ./cmd/wiki serve` instead. You **do not** need Go to use the prebuilt release downloads.

## Commands

```text
miniwiki version
miniwiki serve [-config config.toml] [-addr 127.0.0.1:8080] [-db wiki.db]
miniwiki backup [-out backups/wiki-TIMESTAMP]
miniwiki restore -from backups/wiki-TIMESTAMP
miniwiki check [-db wiki.db]
miniwiki passwd [-db wiki.db] USERNAME
```

Run `miniwiki help` for complete options. **Stop the running server before restore or offline password recovery.**

## Configuration and security

Copy `config.example.toml` to `config.toml` to customize the server. Settings can also be supplied through `MINIWIKI_*` environment variables; see the example file for supported keys.

```toml
addr = "127.0.0.1:8080"
db_path = "wiki.db"
site_name = "MiniWiki"
require_login = true   # optional: require login to access wiki content
secure_cookies = false # change to true when serving over HTTPS
```

A local test may use plain HTTP bound to `127.0.0.1`. For remote access, configure HTTPS or a trusted reverse proxy and review [Deployment and security](docs/DEPLOYMENT.md). Never enable development-only password-reset logging on a public instance.

## Releases and development

[GitHub Actions](https://github.com/Kevv-devv/miniwiki/actions) tests the code and builds the supported platforms. The separate release workflow tests again, cross-compiles, packages archives, generates `SHA256SUMS`, and publishes a GitHub Release **only when a new version tag is pushed**. Manual workflow runs are safe packaging previews and do not publish anything.

See [Maintainer release process](docs/RELEASING.md) for exact commands. Older release notes and security work are preserved in [Development history](docs/README_LEGACY.md) and the version-specific documents at the repository root.

## License

MiniWiki is distributed under the **GNU Affero General Public License, version 3 only (AGPL-3.0-only)**. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

Copyright © 2026 `kevv-devv`.

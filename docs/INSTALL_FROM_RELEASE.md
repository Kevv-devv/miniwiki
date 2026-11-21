# Installing MiniWiki from a GitHub Release

**Prebuilt releases need no Go installation.** Download the appropriate archive from https://github.com/Kevv-devv/miniwiki/releases and unpack it. The executable, license, README, sample config, and this guide are in the archive.

## Choose an archive

| Operating system | Architecture | Archive type |
| --- | --- | --- |
| Linux | AMD64 (x86-64), ARM64 | `.tar.gz` |
| Windows | AMD64 (x86-64) | `.zip` |
| macOS | AMD64 (Intel), ARM64 (Apple Silicon) | `.tar.gz` |
| FreeBSD | AMD64, ARM64 | `.tar.gz` |
| OpenBSD | AMD64, ARM64 | `.tar.gz` |
| NetBSD | AMD64 | `.tar.gz` |

NetBSD ARM64 isn't available in the first beta due to a dependency compatibility issue. Successful cross-compilation does not establish runtime compatibility on every device.

## Linux / BSD / macOS

Download the matching `.tar.gz` and `SHA256SUMS` from the same GitHub Release. For example, on Linux AMD64 with v0.24.2-beta:

```sh
curl -fLO https://github.com/Kevv-devv/miniwiki/releases/download/v0.24.2-beta/miniwiki-v0.24.2-beta-linux-amd64.tar.gz
curl -fLO https://github.com/Kevv-devv/miniwiki/releases/download/v0.24.2-beta/SHA256SUMS
grep -F '  miniwiki-v0.24.2-beta-linux-amd64.tar.gz' SHA256SUMS | sha256sum -c -
tar -xzf miniwiki-v0.24.2-beta-linux-amd64.tar.gz
cd miniwiki-v0.24.2-beta-linux-amd64
./miniwiki version
./miniwiki serve -addr 127.0.0.1:8080
```

For other platforms, substitute the archive filename. Verify with your platform's SHA-256 utility if `sha256sum` is not installed.

A new install prints a **one-time administrator password** in the terminal. Keep it private, sign in at `http://127.0.0.1:8080`, and change it immediately.

Unsigned macOS binaries may trigger security warnings. Download only from the project's official release page and verify the SHA-256 checksum. Don't bypass security prompts for software you haven't verified.

## Windows

1. Download the `miniwiki-v0.24.2-beta-windows-amd64.zip` archive and `SHA256SUMS` from the release page.
2. In PowerShell, run `Get-FileHash .\miniwiki-v0.24.2-beta-windows-amd64.zip -Algorithm SHA256` and compare it to the corresponding line in `SHA256SUMS`.
3. Extract the ZIP into a folder you control. Open PowerShell in that folder.
4. Start MiniWiki with `./miniwiki.exe serve -addr 127.0.0.1:8080`, then visit `http://127.0.0.1:8080`.
5. Save the one-time administrator password from the terminal and change it after logging in.

## Data directory and persistence

MiniWiki stores `wiki.db`, `uploads/`, and `backups/` relative to the **working directory** unless configuration overrides them. For a reliable setup, create a dedicated instance directory and run the binary from it:

```sh
mkdir -p "$HOME/miniwiki-instance"
cd "$HOME/miniwiki-instance"
/path/to/extracted/miniwiki serve -addr 127.0.0.1:8080 -db wiki.db
```

`/path/to/extracted/miniwiki` is a placeholder: substitute the executable's real path. Your source tree is not required to operate the binary.

## Updating an existing wiki

1. Back up your wiki **database and uploads** using `miniwiki backup`, and separately verify you can restore a copy.
2. Read the version-specific upgrade notes in the [tagged source repository](https://github.com/Kevv-devv/miniwiki/tree/v0.24.2-beta/docs) and any migration instructions for the target release.
3. Stop the old MiniWiki server before upgrading the executable.
4. Keep the same data directory and configuration; replace only the executable after a verified backup.
5. Start the new version and confirm it can read existing data.

Never overwrite an existing `wiki.db` or `uploads/` while extracting a release. Archives don't contain a preexisting database or user uploads.

For production service setup, HTTPS, and BSD rc.d examples see [Deployment guide](DEPLOYMENT.md).

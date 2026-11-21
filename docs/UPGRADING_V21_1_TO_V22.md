# Upgrade MiniWiki v21.1 → v22 on Ubuntu

**First stop the old v21.1 server** with Ctrl+C (or `sudo systemctl stop miniwiki` for a service). Never copy a live SQLite database file as an upgrade procedure.

Assuming you unpacked previous versions into `~/Downloads` with the instructions previously supplied, paste these commands:

```bash
cd ~/Downloads
unzip miniwiki_go_v22_audit_remediation.zip -d miniwiki_go_v22_audit_remediation

OLD="$HOME/Downloads/miniwiki_go_v21_1_test_fix/miniwiki_v21_1"
NEW="$HOME/Downloads/miniwiki_go_v22_audit_remediation/miniwiki_v22"
cd "$NEW"

go mod tidy
go mod verify
go test ./...
go vet ./...

# Preserve an independent and consistent snapshot from the stopped v21.1 installation.
BACKUP="$HOME/Downloads/miniwiki-before-v22-$(date +%Y%m%d-%H%M%S)"
MINIWIKI_UPLOAD_DIR="$OLD/uploads" go run ./cmd/wiki backup -config "" -db "$OLD/wiki.db" -out "$BACKUP"

# Carry forward the database, SQLite WAL files (if present), uploads and config.
cp -p "$OLD/wiki.db" "$NEW/wiki.db"
for suffix in -wal -shm; do
    if [ -f "$OLD/wiki.db$suffix" ]; then
        cp -p "$OLD/wiki.db$suffix" "$NEW/wiki.db$suffix"
    fi
done
if [ -d "$OLD/uploads" ]; then cp -a "$OLD/uploads" "$NEW/uploads"; fi
if [ -f "$OLD/config.toml" ]; then cp -p "$OLD/config.toml" "$NEW/config.toml"; fi

go run ./cmd/wiki serve
```

Open http://localhost:8080. MiniWiki should report `0.22.0-dev`.

**Do not perform the data-copy part until the old server is completely stopped.** If you use a custom database or upload path, adjust `OLD` and the backup command accordingly.

### Recovery and online backups

The following examples run from the v22 project directory. Use **a separate terminal** for the backup while the server runs:

```bash
cd ~/Downloads/miniwiki_go_v22_audit_remediation/miniwiki_v22
go run ./cmd/wiki backup -out backups/wiki-$(date +%Y%m%d-%H%M%S)
```

To recover a user password without SMTP, stop the server first, then:

```bash
cd ~/Downloads/miniwiki_go_v22_audit_remediation/miniwiki_v22
go run ./cmd/wiki passwd admin
```

A temporary password prints on the terminal. Existing sessions are revoked, and the new password must be changed after login.

### Scheduled backups (optional)

Add to `config.toml`:

```toml
backup_dir = "backups"
backup_every = "24h"
keep_last = 7
```

Backups are folders holding `wiki.db` and `uploads/`. Secure them like the primary database.

### Upgrade troubleshooting

- **Legacy slug collision:** v22 refuses to merge two pages into the same canonical slug. Correct or rename one of the colliding titles in v21.1, then retry with a fresh copy of the original database.
- **Legacy username/email duplicates:** Case-insensitive duplicates may block startup. Identify duplicates with SQLite and resolve them rather than giving two accounts the same identity:

```bash
sqlite3 "$OLD/wiki.db" "SELECT lower(username), group_concat(username) FROM users GROUP BY lower(username) HAVING count(*) > 1;"
sqlite3 "$OLD/wiki.db" "SELECT lower(email), group_concat(username) FROM users WHERE email<>'' GROUP BY lower(email) HAVING count(*) > 1;"
```

- **Missing go.sum:** `go mod tidy` downloads modules and creates `go.sum`. Save this file in Git for reproducible builds.
- **Race tests:** Run `go test -race ./...` separately if the standard suite passes. On Ubuntu, the race detector requires a working C compiler (e.g. `build-essential`).
- **Roll back:** stop v22 and run your original v21.1 source/data, or use the independently saved pre-v22 snapshot. Do not downgrade a database already migrated to v22 without restoring the old snapshot.

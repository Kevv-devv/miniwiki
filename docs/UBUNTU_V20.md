# MiniWiki v20 — Ubuntu commands

Save `miniwiki_go_v20_security_reliability.zip` to `~/Downloads` and run:

```bash
cd ~/Downloads
unzip miniwiki_go_v20_security_reliability.zip -d miniwiki_go_v20_security_reliability
cd miniwiki_go_v20_security_reliability/miniwiki_v20
go mod tidy
go test ./...
go run ./cmd/wiki serve
```

Open http://localhost:8080 and log in using your existing wiki account (new test databases initialize `admin` / `admin123`; change that password immediately).

To produce and run a native Ubuntu binary instead:

```bash
# After changing into the v20 project folder:
go build -o miniwiki ./cmd/wiki
./miniwiki version
./miniwiki serve
```

The application uses `wiki.db` and `uploads/` relative to the working directory by default. If migrating your actual v19 site, first stop v19, preserve a complete database and upload backup, and reuse those data files (or configure their existing locations via `MINIWIKI_DB` and `MINIWIKI_UPLOAD_DIR`). Do not delete your old version's folder until your real content is backed up and verified.

APP := miniwiki
CMD := ./cmd/wiki
DIST := dist
VERSION ?= dev
LDFLAGS := -s -w

.PHONY: run fmt test build clean release release-current

run:
	go run $(CMD) serve

fmt:
	go fmt ./...

test:
	go test ./...

build:
	mkdir -p $(DIST)
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP) $(CMD)

release-current: fmt test build

# MiniWiki uses modernc.org/sqlite, a pure-Go SQLite driver.
# That keeps these release targets CGO-free and much easier to cross-compile.
release:
	mkdir -p $(DIST)
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-linux-amd64 $(CMD)
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-linux-arm64 $(CMD)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-windows-amd64.exe $(CMD)
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-darwin-amd64 $(CMD)
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-darwin-arm64 $(CMD)
	GOOS=freebsd GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-freebsd-amd64 $(CMD)
	GOOS=freebsd GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-freebsd-arm64 $(CMD)
	GOOS=openbsd GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-openbsd-amd64 $(CMD)
	GOOS=openbsd GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-openbsd-arm64 $(CMD)
	GOOS=netbsd  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-netbsd-amd64 $(CMD)
	GOOS=netbsd  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-netbsd-arm64 $(CMD)

clean:
	rm -rf $(DIST)

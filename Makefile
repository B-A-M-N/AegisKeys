.PHONY: build test vet fmt-check run install clean release

VERSION ?= dev
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DIST_DIR ?= dist
LDFLAGS := -s -w -X aegiskeys/cmd.version=$(VERSION)

build:
	go build -buildvcs=false -ldflags "$(LDFLAGS)" -o aegiskeys .

test:
	go test ./...

vet:
	go vet ./...

fmt-check:
	test -z "$$(gofmt -l .)"

run: build
	./aegiskeys

install: build
	install -d "$(DESTDIR)$(BINDIR)"
	install -m 0755 aegiskeys "$(DESTDIR)$(BINDIR)/aegiskeys"

clean:
	rm -rf "$(DIST_DIR)" aegiskeys

release: clean
	mkdir -p "$(DIST_DIR)"
	GOOS=linux GOARCH=amd64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o "$(DIST_DIR)/aegiskeys_$(VERSION)_linux_amd64" .
	GOOS=linux GOARCH=arm64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o "$(DIST_DIR)/aegiskeys_$(VERSION)_linux_arm64" .
	GOOS=darwin GOARCH=amd64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o "$(DIST_DIR)/aegiskeys_$(VERSION)_darwin_amd64" .
	GOOS=darwin GOARCH=arm64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o "$(DIST_DIR)/aegiskeys_$(VERSION)_darwin_arm64" .
	cd "$(DIST_DIR)" && sha256sum aegiskeys_$(VERSION)_* > SHA256SUMS

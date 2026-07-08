.PHONY: build test vet fmt-check run install clean release demo demo-cli demo-tui demo-full demo-prereqs

VERSION ?= dev
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DIST_DIR ?= dist
VHS ?= vhs
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

demo-prereqs:
	@command -v "$(VHS)" >/dev/null 2>&1 || { echo "VHS is required. Install charm.land/vhs, then rerun make demo."; exit 1; }

demo: demo-cli demo-tui demo-full

demo-cli: build demo-prereqs
	mkdir -p docs/demo tmp
	"$(VHS)" demos/vhs/cli-overview.tape

demo-tui: build demo-prereqs
	mkdir -p docs/demo tmp
	"$(VHS)" demos/vhs/tui-matrix-logo.tape

demo-full: build demo-prereqs
	mkdir -p docs/demo tmp
	"$(VHS)" demos/vhs/full-flow-launch.tape

release: clean
	mkdir -p "$(DIST_DIR)"
	GOOS=linux GOARCH=amd64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o "$(DIST_DIR)/aegiskeys_$(VERSION)_linux_amd64" .
	GOOS=linux GOARCH=arm64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o "$(DIST_DIR)/aegiskeys_$(VERSION)_linux_arm64" .
	GOOS=darwin GOARCH=amd64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o "$(DIST_DIR)/aegiskeys_$(VERSION)_darwin_amd64" .
	GOOS=darwin GOARCH=arm64 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o "$(DIST_DIR)/aegiskeys_$(VERSION)_darwin_arm64" .
	cd "$(DIST_DIR)" && sha256sum aegiskeys_$(VERSION)_* > SHA256SUMS

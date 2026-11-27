VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%d)

LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
DIST    := dist
INSTALL_DIR ?= $(HOME)/Developer/bin

.PHONY: build install release mac mac-arm64 mac-amd64 assets test docs vet fmt check clean

PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

# Builds the ./lid artifact. Host only: inside the sandbox this would write
# a Linux binary the Mac cannot exec.
build:
	@if [ "$(IS_SANDBOX)" = "1" ]; then \
		echo "make build writes ./lid for this Mac."; \
		echo "Inside the sandbox that produces a Linux binary macOS cannot exec."; \
		echo "Run 'make mac' instead, or build on the host."; \
		exit 1; \
	fi
	go build -ldflags "$(LDFLAGS)" -o lid .

# Builds ./lid and installs it over the lid on PATH in $(INSTALL_DIR). It goes
# in through a rename, so a running lid is never overwritten in place. 'lid
# update' then sees a source build and asks before putting a release back.
install: build
	@mkdir -p $(INSTALL_DIR)
	cp lid $(INSTALL_DIR)/.lid-install
	mv -f $(INSTALL_DIR)/.lid-install $(INSTALL_DIR)/lid
	@$(INSTALL_DIR)/lid --version

release:
	go build -ldflags "-s -w $(LDFLAGS)" -o lid .

# Cross-compiles macOS binaries into dist/. Safe from anywhere, sandbox
# included, because it never touches ./lid.
mac: mac-arm64 mac-amd64
	@echo "built:"
	@ls -1 $(DIST)/lid-darwin-*

mac-arm64: $(DIST)
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lid-darwin-arm64 .

mac-amd64: $(DIST)
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/lid-darwin-amd64 .

$(DIST):
	mkdir -p $(DIST)

# Packs the release assets 'lid update' downloads: one lid-$(VERSION)-<os>-<arch>.tar.gz
# per platform, plus the checksums.txt it verifies them against.
assets: $(DIST)
	@rm -f $(DIST)/lid-$(VERSION)-*.tar.gz $(DIST)/checksums.txt
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		echo "packing $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -ldflags "-s -w $(LDFLAGS)" -o $(DIST)/lid . || exit 1; \
		tar -czf $(DIST)/lid-$(VERSION)-$$os-$$arch.tar.gz -C $(DIST) lid || exit 1; \
		rm -f $(DIST)/lid; \
	done
	@cd $(DIST) && if command -v sha256sum >/dev/null 2>&1; then \
		sha256sum lid-$(VERSION)-*.tar.gz > checksums.txt; \
	else \
		shasum -a 256 lid-$(VERSION)-*.tar.gz > checksums.txt; \
	fi
	@echo "release assets in $(DIST):"
	@ls -1 $(DIST)/lid-$(VERSION)-*.tar.gz $(DIST)/checksums.txt

test:
	go test ./...

# Regenerates docs/commands.md from the command tree.
docs:
	LID_UPDATE_DOCS=1 go test ./internal/cli -run TestCommandDocsAreCurrent

vet:
	go vet ./...

fmt:
	gofmt -l -w .

check: fmt vet test

clean:
	rm -f lid
	rm -rf $(DIST)

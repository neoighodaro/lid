VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%d)

LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
DIST    := dist

.PHONY: build release mac mac-arm64 mac-amd64 test vet fmt check clean

# Builds the ./lid artifact that ~/.local/bin/lid symlinks to. Host only:
# inside the sandbox this would write a Linux binary the Mac cannot exec.
build:
	@if [ "$(IS_SANDBOX)" = "1" ]; then \
		echo "make build writes ./lid, which ~/.local/bin/lid symlinks to."; \
		echo "Inside the sandbox that produces a Linux binary macOS cannot exec."; \
		echo "Run 'make mac' instead, or build on the host."; \
		exit 1; \
	fi
	go build -ldflags "$(LDFLAGS)" -o lid .

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

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

check: fmt vet test

clean:
	rm -f lid
	rm -rf $(DIST)

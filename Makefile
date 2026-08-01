# localhostmgr Makefile
# Use a modern Go via Homebrew; the old /usr/local/go/bin/go may be 1.16.

GO ?= /usr/local/Cellar/go/1.26.5/bin/go
BIN ?= $(HOME)/.local/bin/localhostmgr
PKG := .

.PHONY: build install uninstall run clean test vet fmt

build:
	$(GO) build -o $(BIN) $(PKG)

install: build
	@echo "binary installed at $(BIN)"

uninstall:
	rm -f $(BIN)

run:
	$(GO) run $(PKG) $(ARGS)

test:
	$(GO) test ./...

vet:
	$(GO) vet $(PKG)

fmt:
	$(GO) fmt $(PKG)

clean:
	rm -f $(BIN)

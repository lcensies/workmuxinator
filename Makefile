PREFIX  ?= /usr/local
BINDIR  := $(PREFIX)/bin
DESTDIR ?=

VERSION ?= 0.3.0
BUILD_DIR := .build
BIN_PATH := $(BUILD_DIR)/workmuxinator

.PHONY: all build test install uninstall clean

all: build

build:
	mkdir -p $(BUILD_DIR)
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN_PATH) ./cmd/workmuxinator

test:
	go test ./...

install: build
	install -Dm755 $(BIN_PATH) $(DESTDIR)$(BINDIR)/workmuxinator

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/workmuxinator

clean:
	rm -rf $(BUILD_DIR)

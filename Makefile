APP := tracklm-windows
PKG := ./cmd/tracklm-windows
DIST_DIR := dist
MANIFEST := cmd/tracklm-windows/tracklm-windows.exe.manifest
ARCHES := amd64 arm64

GO ?= go
GOOS ?= windows
GOARCH ?= amd64
CGO_ENABLED ?= 0
VERSION ?= 0.1.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo local)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PKG := github.com/labx/tracklm-windows/internal/version
VERSION_LDFLAGS := -X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).Commit=$(COMMIT) -X $(VERSION_PKG).BuildDate=$(BUILD_DATE)
LDFLAGS ?= -s -w -H windowsgui $(VERSION_LDFLAGS)
OUT := $(DIST_DIR)/$(APP)-$(GOARCH).exe
DEFAULT_OUT := $(DIST_DIR)/$(APP).exe
RESOURCE := cmd/tracklm-windows/rsrc_$(GOOS)_$(GOARCH).syso

.DEFAULT_GOAL := build

.PHONY: build build-amd64 build-arm64 build-all debug test generate clean size

build: $(RESOURCE)
	@mkdir -p $(DIST_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) \
		$(GO) build -ldflags="$(LDFLAGS)" -o $(OUT) $(PKG)
	@if [ "$(GOOS)-$(GOARCH)" = "windows-amd64" ]; then cp $(OUT) $(DEFAULT_OUT); fi

build-amd64:
	$(MAKE) build GOARCH=amd64

build-arm64:
	$(MAKE) build GOARCH=arm64

build-all:
	@for arch in $(ARCHES); do $(MAKE) build GOARCH=$$arch; done

debug: LDFLAGS := $(VERSION_LDFLAGS)
debug: $(RESOURCE)
	@mkdir -p $(DIST_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) \
		$(GO) build -o $(DIST_DIR)/$(APP)-$(GOARCH)-debug.exe $(PKG)

$(RESOURCE): $(MANIFEST) cmd/tracklm-windows/resources.go
	$(GO) run github.com/akavel/rsrc@latest -arch $(GOARCH) -manifest $(MANIFEST) -o $(RESOURCE)

generate:
	@for arch in $(ARCHES); do \
		$(GO) run github.com/akavel/rsrc@latest -arch $$arch -manifest $(MANIFEST) -o cmd/tracklm-windows/rsrc_windows_$$arch.syso; \
	done

test:
	$(GO) test ./...

size: build
	@ls -lh $(OUT)

clean:
	rm -rf $(DIST_DIR)

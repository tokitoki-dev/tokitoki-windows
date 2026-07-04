APP := tracklm-windows
PKG := ./cmd/tracklm-windows
DIST_DIR := dist
MANIFEST := cmd/tracklm-windows/tracklm-windows.exe.manifest
ARCHES := amd64 arm64
AGENT_DIR := ../tracklm-goagent

GO ?= go
GOOS ?= windows
GOARCH ?= amd64
CGO_ENABLED ?= 0
VERSION ?= 0.1.0
COMMIT ?= local
BUILD_DATE ?= unknown
VERSION_PKG := github.com/labx/tracklm-windows/internal/version
VERSION_LDFLAGS := -X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).Commit=$(COMMIT) -X $(VERSION_PKG).BuildDate=$(BUILD_DATE)
LDFLAGS ?= -s -w -H windowsgui $(VERSION_LDFLAGS)
OUT := $(DIST_DIR)/$(APP)-$(GOARCH).exe
DEFAULT_OUT := $(DIST_DIR)/$(APP).exe
RESOURCE := cmd/tracklm-windows/rsrc_$(GOOS)_$(GOARCH).syso

ifeq ($(OS),Windows_NT)
MKDIR_DIST = if not exist "$(DIST_DIR)" mkdir "$(DIST_DIR)"
RM_DIST = if exist "$(DIST_DIR)" rmdir /S /Q "$(DIST_DIR)"
SET_GO_ENV = set GOOS=$(GOOS)&& set GOARCH=$(GOARCH)&& set CGO_ENABLED=$(CGO_ENABLED)&&
COPY_DEFAULT = if "$(GOOS)-$(GOARCH)"=="windows-amd64" copy /Y "$(OUT)" "$(DEFAULT_OUT)" >NUL
SHOW_SIZE = dir "$(OUT)"
CHECK_AGENT = if not exist "..\tracklm-goagent\go.mod" (echo Missing ..\tracklm-goagent. Put tracklm-goagent next to tracklm-windows or publish the module and remove the replace directive. && exit /B 1)
else
MKDIR_DIST = mkdir -p $(DIST_DIR)
RM_DIST = rm -rf $(DIST_DIR)
SET_GO_ENV = GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED)
COPY_DEFAULT = if [ "$(GOOS)-$(GOARCH)" = "windows-amd64" ]; then cp $(OUT) $(DEFAULT_OUT); fi
SHOW_SIZE = ls -lh $(OUT)
CHECK_AGENT = test -f $(AGENT_DIR)/go.mod || (echo "Missing $(AGENT_DIR). Put tracklm-goagent next to tracklm-windows or publish the module and remove the replace directive." && exit 1)
endif

.DEFAULT_GOAL := build

.PHONY: build build-amd64 build-arm64 build-all debug test generate clean size check-agent

build: check-agent $(RESOURCE)
	@$(MKDIR_DIST)
	$(SET_GO_ENV) $(GO) build -ldflags="$(LDFLAGS)" -o $(OUT) $(PKG)
	@$(COPY_DEFAULT)

build-amd64:
	$(MAKE) build GOARCH=amd64

build-arm64:
	$(MAKE) build GOARCH=arm64

build-all: build-amd64 build-arm64

debug: LDFLAGS := $(VERSION_LDFLAGS)
debug: check-agent $(RESOURCE)
	@$(MKDIR_DIST)
	$(SET_GO_ENV) $(GO) build -o $(DIST_DIR)/$(APP)-$(GOARCH)-debug.exe $(PKG)

$(RESOURCE): $(MANIFEST) cmd/tracklm-windows/resources.go
	$(GO) run github.com/akavel/rsrc@latest -arch $(GOARCH) -manifest $(MANIFEST) -o $(RESOURCE)

generate:
	$(GO) run github.com/akavel/rsrc@latest -arch amd64 -manifest $(MANIFEST) -o cmd/tracklm-windows/rsrc_windows_amd64.syso
	$(GO) run github.com/akavel/rsrc@latest -arch arm64 -manifest $(MANIFEST) -o cmd/tracklm-windows/rsrc_windows_arm64.syso

test: check-agent
	$(GO) test ./...

size: build
	@$(SHOW_SIZE)

clean:
	$(RM_DIST)

check-agent:
	@$(CHECK_AGENT)

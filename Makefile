GO ?= go
GOARCH ?= amd64
VERSION ?= 0.1.0
COMMIT ?= local
BUILD_DATE ?= unknown
PS ?= powershell
BUILD_SCRIPT := scripts/build.ps1
BUILD_ARGS := -Arch $(GOARCH) -Version $(VERSION) -Commit $(COMMIT) -BuildDate $(BUILD_DATE) -Go $(GO)

.DEFAULT_GOAL := build

.PHONY: build build-amd64 build-arm64 build-all debug test generate clean size check-agent

build:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File $(BUILD_SCRIPT) -Task build $(BUILD_ARGS)

build-amd64:
	$(MAKE) build GOARCH=amd64

build-arm64:
	$(MAKE) build GOARCH=arm64

build-all: build-amd64 build-arm64

debug:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File $(BUILD_SCRIPT) -Task debug $(BUILD_ARGS)

test:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File $(BUILD_SCRIPT) -Task test -Go $(GO)

generate:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File $(BUILD_SCRIPT) -Task generate -Go $(GO)

clean:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File $(BUILD_SCRIPT) -Task clean

size:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File $(BUILD_SCRIPT) -Task size $(BUILD_ARGS)

check-agent:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File $(BUILD_SCRIPT) -Task check-agent

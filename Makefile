PS ?= powershell
ARCH ?= amd64
CLI_VERSION ?=
# App version stamped into the exe. "dev" disables self-update; releases pass
# e.g. `make build VERSION=1.0.0`.
VERSION ?= dev

.DEFAULT_GOAL := build

.PHONY: build debug test clean

# Builds the sibling ../tokitoki-cli from source, gzips it into embedded/,
# then compiles the release exe with the payload embedded as a resource.
build:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1 -Task build -Arch $(ARCH) -CliVersion "$(CLI_VERSION)" -Version "$(VERSION)"

# Console-subsystem build with symbols; no CLI bundling.
debug:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1 -Task debug

test:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1 -Task test

clean:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1 -Task clean

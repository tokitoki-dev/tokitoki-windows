PS ?= powershell
ARCH ?= amd64
CLI_VERSION ?=

.DEFAULT_GOAL := build

.PHONY: build clean

# Builds the sibling ../tokitoki-cli from source, gzips it into embedded/,
# then `cargo build --release` embeds it. CI uses scripts/fetch-cli-release.ps1
# (pinned release download) instead of a local CLI build.
build:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1 -Task build -Arch $(ARCH) -CliVersion "$(CLI_VERSION)"

clean:
	$(PS) -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1 -Task clean

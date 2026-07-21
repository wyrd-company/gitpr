---
docs: true
title: Installation
order: 2
install: true
---

gitpr is a single, dependency-free Go binary. The one-liner above installs it
from the Wyrd Homebrew tap; the sections below cover every supported install
method, plus building from source.

## Homebrew

The formula is published to the `wyrd-company/tools` tap. Tap it once, then
install:

```bash
brew tap wyrd-company/tools
brew install gitpr
```

Upgrade later with:

```bash
brew upgrade gitpr
```

## go install

Requires a Go 1.26+ toolchain. Installs the latest tagged release into
`$(go env GOPATH)/bin`:

```bash
go install github.com/wyrd-company/gitpr/cmd/gitpr@latest
```

Make sure that directory is on your `PATH`. Binaries installed this way report
`dev` for `gitpr --version`, because the release version is stamped in only by
the release build.

## Prebuilt release binaries

Each tagged release publishes archives on
[GitHub Releases](https://github.com/wyrd-company/gitpr/releases) for macOS,
Linux, and Windows on both `amd64` and `arm64`. macOS and Linux ship as
`tar.gz`; Windows ships as `zip`. A `checksums.txt` file accompanies every
release.

1. Download the archive matching your OS and architecture.
2. Extract it and move the `gitpr` binary onto your `PATH`.
3. Verify the download against `checksums.txt` if you want to.

```bash
tar -xzf gitpr_<version>_<os>_<arch>.tar.gz
mv gitpr ~/.local/bin/
```

## Build from source

This repo uses [Task](https://taskfile.dev) for its build. You need:

- Go 1.26 or newer
- [Task](https://taskfile.dev)
- [`tagver`](https://github.com/wyrd-company/tagver) — used to stamp the version
  into the binary

Clone the repo and build:

```bash
git clone https://github.com/wyrd-company/gitpr.git
cd gitpr
task build
```

`task build` compiles `./cmd/gitpr` and installs it to `~/.local/bin/gitpr`,
embedding the `tagver`-calculated version. Ensure `~/.local/bin` is on your
`PATH`.

To build without Task, run `go build` directly — note the version will report
`dev` unless you stamp it yourself:

```bash
go build -o gitpr ./cmd/gitpr
```

## Verify

Confirm the binary is installed and on your `PATH`:

```bash
gitpr --version
```

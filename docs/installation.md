# Installation

hazel is a single binary. It bundles SQLite through modernc.org/sqlite, so it needs no C compiler and no SQLite library.

## Homebrew

```
brew install winebarrel/hazel/hazel
```

Write the tap's name with the cask's. Homebrew's own `hazel` cask is another application.

## Release binaries and packages

The [releases](https://github.com/winebarrel/hazel/releases) page has archives for macOS, Linux and Windows on amd64 and arm64, and `.deb`, `.rpm` and `.apk` packages for Linux.

```
curl -sSLO https://github.com/winebarrel/hazel/releases/download/vX.Y.Z/hazel_X.Y.Z_linux_amd64.tar.gz
tar xf hazel_X.Y.Z_linux_amd64.tar.gz hazel
```

## Go

```
go install github.com/winebarrel/hazel/cmd/hazel@latest
```

## Verifying a download

The release assets, `checksums.txt` and the binaries in the archives carry [GitHub artifact attestations](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations). They show that the file was built by the release workflow of this repository:

```
gh attestation verify hazel_X.Y.Z_linux_amd64.tar.gz --repo winebarrel/hazel
gh attestation verify "$(command -v hazel)" --repo winebarrel/hazel
```

## Demo image

`ghcr.io/winebarrel/hazel-demo` bundles hazel, sqlite3 and a sample database. See [Try it with Docker](index.md#try-it-with-docker).

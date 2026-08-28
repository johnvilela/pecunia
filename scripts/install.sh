#!/bin/sh
# Install kakei to $BIN_DIR (default ~/.local/bin), then run `kakei setup`.
#   curl -sS https://raw.githubusercontent.com/johnvilela/kakei/master/scripts/install.sh | sh
# Inside the repo: builds and installs the local checkout (requires Go).
# Standalone: downloads the latest release tarball, checksum-verified.
set -eu

BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) echo "error: unsupported OS: $(uname -s) (linux and macos only)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "error: unsupported architecture: $(uname -m) (amd64 and arm64 only)" >&2; exit 1 ;;
esac

mkdir -p "$BIN_DIR"
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." 2>/dev/null && pwd)
if [ -f "$repo_root/go.mod" ]; then
    command -v go >/dev/null 2>&1 || { echo "error: go is required (https://go.dev/dl)" >&2; exit 1; }
    go build -C "$repo_root" -trimpath -ldflags="-s -w" -o "$BIN_DIR/kakei" ./cmd
else
    command -v sha256sum >/dev/null 2>&1 || sha256sum() { shasum -a 256 "$@"; }
    version=$(curl -fsSL https://api.github.com/repos/johnvilela/kakei/releases/latest \
        | grep -m1 '"tag_name"' | sed 's/.*"v\{0,1\}\([0-9][^"]*\)".*/\1/')
    [ -n "$version" ] || { echo "error: could not resolve the latest release" >&2; exit 1; }
    asset="kakei_${version}_${os}_${arch}.tar.gz"
    url="https://github.com/johnvilela/kakei/releases/latest/download"
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    curl -fsSL -o "$tmp/$asset" "$url/$asset"
    curl -fsSL -o "$tmp/checksums.txt" "$url/checksums.txt"
    (cd "$tmp" && grep " $asset\$" checksums.txt | sha256sum -c - >/dev/null)
    tar -xzf "$tmp/$asset" -C "$tmp" kakei
    install -m 755 "$tmp/kakei" "$BIN_DIR/kakei"
fi
echo "installed $BIN_DIR/kakei"

if [ -t 0 ]; then
    "$BIN_DIR/kakei" setup
elif (exec </dev/tty) 2>/dev/null; then
    # curl | sh: stdin is the script — borrow the terminal so setup stays interactive
    "$BIN_DIR/kakei" setup </dev/tty
else
    echo "next: run 'kakei setup' to create the database and hook up an AI agent"
fi

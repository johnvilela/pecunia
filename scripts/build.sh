#!/bin/sh
# ponytail: CGO off — modernc.org/sqlite is pure Go, so this is a static single binary.
# Cross-compile: GOOS=darwin GOARCH=arm64 ./build.sh
set -e

CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o kakei ./cmd
echo "built: $(pwd)/kakei"

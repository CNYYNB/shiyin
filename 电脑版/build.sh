#!/bin/bash
# Build 拾音 PC: Windows exe + NSIS installer (run on Linux).
set -euo pipefail
cd "$(dirname "$0")"
export NSISDIR=/workspace/tools/nsis/usr/share/nsis
export PATH=/workspace/tools/nsis/usr/bin:/workspace/tools/go/bin:/workspace/tools/gopath/bin:$PATH
export GOPATH=/workspace/tools/gopath GOMODCACHE=/workspace/tools/gopath/pkg/mod GOCACHE=/workspace/tools/gocache
VERSION=$(grep -o 'appVersion = "[^"]*"' main.go | cut -d'"' -f2)
mkdir -p dist
rm -f "dist/拾音-Setup-$VERSION.exe"
go-winres make --in winres/winres.json --arch amd64 --out rsrc
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w" -o dist/Shiyin.exe .
(cd installer && makensis -V2 -DVERSION="$VERSION" -DOUTFILE="../dist/拾音-Setup-$VERSION.exe" shiyin.nsi)
ls -la dist
file dist/Shiyin.exe "dist/拾音-Setup-$VERSION.exe"

#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"

mkdir -p dist

echo "[BUILD] Building TermChat for Linux (x86_64 / PC)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/termchat-linux-amd64 .
cp dist/termchat-linux-amd64 termchat

echo "[BUILD] Building TermChat for Android / Termux (ARM64)..."
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -ldflags="-s -w" -o dist/termchat-android-arm64 .

echo "[BUILD] Building TermChat for Android / Termux (ARM 32-bit)..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -ldflags="-s -w" -o dist/termchat-android-arm .

echo "[OK] Build complete! Binaries are in ./dist/"
ls -lh dist/

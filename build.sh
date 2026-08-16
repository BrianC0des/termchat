#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"

mkdir -p dist

echo "🔨 Building TermChat for Linux (x86_64 / PC)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/termchat-amd64 .
cp dist/termchat-amd64 termchat

echo "📱 Building TermChat for Android / Termux (ARM64)..."
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -ldflags="-s -w" -o dist/termchat-arm64 .

echo "📱 Building TermChat for Android / Termux (ARM 32-bit)..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -ldflags="-s -w" -o dist/termchat-arm .

echo "✅ Build complete! Binaries are in ./dist/"
ls -lh dist/

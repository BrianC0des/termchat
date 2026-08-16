#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"

mkdir -p dist

echo "🚀 Building TermChat for ALL Operating Systems..."
echo ""

# 1. Linux PC (x86_64 and ARM64)
echo "🐧 Building for Linux (PC x86_64)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/termchat-linux-amd64 .
cp dist/termchat-linux-amd64 termchat

echo "🐧 Building for Linux (ARM64)..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o dist/termchat-linux-arm64 .

# 2. Windows (.exe)
echo "🪟 Building for Windows (64-bit .exe)..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/termchat-windows.exe .

echo "🪟 Building for Windows (ARM64 .exe)..."
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags="-s -w" -o dist/termchat-windows-arm64.exe .

# 3. macOS (Apple Silicon M1/M2/M3/M4 & Intel)
echo "🍎 Building for macOS (Apple Silicon M1/M2/M3/M4)..."
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o dist/termchat-mac-apple-silicon .

echo "🍎 Building for macOS (Intel)..."
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o dist/termchat-mac-intel .

# 4. Android (Termux)
echo "📱 Building for Android / Termux (ARM64)..."
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -ldflags="-s -w" -o dist/termchat-android-arm64 .
cp dist/termchat-android-arm64 dist/termchat-arm64

echo "📱 Building for Android / Termux (ARM 32-bit)..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -ldflags="-s -w" -o dist/termchat-android-arm .

echo ""
echo "✅ All platform binaries built successfully!"
ls -lh dist/

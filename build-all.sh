#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"

rm -rf dist
mkdir -p dist

echo "[BUILD] Building TermChat for 4 Primary Platform Targets..."
echo ""

# 1. Linux PC (x86_64 / amd64)
echo "[1/4] Building for Linux PC (x86_64 / amd64)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/termchat-linux-amd64 .
cp dist/termchat-linux-amd64 termchat

# 2. Windows 64-bit (.exe)
echo "[2/4] Building for Windows (64-bit x86_64 .exe)..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/termchat-windows.exe .

# 3. Android (Termux ARM64)
echo "[3/4] Building for Android Termux (ARM64)..."
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -ldflags="-s -w" -o dist/termchat-android-arm64 .

# 4. macOS (Apple Silicon M1/M2/M3/M4)
echo "[4/4] Building for macOS (Apple Silicon ARM64)..."
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o dist/termchat-mac-apple-silicon .

# 5. Apply UPX compression to Linux PC binary if available
if command -v upx >/dev/null 2>&1; then
    echo "[UPX] Applying high-efficiency compression..."
    upx --best --lzma dist/termchat-linux-amd64 2>/dev/null || true
fi

# 6. Create clean .tar.zst and .zip packages
echo "[ARCHIVE] Generating .tar.zst and .zip release archives..."
cd dist
tar --zstd -cvf termchat-linux-amd64.tar.zst termchat-linux-amd64
tar --zstd -cvf termchat-android-arm64.tar.zst termchat-android-arm64
tar --zstd -cvf termchat-mac-apple-silicon.tar.zst termchat-mac-apple-silicon
zip termchat-windows.zip termchat-windows.exe
sha256sum * > checksums.txt
cd ..

echo ""
echo "[OK] 4 Core Release Packages Created Successfully!"
ls -lh dist/


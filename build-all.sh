#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"

mkdir -p dist

echo "[BUILD] Building TermChat for ALL Operating Systems..."
echo ""

# 1. Linux PC (x86_64 and ARM64)
echo "[LINUX] Building for Linux (PC x86_64)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/termchat-linux-amd64 .
cp dist/termchat-linux-amd64 termchat

echo "[LINUX] Building for Linux (ARM64)..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o dist/termchat-linux-arm64 .

# 2. Windows (.exe)
echo "[WIN] Building for Windows (64-bit .exe)..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/termchat-windows.exe .

echo "[WIN] Building for Windows (ARM64 .exe)..."
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags="-s -w" -o dist/termchat-windows-arm64.exe .

# 3. macOS (Apple Silicon M1/M2/M3/M4 & Intel)
echo "[MAC] Building for macOS (Apple Silicon M1/M2/M3/M4)..."
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o dist/termchat-mac-apple-silicon .

echo "[MAC] Building for macOS (Intel)..."
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o dist/termchat-mac-intel .

# 4. Android (Termux)
echo "[TERMUX] Building for Android / Termux (ARM64)..."
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -ldflags="-s -w" -o dist/termchat-android-arm64 .

echo "[TERMUX] Building for Android / Termux (ARM 32-bit)..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -ldflags="-s -w" -o dist/termchat-android-arm .

# 5. Apply UPX compression if installed
if command -v upx >/dev/null 2>&1; then
    echo "[UPX] Applying UPX high-ratio compression..."
    upx --best --lzma dist/termchat-linux-amd64 2>/dev/null || true
    upx --best --lzma dist/termchat-windows.exe 2>/dev/null || true
    upx --best --lzma dist/termchat-android-arm 2>/dev/null || true
fi

# 6. Create Zstandard and Gzip Archives in dist/
cd dist
tar --zstd -cvf termchat-linux-amd64.tar.zst termchat-linux-amd64
tar --zstd -cvf termchat-linux-arm64.tar.zst termchat-linux-arm64
tar --zstd -cvf termchat-mac-apple-silicon.tar.zst termchat-mac-apple-silicon
tar --zstd -cvf termchat-mac-intel.tar.zst termchat-mac-intel
tar --zstd -cvf termchat-android-arm64.tar.zst termchat-android-arm64
tar --zstd -cvf termchat-android-arm.tar.zst termchat-android-arm
tar -czvf termchat-linux-amd64.tar.gz termchat-linux-amd64
tar -czvf termchat-linux-arm64.tar.gz termchat-linux-arm64
tar -czvf termchat-mac-apple-silicon.tar.gz termchat-mac-apple-silicon
tar -czvf termchat-mac-intel.tar.gz termchat-mac-intel
tar -czvf termchat-android-arm64.tar.gz termchat-android-arm64
tar -czvf termchat-android-arm.tar.gz termchat-android-arm
zip termchat-windows.zip termchat-windows.exe
zip termchat-windows-arm64.zip termchat-windows-arm64.exe
sha256sum * > checksums.txt
cd ..

echo ""
echo "[OK] All platform binaries and .tar.zst archives built successfully!"
ls -lh dist/

#!/usr/bin/env bash
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"

LOCAL_IP=$(ip -4 addr show scope global | grep inet | awk '{print $2}' | cut -d/ -f1 | head -n 1)
PORT=8000

echo "=========================================================="
echo "[NET] TermChat Phone Quick-Install Server"
echo "=========================================================="
echo ""
echo "[TERMUX] Run this single command inside your Termux terminal:"
echo ""
echo "   curl -sSL http://${LOCAL_IP}:${PORT}/dist/termchat-android-arm64 -o \$PREFIX/bin/termchat && chmod +x \$PREFIX/bin/termchat"
echo ""
echo "Then simply type:"
echo "   termchat"
echo ""
echo "Serving files on http://${LOCAL_IP}:${PORT}... (Press Ctrl+C when done)"
echo "=========================================================="

python3 -m http.server $PORT

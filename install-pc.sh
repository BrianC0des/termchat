#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p ~/.local/bin

if [ -f "$DIR/dist/termchat-amd64" ]; then
    cp "$DIR/dist/termchat-amd64" ~/.local/bin/termchat
else
    cd "$DIR" && go build -o ~/.local/bin/termchat .
fi

chmod +x ~/.local/bin/termchat
echo "✨ TermChat installed to ~/.local/bin/termchat"
echo "You can now run 'termchat' from anywhere in your terminal!"

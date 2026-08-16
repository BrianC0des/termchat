# ⚡ TermChat

**TermChat** is a cross-platform, peer-to-peer and cloud-connected TUI (Terminal User Interface) companion and secret messenger for **Windows**, **macOS**, **Linux**, and **Android (Termux)**.

Powered by **Go**, **Bubble Tea**, **Lip Gloss**, and **24/7 Global WebSockets**.

---

## ⚡ 1-Line Quick Installation (via Terminal)

Run the command for your operating system to download and install TermChat instantly:

### 📱 Android (Termux)
```bash
curl -sSL https://github.com/BrianC0des/termchat/releases/download/v1.0.0/termchat-android-arm64 -o $PREFIX/bin/termchat && chmod +x $PREFIX/bin/termchat
```

### 🐧 Linux (x86_64 / PC / Laptop)
```bash
mkdir -p ~/.local/bin && curl -sSL https://github.com/BrianC0des/termchat/releases/download/v1.0.0/termchat-linux-amd64 -o ~/.local/bin/termchat && chmod +x ~/.local/bin/termchat
```

### 🐧 Linux (ARM64 / Raspberry Pi)
```bash
mkdir -p ~/.local/bin && curl -sSL https://github.com/BrianC0des/termchat/releases/download/v1.0.0/termchat-linux-arm64 -o ~/.local/bin/termchat && chmod +x ~/.local/bin/termchat
```

### 🍎 macOS (Apple Silicon M1/M2/M3/M4)
```bash
sudo curl -sSL https://github.com/BrianC0des/termchat/releases/download/v1.0.0/termchat-mac-apple-silicon -o /usr/local/bin/termchat && sudo chmod +x /usr/local/bin/termchat
```

### 🍎 macOS (Intel)
```bash
sudo curl -sSL https://github.com/BrianC0des/termchat/releases/download/v1.0.0/termchat-mac-intel -o /usr/local/bin/termchat && sudo chmod +x /usr/local/bin/termchat
```

### 🪟 Windows (PowerShell)
```powershell
mkdir -Force "$HOME\bin"; Invoke-WebRequest -Uri "https://github.com/BrianC0des/termchat/releases/download/v1.0.0/termchat-windows.exe" -OutFile "$HOME\bin\termchat.exe"; [Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$HOME\bin", "User"); $env:Path += ";$HOME\bin"; termchat
```
*(Or simply download and run `.\termchat.exe`)*

---

## 🚀 How to Launch & Create Rooms

### 1. Join a 24/7 Cloud Room (Works Anywhere in the World):
```bash
termchat -name "YourNick" -room "secret-squad" -pass "mysecretpassword123"
```
*(Connected via the 24/7 Global Relay at `wss://termchat-o51d.onrender.com/ws`)*

### 2. Local Wi-Fi Mode (Zero-Config Auto-Discovery):
```bash
termchat
```
*(Automatically discovers and connects to all devices on the same Wi-Fi without internet!)*

---

## ⌨️ Command Cheatsheet (Inside Chat)

| Command | Shortcut | Description |
|---|---|---|
| `/room <name> [pass]` | | Create or join a 24/7 Cloud room with optional AES-256 password |
| `/auth <pass>` | | Lock or unlock AES-256 End-to-End Encryption in current chat |
| `/copy` or `/cp` | | Copy the latest message to your OS clipboard (`/copy all` for full log) |
| `/paste` or `/p` | `Ctrl + V` | Paste system clipboard directly into chat bar |
| `/files` or `/vault` | `Ctrl + F` | Browse all uploaded files in the room & click to download |
| `/get <num\|url>` | | Download shared file by index number (e.g. `/get 1`) or URL |
| `/browse` | `Ctrl + O` | Open interactive file explorer to select & send files |
| `/send <filepath>` | `Tab` | Send / upload file to everyone in the room |
| `/clip` or `/c` | | Push your clipboard directly to connected peers |
| `/battery` | | Query phone battery percentage and charging status |
| `/ring` or `/find` | | Ring/vibrate connected mobile device |
| `/notify <msg>` | | Push notification popup to phone lock screen |
| `@agy <prompt>` | | Ask Antigravity AI anything directly in the chatroom |
| `/ip` / `/peers` | | Show local network info and connected peers |
| `/clear` | | Clear screen messages |
| `/help` | `F1` | Open in-app help cheatsheet |
| `/quit` | `Ctrl + C` | Exit TermChat |

---

## 🔒 Security & Privacy
- **End-to-End Encryption**: When a room has a password (`-pass` or `/auth`), messages and files are encrypted with **AES-256-GCM** before leaving your device.
- **Zero Server Logging**: The relay server acts purely as a stateless WebSocket router and cannot decrypt any password-protected traffic.

---

## 🛠️ Building From Source

```bash
git clone https://github.com/BrianC0des/termchat.git
cd termchat
./build-all.sh
```
Binary outputs will be placed in `./dist/`.

# ⚡ TermChat

**TermChat** is a peer-to-peer TUI (Terminal User Interface) companion and messenger designed for instant chat, high-speed file transfers, shared clipboard, and hardware control between your **PC** and your **Phone (Termux)** over local Wi-Fi.

Powered by **Go**, **Bubble Tea**, and **Lip Gloss**.

---

## ✨ Features

- 🔍 **Zero-Config LAN Auto-Discovery**: Automatically finds and connects your PC and Termux phone over Wi-Fi (UDP broadcast).
- 💬 **Live TUI Chat**: Real-time messaging with timestamps, sender tags, auto-scrolling, and responsive layouts.
- 📁 **High-Speed File Transfers**: Stream files with live progress bars (`/send <path>` or interactive `Ctrl+O` file browser).
- 📋 **Shared Clipboard Sync**: Seamlessly sync clipboard between PC (`wl-copy`/`xclip`) and Android (`termux-clipboard-set`) with `/clip`.
- 🔋 **Battery Monitoring**: Query phone battery percentage and charging state directly with `/battery`.
- 📱 **Hardware Controls**: Ring/vibrate phone (`/ring`), send push notifications (`/notify`), or open browser links (`/open <url>`).
- 🎵 **Media Remote Control**: Control music/video playback on PC from your phone (`/play`, `/pause`, `/next`).
- 💻 **Remote Command Runner**: Execute remote shell commands and stream back the terminal output (`/exec <cmd>`).
- 🔒 **End-to-End Encryption (E2EE)**: Optional AES-256-GCM encrypted room (`/auth <passphrase>`).
- 📶 **ASCII QR Code Pairing**: Generate an instant QR code in your terminal (`/qr`).
- 💾 **Persistent Chat History**: Automatically remembers recent messages across sessions (`~/.local/share/termchat/history.jsonl`).

---

## 🚀 Quick Setup

### 1. On Your PC
The binary is already built and installed to `~/.local/bin/termchat`:
```bash
termchat
```

---

### 2. On Your Phone (Termux)

Make sure your phone and PC are connected to the same Wi-Fi (or mobile hotspot).

**One-line install from PC:**
1. On your PC:
   ```bash
   cd ~/Projects/termchat
   ./serve-termux.sh
   ```
2. In **Termux** on your phone:
   ```bash
   curl -sSL http://<YOUR_PC_IP>:8000/dist/termchat-arm64 -o $PREFIX/bin/termchat && chmod +x $PREFIX/bin/termchat
   ```
3. Run on Termux:
   ```bash
   termchat
   ```

---

## ⌨️ Full Command Cheatsheet

| Command | Key / Shortcut | Description | Example |
|---|---|---|---|
| `/browse` | `Ctrl + O` | Open interactive file explorer to pick and send files | `Ctrl+O` |
| `/send <path>` | `Tab` autocompletion | Send a file directly by path | `/send ~/Pictures/photo.jpg` |
| `/clip` or `/c` | | Sync local clipboard to connected device | `/clip` |
| `/battery` | | Check peer device battery % & charging status | `/battery` |
| `/notify <msg>` | | Send a native popup notification to phone screen | `/notify Pick up groceries!` |
| `/ring` or `/find` | | Ring/vibrate phone at max volume to locate it | `/ring` |
| `/open <url>` | | Open a web link in peer's default browser | `/open https://github.com` |
| `/play` / `/next` | | Play/Pause/Skip media playback | `/play`, `/next`, `/prev` |
| `/exec <cmd>` | | Run shell command on peer and stream output | `/exec uname -a` |
| `/auth <pass>` | | Enable End-to-End AES-256 Encryption | `/auth secret123` |
| `/qr` | | Display ASCII QR Code for instant pairing | `/qr` |
| `/nick <name>` | | Change your chat display name on the fly | `/nick AndroidPhone` |
| `/dir <path>` | | Change incoming downloads folder | `/dir ~/storage/downloads` |
| `/ip` / `/peers` | | Show local IP addresses & connected peers | `/ip`, `/peers` |
| `/help` | `F1` | Show interactive help modal | `/help` |
| `/clear` | | Clear chat history screen | `/clear` |
| `/quit` | `Ctrl + C` | Exit TermChat | `/quit` |

---

## 📱 Termux Integration & Permissions

To enable all phone hardware features in Termux:
1. Install Termux:API addon from F-Droid or GitHub.
2. Inside Termux, run:
   ```bash
   pkg install termux-api
   termux-setup-storage
   ```
3. In TermChat on phone, set your download folder to your Android gallery/downloads:
   ```text
   /dir ~/storage/downloads
   ```

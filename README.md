# :: TERMCHAT ::

> **Terminal-First Developer Collab Room & Secret Messenger** for **Linux**, **Windows**, **macOS (Apple Silicon)**, and **Android (Termux)**.

[![Release](https://img.shields.io/github/v/release/BrianC0des/termchat?style=flat-square&color=50fa7b)](https://github.com/BrianC0des/termchat/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/BrianC0des/termchat/release.yml?style=flat-square)](https://github.com/BrianC0des/termchat/actions)
[![License](https://img.shields.io/github/license/BrianC0des/termchat?style=flat-square)](LICENSE)

TermChat brings modern developer collaboration directly into your terminal. Zero-commit live git diff sharing, 1-command patch application, GitHub PR/Issue cards, Discord-style collapsible code folding, external editor compose (`nvim`/`nano`), and end-to-end encrypted team rooms.

---

## ⚡ 1-Line Universal Install

Install or update TermChat with a single command:

### 🐧 Linux, 🍏 macOS (Apple Silicon), & 📱 Android (Termux)
```bash
curl -fsSL https://raw.githubusercontent.com/BrianC0des/termchat/main/install.sh | bash
```

### 🪟 Windows (PowerShell)
```powershell
irm https://raw.githubusercontent.com/BrianC0des/termchat/main/install.ps1 | iex
```

---

## 🐙 Project Collab Rooms (Auto-Join on `git clone`)

Turn any Git repository into an instant developer collaboration space:

### 1. Initialize your project room
```bash
cd my-project
termchat init my-team-room
```
This generates `.termchat/room.json` linked to your Git remote repository.

### 2. Teammates clone and auto-join
```bash
git clone https://github.com/my-org/my-project.git
cd my-project && termchat
```
TermChat automatically detects `.termchat/room.json` and connects your team into the project room in seconds!

---

## 🚀 Key Features

### 🌿 Zero-Commit Git Diff Sharing & 1-Key Patching
- `/diff` (or `/diff staged`): Captures your current uncommitted changes and broadcasts an interactive patch card.
- `/apply <patch_id>`: Any teammate can apply the diff directly to their local repository with safe collision dry-runs.
- `/branch` & `/checkout <branch>`: Inspect branches and switch workspaces without leaving chat.

### 🐙 Native GitHub PR, Issue & CI/CD Integration
- `/pr <number>`: Live pull request inspection (review approvals, additions/deletions, branches).
- `/checkout #<number>`: Check out any PR branch locally with 1 command.
- `/issue <number>`: Interactive issue preview cards.
- `/ci`: Real-time GitHub Actions workflow status report.

### 📝 External Editor Compose (`nvim` / `nano` / `vim`)
- Press `Ctrl+X` or type `/editor` to open your favorite editor to compose long code blocks, markdown notes, or architectural thoughts. Auto-populates into chat upon save.

### ⌨️ Modern Terminal UX
- **Multiline Input**: Press `Shift+Enter` or `Alt+Enter` to insert newlines without sending.
- **Collapsible Code Blocks**: Press `Ctrl+E` or `F4` to fold/unfold long stacktraces and code snippets.
- **Clean Toast Status Bar**: Transient status notifications (`⚡`) display in the header bar instead of cluttering chat history.
- **Interactive File Vault**: Press `Ctrl+F` to browse shared room files or `Ctrl+O` to open the visual file picker with custom folder (`📁`) and language icons (`🐹`, `🐍`, `🦀`, `📜`, `⚙️`, `🌐`).

### 🛡️ End-to-End Encryption & Device Identity
- **AES-256-GCM E2E**: Encrypts messages and file transfers with key verification codes.
- **Ed25519 Cryptographic Identity**: Persistent device keys (`/identity`) preventing impersonation.
- **Room Moderation**: `/invite` (1-click magic link & QR), `/kick`, `/ban`, `/unban`, and `/banlist`.

---

## 📋 Slash Command Cheatsheet

| Command | Shortcut | Description |
|---|---|---|
| `/diff` / `/patch` | | Broadcast uncommitted Git diff card with `#patch-xxxx` ID |
| `/apply <patch_id>` | | Safely apply shared patch to your local workspace |
| `/branch` / `/checkout <name>` | | Inspect active branch or switch branches |
| `/pr <#>` / `/checkout #<#>` | | Fetch GitHub PR card or checkout PR branch |
| `/issue <#>` / `/ci` | | Preview GitHub Issue or check GitHub Actions CI status |
| `/editor` / `/compose` | `Ctrl + X` | Open `$EDITOR` (`nvim`/`nano`/`vim`) to compose text |
| `/init [room]` | | Scaffold `.termchat/room.json` for team auto-join |
| `/room <name> [pass]` | | Join or create a 24/7 cloud room |
| `/invite` / `/qr` | | Generate 1-click room invite link and ASCII QR code |
| `/identity` / `/whoami` | | Show device Ed25519 fingerprint & public key |
| `/kick <user>` / `/ban <user>` | | Room moderation and banlist management |
| `/files` | `Ctrl + F` | Open Shared Files Vault modal |
| `/get <id\|#\|name>` | | 1-command download shared room file |
| `/browse` / `/send <file>` | `Ctrl + O` | Visual file explorer / send file with optional TTL |
| `/theme <name>` | | Switch themes (`catppuccin`, `dracula`, `nord`, `matrix`, `tokyonight`) |
| `/update` | | 1-Click self-update binary to latest release |
| `/help` | `F1` | Interactive help modal |

---

## 📦 4-Platform Architecture

TermChat compiles and releases dedicated native binaries for:
- **Linux x86_64** (`.tar.zst`)
- **Windows x86_64** (`.zip`)
- **macOS Apple Silicon M1-M4** (`.tar.zst`)
- **Android Termux ARM64** (`.tar.zst`)

---

## 🛠️ Building From Source

```bash
git clone https://github.com/BrianC0des/termchat.git
cd termchat
go build -o termchat .
```

To build all 4 release targets:
```bash
./build-all.sh
```

---

## 📜 License
MIT License © [BrianC0des](https://github.com/BrianC0des)

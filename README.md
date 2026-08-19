# :: TERMCHAT ::

> **Terminal-First Developer Collab Room & Secret Messenger** for **Linux**, **Windows**, **macOS (Apple Silicon)**, and **Android (Termux)**.

[![Release](https://img.shields.io/github/v/release/BrianC0des/termchat?style=flat-square&color=50fa7b)](https://github.com/BrianC0des/termchat/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/BrianC0des/termchat/release.yml?style=flat-square)](https://github.com/BrianC0des/termchat/actions)
[![License](https://img.shields.io/github/license/BrianC0des/termchat?style=flat-square)](LICENSE)

TermChat brings modern developer collaboration directly into your terminal. Zero-commit live git diff sharing, 1-command patch application, GitHub PR/Issue cards, Discord-style collapsible code folding, external editor compose (`nvim`/`nano`), Chrome-style differential binary delta updates, and end-to-end encrypted team rooms.

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
This generates `.termchat/room.json` linked to your Git remote repository with your Ed25519 identity as creator.

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
- `/pr <number>`: Live pull request inspection (review approvals, additions/deletions, branches) with `Ctrl+E` folding.
- `/checkout #<number>`: Check out any PR branch locally with 1 command.
- `/issue <number>`: Interactive issue preview cards with label badges and description.
- `/ci`: Real-time GitHub Actions workflow status report.

### ⚡ Chrome-Style Differential Delta Updates
- Micro-patch updates (`.delta.zst` ~100KB vs 5.5MB full binary).
- In-memory reconstruction in <15ms with SHA-256 integrity verification and atomic zero-downtime swap.

### 📝 External Editor Compose (`nvim` / `nano` / `vim`)
- Press `Ctrl+X` or type `/editor` to open your favorite editor to compose long code blocks, markdown notes, or architectural thoughts. Auto-populates into chat upon save.

### ⌨️ Modern Terminal UX
- **Universal Multiline Input**: Press `Shift+Enter`, `Alt+Enter`, `Ctrl+J`, or `Ctrl+N` to insert newlines without sending.
- **Dynamic Text Alignment**: Continuation lines align with the sender nickname column.
- **Collapsible Code Blocks**: Press `Ctrl+E` or `F4` to fold/unfold long stacktraces and code snippets.
- **Clean Toast Status Bar**: Transient status notifications (`::`) display in the header bar instead of cluttering chat history.
- **Interactive File Vault**: Press `Ctrl+F` to browse shared room files or `Ctrl+O` to open the visual file picker with clean developer CLI tags (`[DIR]`, `[go]`, `[py]`, `[rs]`, `[ts]`, `[img]`).

### 🛡️ End-to-End Encryption & Device Identity
- **AES-256-GCM E2E**: Encrypts messages and file transfers with key verification codes.
- **Ed25519 Cryptographic Identity**: Persistent device keys (`/identity`) preventing impersonation.
- **Room Moderation & Self-Destruct**: `/create [room] [pw]`, `/join <room> [pw]`, `/invite` (1-click magic link & QR), `/destroy <code>` (instant RAM wipe), `/kick`, `/ban`, `/unban`.

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
| `/create [name] [pw]` | | Create & host new cloud room with optional AES-256 password |
| `/join <name> [pw]` | | Join existing room or switch channels |
| `/init [room]` | | Scaffold `.termchat/room.json` for team auto-join |
| `/invite` / `/qr` | | Generate 1-click room invite link and ASCII QR code |
| `/destroy <code>` | | Room creator instant self-destruct: zero RAM & wipe |
| `/expire /autodel` | | Room self-destruct countdown & disappearing messages |
| `/identity` / `/whoami` | | Show device Ed25519 fingerprint & public key |
| `/kick <user>` / `/ban <user>` | | Room moderation and banlist management |
| `/files` | `Ctrl + F` | Open Shared Files Vault modal |
| `/get <id\|#\|name>` | | 1-command download shared room file |
| `/browse` / `/send <file>` | `Ctrl + O` | Visual file explorer / send file with clean tags |
| `/theme <name>` | | Switch themes (`catppuccin`, `dracula`, `nord`, `matrix`, `tokyonight`) |
| `/update` | | 1-Click differential binary self-update to latest release |
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

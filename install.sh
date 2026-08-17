#!/usr/bin/env bash
# ==============================================================================
# TermChat - Developer Collaboration Room Installer for Linux, macOS & Android
# Usage: curl -fsSL https://raw.githubusercontent.com/BrianC0des/termchat/main/install.sh | bash
# ==============================================================================

set -e

REPO="BrianC0des/termchat"
GITHUB_LATEST_API="https://api.github.com/repos/${REPO}/releases/latest"
INSTALL_DIR="${HOME}/.local/bin"

# Color formatting
BOLD="\033[1m"
GREEN="\033[1;32m"
BLUE="\033[1;34m"
CYAN="\033[1;36m"
YELLOW="\033[1;33m"
RED="\033[1;31m"
RESET="\033[0m"

echo -e "${CYAN}${BOLD}"
echo "  ╔═══════════════════════════════════════════════════════╗"
echo "  ║   🚀 TermChat — Terminal Developer Collab Hub         ║"
echo "  ╚═══════════════════════════════════════════════════════╝"
echo -e "${RESET}"

# 1. Detect Operating System & Architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "${OS}" in
    linux*)
        if [ -n "${TERMUX_VERSION:-}" ] || [ -d "/data/data/com.termux" ]; then
            TARGET_OS="android"
            INSTALL_DIR="${PREFIX:-/data/data/com.termux/files/usr}/bin"
        else
            TARGET_OS="linux"
        fi
        ;;
    darwin*)
        TARGET_OS="mac"
        ;;
    msys*|mingw*|cygwin*)
        echo -e "${YELLOW}Detected Windows environment.${RESET}"
        echo -e "Please run the native PowerShell installer:"
        echo -e "  ${BOLD}irm https://raw.githubusercontent.com/${REPO}/main/install.ps1 | iex${RESET}"
        exit 1
        ;;
    *)
        echo -e "${RED}Unsupported Operating System: ${OS}${RESET}"
        exit 1
        ;;
esac

case "${ARCH}" in
    x86_64|amd64)
        TARGET_ARCH="amd64"
        ;;
    aarch64|arm64)
        if [ "${TARGET_OS}" = "mac" ]; then
            TARGET_ARCH="apple-silicon"
        else
            TARGET_ARCH="arm64"
        fi
        ;;
    *)
        echo -e "${RED}Unsupported CPU Architecture: ${ARCH}${RESET}"
        exit 1
        ;;
esac

# Construct primary asset filename
if [ "${TARGET_OS}" = "mac" ]; then
    ASSET_NAME="termchat-mac-apple-silicon.tar.zst"
elif [ "${TARGET_OS}" = "android" ]; then
    ASSET_NAME="termchat-android-arm64.tar.zst"
else
    ASSET_NAME="termchat-linux-amd64.tar.zst"
fi

echo -e "${BLUE}Detected Platform:${RESET} ${BOLD}${TARGET_OS} (${TARGET_ARCH})${RESET}"
echo -e "${BLUE}Target Package:${RESET}    ${BOLD}${ASSET_NAME}${RESET}"
echo -e "${BLUE}Install Path:${RESET}      ${BOLD}${INSTALL_DIR}/termchat${RESET}"
echo ""

# 2. Fetch Latest Release Version
echo -e "${YELLOW}Fetching latest release info...${RESET}"
TAG=""
if command -v curl >/dev/null 2>&1; then
    TAG=$(curl -sSL -H "Accept: application/vnd.github.v3+json" "${GITHUB_LATEST_API}" | grep '"tag_name":' | head -n 1 | sed -E 's/.*"([^"]+)".*/\1/')
fi

if [ -z "${TAG}" ]; then
    TAG="v1.9.8"
    echo -e "${YELLOW}Using release version: ${TAG}${RESET}"
else
    echo -e "${GREEN}Found latest version: ${TAG}${RESET}"
fi

# 3. Mirror URLs
URL_GITHUB="https://github.com/${REPO}/releases/download/${TAG}/${ASSET_NAME}"
URL_FASTLY="https://raw.githubusercontent.com/${REPO}/binaries/${ASSET_NAME}"
URL_HF="https://huggingface.co/datasets/BrianC0des/termchat-releases/resolve/main/${ASSET_NAME}"
URL_CHECKSUM="https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"

# 4. Create Temporary Directory
TMP_DIR=$(mktemp -d)
cleanup() {
    rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

# 5. Download with Automatic Fallback
echo -e "${YELLOW}Downloading ${ASSET_NAME}...${RESET}"
DOWNLOAD_SUCCESS=0

for URL in "${URL_GITHUB}" "${URL_FASTLY}" "${URL_HF}"; do
    echo -e "  Trying: ${BLUE}${URL}${RESET}"
    if curl -fsSL --connect-timeout 8 --max-time 30 -o "${TMP_DIR}/${ASSET_NAME}" "${URL}"; then
        DOWNLOAD_SUCCESS=1
        echo -e "${GREEN}  ✓ Downloaded successfully!${RESET}"
        break
    else
        echo -e "${YELLOW}  ✗ Failed, trying next mirror...${RESET}"
    fi
done

if [ "${DOWNLOAD_SUCCESS}" -ne 1 ]; then
    echo -e "${RED}Error: Failed to download TermChat from all mirrors.${RESET}"
    exit 1
fi

# 6. Extract Archive
echo -e "${YELLOW}Extracting binary...${RESET}"
mkdir -p "${TMP_DIR}/extracted"

if command -v tar >/dev/null 2>&1 && tar --help 2>&1 | grep -q "zstd"; then
    tar --zstd -xf "${TMP_DIR}/${ASSET_NAME}" -C "${TMP_DIR}/extracted"
elif command -v unzstd >/dev/null 2>&1; then
    unzstd -c "${TMP_DIR}/${ASSET_NAME}" | tar -xf - -C "${TMP_DIR}/extracted"
else
    # Fallback to direct tar extraction
    tar -xf "${TMP_DIR}/${ASSET_NAME}" -C "${TMP_DIR}/extracted" || {
        echo -e "${RED}Error: zstd extraction required. Please install 'zstd' package.${RESET}"
        exit 1
    }
fi

# Find extracted binary name
BINARY_PATH=$(find "${TMP_DIR}/extracted" -type f -name "termchat*" ! -name "*.tar.*" ! -name "*.txt" | head -n 1)
if [ -z "${BINARY_PATH}" ]; then
    echo -e "${RED}Error: Binary not found in extracted archive.${RESET}"
    exit 1
fi

# 7. Install to Destination
mkdir -p "${INSTALL_DIR}"
cp -f "${BINARY_PATH}" "${INSTALL_DIR}/termchat"
chmod +x "${INSTALL_DIR}/termchat"

echo ""
echo -e "${GREEN}${BOLD}═══════════════════════════════════════════════════════${RESET}"
echo -e "${GREEN}${BOLD}  ✓ TermChat ${TAG} successfully installed!${RESET}"
echo -e "${GREEN}${BOLD}═══════════════════════════════════════════════════════${RESET}"
echo ""

# 8. Check PATH
case ":${PATH}:" in
    *:"${INSTALL_DIR}":*) ;;
    *)
        echo -e "${YELLOW}Note: '${INSTALL_DIR}' is not currently in your PATH.${RESET}"
        echo -e "Add it by running:"
        echo -e "  ${BOLD}export PATH=\"\$HOME/.local/bin:\$PATH\"${RESET}"
        echo -e "or add that line to your ${BOLD}~/.bashrc${RESET} or ${BOLD}~/.zshrc${RESET}."
        echo ""
        ;;
esac

# 9. Smart Post-Install Hook: Check for .termchat/room.json in current directory
if [ -f ".termchat/room.json" ]; then
    echo -e "${CYAN}🐙 Found project collab room in current directory (.termchat/room.json)!${RESET}"
    echo -e "Type ${BOLD}termchat${RESET} to join your team's room immediately."
else
    echo -e "Type ${BOLD}termchat${RESET} to launch your developer collab room."
fi

#!/usr/bin/env sh
# FleetQ Bridge installer
# Usage: curl -sSL https://get.fleetq.net/bridge | sh
set -e

REPO="escapeboy/fleetq-bridge"
BINARY="fleetq-bridge"
INSTALL_DIR="/usr/local/bin"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { printf "${GREEN}==>${NC} %s\n" "$1"; }
warn() { printf "${YELLOW}warn:${NC} %s\n" "$1"; }
err()  { printf "${RED}error:${NC} %s\n" "$1" >&2; exit 1; }

# Detect OS
OS="$(uname -s)"
case "$OS" in
  Linux*)  OS=linux ;;
  Darwin*) OS=darwin ;;
  *)       err "Unsupported OS: $OS. Download manually from https://github.com/$REPO/releases" ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)          ARCH=amd64 ;;
  amd64)           ARCH=amd64 ;;
  arm64|aarch64)   ARCH=arm64 ;;
  *)               err "Unsupported architecture: $ARCH" ;;
esac

# Fetch latest version tag from GitHub
log "Fetching latest version..."
LATEST=$(curl -sSf "https://api.github.com/repos/$REPO/releases/latest" \
  | grep '"tag_name"' \
  | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')

if [ -z "$LATEST" ]; then
  err "Could not determine latest version. Check https://github.com/$REPO/releases"
fi

log "Latest version: $LATEST"

# Construct download URL
FILENAME="${BINARY}_${OS}_${ARCH}"
[ "$OS" = "windows" ] && FILENAME="${FILENAME}.exe"
URL="https://github.com/$REPO/releases/download/$LATEST/$FILENAME"

# Download
TMP="$(mktemp)"
log "Downloading $FILENAME..."
if ! curl -sSfL "$URL" -o "$TMP"; then
  err "Download failed. Check https://github.com/$REPO/releases/$LATEST for available assets."
fi

# Install
chmod +x "$TMP"

if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "$INSTALL_DIR/$BINARY"
else
  log "Installing to $INSTALL_DIR (requires sudo)..."
  sudo mv "$TMP" "$INSTALL_DIR/$BINARY"
fi

# Verify
if command -v "$BINARY" >/dev/null 2>&1; then
  log "Installed successfully: $(command -v $BINARY)"
  printf "\n${GREEN}FleetQ Bridge $LATEST installed.${NC}\n\n"
  printf "Next steps:\n"
  printf "  1. Get your API key from https://fleetq.net/team (AI Keys tab)\n"
  printf "  2. Run: ${YELLOW}fleetq-bridge login --api-key flq_team_...${NC}\n"
  printf "  3. Run: ${YELLOW}fleetq-bridge install${NC}  (auto-start on login)\n\n"
else
  warn "Installed but '$BINARY' not in PATH. Add $INSTALL_DIR to your PATH."
fi

#!/bin/sh
# Preflight installer for macOS and Linux
# Usage: curl -fsSL https://raw.githubusercontent.com/bluecadet/preflight/main/install.sh | sh

set -e

REPO="bluecadet/preflight"
INSTALL_DIR="${PREFLIGHT_INSTALL_DIR:-/usr/local/bin}"

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

case "$OS" in
  darwin|linux) ;;
  *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

# Fetch latest release tag
VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
if [ -z "$VERSION" ]; then
  echo "Failed to determine latest release version." >&2
  exit 1
fi

ASSET="preflight-${VERSION}-${OS}-${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"

# Download and extract
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Downloading preflight $VERSION ($OS/$ARCH)..."
curl -fsSL "$URL" -o "$TMP/$ASSET"
tar -xzf "$TMP/$ASSET" -C "$TMP"

# Install binary
if [ ! -d "$INSTALL_DIR" ]; then
  mkdir -p "$INSTALL_DIR"
fi

if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP/preflight" "$INSTALL_DIR/preflight"
else
  sudo mv "$TMP/preflight" "$INSTALL_DIR/preflight"
fi

chmod +x "$INSTALL_DIR/preflight"

echo "preflight $VERSION installed to $INSTALL_DIR/preflight"

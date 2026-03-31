#!/bin/sh
# Preflight installer for macOS and Linux
# Usage: curl -fsSL https://raw.githubusercontent.com/bluecadet/preflight/main/install.sh | sh

set -eu

REPO="bluecadet/preflight"
INSTALL_DIR="${PREFLIGHT_INSTALL_DIR:-/usr/local/bin}"
VERSION="${PREFLIGHT_VERSION:-}"
CHECKSUM_ASSET="preflight_checksums.txt"

fail() {
  echo "$1" >&2
  exit 1
}

sha256_file() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  fail "No SHA-256 tool found (need shasum or sha256sum)."
}

normalize_version() {
  case "$1" in
    v*) printf '%s' "$1" ;;
    *) printf 'v%s' "$1" ;;
  esac
}

release_tag() {
  if [ -n "$VERSION" ]; then
    normalize_version "$VERSION"
    return
  fi

  curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' \
    | head -n 1
}

OS="$(uname -s)"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|arm64) ;;
  aarch64) ARCH="arm64" ;;
  *) fail "Unsupported architecture: $ARCH" ;;
esac

case "$OS" in
  Darwin|Linux) ;;
  *) fail "Unsupported OS: $OS" ;;
esac

TAG="$(release_tag)"
[ -n "$TAG" ] || fail "Failed to determine a release tag."

ASSET="preflight_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/$REPO/releases/download/$TAG"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

ARCHIVE_PATH="$TMP/$ASSET"
CHECKSUM_PATH="$TMP/$CHECKSUM_ASSET"

echo "Downloading preflight $TAG ($OS/$ARCH)..."
curl -fsSL "$BASE_URL/$ASSET" -o "$ARCHIVE_PATH" || fail "Failed to download $ASSET for $TAG."
curl -fsSL "$BASE_URL/$CHECKSUM_ASSET" -o "$CHECKSUM_PATH" || fail "Failed to download $CHECKSUM_ASSET for $TAG."

EXPECTED="$(grep "[[:space:]]$ASSET\$" "$CHECKSUM_PATH" | awk '{print $1}' | head -n 1)"
[ -n "$EXPECTED" ] || fail "Could not find checksum entry for $ASSET in $CHECKSUM_ASSET."
ACTUAL="$(sha256_file "$ARCHIVE_PATH")"
[ "$EXPECTED" = "$ACTUAL" ] || fail "Checksum verification failed for $ASSET."

tar -xzf "$ARCHIVE_PATH" -C "$TMP" || fail "Failed to extract $ASSET."
[ -f "$TMP/preflight" ] || fail "Archive did not contain the preflight binary."

if [ ! -d "$INSTALL_DIR" ]; then
  mkdir -p "$INSTALL_DIR" 2>/dev/null || sudo mkdir -p "$INSTALL_DIR"
fi

if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP/preflight" "$INSTALL_DIR/preflight"
else
  sudo mv "$TMP/preflight" "$INSTALL_DIR/preflight"
fi

chmod +x "$INSTALL_DIR/preflight"
"$INSTALL_DIR/preflight" --version >/dev/null 2>&1 || fail "Installed binary failed the --version self-check."

echo "preflight $TAG installed to $INSTALL_DIR/preflight"

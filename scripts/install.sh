#!/bin/sh
set -e

OWNER="huangzheng2016"
REPO="eTerm"
API="https://api.github.com/repos/${OWNER}/${REPO}/releases/latest"
BASE="https://github.com/${OWNER}/${REPO}/releases/download"

if [ -z "${TAG:-}" ]; then
  TAG=$(curl -fsSL "$API" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
fi
if [ -z "$TAG" ]; then
  echo "Could not read latest release tag." >&2
  exit 1
fi

UNAME_S=$(uname -s)
UNAME_M=$(uname -m)
case "$UNAME_S" in
  Linux*) OS=linux ;;
  Darwin*) OS=darwin ;;
  MINGW*|MSYS*|CYGWIN*) OS=windows ;;
  *) echo "Unsupported OS: $UNAME_S" >&2; exit 1 ;;
esac

case "$UNAME_M" in
  x86_64|amd64) GOARCH=amd64 ;;
  arm64|aarch64) GOARCH=arm64 ;;
  *) echo "Unsupported CPU: $UNAME_M" >&2; exit 1 ;;
esac

if [ "$OS" = "darwin" ] && [ "$GOARCH" = "arm64" ]; then
  ASSET_BASE="eterm_darwin_arm64"
  ARCHIVE="eterm_darwin_arm64.tar.gz"
elif [ "$OS" = "darwin" ] && [ "$GOARCH" = "amd64" ]; then
  ASSET_BASE="eterm_darwin_amd64"
  ARCHIVE="eterm_darwin_amd64.tar.gz"
elif [ "$OS" = "linux" ]; then
  ASSET_BASE="eterm_linux_${GOARCH}"
  ARCHIVE="eterm_linux_${GOARCH}.tar.gz"
else
  ASSET_BASE="eterm_windows_${GOARCH}"
  ARCHIVE="eterm_windows_${GOARCH}.zip"
fi

URL="${BASE}/${TAG}/${ARCHIVE}"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

echo "Downloading ${ARCHIVE} (${TAG})..."
curl -fsSL "$URL" -o "${TMP}/${ARCHIVE}"

cd "$TMP"
case "$ARCHIVE" in
  *.zip)
    unzip -oq "$ARCHIVE"
    BIN_SRC="${ASSET_BASE}.exe"
    DEST_NAME="eterm.exe"
    ;;
  *)
    tar -xzf "$ARCHIVE"
    BIN_SRC="$ASSET_BASE"
    DEST_NAME="eterm"
    ;;
esac

if [ "$OS" = "windows" ]; then
  DEF_INSTALL="$HOME/bin"
else
  DEF_INSTALL="$HOME/.local/bin"
fi
INSTALL_DIR="${INSTALL_DIR:-$DEF_INSTALL}"

mkdir -p "$INSTALL_DIR"
cp "$BIN_SRC" "$INSTALL_DIR/$DEST_NAME"
chmod +x "$INSTALL_DIR/$DEST_NAME" 2>/dev/null || true

echo "Installed: $INSTALL_DIR/$DEST_NAME"

case ":$PATH:" in
  *:"$INSTALL_DIR":*) ;;
  *) printf '%s\n' "Add to PATH if needed (example): export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac

#!/bin/sh
# Konvu CLI installer
# Usage: curl -sSL https://raw.githubusercontent.com/KonvuInc/konvu-cli/main/scripts/install.sh | sh
set -e

REPO="KonvuInc/konvu-cli"
INSTALL_DIR="${KONVU_INSTALL_DIR:-/usr/local/bin}"

# Detect OS and architecture.
detect_platform() {
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"

  case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *)      echo "Unsupported OS: $OS" >&2; exit 1 ;;
  esac

  case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)             echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
  esac
}

# Get the latest release version from GitHub.
get_latest_version() {
  if command -v curl >/dev/null 2>&1; then
    VERSION="$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"v\(.*\)".*/\1/')"
  elif command -v wget >/dev/null 2>&1; then
    VERSION="$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"v\(.*\)".*/\1/')"
  else
    echo "Error: curl or wget is required" >&2
    exit 1
  fi

  if [ -z "$VERSION" ]; then
    echo "Error: could not determine latest version" >&2
    exit 1
  fi
}

download_and_install() {
  FILENAME="konvu-${OS}-${ARCH}.tar.gz"
  URL="https://github.com/${REPO}/releases/download/v${VERSION}/${FILENAME}"
  CHECKSUM_URL="https://github.com/${REPO}/releases/download/v${VERSION}/checksums.txt"

  TMPDIR="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR"' EXIT

  echo "Downloading konvu v${VERSION} for ${OS}-${ARCH}..."

  if command -v curl >/dev/null 2>&1; then
    curl -sSL -o "${TMPDIR}/${FILENAME}" "$URL"
    curl -sSL -o "${TMPDIR}/checksums.txt" "$CHECKSUM_URL"
  else
    wget -q -O "${TMPDIR}/${FILENAME}" "$URL"
    wget -q -O "${TMPDIR}/checksums.txt" "$CHECKSUM_URL"
  fi

  # Verify checksum.
  EXPECTED="$(grep "${FILENAME}" "${TMPDIR}/checksums.txt" | awk '{print $1}')"
  if [ -z "$EXPECTED" ]; then
    echo "Error: checksum not found for ${FILENAME}" >&2
    exit 1
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL="$(sha256sum "${TMPDIR}/${FILENAME}" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    ACTUAL="$(shasum -a 256 "${TMPDIR}/${FILENAME}" | awk '{print $1}')"
  else
    echo "Warning: cannot verify checksum (sha256sum/shasum not found)" >&2
    ACTUAL="$EXPECTED"
  fi

  if [ "$ACTUAL" != "$EXPECTED" ]; then
    echo "Error: checksum verification failed!" >&2
    echo "  Expected: $EXPECTED" >&2
    echo "  Got:      $ACTUAL" >&2
    exit 1
  fi

  echo "Checksum verified."

  # Extract and install.
  tar -xzf "${TMPDIR}/${FILENAME}" -C "$TMPDIR"

  if [ -w "$INSTALL_DIR" ]; then
    mv "${TMPDIR}/konvu" "$INSTALL_DIR/konvu"
  else
    echo "Installing to ${INSTALL_DIR} (requires sudo)..."
    sudo mv "${TMPDIR}/konvu" "$INSTALL_DIR/konvu"
  fi

  chmod +x "$INSTALL_DIR/konvu"
  echo "konvu v${VERSION} installed to ${INSTALL_DIR}/konvu"
}

detect_platform
get_latest_version
download_and_install

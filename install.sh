#!/bin/sh
# Vela installer — Linux / macOS
# Usage: curl -fsSL https://raw.githubusercontent.com/Kinfxhk/vela/main/install.sh | sh
set -eu

REPO="Kinfxhk/vela"
BIN="vela"
PREFIX="${PREFIX:-$HOME/.local}"
BINDIR="$PREFIX/bin"

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac
case "$os" in
  darwin|linux) ;;
  *) die "unsupported OS: $os (use Docker on Windows)" ;;
esac

say "Vela installer"
say "  platform: ${os}/${arch}"
say "  target:   ${BINDIR}/${BIN}"

mkdir -p "$BINDIR"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Prefer the latest release asset; fall back to building from source.
url="https://github.com/${REPO}/releases/latest/download/vela-${os}-${arch}"
say "  downloading ${url}"
if curl -fL --retry 3 --connect-timeout 10 -o "$tmp/vela" "$url" 2>/dev/null; then
  :
else
  say "  no release asset found; trying source build"
  command -v go >/dev/null 2>&1 || die "Go is not installed and no release asset exists yet"
  git clone --depth 1 "https://github.com/${REPO}" "$tmp/src" >/dev/null 2>&1 || die "clone failed"
  (cd "$tmp/src" && go build -trimpath -ldflags "-s -w" -o "$tmp/vela" .) || die "build failed"
fi

chmod +x "$tmp/vela"
# Refuse to install a file that is not a runnable binary for this platform.
"$tmp/vela" version >/dev/null 2>&1 || die "downloaded file does not run on this platform"
mv "$tmp/vela" "$BINDIR/$BIN"
say "  installed $("$BINDIR/$BIN" version)"

case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *) say ""; say "Add this to your shell profile:"; say "  export PATH=\"$BINDIR:\$PATH\"" ;;
esac

if [ -t 0 ]; then
  if [ ! -f "${XDG_CONFIG_HOME:-$HOME/.config}/vela/config.json" ] && [ ! -f "$HOME/Library/Application Support/vela/config.json" ]; then
    say ""
    say "Next: run 'vela init' and choose a folder."
  fi
fi

say ""
say "Done. Commands: vela init | vela scan | vela serve | vela doctor"

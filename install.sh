#!/bin/sh
# Nuitelle installer — Linux / macOS
# Usage: curl -fsSL https://raw.githubusercontent.com/Kinfxhk/nuitelle/main/install.sh | sh
set -eu

REPO="Kinfxhk/nuitelle"
BIN="nuitelle"
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

say "Nuitelle installer"
say "  platform: ${os}/${arch}"
say "  target:   ${BINDIR}/${BIN}"

mkdir -p "$BINDIR"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Prefer the latest release asset; fall back to building from source.
url="https://github.com/${REPO}/releases/latest/download/nuitelle-${os}-${arch}"
say "  downloading ${url}"
if curl -fL --retry 3 --connect-timeout 10 -o "$tmp/nuitelle" "$url" 2>/dev/null; then
  :
else
  say "  no release asset found; trying source build"
  command -v go >/dev/null 2>&1 || die "Go is not installed and no release asset exists yet"
  git clone --depth 1 "https://github.com/${REPO}" "$tmp/src" >/dev/null 2>&1 || die "clone failed"
  (cd "$tmp/src" && go build -trimpath -ldflags "-s -w" -o "$tmp/nuitelle" .) || die "build failed"
fi

chmod +x "$tmp/nuitelle"
# Refuse to install a file that is not a runnable binary for this platform.
"$tmp/nuitelle" version >/dev/null 2>&1 || die "downloaded file does not run on this platform"
mv "$tmp/nuitelle" "$BINDIR/$BIN"
say "  installed $("$BINDIR/$BIN" version)"

case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *) say ""; say "Add this to your shell profile:"; say "  export PATH=\"$BINDIR:\$PATH\"" ;;
esac

# Optional tools (ffmpeg, whisper.cpp). Playback works without them; they add
# covers, metadata and local subtitles. NUITELLE_DEPS=skip|ask|yes (default ask).
# Under `curl | sh` stdin is the script, so prompts read from /dev/tty.
deps="${NUITELLE_DEPS:-ask}"
say ""
say "Checking optional tools…"
case "$deps" in
  skip) say "  skipped (NUITELLE_DEPS=skip); run 'nuitelle doctor --fix' later" ;;
  yes)  "$BINDIR/$BIN" doctor --yes </dev/null || true ;;
  *)
    if [ -r /dev/tty ] && (: </dev/tty) 2>/dev/null; then
      "$BINDIR/$BIN" doctor --fix </dev/tty || true
    else
      "$BINDIR/$BIN" doctor || true
      say "  no terminal for prompts; run 'nuitelle doctor --fix' to install missing tools"
    fi ;;
esac

if [ ! -f "${XDG_CONFIG_HOME:-$HOME/.config}/nuitelle/config.json" ] && [ ! -f "$HOME/Library/Application Support/nuitelle/config.json" ]; then
  say ""
  say "Next: run 'nuitelle init' and choose a folder, then 'nuitelle doctor --fix'."
fi

say ""
say "Done. Commands: nuitelle init | nuitelle scan | nuitelle serve | nuitelle doctor --fix"

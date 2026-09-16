#!/bin/sh
# agent-handoff installer — one line, any box:
#   curl -fsSL https://raw.githubusercontent.com/adeelahmad/package/master/install.sh | sh
# Tries a prebuilt release binary first; falls back to building from source with Go.
# Installs to $HANDOFF_INSTALL_DIR, else ~/.local/bin. POSIX sh, no bashisms.
set -eu

REPO="adeelahmad/package"
REF="${HANDOFF_REF:-master}"
BIN="handoff"
DIR="${HANDOFF_INSTALL_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "install: unsupported arch $arch (need amd64/arm64)" >&2; exit 1 ;;
esac
case "$os" in
  linux|darwin) ;;
  *) echo "install: unsupported OS $os (need linux/darwin)" >&2; exit 1 ;;
esac

mkdir -p "$DIR"
asset="handoff-$os-$arch"
url="https://github.com/$REPO/releases/latest/download/$asset"

fetched=0
if command -v curl >/dev/null 2>&1; then
  if curl -fsSL -o "$DIR/$BIN.tmp" "$url" 2>/dev/null; then fetched=1; fi
elif command -v wget >/dev/null 2>&1; then
  if wget -q -O "$DIR/$BIN.tmp" "$url" 2>/dev/null; then fetched=1; fi
fi

if [ "$fetched" = 1 ]; then
  mv "$DIR/$BIN.tmp" "$DIR/$BIN"
  chmod +x "$DIR/$BIN"
else
  rm -f "$DIR/$BIN.tmp"
  if ! command -v go >/dev/null 2>&1; then
    echo "install: no prebuilt release found and Go is not installed." >&2
    echo "install: install Go (https://go.dev/dl) and re-run, or build on another machine:" >&2
    echo "  git clone --depth 1 https://github.com/$REPO && cd package/tool && go build -o $BIN ." >&2
    exit 1
  fi
  echo "install: no prebuilt release; building from source with $(go version | cut -d' ' -f3)"
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  if command -v git >/dev/null 2>&1; then
    git clone --quiet --depth 1 --branch "$REF" "https://github.com/$REPO" "$tmp/src"
  else
    curl -fsSL "https://github.com/$REPO/archive/refs/heads/$REF.tar.gz" | tar -xz -C "$tmp"
    mv "$tmp"/package-* "$tmp/src"
  fi
  if [ ! -d "$tmp/src/tool" ]; then
    echo "install: ref '$REF' of $REPO has no tool/ directory — set HANDOFF_REF to a branch that does" >&2
    exit 1
  fi
  (cd "$tmp/src/tool" && CGO_ENABLED=0 go build -ldflags="-s -w" -o "$DIR/$BIN" .)
fi

"$DIR/$BIN" selfcheck
echo "installed: $DIR/$BIN ($("$DIR/$BIN" version))"
case ":$PATH:" in
  *":$DIR:"*) ;;
  *) echo "note: add $DIR to your PATH" ;;
esac

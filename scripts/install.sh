#!/bin/sh
# Install ghprofile without needing Go.
#
#   curl -sSfL https://raw.githubusercontent.com/Tessie475/ghprofile/main/scripts/install.sh | sh
#
# Environment:
#   GHPROFILE_VERSION   version to install, e.g. v0.1.0 (default: latest)
#   GHPROFILE_BIN_DIR   where to put the binary (default: first writable of
#                       /usr/local/bin, ~/.local/bin)
#
# The download is checked against the release's published checksums file before
# anything is installed. If you would rather not pipe a script to a shell,
# which is a reasonable thing to prefer for a tool that handles SSH keys, read
# this file first or download the archive from the releases page by hand.

set -eu

REPO="Tessie475/ghprofile"
BINARY="ghprofile"

die() { printf 'install: %s\n' "$1" >&2; exit 1; }
note() { printf '%s\n' "$1"; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed"; }

need uname
need tar
need mkdir

download() {
  if command -v curl >/dev/null 2>&1; then
    curl -sSfL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$2" "$1"
  else
    die "either curl or wget is required"
  fi
}

detect_platform() {
  os=$(uname -s)
  arch=$(uname -m)

  case "$os" in
    Darwin) os=darwin ;;
    Linux)  os=linux ;;
    *) die "unsupported operating system: $os. download a release by hand from https://github.com/$REPO/releases" ;;
  esac

  case "$arch" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) die "unsupported architecture: $arch" ;;
  esac

  printf '%s %s' "$os" "$arch"
}

latest_version() {
  tmp=$(mktemp)
  download "https://api.github.com/repos/$REPO/releases/latest" "$tmp" ||
    die "could not reach the GitHub API. set GHPROFILE_VERSION to install a specific version"

  # Avoid a jq dependency: pull the tag out of the JSON directly.
  v=$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$tmp" | head -1)
  rm -f "$tmp"

  [ -n "$v" ] || die "no published release found. build from source, or see https://github.com/$REPO"
  printf '%s' "$v"
}

choose_bin_dir() {
  if [ -n "${GHPROFILE_BIN_DIR:-}" ]; then
    printf '%s' "$GHPROFILE_BIN_DIR"
    return
  fi
  if [ -w /usr/local/bin ] 2>/dev/null; then
    printf '%s' /usr/local/bin
    return
  fi
  printf '%s' "$HOME/.local/bin"
}

verify_checksum() {
  archive=$1
  sums=$2
  name=$3

  expected=$(grep " $name\$" "$sums" | awk '{print $1}' | head -1)
  [ -n "$expected" ] || die "$name is not listed in the checksums file"

  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$archive" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$archive" | awk '{print $1}')
  else
    note "warning: no sha256 tool found, skipping checksum verification"
    return
  fi

  [ "$expected" = "$actual" ] || die "checksum mismatch for $name. refusing to install"
  note "checksum ok"
}

main() {
  platform=$(detect_platform)
  os=${platform% *}
  arch=${platform#* }

  version=${GHPROFILE_VERSION:-$(latest_version)}
  numeric=${version#v}

  name="${BINARY}_${numeric}_${os}_${arch}.tar.gz"
  sums="${BINARY}_${numeric}_checksums.txt"
  base="https://github.com/$REPO/releases/download/$version"

  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT

  note "installing $BINARY $version for $os/$arch"

  download "$base/$name" "$tmp/$name" || die "could not download $base/$name"
  download "$base/$sums" "$tmp/$sums" || die "could not download the checksums file"
  verify_checksum "$tmp/$name" "$tmp/$sums" "$name"

  tar -xzf "$tmp/$name" -C "$tmp"
  [ -f "$tmp/$BINARY" ] || die "the archive did not contain $BINARY"

  bin_dir=$(choose_bin_dir)
  mkdir -p "$bin_dir"
  install -m 0755 "$tmp/$BINARY" "$bin_dir/$BINARY" 2>/dev/null ||
    { cp "$tmp/$BINARY" "$bin_dir/$BINARY" && chmod 0755 "$bin_dir/$BINARY"; }

  note "installed $bin_dir/$BINARY"

  if command -v "$BINARY" >/dev/null 2>&1; then
    note ""
    "$bin_dir/$BINARY" version
    note ""
    note "next: $BINARY add personal --email you@example.com --default"
  else
    note ""
    note "$bin_dir is not on your PATH. add it:"
    note "  echo 'export PATH=\"\$PATH:$bin_dir\"' >> ~/.zshrc && source ~/.zshrc"
  fi
}

# Sourcing with GHPROFILE_INSTALL_LIB=1 loads the functions without running,
# which is how the checks below are tested without a published release.
[ "${GHPROFILE_INSTALL_LIB:-}" = "1" ] || main "$@"

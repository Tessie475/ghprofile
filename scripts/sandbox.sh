#!/usr/bin/env bash
# Rehearse the full ghprofile flow against a throwaway copy of your real
# dotfiles. Your actual $HOME is never written to.
#
#   ./scripts/sandbox.sh            build a sandbox and run the whole flow
#   ./scripts/sandbox.sh -k         keep the sandbox afterwards
#
# Everything happens under a temporary directory pointed at by GHPROFILE_HOME.

set -euo pipefail

KEEP=0
[ "${1:-}" = "-k" ] && KEEP=1

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SANDBOX="$(mktemp -d "${TMPDIR:-/tmp}/ghprofile-sandbox.XXXXXX")"
# TMPDIR often ends in a slash, and the tool writes cleaned paths, so normalise.
SANDBOX="$(cd "$SANDBOX" && pwd)"
BIN="$REPO/bin/ghprofile"

cleanup() {
  if [ "$KEEP" = "1" ]; then
    echo
    echo "sandbox kept at: $SANDBOX"
  else
    rm -rf "$SANDBOX"
  fi
}
trap cleanup EXIT

say() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
scrub() { sed "s|$SANDBOX|~|g"; }

say "Building"
(cd "$REPO" && go build -o "$BIN" ./cmd/ghprofile)

say "Copying your real config into the sandbox (read only)"
mkdir -p "$SANDBOX/.ssh" "$SANDBOX/.config/ghprofile"
[ -f "$HOME/.ssh/config" ] && cp "$HOME/.ssh/config" "$SANDBOX/.ssh/config"
[ -f "$HOME/.gitconfig" ] && cp "$HOME/.gitconfig" "$SANDBOX/.gitconfig"
cp "$HOME"/.ssh/*.pub "$SANDBOX/.ssh/" 2>/dev/null || true
chmod 700 "$SANDBOX/.ssh"
echo "sandbox home: $SANDBOX"

if [ -f "$HOME/.config/ghprofile/profiles.yaml" ]; then
  say "Using your existing profiles file"
  cp "$HOME/.config/ghprofile/profiles.yaml" "$SANDBOX/.config/ghprofile/profiles.yaml"
else
  say "No profiles file yet, writing a starter one"
  GHPROFILE_HOME="$SANDBOX" "$BIN" init >/dev/null
  echo "edit $SANDBOX/.config/ghprofile/profiles.yaml if you want a realistic run"
fi

export GHPROFILE_HOME="$SANDBOX"

say "show"
"$BIN" show | scrub

say "doctor (before)"
"$BIN" doctor | scrub || true

say "adopt: remove hand-written stanzas so managed blocks can take effect"
"$BIN" adopt | scrub

say "plan: what would change"
"$BIN" plan | scrub

say "apply: generates real keys, inside the sandbox only"
"$BIN" apply | scrub

say "plan again: must report no changes"
"$BIN" plan | scrub

say "apply again: must be a no-op"
"$BIN" apply | scrub

say "doctor (after)"
"$BIN" doctor | scrub || true

say "resulting ~/.ssh/config"
scrub < "$SANDBOX/.ssh/config"

say "resulting ~/.gitconfig"
scrub < "$SANDBOX/.gitconfig"

say "Done. Nothing outside the sandbox was touched."

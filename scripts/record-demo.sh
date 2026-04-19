#!/usr/bin/env bash
# record-demo.sh — Record the EarlyWatch demo as an animated GIF using t-rec.
#
# Wraps scripts/demo.sh with t-rec (https://github.com/sassman/t-rec-rs) to
# capture the interactive demo as an animated GIF with:
#   • Dark-mode console  (black background, near-white foreground)
#   • 15 fps capture rate
#   • ~1920×1080 terminal window  (1080p)
#
# The recording ends automatically when demo.sh exits.
#
# Prerequisites:
#   • t-rec       — https://github.com/sassman/t-rec-rs
#                   macOS:  brew install t-rec
#                   Linux:  sudo apt-get install imagemagick && cargo install t-rec
#   • imagemagick — required by t-rec (already listed above)
#   • A prepared cluster — run scripts/demo-setup.sh before recording
#
# Usage:
#   bash scripts/record-demo.sh [--output <name>] [--skip-cleanup]
#
#   --output <name>   Base name for the output GIF (default: earlywatch-demo).
#                     t-rec appends the .gif extension automatically.
#   --skip-cleanup    Passed through to demo.sh — keeps the kind cluster alive
#                     after the demo finishes so you can inspect resources.
set -euo pipefail

# ── Argument parsing ──────────────────────────────────────────────────────────
OUTPUT_NAME="earlywatch-demo"
DEMO_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      if [[ $# -lt 2 || -z "${2:-}" ]]; then
        echo "Missing value for --output" >&2
        echo "Usage: bash scripts/record-demo.sh [--output <name>] [--skip-cleanup]" >&2
        exit 1
      fi
      OUTPUT_NAME="$2"
      shift 2
      ;;
    --output=*)
      OUTPUT_NAME="${1#--output=}"
      shift
      ;;
    --skip-cleanup)
      DEMO_ARGS+=("--skip-cleanup")
      shift
      ;;
    *)
      echo "Unknown argument: $1" >&2
      echo "Usage: bash scripts/record-demo.sh [--output <name>] [--skip-cleanup]" >&2
      exit 1
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

print_install_hints() {
  echo "" >&2
  echo "Install it from https://github.com/sassman/t-rec-rs:" >&2
  echo "  macOS:  brew install t-rec" >&2
  echo "  Linux:  sudo apt-get install imagemagick && cargo install t-rec" >&2
}

# ── Prerequisite check ────────────────────────────────────────────────────────
if ! command -v t-rec &>/dev/null; then
  echo "Error: t-rec not found in PATH." >&2
  print_install_hints
  exit 1
fi

if ! command -v magick &>/dev/null && ! command -v convert &>/dev/null; then
  echo "Error: ImageMagick not found in PATH (expected 'magick' or 'convert')." >&2
  print_install_hints
  exit 1
fi
# ── Dark-mode console ─────────────────────────────────────────────────────────
# Use xterm-compatible OSC escape sequences to switch the terminal to a dark
# color scheme before recording begins.  These sequences are supported by most
# modern terminal emulators (iTerm2, GNOME Terminal, Konsole, xterm, …).
#   OSC 11 → background color
#   OSC 10 → foreground (text) color
printf '\033]11;#000000\007'   # background → black
printf '\033]10;#f8f8f2\007'   # foreground → near-white (Dracula palette)
printf '\033[2J\033[H'         # clear screen so the dark background fills it

# ── Resize terminal to ~1080p ─────────────────────────────────────────────────
# VT sequence CSI 8 ; rows ; cols t asks the terminal emulator to resize the
# window to the given number of character rows and columns.  At a typical
# monospace font (≈16 px/row, ≈8 px/col) this maps to roughly 1920×1080 px.
printf '\033[8;67;240t'
sleep 0.3   # give the terminal emulator time to redraw at the new size

# ── Record with t-rec ─────────────────────────────────────────────────────────
echo "Recording EarlyWatch demo at 15 fps → ${OUTPUT_NAME}.gif"
echo "The recording will end automatically when the demo finishes."
echo ""

t-rec \
  --fps 15 \
  --output "${OUTPUT_NAME}" \
  --quiet \
  bash "${SCRIPT_DIR}/demo.sh" "${DEMO_ARGS[@]+"${DEMO_ARGS[@]}"}"

echo ""
echo "Recording saved: ${OUTPUT_NAME}.gif"

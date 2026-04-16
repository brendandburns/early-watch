#!/usr/bin/env bash
# demo-util.sh — Shared utilities for EarlyWatch demo scripts.
#
# Source this file from demo-setup.sh and demo.sh:
#
#   # shellcheck source=scripts/demo-util.sh
#   source "$(dirname "${BASH_SOURCE[0]}")/demo-util.sh"
#
# After sourcing, the following variables and functions are available:
#
#   Variables:
#     BOLD, CYAN, GREEN, YELLOW, RED, DIM, RESET  — ANSI color codes
#     CLUSTER_NAME                                 — kind cluster name
#     REPO_ROOT                                    — absolute path to repo root
#     WATCHCTL                                     — absolute path to watchctl binary
#
#   Functions:
#     print_header <text>   — print a prominent section header
#     print_step   <text>   — print a numbered step label
#     print_info   <text>   — print a dimmed informational line
#     print_success <text>  — print a green success message
#     print_error  <text>   — print a red error message
#     print_cmd    <text>   — print a command line (for display only)
#     pause                 — wait for the user to press Enter
#     run_cmd      <cmd…>   — print and then execute a command

# ── ANSI color codes ─────────────────────────────────────────────────────────
BOLD=$'\033[1m'
CYAN=$'\033[1;36m'
GREEN=$'\033[1;32m'
YELLOW=$'\033[1;33m'
RED=$'\033[1;31m'
DIM=$'\033[2m'
RESET=$'\033[0m'

# ── Common paths ─────────────────────────────────────────────────────────────
# shellcheck disable=SC2034  # used by the scripts that source this file
CLUSTER_NAME="earlywatch-demo"
# BASH_SOURCE[1] is the script that sourced this file; resolve from its directory.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[1]}")/.." && pwd)"
# shellcheck disable=SC2034  # used by the scripts that source this file
WATCHCTL="$REPO_ROOT/watchctl"

# ── Helper functions ─────────────────────────────────────────────────────────
print_header() {
  echo ""
  echo "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo "${BOLD}  $1${RESET}"
  echo "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
}

print_step() {
  echo ""
  echo "${YELLOW}▶  $1${RESET}"
}

print_info() {
  echo "${DIM}   $1${RESET}"
}

print_success() {
  echo "${GREEN}✔  $1${RESET}"
}

print_error() {
  echo "${RED}✘  $1${RESET}"
}

print_cmd() {
  echo "${BOLD}   \$ $1${RESET}"
}

pause() {
  echo ""
  echo -n "${DIM}   Press Enter to continue...${RESET}"
  read -r _
}

run_cmd() {
  print_cmd "$*"
  "$@"
}

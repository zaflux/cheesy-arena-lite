#!/usr/bin/env bash
set -euo pipefail

MIN_GO_VERSION="1.20"
REQUIRED_PACKAGES=(
  ca-certificates
  git
  golang-go
  zip
)

log() {
  printf '[build] %s\n' "$*"
}

fail() {
  printf '[build] ERROR: %s\n' "$*" >&2
  exit 1
}

require_command() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || fail "Required command '$cmd' was not found."
}

check_os() {
  if [[ ! -f /etc/os-release ]]; then
    fail "Cannot determine operating system (/etc/os-release not found)."
  fi

  # shellcheck disable=SC1091
  source /etc/os-release

  if [[ "${ID:-}" != "debian" ]]; then
    fail "This script targets Debian. Detected ID='${ID:-unknown}'."
  fi

  if [[ "${VERSION_CODENAME:-}" != "trixie" ]]; then
    log "Warning: expected Debian codename 'trixie', detected '${VERSION_CODENAME:-unknown}'."
  fi
}

install_missing_packages() {
  local missing=()
  local pkg

  require_command apt-get
  require_command dpkg-query

  for pkg in "${REQUIRED_PACKAGES[@]}"; do
    if ! dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q 'install ok installed'; then
      missing+=("$pkg")
    fi
  done

  if [[ ${#missing[@]} -eq 0 ]]; then
    log "All required Debian packages are already installed."
    return
  fi

  log "Installing missing Debian packages: ${missing[*]}"

  if [[ "${EUID}" -eq 0 ]]; then
    apt-get update
    apt-get install -y "${missing[@]}"
  else
    require_command sudo
    sudo apt-get update
    sudo apt-get install -y "${missing[@]}"
  fi
}

verify_go_version() {
  local goversion

  require_command go
  goversion="$(go env GOVERSION 2>/dev/null || true)"

  if [[ -z "$goversion" ]]; then
    goversion="$(go version | awk '{print $3}')"
  fi

  goversion="${goversion#go}"

  if [[ "$(printf '%s\n' "$MIN_GO_VERSION" "$goversion" | sort -V | head -n1)" != "$MIN_GO_VERSION" ]]; then
    fail "Go ${MIN_GO_VERSION}+ is required, found ${goversion}."
  fi

  log "Using Go ${goversion}."
}

build_project() {
  local repo_root
  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

  cd "$repo_root"

  log "Downloading Go module dependencies..."
  go mod download

  log "Verifying Go modules..."
  go mod verify

  log "Building project binary..."
  go build -o cheesy-arena-lite ./

  log "Build completed successfully: ${repo_root}/cheesy-arena-lite"
}

main() {
  check_os
  install_missing_packages
  verify_go_version
  build_project
}

main "$@"
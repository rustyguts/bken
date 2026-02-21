#!/usr/bin/env bash
set -euo pipefail

readonly REQUIRED_PKG_CONFIG_MODULES=(
  portaudio-2.0
  opus
  opusfile
)

log() {
  printf '[deps] %s\n' "$*"
}

run_root() {
  if [[ "${EUID}" -eq 0 ]]; then
    "$@"
  else
    sudo "$@"
  fi
}

verify_pkg_config_modules() {
  local missing=0
  local module
  for module in "${REQUIRED_PKG_CONFIG_MODULES[@]}"; do
    if ! pkg-config --exists "${module}"; then
      log "${module} pkg-config metadata is missing after install."
      missing=1
    fi
  done
  if [[ "${missing}" -ne 0 ]]; then
    exit 1
  fi
}

install_macos() {
  if ! command -v brew >/dev/null 2>&1; then
    log "Homebrew is required on macOS. Install it first: https://brew.sh"
    exit 1
  fi

  log "Installing macOS dependencies with Homebrew..."
  local packages=(
    portaudio
    opus
    opusfile
    pkg-config
  )
  brew install "${packages[@]}"
}

install_linux_apt() {
  log "Installing Linux dependencies with apt..."
  local packages=(
    build-essential
    pkg-config
    libgtk-3-dev
    libwebkit2gtk-4.1-dev
    portaudio19-dev
    libopus-dev
    libopusfile-dev
  )
  run_root apt-get update
  run_root apt-get install -y "${packages[@]}"
}

install_linux_dnf() {
  log "Installing Linux dependencies with dnf..."
  local packages=(
    gcc
    gcc-c++
    make
    pkgconf-pkg-config
    gtk3-devel
    webkit2gtk4.1-devel
    portaudio-devel
    opus-devel
    opusfile-devel
  )
  run_root dnf install -y "${packages[@]}"
}

install_linux_pacman() {
  log "Installing Linux dependencies with pacman..."
  local packages=(
    base-devel
    pkgconf
    gtk3
    webkit2gtk-4.1
    portaudio
    opus
    opusfile
  )
  run_root pacman -Sy --noconfirm --needed "${packages[@]}"
}

install_linux() {
  if command -v apt-get >/dev/null 2>&1; then
    install_linux_apt
  elif command -v dnf >/dev/null 2>&1; then
    install_linux_dnf
  elif command -v pacman >/dev/null 2>&1; then
    install_linux_pacman
  else
    log "Unsupported Linux package manager. Supported: apt, dnf, pacman."
    exit 1
  fi
}

main() {
  case "$(uname -s)" in
    Darwin)
      install_macos
      ;;
    Linux)
      install_linux
      ;;
    *)
      log "Unsupported OS for this script. Use install-native-deps.ps1 on Windows."
      exit 1
      ;;
  esac

  verify_pkg_config_modules

  log "Native development dependencies are installed."
  log "Next: from client/, run: wails dev"
}

main "$@"

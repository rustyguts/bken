#!/usr/bin/env bash
set -euo pipefail

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

install_macos() {
  if ! command -v brew >/dev/null 2>&1; then
    log "Homebrew is required on macOS. Install it first: https://brew.sh"
    exit 1
  fi

  log "Installing macOS dependencies with Homebrew..."
  brew install portaudio opus opusfile pkg-config
}

install_linux_apt() {
  log "Installing Linux dependencies with apt..."
  run_root apt-get update
  run_root apt-get install -y \
    build-essential \
    pkg-config \
    libgtk-3-dev \
    libwebkit2gtk-4.1-dev \
    portaudio19-dev \
    libopus-dev \
    libopusfile-dev
}

install_linux_dnf() {
  log "Installing Linux dependencies with dnf..."
  run_root dnf install -y \
    gcc \
    gcc-c++ \
    make \
    pkgconf-pkg-config \
    gtk3-devel \
    webkit2gtk4.1-devel \
    portaudio-devel \
    opus-devel \
    opusfile-devel
}

install_linux_pacman() {
  log "Installing Linux dependencies with pacman..."
  run_root pacman -Sy --noconfirm --needed \
    base-devel \
    pkgconf \
    gtk3 \
    webkit2gtk-4.1 \
    portaudio \
    opus \
    opusfile
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

  if ! pkg-config --exists portaudio-2.0; then
    log "portaudio pkg-config metadata is missing after install."
    exit 1
  fi
  if ! pkg-config --exists opus; then
    log "opus pkg-config metadata is missing after install."
    exit 1
  fi
  if ! pkg-config --exists opusfile; then
    log "opusfile pkg-config metadata is missing after install."
    exit 1
  fi

  log "Native development dependencies are installed."
  log "Next: from client/, run: wails dev"
}

main "$@"

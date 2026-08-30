#!/usr/bin/env bash

set -euo pipefail

repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
install_dir="${HOME}/.local/bin"
build_path="$(mktemp "${TMPDIR:-/tmp}/stash.XXXXXX")"

cleanup() {
    rm -f -- "${build_path}"
}
trap cleanup EXIT

if command -v pacman >/dev/null 2>&1; then
    echo "Installing STASH dependencies..."
    sudo pacman -S --needed git go csound
else
    echo "Error: this installer currently supports Manjaro and Arch Linux." >&2
    exit 1
fi

echo "Building STASH..."
cd -- "${repo_dir}"
go build -o "${build_path}" ./cmd/stash

echo "Installing STASH to ${install_dir}/stash..."
install -Dm755 "${build_path}" "${install_dir}/stash"

echo
echo "STASH installed successfully."

case ":${PATH}:" in
    *":${install_dir}:"*)
        echo "Try it with: stash -l"
        ;;
    *)
        echo "Add this line to your shell configuration, then open a new terminal:"
        echo "export PATH=\"\$HOME/.local/bin:\$PATH\""
        ;;
esac

#!/bin/sh
set -eu
source_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
install_dir=${PREVIEWCTL_INSTALL_DIR:-"$HOME/.local/bin"}
if [ ! -f "$source_dir/previewctl" ]; then
    printf '%s\n' 'Extract the macOS/Linux release archive before installing.' >&2
    exit 1
fi
mkdir -p "$install_dir"
if [ "$source_dir/previewctl" != "$install_dir/previewctl" ]; then
    cp "$source_dir/previewctl" "$install_dir/previewctl"
fi
chmod 755 "$install_dir/previewctl"
printf 'Installed: %s/previewctl\n' "$install_dir"
printf '%s\n' 'Add the installation directory to PATH in your shell configuration.'
if [ "$install_dir" = "$HOME/.local/bin" ]; then
    printf '%s\n' 'export PATH="$HOME/.local/bin:$PATH"'
fi
printf '%s\n' 'Open a new terminal, then run: previewctl version'

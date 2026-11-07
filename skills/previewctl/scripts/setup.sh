#!/bin/sh
set -eu
umask 077

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
version=$(tr -d '\r\n' < "$script_dir/VERSION")
case "$(uname -s)" in
    Darwin) os=darwin; default_cache="$HOME/Library/Caches/previewctl" ;;
    Linux) os=linux; default_cache="${XDG_CACHE_HOME:-$HOME/.cache}/previewctl" ;;
    *) printf '%s\n' 'Unsupported OS. Use setup.ps1 on Windows.' >&2; exit 1 ;;
esac
case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) printf '%s\n' 'Unsupported CPU. previewctl requires x64 or ARM64.' >&2; exit 1 ;;
esac

checksum() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        printf '%s\n' 'Install sha256sum or shasum before using this skill.' >&2
        return 1
    fi
}

verify() {
    expected=$(tr -d '\r' < "$script_dir/checksums.txt" | awk -v name="$2" '$2 == name {print $1}')
    actual=$(checksum "$1")
    if [ -z "$expected" ] || [ "$actual" != "$expected" ]; then
        printf 'SHA-256 mismatch: %s\n' "$1" >&2
        return 1
    fi
}

platform="${os}_${arch}"
cache_dir="${PREVIEWCTL_CACHE_DIR:-$default_cache}/$version/$platform"
mkdir -p "$cache_dir"
cache_dir=$(CDPATH= cd -- "$cache_dir" && pwd)
binary="$cache_dir/previewctl"
if [ -f "$binary" ]; then
    verify "$binary" "$platform/previewctl"
    printf '%s\n' "$binary"
    exit 0
fi

command -v curl >/dev/null 2>&1 || { printf '%s\n' 'Install curl before using this skill.' >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { printf '%s\n' 'Install tar before using this skill.' >&2; exit 1; }
temp_dir=$(mktemp -d "$cache_dir/.download.XXXXXX")
trap 'rm -rf -- "$temp_dir"' EXIT
trap 'exit 1' HUP INT TERM
archive="previewctl_${version}_${platform}.tar.gz"
printf 'Downloading previewctl v%s for %s...\n' "$version" "$platform" >&2
curl --fail --silent --show-error --location --connect-timeout 15 --max-time 120 \
    --proto '=https' --proto-redir '=https' \
    "https://github.com/kamedesuyo/previewctl/releases/download/v$version/$archive" \
    --output "$temp_dir/$archive"
verify "$temp_dir/$archive" "$archive"
tar -xzf "$temp_dir/$archive" -C "$temp_dir" previewctl
verify "$temp_dir/previewctl" "$platform/previewctl"
chmod 755 "$temp_dir/previewctl"
mv -f -- "$temp_dir/previewctl" "$binary"
printf '%s\n' "$binary"

#!/bin/sh
# build-xkb-archive.sh
#
# Builds xkbdata/xkeyboard-config.tar.xz, the archive keytrans embeds
# (xkb_embedded.go) so that keymaps can be compiled on a machine without
# xkeyboard-config installed (unxed/keytrans#1).
#
# Source: the *built* xkeyboard-config data of a distribution package
# (Debian/Ubuntu "xkb-data", usually /usr/share/X11/xkb). The upstream release
# tarball is not usable as is: it ships rules/ only as unassembled *.part files
# that meson glues into rules/evdev, rules/base and the catalogs at build time,
# so reproducing them here would mean running xkeyboard-config's own build.
# The distribution package is that build's output, and its data files are
# upstream's, unmodified in practice. The upstream COPYING (of the same
# xkeyboard-config version) is stored in the archive next to the data.
#
# The whole tree is kept (compat, geometry, keycodes, rules, symbols, types):
# about 340 KB compressed for xkeyboard-config 2.41, which is fine per the
# decision on the issue. Symlinks (rules/base -> evdev) are stored as copies so
# extraction never has to create links.
#
# The archive is deterministic for a given input (sorted names, zero mtime and
# owners, xz -9e), so rebuilding from the same package gives the same bytes.
#
# Usage: scripts/build-xkb-archive.sh [/path/to/xkb] [/path/to/COPYING]
# Requires: GNU tar, xz. Reads the system data only; it builds nothing else.

set -e

XKB_DIR="${1:-/usr/share/X11/xkb}"
COPYING="${2:-}"
OUT="$(dirname "$0")/../xkbdata/xkeyboard-config.tar.xz"

if [ ! -f "$XKB_DIR/rules/evdev" ] || [ ! -d "$XKB_DIR/symbols" ]; then
    echo "not an xkeyboard-config tree: $XKB_DIR (need rules/evdev and symbols/)" >&2
    exit 1
fi
if [ -z "$COPYING" ] || [ ! -f "$COPYING" ]; then
    echo "pass the upstream COPYING of the same xkeyboard-config version as the 2nd argument" >&2
    exit 1
fi

mkdir -p "$(dirname "$OUT")"
tar --create --dereference --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner \
    --mode='u=rwX,go=rX' \
    -C "$XKB_DIR" compat geometry keycodes rules symbols types \
    -C "$(dirname "$COPYING")" "$(basename "$COPYING")" \
    | xz -9e -c > "$OUT"

ls -l "$OUT"
sha256sum "$OUT"

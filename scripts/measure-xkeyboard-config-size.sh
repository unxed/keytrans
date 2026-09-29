#!/bin/sh
# measure-xkeyboard-config-size.sh
#
# Reproduces the size estimate discussed in
# https://github.com/unxed/keytrans/issues/1: how much would embedding
# xkeyboard-config into the keytrans binary add, compressed and
# uncompressed, for (a) the full upstream dataset and (b) a minimal
# subset that keeps only what keytrans/xkb-go actually needs to resolve
# a keymap from RMLVO names (no GUI catalogs, no keyboard geometry
# pictures, no legacy XFree86 rule duplicates).
#
# Requires: an xkeyboard-config install (Debian/Ubuntu package
# "xkb-data", usually under /usr/share/X11/xkb), tar, xz, gzip. zstd is
# used if present. This script does not modify or build keytrans; it
# only measures data already present on the local system, so it is safe
# to run outside of a Go toolchain / CI sandbox.
#
# Usage: scripts/measure-xkeyboard-config-size.sh [/path/to/xkb]

set -e

XKB_DIR="${1:-/usr/share/X11/xkb}"
if [ ! -d "$XKB_DIR" ]; then
    echo "xkeyboard-config directory not found: $XKB_DIR" >&2
    echo "Pass a path explicitly, or install the xkb-data package." >&2
    exit 1
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

human() {
    # portable-ish byte -> human readable, du -h isn't consistent across
    # platforms for plain files
    awk -v b="$1" 'BEGIN {
        split("B KB MB GB", u, " ")
        i = 1
        while (b >= 1024 && i < 4) { b /= 1024; i++ }
        printf "%.0f%s", b, u[i]
    }'
}

measure_one() {
    label="$1"
    tarpath="$2"

    rawsize=$(wc -c < "$tarpath")
    gzsize=$(gzip -9 -c "$tarpath" | wc -c)
    xzsize=$(xz -9 -c "$tarpath" | wc -c)
    if command -v zstd >/dev/null 2>&1; then
        zstsize=$(zstd -19 -c "$tarpath" 2>/dev/null | wc -c)
    else
        zstsize="n/a"
    fi

    printf '%-28s raw=%-8s gzip-9=%-8s xz-9=%-8s zstd-19=%s\n' \
        "$label" \
        "$(human "$rawsize")" \
        "$(human "$gzsize")" \
        "$(human "$xzsize")" \
        "$([ "$zstsize" = "n/a" ] && echo n/a || human "$zstsize")"
}

echo "Source: $XKB_DIR"
echo

# --- (a) full upstream tree, as installed ---
tar -cf "$WORK/full.tar" -C "$XKB_DIR" .
measure_one "full xkeyboard-config" "$WORK/full.tar"

# --- (b) minimal subset needed by xkb-go's NewKeymapFromNames path ---
# keeps: compat, keycodes, types, symbols (all needed to compile any
# keymap), rules/evdev only (plain-text rules file, the one modern
# systems use; drops the "base"/"xorg" legacy duplicate and the
# .xml/.lst GUI catalogs, which are only consumed by layout-picker UIs,
# never by keymap compilation)
# drops: geometry (physical keyboard pictures, unused for text/keysym
# translation)
MIN="$WORK/min"
mkdir -p "$MIN/compat" "$MIN/keycodes" "$MIN/types" "$MIN/symbols" "$MIN/rules"
cp -r "$XKB_DIR/compat/." "$MIN/compat/"
cp -r "$XKB_DIR/keycodes/." "$MIN/keycodes/"
cp -r "$XKB_DIR/types/." "$MIN/types/"
cp -r "$XKB_DIR/symbols/." "$MIN/symbols/"
cp "$XKB_DIR/rules/evdev" "$MIN/rules/evdev"

tar -cf "$WORK/min.tar" -C "$MIN" .
measure_one "minimal (no geometry/xml/lst)" "$WORK/min.tar"

echo
echo "Per-directory raw sizes in the source tree:"
for d in compat geometry keycodes rules symbols types; do
    [ -d "$XKB_DIR/$d" ] && printf '  %-10s %s\n' "$d" "$(du -sh "$XKB_DIR/$d" | cut -f1)"
done

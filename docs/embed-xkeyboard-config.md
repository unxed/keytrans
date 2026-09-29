# Embedding xkeyboard-config: size estimate (issue #1, first slice)

This document answers the first-slice question from
[#1](https://github.com/unxed/keytrans/issues/1): *how much would embedding
`xkeyboard-config` add to the built binary, using compression?* It does not
implement the embedding itself — see "Next steps" below for what is still
open.

## Numbers

Measured against Ubuntu's `xkb-data 2.41-2ubuntu1.1` package
(`/usr/share/X11/xkb`), using `scripts/measure-xkeyboard-config-size.sh`
(re-run it locally to reproduce; it only reads system data, it does not
touch this repo or require a Go toolchain):

| Dataset | Raw | gzip -9 | xz -9 | zstd -19 |
|---|---|---|---|---|
| Full upstream tree (compat+geometry+keycodes+rules+symbols+types) | 3.6 MB | 533 KB | **339 KB** | 365 KB |
| Minimal subset (see below) | 2.4 MB | 416 KB | **287 KB** | 312 KB |

The "minimal subset" drops two things that `xkb-go`'s
`Context.NewKeymapFromNames` never reads:

- `geometry/` (404 KB raw) — physical keyboard pictures, used only by
  graphical keyboard-layout viewers, irrelevant to keysym/text translation.
- `rules/*.xml`, `rules/*.lst`, and the legacy `base`/`xorg` rule-file
  duplicates (rules dir goes from 804 KB to ~35 KB) — the `.xml`/`.lst`
  files are human-readable catalogs consumed by GUI layout pickers
  (GNOME Control Center, etc.), not by rule resolution itself. Modern
  systems use the `evdev` rules file; `base`/`xorg` is a legacy alias kept
  for old XFree86-era setups. `xkb-go`'s default `RuleNames.Rules` is
  already `"evdev"`.

So a realistic embed, compressed with `xz -9`, lands at **~290–340 KB**,
depending on whether the legacy/GUI-only files are trimmed. This is in the
same ballpark as the ~200 KB the issue guessed, not far off, though on the
higher side once you include enough layouts to be useful (see "Coverage"
below — the above numbers are for the *entire* upstream symbols set, i.e.
already "all languages", not a curated subset).

**Correction to the issue's RAM estimate:** the issue text says embedding
would need "only ~50 KB of RAM". That undercounts by roughly two orders of
magnitude. `xz`/`zstd` shrink the *compressed, at-rest* size, but
`xkb-go`'s rule/symbol parser needs the **decompressed** text to work with
(it's a text-format parser, not a streaming one — see "Architecture
finding" below). Decompressed, the minimal subset is ~2.4 MB, the full
tree ~3.6 MB. That's the real transient memory/disk cost while a keymap is
being compiled, not 50 KB. It's still small in absolute terms (comparable
to a couple of medium PNGs), just not what the issue stated.

## Architecture finding: xkb-go needs a real filesystem path, not `embed.FS`

`github.com/unxed/xkb-go` (`context.go`, `rules.go`) resolves include
paths and reads keymap component files with `os.Open`/`os.ReadFile`
against real filesystem paths (default: `/usr/share/X11/xkb`,
`~/.config/xkb`, etc. via `Context.addDefaultIncludePaths`). It has no
`fs.FS`/`embed.FS`-based loading path today.

This means `go:embed`-ing the data into the `keytrans` binary is not a
drop-in swap — the embedded bytes need to be materialized onto a real
filesystem location before `xkb-go` can read them. The good news: this
does **not** require an upstream change to `xkb-go`. Its public API
already supports pointing it at an arbitrary extra directory:

- `xkb.NewContext(ctx, xkb.ContextNoFlags)` (optionally
  `ContextNoDefaultIncludes` to skip the host's own, possibly broken,
  `/usr/share/X11/xkb`)
- `(*Context).PrependIncludePath(dir)` to make that directory searched
  first (or only, combined with `ContextNoDefaultIncludes`)

So the shape of a future implementation in `keytrans` is: on first use,
extract the embedded (compressed) archive to a cache directory (e.g.
under `os.UserCacheDir()`, keyed by a content hash so repeat runs skip
re-extracting), then `PrependIncludePath` that directory before calling
`NewKeymapFromNames`. This keeps the change entirely inside `keytrans`
(`backend_purexkb.go` / a new small helper), no `xkb-go` PR needed for
this part.

## Related finding: no RMLVO source without X11 yet, and xkb-go's env-var fallback is a no-op

The issue's real motivation (per its title) is X11-independence, which is
two separate problems:

1. Compiling a keymap without reading `xkeyboard-config` off the host
   filesystem — addressed above (embed + extract-to-cache).
2. Getting the **RMLVO names** (rules/model/layout/variant/options)
   without an X11 connection at all. Today `backend_purexkb.go` only gets
   these from the `_XKB_RULES_NAMES` window property on the X server
   (see `x11_factory.go`'s fallback chain in `PROJECT.md`), so it's still
   unusable headless or on Wayland without XWayland.

`xkb-go` defines a `ContextNoEnvironmentNames` flag whose doc comment says
it "prevents reading RMLVO names from environment variables... Affects
`XKB_DEFAULT_RULES`, `XKB_DEFAULT_MODEL`, `XKB_DEFAULT_LAYOUT`, etc." —
but grepping the library, that env-var reading is not actually
implemented anywhere; the flag is currently a no-op. So a no-X11 RMLVO
source (env vars and/or explicit user config as a fallback when there is
no X connection) needs to be implemented in `keytrans` itself, not
delegated to `xkb-go`.

## Coverage vs. size trade-off (not decided yet)

The numbers above are for *all* of `xkeyboard-config`'s symbols (every
layout upstream ships, well over 100 base layouts plus variants). If full
coverage turns out to be too heavy for some target, a curated subset
(e.g. Latin + Cyrillic + a handful of other popular layouts) would cut
`symbols/` — the largest single directory at 2.5 MB raw — substantially,
at the cost of failing for less common layouts. This repo has not decided
between "embed everything, ~300 KB compressed" and "embed a curated
subset" — that's a product decision for a follow-up step, not something
this measurement pass should silently pick.

## Next steps (out of scope for this change)

This change is measurement only. Not yet implemented:

- `go:embed` of the chosen dataset (full or curated) plus a
  compress/decompress step (the repo doesn't vendor a compression
  library yet; `compress/flate` from the stdlib avoids a new dependency
  at the cost of a worse ratio than `xz`/`zstd` — worth benchmarking
  before picking one).
- The extract-to-cache-dir + `PrependIncludePath` wiring in
  `backend_purexkb.go` (or a new backend), including cache invalidation
  and concurrent-process-safety for the extraction step.
- A no-X11 RMLVO source (env vars / explicit config) so the embedded
  data is actually reachable without `_XKB_RULES_NAMES`.

## Implementation (issue #1, second slice)

The owner's answers on the issue: embed the **full tree** (about 400 KB is not
a problem); choose the compression among the dependencies of `unxed/zipper`;
choose the way to get the data.

### Compression: xz via `github.com/unxed/xz`

`unxed/zipper` (through `unxed/tar`, `unxed/zip`, `unxed/archives`) depends on
`github.com/unxed/xz` (also `klauspost/compress`, `andybalholm/brotli`,
`pierrec/lz4`, `dsnet/compress`, ...). Of these, xz/LZMA2 gives the smallest
archive of this text-heavy data (343 KB for the full tree, against about
365 KB for zstd -19 and 533 KB for gzip -9, see the table above), and
`unxed/xz` is pure Go with no dependencies of its own (its `go.mod` has none),
so it adds one module to `keytrans` and nothing below it. Decompression speed
does not matter here: the archive is unpacked once per archive version.

### Data: the distribution's built xkeyboard-config, in a committed archive

The upstream release tarball cannot be embedded as it is: `rules/` is shipped
there as unassembled `*.part` files that meson concatenates into `rules/evdev`,
`rules/base` and the catalogs at build time. The distribution package (Debian/
Ubuntu `xkb-data`) is the output of that build, with upstream's data files, so
`scripts/build-xkb-archive.sh` packs it (`compat geometry keycodes rules
symbols types`, plus the upstream `COPYING` of the same version) into
`xkbdata/xkeyboard-config.tar.xz`, deterministically (sorted, zero mtime and
owners, `xz -9e`), symlinks stored as copies. The archive is committed and is
rebuilt by hand when xkeyboard-config is updated; the current one is built from
`xkb-data 2.41-2ubuntu1.1`.

### Unpacking and use

`xkb_embedded.go` embeds the archive (`go:embed`) and unpacks it once, into
`os.UserCacheDir()/keytrans/xkb-<hash of the archive>/` (atomically, through a
temporary directory and a rename; a private temporary directory when there is
no cache directory). `compileKeymapFromNames` first compiles with the system's
own data and only if that fails retries with the unpacked copy put in front of
xkb-go's include paths (`Context.PrependIncludePath`), so a system's local
changes still win where there are any. `backend_purexkb.go` uses it.

### RMLVO without X11

`rmlvoWithDefaults` fills the names an X server does not publish (or when there
is no X server): `XKB_DEFAULT_RULES/MODEL/LAYOUT/VARIANT/OPTIONS` first, then
`evdev` / `pc105` / `us`. Names already set are kept. Nothing else in the
backend changed: `purexkb` still needs an X connection for the keyboard state,
so a fully X11-free backend is the next slice.

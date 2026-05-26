# keytrans

`keytrans` is a pure-Go, zero-CGO, fallback-aware keyboard layout and text translation library for Unix windowing systems (X11 and Wayland).

## Why keytrans?

In the Go desktop development ecosystem, handling localized keyboard input (e.g. typing Cyrillic, Greek, or Hanja) and detecting keyboard shortcuts is notoriously complex:

1.  **Wrappers around libxkbcommon or libX11** is problematic on systems where ffi is unavailable.
2.  **Pure-Go implementations** ignore layout groups entirely, falling back to English QWERTY, or fail to handle modifiers like `AltGr` (Level 3 shift) and `NumLock` on the keypad.

`keytrans` bridges this gap. It provides a **Unified Keyboard Translation Pipeline** that automatically selects the most robust available backend on the host OS.

## The Fallback Chain

When initializing an X11 translator, `keytrans` attempts the following backends in order:

```
┌────────────────────────────────────────────────────────┐
│ 1. libxkbcommon (FFI)                                  │ -> Best. Native multi-layout, uses xkbcommon.
└───────────────────────────┬────────────────────────────┘
                            ▼ (fails or -tags noffi)
┌────────────────────────────────────────────────────────┐
│ 2. libX11 XIM (FFI)                                    │ -> Native X11 input method (Xutf8LookupString).
└───────────────────────────┬────────────────────────────┘
                            ▼ (fails)
┌────────────────────────────────────────────────────────┐
│ 3. xkbcomp (Pure Go)                                   │ -> Runs `xkbcomp $DISPLAY` and parses map with xkb-go.
└───────────────────────────┬────────────────────────────┘
                            ▼ (fails or xkbcomp missing)
┌────────────────────────────────────────────────────────┐
│ 4. Core X11 Heuristics (Pure Go)                       │ -> Reverse-engineers ModMap & keypad, zero-CGO.
└────────────────────────────────────────────────────────┘
```

If the system has no dynamic loading capabilities or if compiled with `-tags noffi`, `keytrans` gracefully falls back to purely Go-based parsing (`xkbcomp` or `corex11` heuristics), keeping compilation 100% clean and portable.

## Manual Backend Selection

While the automatic fallback chain is recommended for production, you can force the translator to use a specific backend during development, testing, or debugging.

To do this, specify the `PreferredBackend` field in `OSInfo`:

```go
info := keytrans.OSInfo{
	XgbConn:          conn,
	WindowID:         uint32(winID),
	PreferredBackend: "corex11", // Force Core X11 heuristics
}
```

Supported backend strings:
*   `"libxkbcommon"` (requires `libxkbcommon.so.0` and FFI support)
*   `"libX11-XIM"` (requires `libX11.so.6` and FFI support)
*   `"xkbcomp"` (requires `xkbcomp` binary available in `$PATH`)
*   `"corex11"` (pure Go fallback, always available)

### Forcing via Environment Variable

Alternatively, you can force a specific backend globally using the `KEYTRANS_BACKEND` environment variable:

```bash
export KEYTRANS_BACKEND="corex11"
./my_app
```

**Important:** Unlike the programmatic `PreferredBackend` option, if the backend requested via `KEYTRANS_BACKEND` fails to initialize (e.g. if you request `libxkbcommon` but the shared library is missing), `keytrans` will immediately log a critical error and **terminate the application** (`os.Exit(1)`).

## Installation

```bash
go get github.com/unxed/keytrans
```

## Quick Start

### 1. Initializing the Translator (X11 example via XGB)

```go
package main

import (
	"github.com/jezek/xgb"
	"github.com/unxed/keytrans"
)

func main() {
	// Connect to X server using XGB
	conn, _ := xgb.NewConn()
	defer conn.Close()

	// Setup OS-specific connection info
	info := keytrans.OSInfo{
		DisplayString:    ":0",
		XgbConn:          conn,
		WindowID:         uint32(winID), // Required for XIM (libX11) backend
		PreferredBackend: "",            // Optional: force a specific backend (e.g. "corex11")
	}

	// Create fallback-aware translator
	translator := keytrans.NewX11Translator(info)
	defer translator.Close()

	println("Active backend:", translator.Name())
}
```

### 2. Handling Key Events in your Loop

When you receive a raw KeyPress or KeyRelease event from your window manager, feed it directly to the translator:

```go
// Inside your event loop
switch e := ev.(type) {
case xproto.KeyPressEvent:
    // Translate keycode & state into logical key and Unicode character
    event := translator.TranslateX11(e.Detail, e.State, true)

    if event.Char != 0 {
        fmt.Printf("User typed: %c\n", event.Char)
    }
    fmt.Printf("Virtual Key Code: 0x%X\n", event.VirtualKeyCode)
}
```

### 3. Handling Keyboard Layout Changes (MappingNotify)

When a user changes their keyboard layout settings (e.g., via `setxkbmap` or by plugging in a new keyboard), X11 sends a `MappingNotify` event. Since `keytrans` does not run its own event loop, the recommended approach is to handle this event in your application's loop and recreate the translator to reload the mapping:

```go
switch e := ev.(type) {
case xproto.MappingNotifyEvent:
    // Recreate the translator to reload the updated mapping
    translator.Close()
    translator = keytrans.NewX11Translator(info)
}
```

## Output Format: Win32-style Events

`keytrans` produces keyboard events using the unified `winkeys.InputEvent` structure, which follows the Win32 input model. This design decision was made for several practical reasons:

1.  **Heritage:** The library was originally written for the `vtui` project, which relies on this format.
2.  **Industry Standard:** The Win32 model is a well-known, simple, and unambiguous way to describe keyboard input. It provides a clean separation between the logical key (Virtual Key Code), the state of modifiers (Shift, Ctrl, Alt), and the resulting Unicode character. This makes event processing straightforward and predictable.
3.  **Maximum Flexibility:** You are not locked into the Win32 ecosystem. If you prefer to handle key logic using raw X11 keycodes, you can do so—simply use the `Char` field from the translated event to get the localized Unicode text and ignore the Virtual Key Code entirely.

## Build Tags
```

If you want to completely disable FFI and ensure that your binary doesn't even attempt to dynamically load C libraries at runtime (e.g. for static scratch builds), build your project with the `noffi` tag:

```bash
go build -tags noffi .
```

This will strip out the `libxkbcommon` and `libX11` backends, compiling only the pure Go implementations.

## Compatibility

Important note for developers and AI models: We cannot rely on group information in X11 events, as some X servers always report 0. Additionally, we cannot rely on the Mod5 value in X11 events, because certain X servers set this flag for any group other than 0, regardless of whether AltGr is pressed. Please ensure all future development takes these factors into consideration.

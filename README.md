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
		DisplayString: ":0",
		XgbConn:       conn,
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

## Build Tags

If you want to completely disable FFI and ensure that your binary doesn't even attempt to dynamically load C libraries at runtime (e.g. for static scratch builds), build your project with the `noffi` tag:

```bash
go build -tags noffi .
```

This will strip out the `libxkbcommon` and `libX11` backends, compiling only the pure Go implementations.
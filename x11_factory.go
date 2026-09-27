package keytrans

import (
	"log/slog"
	"os"
)

// NewX11Translator attempts to initialize the best available X11 keyboard translator
// using a fallback chain.
//
// Fallback order:
// 1. xkbgo-x11 (Pure Go) - Reads the real device keymap over the XKB wire protocol.
// 2. libxkbcommon (FFI) - Native multi-layout support via the system library.
// 3. libX11 XIM (FFI) - Extremely robust for X11, handles complex IMEs.
// 4. purexkb (Pure Go) - Approximates the layout from RMLVO names and compiles it.
// 5. dynamicxkb (Pure Go) - Reconstructs the keymap dynamically in Go memory.
// 6. xkbcomp (Pure Go) - Parses X server map using xkb-go.
// 7. Core X11 (Pure Go) - Reverse-engineers modifiers using smart heuristics.
func NewX11Translator(info OSInfo) Translator {
	// 0. Check if a specific backend is strictly forced via environment variable
	if envBackend := os.Getenv("KEYTRANS_BACKEND"); envBackend != "" {
		var t Translator
		switch envBackend {
		case "xkbgo-x11":
			t = newXkbgoX11Translator(info)
		case "libxkbcommon":
			t = newXkbcommonTranslator(info)
		case "libX11-XIM":
			t = newX11XIMTranslator(info)
		case "purexkb":
			t = newPureXKBTranslator(info)
		case "xkbcomp":
			t = newXkbcompTranslator(info)
		case "corex11":
			t = newCoreX11Translator(info)
		case "dynamicxkb":
			t = newDynamicXkbTranslator(info)
		default:
			slog.Error("keytrans: unknown forced backend in KEYTRANS_BACKEND", "backend", envBackend)
			os.Exit(1)
		}
		if t == nil {
			slog.Error("keytrans: forced backend in KEYTRANS_BACKEND failed to initialize", "backend", envBackend)
			os.Exit(1)
		}
		return t
	}

	// Check if a specific backend is requested by the user
	if info.PreferredBackend != "" {
		switch info.PreferredBackend {
		case "xkbgo-x11":
			if t := newXkbgoX11Translator(info); t != nil {
				return t
			}
		case "libxkbcommon":
			if t := newXkbcommonTranslator(info); t != nil {
				return t
			}
		case "libX11-XIM":
			if t := newX11XIMTranslator(info); t != nil {
				return t
			}
		case "purexkb":
			if t := newPureXKBTranslator(info); t != nil {
				return t
			}
		case "dynamicxkb":
			if t := newDynamicXkbTranslator(info); t != nil {
				return t
			}
		case "xkbcomp":
			if t := newXkbcompTranslator(info); t != nil {
				return t
			}
		case "corex11":
			if t := newCoreX11Translator(info); t != nil {
				return t
			}
		default:
			slog.Warn("keytrans: unknown preferred backend, falling back to auto-detection", "backend", info.PreferredBackend)
		}
	}

	// 1. Try xkbgo-x11 (queries the real device keymap over the XKB wire
	// protocol, implemented in backend_xkbgo_x11.go). It requires neither
	// CGO nor FFI and reads the keymap the server actually resolved for
	// the device, so it is tried before the FFI backends.
	if t := newXkbgoX11Translator(info); t != nil {
		slog.Info("keytrans: using xkbgo-x11 native-go backend")
		return t
	}

	// 2. Try libxkbcommon (implemented in backend_xkbcommon.go)
	if t := newXkbcommonTranslator(info); t != nil {
		slog.Info("keytrans: using libxkbcommon backend")
		return t
	}

	// 3. Try libX11 XIM (implemented in backend_x11xim.go)
	if t := newX11XIMTranslator(info); t != nil {
		slog.Info("keytrans: using libX11 XIM backend")
		return t
	}

	// 4. Try purexkb (compiles rules natively in Go, implemented in backend_purexkb.go)
	if t := newPureXKBTranslator(info); t != nil {
		slog.Info("keytrans: using purexkb native-go backend")
		return t
	}

	// 5. Try dynamicxkb (implemented in backend_dynamicxkb.go)
	if t := newDynamicXkbTranslator(info); t != nil {
		slog.Info("keytrans: using dynamicxkb native-go backend")
		return t
	}

	// 6. Try xkbcomp + xkb-go (implemented in backend_xkbcomp.go)
	if t := newXkbcompTranslator(info); t != nil {
		slog.Info("keytrans: using xkbcomp pure-go backend")
		return t
	}

	// 7. Fallback to Core X11 Heuristics
	slog.Info("keytrans: using Core X11 heuristics fallback")
	return newCoreX11Translator(info)
}

package keytrans

import (
	"log/slog"
)

// NewX11Translator attempts to initialize the best available X11 keyboard translator
// using a fallback chain.
//
// Fallback order:
// 1. libxkbcommon (FFI) - Best, native multi-layout support, standard.
// 2. libX11 XIM (FFI) - Extremely robust for X11, handles complex IMEs.
// 3. xkbcomp (Pure Go) - Parses X server map using xkb-go.
// 4. Core X11 (Pure Go) - Reverse-engineers modifiers using smart heuristics.
func NewX11Translator(info OSInfo) Translator {
	// 1. Try libxkbcommon (implemented in backend_xkbcommon.go)
	if t := newXkbcommonTranslator(info); t != nil {
		slog.Info("keytrans: using libxkbcommon backend")
		return t
	}

	// 2. Try libX11 XIM (implemented in backend_x11xim.go)
	if t := newX11XIMTranslator(info); t != nil {
		slog.Info("keytrans: using libX11 XIM backend")
		return t
	}

	// 3. Try xkbcomp + xkb-go (implemented in backend_xkbcomp.go)
	if t := newXkbcompTranslator(info); t != nil {
		slog.Info("keytrans: using xkbcomp pure-go backend")
		return t
	}

	// 4. Fallback to Core X11 Heuristics
	slog.Info("keytrans: using Core X11 heuristics fallback")
	return newCoreX11Translator(info)
}
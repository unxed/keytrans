package keytrans

// Translator defines the interface for translating OS-specific raw keyboard
// events into unified KeyEvent structures.
type Translator interface {
	// Name returns the name of the active backend (e.g., "libxkbcommon", "corex11").
	Name() string

	// TranslateX11 translates an X11 KeyPress/KeyRelease event.
	// detail: X11 keycode
	// state: X11 modifier state mask
	// isDown: true for KeyPress, false for KeyRelease
	TranslateX11(detail uint8, state uint16, isDown bool) KeyEvent

	// TranslateWayland translates a Wayland keyboard event.
	// Wayland splits modifier state updates from key events, so the translator
	// must maintain internal state.
	// keycode: evdev keycode
	// isDown: true for KeyPress, false for KeyRelease
	TranslateWayland(keycode uint32, isDown bool) KeyEvent

	// UpdateWaylandModifiers updates the internal state for Wayland.
	UpdateWaylandModifiers(modsDepressed, modsLatched, modsLocked, group uint32)

	// Close releases any resources held by the translator.
	Close()
}
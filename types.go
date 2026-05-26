package keytrans

import "github.com/unxed/vtinput"

// KeyEvent represents the result of a translation.
type KeyEvent struct {
	VirtualKeyCode  uint16 // vtinput.VK_* constants
	Char            rune   // Translated Unicode character (0 if none)
	ControlKeyState vtinput.ControlKeyState
}

// OSInfo provides context to the translator (e.g., connection handles).
type OSInfo struct {
	DisplayString string      // e.g., ":0" for X11, "wayland-0" for Wayland
	XgbConn       interface{} // *xgb.Conn (passed as interface{} to avoid forcing xgb dep if unused)
}
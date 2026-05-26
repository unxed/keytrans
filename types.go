package keytrans

// OSInfo provides context to the translator (e.g., connection handles).
type OSInfo struct {
	DisplayString    string      // e.g., ":0" for X11, "wayland-0" for Wayland
	XgbConn          interface{} // *xgb.Conn (passed as interface{} to avoid forcing xgb dep if unused)
	WindowID         uint32      // Required for XIM (X Input Method) backend
	PreferredBackend string      // Force a specific translator backend ("libxkbcommon", "libX11-XIM", "xkbcomp", "corex11")
}

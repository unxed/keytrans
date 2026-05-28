package keytrans

import (
	"testing"

	"github.com/jezek/xgb"
)

func TestXIM_WindowIDGuard(t *testing.T) {
	// If WindowID is 0, XIM must return nil to avoid server-side BadWindow error.
	info := OSInfo{
		WindowID: 0,
		XgbConn:  &xgb.Conn{}, // Fake conn to pass type check
	}

	xim := newX11XIMTranslator(info)
	if xim != nil {
		t.Errorf("newX11XIMTranslator should return nil for WindowID=0")
	}
}

func TestX11XIMTranslator_Failures(t *testing.T) {
	// Nil connection
	tr := newX11XIMTranslator(OSInfo{XgbConn: nil})
	if tr != nil {
		t.Error("Expected nil translator for nil connection")
	}

	// Invalid connection type
	tr = newX11XIMTranslator(OSInfo{XgbConn: "invalid"})
	if tr != nil {
		t.Error("Expected nil translator for invalid connection type")
	}
}
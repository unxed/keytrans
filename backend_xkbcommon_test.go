package keytrans

import (
	"testing"
)

func TestXkbcommonTranslator_Failures(t *testing.T) {
	// Nil connection
	tr := newXkbcommonTranslator(OSInfo{XgbConn: nil})
	if tr != nil {
		t.Error("Expected nil translator for nil connection")
	}

	// Invalid connection type
	tr = newXkbcommonTranslator(OSInfo{XgbConn: "invalid"})
	if tr != nil {
		t.Error("Expected nil translator for invalid connection type")
	}
}
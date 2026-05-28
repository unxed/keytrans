package keytrans

import (
	"testing"

	"github.com/jezek/xgb"
	"github.com/unxed/winkeys"
)

func TestKeysymToVK(t *testing.T) {
	tests := []struct {
		keysym uint32
		wantVK uint16
	}{
		{0xff51, winkeys.VK_LEFT},
		{0xff8d, winkeys.VK_RETURN},   // KP_Enter
		{0xffb5, winkeys.VK_NUMPAD5}, // KP_5
		{0xffab, winkeys.VK_ADD},     // KP_Add
		{0x0061, winkeys.VK_A},       // 'a'
		{0x0041, winkeys.VK_A},       // 'A'
		{0x0033, winkeys.VK_3},       // '3'
	}

	for _, tt := range tests {
		got := keysymToVK(tt.keysym)
		if got != tt.wantVK {
			t.Errorf("keysymToVK(0x%x) = 0x%x, want 0x%x", tt.keysym, got, tt.wantVK)
		}
	}
}

func TestTranslateModifiers(t *testing.T) {
	tests := []struct {
		state uint16
		want  winkeys.ControlKeyState
	}{
		{1, winkeys.ShiftPressed},
		{4, winkeys.LeftCtrlPressed},
		{8, winkeys.LeftAltPressed},
		{2, winkeys.CapsLockOn},
		{16, winkeys.NumLockOn},
		{1 | 4, winkeys.ShiftPressed | winkeys.LeftCtrlPressed},
	}

	for _, tt := range tests {
		got := translateModifiers(tt.state)
		if got != tt.want {
			t.Errorf("translateModifiers(%d) = %d, want %d", tt.state, got, tt.want)
		}
	}
}

func TestNewX11Translator_Fallback(t *testing.T) {
	// If no valid connection is passed, the factory must safely return nil without panicking.
	info := OSInfo{
		DisplayString: ":99",
		XgbConn:       nil,
	}

	translator := NewX11Translator(info)
	if translator != nil {
		t.Errorf("Expected nil translator for nil connection, got %T (%s)", translator, translator.Name())
	}
}

func TestKeysymToVK_Cyrillic(t *testing.T) {
	cyrillicEscSym := uint32(0x6d3) // 'с'
	if vk := keysymToVK(cyrillicEscSym); vk != 0 {
		t.Errorf("keysymToVK(Cyrillic_es) should be 0, got 0x%x", vk)
	}
}

func TestTranslateModifiers_Extended(t *testing.T) {
	// 2 = LockMask (CapsLock), 16 = Mod2Mask (usually NumLock)
	state := uint16(2 | 16)
	got := translateModifiers(state)
	want := winkeys.CapsLockOn | winkeys.NumLockOn

	if got != want {
		t.Errorf("translateModifiers(2|16) = %v, want %v", got, want)
	}
}

func TestXKBStateParsing_Manual(t *testing.T) {
	// Mocking the structure of a reply from XKB:GetState
	reply := make([]byte, 18)
	reply[13] = 1            // lockedGroup = 1
	xgb.Put16(reply[14:], 2) // baseGroup = 2 (LE)

	baseGroup := int16(xgb.Get16(reply[14:]))
	lockedGroup := reply[13]
	group := int(baseGroup) + int(lockedGroup)

	if group != 3 {
		t.Errorf("XKB State parsing logic failed: base(%d) + locked(%d) = %d, want 3", baseGroup, lockedGroup, group)
	}
}

func TestNewX11Translator_InvalidConnType(t *testing.T) {
	// Verify that factory doesn't panic if an invalid connection type is supplied
	info := OSInfo{
		DisplayString: ":99",
		XgbConn:       "this is a string, not *xgb.Conn",
	}

	translator := NewX11Translator(info)
	if translator != nil {
		t.Errorf("Expected nil translator for invalid connection type, got %T", translator)
	}
}

func TestKeysymToVK_EdgeCases(t *testing.T) {
	// Test unmapped / invalid keysyms
	invalidKeysyms := []uint32{0, 0x12345678, 0xffffffff}
	for _, sym := range invalidKeysyms {
		got := keysymToVK(sym)
		if got != 0 {
			t.Errorf("keysymToVK(0x%x) = %d, want 0 (unmapped)", sym, got)
		}
	}
}

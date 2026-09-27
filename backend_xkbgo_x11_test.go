package keytrans

import (
	"testing"

	"github.com/unxed/xkb-go"
)

func TestXkbgoX11Translator(t *testing.T) {
	// Compile the standard test keymap from xkb-go. This exercises the
	// keysym translation logic shared with purexkb/xkbcomp; the actual
	// wire-protocol keymap retrieval (x11.NewKeymapFromX11Device) is
	// covered by xkb-go's own tests against synthetic replies, since it
	// requires a live X server connection that isn't available in CI.
	keymap := xkb.TestKeymap()
	if keymap == nil {
		t.Fatal("Failed to get test keymap from xkb-go")
	}

	// Instantiate xkbgoX11Translator with conn = nil to bypass network queries
	tr := &xkbgoX11Translator{
		conn:     nil,
		xkbState: keymap.NewState(),
	}

	if got := tr.Name(); got != "xkbgo-x11" {
		t.Errorf("Name() = %q, want %q", got, "xkbgo-x11")
	}

	tests := []struct {
		name         string
		keycode      uint8
		state        uint16 // modifiers (1 = Shift, 2 = CapsLock)
		isDown       bool
		expectedChar rune
		expectedVK   uint16
	}{
		{
			name:         "Letter 'A' lowercase",
			keycode:      38, // Keycode 38 is 'A' in xkb-go test keymap
			state:        0,
			isDown:       true,
			expectedChar: 'a',
			expectedVK:   0x41, // VK_A
		},
		{
			name:         "Letter 'A' uppercase with Shift",
			keycode:      38,
			state:        1, // ShiftMask = 1
			isDown:       true,
			expectedChar: 'A',
			expectedVK:   0x41,
		},
		{
			name:         "Letter 'A' uppercase with CapsLock",
			keycode:      38,
			state:        2, // LockMask = 2
			isDown:       true,
			expectedChar: 'A',
			expectedVK:   0x41,
		},
		{
			name:         "Letter 'A' lowercase with Shift + CapsLock",
			keycode:      38,
			state:        3, // Shift + Lock
			isDown:       true,
			expectedChar: 'a',
			expectedVK:   0x41,
		},
		{
			name:         "Number '1'",
			keycode:      10, // Keycode 10 is '1' in xkb-go test keymap
			state:        0,
			isDown:       true,
			expectedChar: '1',
			expectedVK:   0x31, // VK_1
		},
		{
			name:         "Number '1' shifted to symbol '!'",
			keycode:      10,
			state:        1,
			isDown:       true,
			expectedChar: '!',
			expectedVK:   0x31,
		},
		{
			name:         "Special key 'Escape'",
			keycode:      9, // Keycode 9 is ESC
			state:        0,
			isDown:       true,
			expectedChar: 0,
			expectedVK:   0x1b, // VK_ESCAPE
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := tr.TranslateX11(tc.keycode, tc.state, tc.isDown)

			if event.Char != tc.expectedChar {
				t.Errorf("TranslateX11 failed for Char. Expected '%c' (0x%X), got '%c' (0x%X)",
					tc.expectedChar, tc.expectedChar, event.Char, event.Char)
			}

			if event.VirtualKeyCode != tc.expectedVK {
				t.Errorf("TranslateX11 failed for VK. Expected 0x%X, got 0x%X",
					tc.expectedVK, event.VirtualKeyCode)
			}

			if event.InputSource != "xkbgo-x11" {
				t.Errorf("TranslateX11 InputSource = %q, want %q", event.InputSource, "xkbgo-x11")
			}
		})
	}

	// Verify that calling Close doesn't panic
	tr.Close()
}

func TestXkbgoX11Translator_Wayland(t *testing.T) {
	keymap := xkb.TestKeymap()
	if keymap == nil {
		t.Fatal("Failed to get test keymap from xkb-go")
	}

	tr := &xkbgoX11Translator{
		conn:     nil,
		xkbState: keymap.NewState(),
	}

	// 1. Test basic Wayland translation (evdev keycode 30 -> X11 keycode 38 -> 'a')
	ev := tr.TranslateWayland(30, true)
	if ev.Char != 'a' || ev.VirtualKeyCode != 0x41 {
		t.Errorf("TranslateWayland failed for base key. Expected 'a'/0x41, got '%c'/0x%X",
			ev.Char, ev.VirtualKeyCode)
	}

	// 2. Test Wayland modifier update (Shift)
	tr.UpdateWaylandModifiers(1, 0, 0, 0) // modsDepressed = 1 (ShiftMask)
	ev = tr.TranslateWayland(30, true)
	if ev.Char != 'A' {
		t.Errorf("TranslateWayland failed after UpdateWaylandModifiers. Expected 'A', got '%c'", ev.Char)
	}

	// 3. Test Wayland modifier reset
	tr.UpdateWaylandModifiers(0, 0, 0, 0)
	ev = tr.TranslateWayland(30, true)
	if ev.Char != 'a' {
		t.Errorf("TranslateWayland failed after resetting modifiers. Expected 'a', got '%c'", ev.Char)
	}
}

// TestNewXkbgoX11Translator_NoConn verifies the factory function returns nil
// (instead of panicking) when OSInfo carries no usable *xgb.Conn, matching
// the behavior of the other X11 backends so the fallback chain in
// x11_factory.go can move on to the next candidate.
func TestNewXkbgoX11Translator_NoConn(t *testing.T) {
	if tr := newXkbgoX11Translator(OSInfo{}); tr != nil {
		t.Errorf("expected nil translator when XgbConn is absent, got %#v", tr)
	}

	if tr := newXkbgoX11Translator(OSInfo{XgbConn: "not-a-conn"}); tr != nil {
		t.Errorf("expected nil translator when XgbConn has the wrong type, got %#v", tr)
	}
}

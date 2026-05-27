package keytrans

import (
	"testing"

	"github.com/unxed/xkb-go"
)

func TestPureXKBTranslator(t *testing.T) {
	// Compile the standard test keymap from xkb-go
	keymap := xkb.TestKeymap()
	if keymap == nil {
		t.Fatal("Failed to get test keymap from xkb-go")
	}

	// Instantiate pureXKBTranslator with conn = nil to bypass network queries
	tr := &pureXKBTranslator{
		conn:     nil,
		xkbState: keymap.NewState(),
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
		})
	}

	// Verify that calling Close doesn't panic
	tr.Close()
}

func TestPureXKBTranslator_Wayland(t *testing.T) {
	keymap := xkb.TestKeymap()
	if keymap == nil {
		t.Fatal("Failed to get test keymap from xkb-go")
	}

	tr := &pureXKBTranslator{
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
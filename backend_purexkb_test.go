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
}
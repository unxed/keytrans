package keytrans

import (
	"testing"

	"github.com/unxed/winkeys"
	"github.com/unxed/xkb-go"
)

func TestDynamicXkbTranslator(t *testing.T) {
	keymap := xkb.TestKeymap()
	if keymap == nil {
		t.Fatal("Failed to get test keymap from xkb-go")
	}

	tr := &dynamicXkbTranslator{
		conn:     nil,
		xkbState: keymap.NewState(),
	}

	if name := tr.Name(); name != "dynamicxkb" {
		t.Errorf("Expected backend name to be 'dynamicxkb', got %q", name)
	}

	tests := []struct {
		name         string
		keycode      uint8
		state        uint16
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
			expectedVK:   winkeys.VK_A,
		},
		{
			name:         "Letter 'A' uppercase with Shift",
			keycode:      38,
			state:        1, // ShiftMask = 1
			isDown:       true,
			expectedChar: 'A',
			expectedVK:   winkeys.VK_A,
		},
		{
			name:         "Special key 'Escape'",
			keycode:      9, // Keycode 9 is ESC
			state:        0,
			isDown:       true,
			expectedChar: 0,
			expectedVK:   winkeys.VK_ESCAPE,
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

func TestSymToStr(t *testing.T) {
	tests := []struct {
		sym  uint32
		want string
	}{
		{0, "NoSymbol"},
		{0x01000100, "0x1000100"},
		{0x0061, "a"},
		{0xff51, "Left"},
	}

	for _, tt := range tests {
		got := symToStr(tt.sym)
		if got != tt.want {
			t.Errorf("symToStr(0x%x) = %q, want %q", tt.sym, got, tt.want)
		}
	}
}

func TestContainsNonAlphanumeric(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"a_b_c", false},
		{"abc123", false},
		{"abc-123", true},
		{"abc 123", true},
	}

	for _, tt := range tests {
		got := containsNonAlphanumeric(tt.s)
		if got != tt.want {
			t.Errorf("containsNonAlphanumeric(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestDynamicXkbUnimplemented(t *testing.T) {
	tr := &dynamicXkbTranslator{}

	// These should not panic and should return empty structures
	ev := tr.TranslateWayland(30, true)
	if ev.VirtualKeyCode != 0 || ev.Char != 0 {
		t.Errorf("Expected empty KeyEvent for TranslateWayland, got %+v", ev)
	}

	tr.UpdateWaylandModifiers(0, 0, 0, 0)
	tr.Close()
}

func TestNewDynamicXkbTranslator_Failures(t *testing.T) {
	// Nil connection
	tr := newDynamicXkbTranslator(OSInfo{XgbConn: nil})
	if tr != nil {
		t.Error("Expected nil translator for nil connection")
	}

	// Invalid connection type
	tr = newDynamicXkbTranslator(OSInfo{XgbConn: "invalid"})
	if tr != nil {
		t.Error("Expected nil translator for invalid connection type")
	}
}
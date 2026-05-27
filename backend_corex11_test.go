package keytrans

import (
	"testing"

	"github.com/jezek/xgb/xproto"
)

func TestCoreX11LookupTopology(t *testing.T) {
	// Helper to create a dummy translator to test the lookup heuristic without X11 connection
	makeTranslator := func(syms []uint32) *coreX11Translator {
		xkbSyms := make([]xproto.Keysym, len(syms))
		for i, s := range syms {
			xkbSyms[i] = xproto.Keysym(s)
		}

		return &coreX11Translator{
			minKeycode:     8,
			maxKeycode:     8,
			symsPerKey:     len(syms),
			syms:           xkbSyms,
			modeSwitchMask: 0x20, // Mod5
			altGrMask:      0x80, // ISO_Level3_Shift
			numLockMask:    0x10,
		}
	}

	tests := []struct {
		name           string
		syms           []uint32
		keycode        int    // Optional, defaults to 8
		state          uint16 // modifiers (1 = Shift, 2 = CapsLock, 0x10 = NumLock, 0x80 = AltGr)
		group          int
		expectedResult uint32
	}{
		// 1. Basic 2-element array (Base, Shift) - Typical for simple layouts or XQuartz
		{
			name: "Basic 2-elem Group 0",
			syms: []uint32{'a', 'A'},
			state: 0, group: 0, expectedResult: 'a',
		},
		{
			name: "Basic 2-elem Group 0 Shift",
			syms: []uint32{'a', 'A'},
			state: 1, group: 0, expectedResult: 'A', // ShiftMask = 1
		},
		{
			name: "Basic 2-elem Group 1 (Fallback to 0)",
			syms: []uint32{'a', 'A'},
			state: 0, group: 1, expectedResult: 'a', // Out of bounds baseIdx falls back to 0
		},

		// 2. Multi-group 4-element array (e.g. English + Russian mapped sequentially)
		{
			name: "Multi-group 4-elem Group 0",
			syms: []uint32{'a', 'A', 0x06c1 /* Cyrillic_a */, 0x06e1 /* Cyrillic_A */},
			state: 0, group: 0, expectedResult: 'a',
		},
		{
			name: "Multi-group 4-elem Group 1",
			syms: []uint32{'a', 'A', 0x06c1, 0x06e1},
			state: 0, group: 1, expectedResult: 0x06c1,
		},
		{
			name: "Multi-group 4-elem Group 0 AltGr (Should ignore)",
			syms: []uint32{'a', 'A', 0x06c1, 0x06e1},
			state: 0x80, group: 0, expectedResult: 'a', // AltGr ignored because syms[2] is a letter
		},

		// 3. Single-group 4-element array with AltGr (e.g. macOS option key or German layout)
		{
			name: "Single-group 4-elem Group 0",
			syms: []uint32{'e', 'E', 0x20ac /* Euro */, 0x00a2 /* Cent */},
			state: 0, group: 0, expectedResult: 'e',
		},
		{
			name: "Single-group 4-elem Group 0 AltGr",
			syms: []uint32{'e', 'E', 0x20ac, 0x00a2},
			state: 0x80, group: 0, expectedResult: 0x20ac, // AltGr jumps to +2 because Euro is not a letter
		},

		// 4. Standard Xorg 8-element array (Full XKB flattening matrix)
		{
			name: "Xorg 8-elem Group 0",
			syms: []uint32{'a', 'A', 0x06c1, 0x06e1, 0x00e6 /* ae */, 0x00c6 /* AE */, 0, 0},
			state: 0, group: 0, expectedResult: 'a',
		},
		{
			name: "Xorg 8-elem Group 1",
			syms: []uint32{'a', 'A', 0x06c1, 0x06e1, 0x00e6, 0x00c6, 0, 0},
			state: 0, group: 1, expectedResult: 0x06c1,
		},
		{
			name: "Xorg 8-elem Group 0 AltGr",
			syms: []uint32{'a', 'A', 0x06c1, 0x06e1, 0x00e6, 0x00c6, 0, 0},
			state: 0x80, group: 0, expectedResult: 0x00e6, // +4 offset applied safely
		},
		{
			name: "Xorg 8-elem Group 1 AltGr (Empty fallback to Base)",
			syms: []uint32{'a', 'A', 0x06c1, 0x06e1, 0x00e6, 0x00c6, 0, 0},
			state: 0x80, group: 1, expectedResult: 0x06c1, // Group 1 Base returned because +4 target is empty
		},

		// 5. Group 1 AltGr (Successful)
		{
			name: "Xorg 8-elem Group 1 AltGr (Successful)",
			syms: []uint32{'a', 'A', 0x06c1, 0x06e1, 0x00e6, 0x00c6, 0x01000410 /* Cyrillic A Unicode */, 0},
			state: 0x80, group: 1, expectedResult: 0x01000410, // Group 1 AltGr found at index 6 (+4 from base 2)
		},

		// 6. CapsLock Testing (using isLetterKeysym)
		{
			name: "CapsLock on Latin Letter (a -> A)",
			syms: []uint32{'a', 'A'},
			state: 2, group: 0, expectedResult: 'A', // CapsLock = 2
		},
		{
			name: "CapsLock on Cyrillic Letter (cyr_a -> cyr_A)",
			syms: []uint32{0x06c1, 0x06e1},
			state: 2, group: 0, expectedResult: 0x06e1, // Cyrillic CapsLock works natively
		},
		{
			name: "CapsLock on non-letter (1 -> 1)",
			syms: []uint32{'1', '!'},
			state: 2, group: 0, expectedResult: '1', // CapsLock doesn't affect numbers
		},

		// 7. Keypad / NumLock Testing
		{
			name: "Keypad with NumLock OFF",
			syms: []uint32{0xff95 /* KP_Home */, 0xffb7 /* KP_7 */},
			state: 0, group: 0, expectedResult: 0xff95,
		},
		{
			name: "Keypad with NumLock ON",
			syms: []uint32{0xff95, 0xffb7},
			state: 0x10, group: 0, expectedResult: 0xffb7, // NumLockMask = 0x10, returns shifted KP_7
		},

		// 8. Keycode boundaries
		{
			name:    "Keycode below minimum",
			syms:    []uint32{'a', 'A'},
			keycode: 7, // minKeycode is 8
			state:   0, group: 0, expectedResult: 0, // Should return 0 immediately
		},
		{
			name:    "Keycode above maximum",
			syms:    []uint32{'a', 'A'},
			keycode: 9, // maxKeycode is 8
			state:   0, group: 0, expectedResult: 0, // Should return 0 immediately
		},

		// 9. All-zero / Empty keysym array
		{
			name: "All-zero keysyms",
			syms: []uint32{0, 0},
			state: 0, group: 0, expectedResult: 0, // Should return 0
		},

		// 10. Lowercase-to-Uppercase fallback (shiftedSym == baseSym)
		{
			name: "Single-symbol fallback to Uppercase",
			syms: []uint32{'a'}, // Only one symbol, baseSym == shiftedSym == 'a'
			state: 1, group: 0, expectedResult: 'A', // Should dynamically convert to 'A' (0x41)
		},

		// 11. International layout detection (Greek)
		{
			name: "Greek Group 1 detection (isNonLatin)",
			syms: []uint32{'a', 'A', 0x07e1 /* Greek_alpha */, 0x07c1 /* Greek_ALPHA */},
			state: 0, group: 1, expectedResult: 0x07e1,
		},

		// 12. CapsLock on Latin-1 Accent (æ -> Æ)
		{
			name: "CapsLock on Latin-1 Accent (æ -> Æ)",
			syms: []uint32{0x00e6 /* æ */, 0x00c6 /* Æ */},
			state: 2, group: 0, expectedResult: 0x00c6, // CapsLock changes case for accents too
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := makeTranslator(tc.syms)
			kc := tc.keycode
			if kc == 0 {
				kc = 8
			}
			res := tr.lookup(kc, tc.state, tc.group)
			if res != tc.expectedResult {
				t.Errorf("Lookup failed. Expected 0x%X, got 0x%X", tc.expectedResult, res)
			}
		})
	}
}
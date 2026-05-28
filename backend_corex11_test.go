package keytrans

import (
	"testing"

	"github.com/jezek/xgb/xproto"
	"github.com/unxed/winkeys"
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

func TestCoreX11Lookup(t *testing.T) {
	// Create a mock coreX11Translator to verify lookup logic under various states.
	// Keycode 24 (offset = (24-8)*4 = 64 with symsPerKey=4)
	syms := make([]xproto.Keysym, 512)
	offset := (24 - 8) * 4

	// Group 0: Level 0 = 'q' (0x71), Level 1 = 'Q' (0x51)
	syms[offset+0] = 0x71
	syms[offset+1] = 0x51
	// Group 1: Level 0 = 'й' (0x6aa), Level 1 = 'Й' (0x6ba)
	syms[offset+2] = 0x6aa
	syms[offset+3] = 0x6ba

	trans := &coreX11Translator{
		minKeycode:     8,
		maxKeycode:     100,
		symsPerKey:     4,
		syms:           syms,
		numLockMask:    16,  // Mod2
		modeSwitchMask: 32,  // Mod3
		altGrMask:      128, // Mod5
	}

	tests := []struct {
		name  string
		state uint16
		group int
		want  uint32
	}{
		{"Base Group 0", 0, 0, 0x71},
		{"Shift Group 0", 1, 0, 0x51},
		{"CapsLock Group 0", 2, 0, 0x51},
		{"Shift+CapsLock Group 0", 3, 0, 0x71},
		{"Base Group 1 (XKB state)", 0, 1, 0x6aa},
		{"Shift Group 1 (XKB state)", 1, 1, 0x6ba},
		{"Base Group 1 (ModeSwitch mask)", 32, 0, 0x6aa},
		{"Shift Group 1 (ModeSwitch mask)", 32 | 1, 0, 0x6ba},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trans.lookup(24, tt.state, tt.group)
			if got != tt.want {
				t.Errorf("lookup(kc=24, state=0x%x, group=%d) = 0x%x, want 0x%x", tt.state, tt.group, got, tt.want)
			}
		})
	}
}

func TestCoreX11Lookup_Keypad(t *testing.T) {
	// Create a mock coreX11Translator for keypad keys (NumLock/Shift inversion tests)
	syms := make([]xproto.Keysym, 512)
	offset := (79 - 8) * 4 // Keycode 79 (KP_1 / End)

	// Index 0: KeysymKPEnd (0xff9c), Index 1: KeysymKP1 (0xffb1)
	syms[offset+0] = 0xff9c
	syms[offset+1] = 0xffb1

	trans := &coreX11Translator{
		minKeycode:  8,
		maxKeycode:  100,
		symsPerKey:  4,
		syms:        syms,
		numLockMask: 16, // Mod2
	}

	tests := []struct {
		name  string
		state uint16
		want  uint32
	}{
		{"NumLock Off, Shift Off", 0, 0xff9c},       // End
		{"NumLock On, Shift Off", 16, 0xffb1},      // '1'
		{"NumLock Off, Shift On", 1, 0xffb1},       // '1'
		{"NumLock On, Shift On", 16 | 1, 0xff9c},   // End
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trans.lookup(79, tt.state, 0)
			if got != tt.want {
				t.Errorf("lookup(kc=79) = 0x%x, want 0x%x", got, tt.want)
			}
		})
	}
}

func TestCoreX11Lookup_CaseSynthesis_Cyrillic(t *testing.T) {
	// Create a mock translator where a key has only a lowercase Cyrillic symbol.
	// We want to verify that Case Synthesis works for non-Latin characters.
	// Keycode 24 (offset = (24-8)*4 = 64)
	syms := make([]xproto.Keysym, 256)
	offset := (24 - 8) * 4

	// Simulated broken server: only has 'й' (0x6ca) for both levels
	syms[offset+0] = 0x06ca
	syms[offset+1] = 0x06ca // No uppercase declared

	trans := &coreX11Translator{
		minKeycode: 8,
		maxKeycode: 100,
		symsPerKey: 4,
		syms:       syms,
	}

	// With Shift active, it must synthesize uppercase 'Й' (0x06ea)
	got := trans.lookup(24, 1, 0)
	want := uint32(0x06ea) // Keysym for 'Й'
	if got != want {
		t.Errorf("Cyrillic Case Synthesis failed: got 0x%x, want 0x%x (Й)", got, want)
	}
}

func TestCoreX11Lookup_MultiLayoutGroupWidth(t *testing.T) {
	// Simulate a bilingual layout (Eng + Rus) where symsPerKey = 12.
	syms := make([]xproto.Keysym, 12)
	syms[0] = 0x002C // Index 0: comma
	syms[1] = 0x003C // Index 1: less
	syms[2] = 0x06C2 // Index 2: Cyrillic_be (Letter!)
	syms[3] = 0x06E2 // Index 3: Cyrillic_BE (Letter!)
	syms[4] = 0x00AB // Index 4: guillemetleft (AltGr for Group 0)

	trans := &coreX11Translator{
		minKeycode: 8,
		maxKeycode: 100,
		symsPerKey: 12,
		syms:       syms,
		altGrMask:  128, // Mod5
	}

	// 1. Without AltGr, Group 0 must resolve to Index 0 (comma)
	if got := trans.lookup(8, 0, 0); got != 0x002C {
		t.Errorf("GroupWidth heuristic failed (Base): got 0x%x, want 0x002C", got)
	}

	// 2. With AltGr active, it must identify groupWidth=2 and find Index 4 (guillemetleft)
	if got := trans.lookup(8, 128, 0); got != 0x00AB {
		t.Errorf("GroupWidth heuristic failed (AltGr): got 0x%x, want 0x00AB", got)
	}
}

func TestCoreX11Lookup_AltGrPriority(t *testing.T) {
	// Verify that larger offsets (like 4) are checked before smaller ones (like 2).
	syms := make([]xproto.Keysym, 8)
	syms[0] = 0x002E // Index 0: period
	syms[2] = 0x06C0 // Index 2: Cyrillic_yu
	syms[4] = 0x00BB // Index 4: guillemetright

	trans := &coreX11Translator{
		minKeycode: 8,
		maxKeycode: 100,
		symsPerKey: 8,
		syms:       syms,
		altGrMask:  128,
	}

	// Must resolve to 0x00BB (Index 4), NOT 0x06C0 (Index 2)
	if got := trans.lookup(8, 128, 0); got != 0x00BB {
		t.Errorf("AltGr Priority check failed: got 0x%x, want 0x00BB", got)
	}
}

func TestCoreX11Lookup_BoundaryCases(t *testing.T) {
	trans := &coreX11Translator{
		minKeycode: 8,
		maxKeycode: 100,
		symsPerKey: 4,
		syms:       make([]xproto.Keysym, 10), // Artificially small array
	}

	// Case 1: Keycode too low
	if got := trans.lookup(4, 0, 0); got != 0 {
		t.Errorf("Expected 0 for low out-of-bounds keycode, got %d", got)
	}

	// Case 2: Keycode too high
	if got := trans.lookup(105, 0, 0); got != 0 {
		t.Errorf("Expected 0 for high out-of-bounds keycode, got %d", got)
	}

	// Case 3: Keycode in range, but offset out of bounds of short syms array (keycode 24)
	if got := trans.lookup(24, 0, 0); got != 0 {
		t.Errorf("Expected 0 for out-of-bounds syms array offset, got %d", got)
	}
}

func TestCoreX11TranslateX11_PositionalVKFallback(t *testing.T) {
	syms := make([]xproto.Keysym, 512)
	offset := (54 - 8) * 4

	syms[offset+0] = 0x63  // Group 0, Level 0 = 'c'
	syms[offset+1] = 0x43  // Group 0, Level 1 = 'C'
	syms[offset+2] = 0x6d3 // Group 1, Level 0 = 'с'
	syms[offset+3] = 0x6f3 // Group 1, Level 1 = 'С'

	trans := &coreX11Translator{
		minKeycode:     8,
		maxKeycode:     100,
		symsPerKey:     4,
		syms:           syms,
		modeSwitchMask: 32, // Mod3
	}

	event := trans.TranslateX11(54, 32, true)

	if event.Char != 'с' {
		t.Errorf("Expected translated character to be 'с', got '%c'", event.Char)
	}

	if event.VirtualKeyCode != winkeys.VK_C {
		t.Errorf("Expected fallback VirtualKeyCode to be VK_C (0x43), got 0x%X", event.VirtualKeyCode)
	}
}

func TestCoreX11Lookup_CapsLockOnNonLetters(t *testing.T) {
	syms := make([]xproto.Keysym, 256)
	offset := (10 - 8) * 4 // Keycode 10 (QWERTY '1')
	syms[offset+0] = 0x31 // '1'
	syms[offset+1] = 0x21 // '!'

	trans := &coreX11Translator{
		minKeycode: 8,
		maxKeycode: 100,
		symsPerKey: 4,
		syms:       syms,
	}

	got := trans.lookup(10, 2, 0)
	if got != 0x31 {
		t.Errorf("Expected CapsLock to have no effect on '1' keysym, got 0x%x, want 0x31", got)
	}
}

func TestCoreX11TranslateWayland_Noop(t *testing.T) {
	trans := &coreX11Translator{}
	event := trans.TranslateWayland(16, true)
	if event.VirtualKeyCode != 0 || event.Char != 0 {
		t.Errorf("Expected empty KeyEvent for coreX11.TranslateWayland, got %+v", event)
	}
}

func TestCoreX11Lookup_AltGrDynamicOffset(t *testing.T) {
	syms := make([]xproto.Keysym, 256)
	offset := (24 - 8) * 4 // Keycode 24

	syms[offset+0] = 0x61
	syms[offset+1] = 0x41
	syms[offset+2] = 0x00e6
	syms[offset+3] = 0x00c6

	trans := &coreX11Translator{
		minKeycode: 8,
		maxKeycode: 100,
		symsPerKey: 4,
		syms:       syms,
		altGrMask:  128, // Mod5
	}

	got := trans.lookup(24, 128, 0)
	if got != 0x00e6 {
		t.Errorf("Expected AltGr to resolve to 'æ' (0x00e6), got 0x%x", got)
	}

	gotShifted := trans.lookup(24, 129, 0)
	if gotShifted != 0x00c6 {
		t.Errorf("Expected AltGr+Shift to resolve to 'Æ' (0x00c6), got 0x%x", gotShifted)
	}
}

func TestCoreX11TranslateX11_ModifierKeys(t *testing.T) {
	syms := make([]xproto.Keysym, 256)
	offset := (50 - 8) * 4  // Keycode 50 is Left Shift (LFSH)
	syms[offset+0] = 0xffe1 // KeysymShiftL

	trans := &coreX11Translator{
		minKeycode: 8,
		maxKeycode: 100,
		symsPerKey: 4,
		syms:       syms,
	}

	event := trans.TranslateX11(50, 0, true)

	if event.VirtualKeyCode != winkeys.VK_LSHIFT {
		t.Errorf("Expected VK_LSHIFT (0x%X), got 0x%X", winkeys.VK_LSHIFT, event.VirtualKeyCode)
	}

	if event.Char != 0 {
		t.Errorf("Expected Char to be 0 for modifier key, got '%c'", event.Char)
	}
}

func TestCoreX11TranslateX11_PositionalVKFallback_GroupBased(t *testing.T) {
	syms := make([]xproto.Keysym, 512)
	offset := (54 - 8) * 4

	syms[offset+0] = 0x63  // Group 0, Level 0 = 'c'
	syms[offset+2] = 0x6d3 // Group 1, Level 0 = 'с'

	trans := &coreX11Translator{
		minKeycode: 8,
		maxKeycode: 100,
		symsPerKey: 4,
		syms:       syms,
	}

	sym := trans.lookup(54, 0, 1)
	if sym != 0x6d3 {
		t.Errorf("Expected Cyrillic 'с', got 0x%x", sym)
	}

	vk := keysymToVK(sym)
	if vk != 0 {
		t.Errorf("Expected VK to be 0 for Cyrillic 'с', got 0x%x", vk)
	}

	baseSym := trans.lookup(54, 0, 0)
	fallbackVK := keysymToVK(baseSym)

	if fallbackVK != winkeys.VK_C {
		t.Errorf("Positional fallback failed: expected VK_C (0x43), got 0x%X", fallbackVK)
	}
}

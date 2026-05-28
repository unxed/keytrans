package keytrans

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jezek/xgb/xproto"
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

func TestDynamicXkbShiftLockType(t *testing.T) {
	// Test compiling and running our custom DYN_TWO_LEVEL with ShiftLock mapping
	keymapStr := `xkb_keymap {
		xkb_keycodes {
			minimum = 8;
			maximum = 255;
			<I9> = 9;
		};
		xkb_types {
			type "DYN_TWO_LEVEL" {
				modifiers = Shift+Lock;
				map[Shift] = Level2;
				map[Lock] = Level2;
				map[Shift+Lock] = Level1;
			};
		};
		xkb_compatibility {};
		xkb_symbols {
			key <I9> {
				type[Group1] = "DYN_TWO_LEVEL",
				symbols[Group1] = [ 1, exclam ]
			};
		};
	};`

	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromString([]byte(keymapStr), xkb.KeymapFormatTextV1)
	if err != nil {
		t.Fatalf("Failed to compile ShiftLock test keymap: %v", err)
	}

	state := keymap.NewState()

	// 1. Base (no modifiers) -> '1'
	state.UpdateMask(0, 0, 0, 0, 0, 0)
	if sym := state.KeyGetOneSym(9); sym != 0x31 {
		t.Errorf("Expected '1' (0x31) without modifiers, got 0x%X", sym)
	}

	// 2. Shift active -> 'exclam'
	state.UpdateMask(1, 0, 0, 0, 0, 0) // ShiftMask = 1
	if sym := state.KeyGetOneSym(9); sym != 0x21 {
		t.Errorf("Expected '!' (0x21) with Shift, got 0x%X", sym)
	}

	// 3. Lock (CapsLock) active -> 'exclam' (Traditional German ShiftLock behavior!)
	state.UpdateMask(2, 0, 0, 0, 0, 0) // LockMask = 2
	if sym := state.KeyGetOneSym(9); sym != 0x21 {
		t.Errorf("Expected '!' (0x21) with CapsLock (ShiftLock behavior), got 0x%X", sym)
	}

	// 4. Shift + Lock active -> '1'
	state.UpdateMask(3, 0, 0, 0, 0, 0) // Shift+Lock = 3
	if sym := state.KeyGetOneSym(9); sym != 0x31 {
		t.Errorf("Expected '1' (0x31) with Shift+CapsLock, got 0x%X", sym)
	}
}

func TestDynamicXkbAltGrFourLevelType(t *testing.T) {
	// Test compiling and running our custom DYN_FOUR_LEVEL_ALPHA type with Mod5/AltGr
	keymapStr := `xkb_keymap {
		xkb_keycodes {
			minimum = 8;
			maximum = 255;
			<I59> = 59;
		};
		xkb_types {
			type "DYN_FOUR_LEVEL_ALPHA" {
				modifiers = Shift+Lock+Mod5;
				map[Shift] = Level2;
				map[Lock] = Level2;
				map[Shift+Lock] = Level1;
				map[Mod5] = Level3;
				map[Shift+Mod5] = Level4;
				map[Lock+Mod5] = Level4;
				map[Shift+Lock+Mod5] = Level3;
			};
		};
		xkb_compatibility {};
		xkb_symbols {
			key <I59> {
				type[Group1] = "DYN_FOUR_LEVEL_ALPHA",
				symbols[Group1] = [ comma, less, guillemetleft, less ],
				type[Group2] = "DYN_FOUR_LEVEL_ALPHA",
				symbols[Group2] = [ Cyrillic_be, Cyrillic_BE, guillemetleft, less ]
			};
		};
	};`

	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromString([]byte(keymapStr), xkb.KeymapFormatTextV1)
	if err != nil {
		t.Fatalf("Failed to compile AltGr test keymap: %v", err)
	}

	state := keymap.NewState()

	// Group 0 (English layout):
	// 1. Base (no modifiers) -> 'comma' (0x2c)
	state.UpdateMask(0, 0, 0, 0, 0, 0)
	if sym := state.KeyGetOneSym(59); sym != 0x2c {
		t.Errorf("Group 0 Base expected 'comma' (0x2c), got 0x%X", sym)
	}

	// 2. AltGr active (Mod5 = 0x80) -> 'guillemetleft' (0x00ab, typographical quote «)
	state.UpdateMask(0x80, 0, 0, 0, 0, 0)
	if sym := state.KeyGetOneSym(59); sym != 0x00ab {
		t.Errorf("Group 0 AltGr expected 'guillemetleft' (0x00ab), got 0x%X", sym)
	}

	// Group 1 (Russian layout, group index = 1):
	// 3. Base (no modifiers) -> 'Cyrillic_be' (0x06C2, 'б')
	state.UpdateMask(0, 0, 0, 0, 0, 1) // lockedGroup = 1
	if sym := state.KeyGetOneSym(59); sym != 0x06C2 {
		t.Errorf("Group 1 Base expected 'Cyrillic_be' (0x06C2), got 0x%X", sym)
	}

	// 4. AltGr active (Mod5 = 0x80) -> 'guillemetleft' (0x00ab, typographical quote «)
	state.UpdateMask(0x80, 0, 0, 0, 0, 1) // Mod5 active, lockedGroup = 1
	if sym := state.KeyGetOneSym(59); sym != 0x00ab {
		t.Errorf("Group 1 AltGr expected 'guillemetleft' (0x00ab), got 0x%X", sym)
	}
}

func TestDynamicXkbTripleLayoutAltGr(t *testing.T) {
	// Real-world Xorg 12-sym layout reconstruction test.
	// We simulate the exact structure from the bug report:
	// 0,1: G1 Base, 2,3: G2 Base, 4,5: G1 AltGr, 6,7: G2 AltGr, 8,9: G3 Base, 10,11: G3 AltGr

	// Create mock symbols for Keycode 59
	syms := make([]uint32, 12)
	syms[0], syms[1] = 0x2c, 0x3c    // Eng: comma, less
	syms[2], syms[3] = 0x6c2, 0x6e2  // Rus: be, BE
	syms[4], syms[5] = 0xab, 0x3c     // AltGr Eng: «, <
	syms[6], syms[7] = 0xab, 0x3c     // AltGr Rus: «, <
	syms[8], syms[9] = 0x63, 0x43    // G3: c, C
	syms[10], syms[11] = 0x100, 0x101 // AltGr G3: extra

	xkbSyms := make([]xproto.Keysym, 12)
	for i, s := range syms { xkbSyms[i] = xproto.Keysym(s) }

	// Trigger keymap generation logic (internal part of reloadKeymap)
	// We'll verify by compiling the string it generates.
	var b strings.Builder
	b.WriteString("xkb_keymap {\nxkb_keycodes { minimum=8; maximum=255; <I59>=59; };\n")
	b.WriteString("xkb_types {\n" +
		`type "DYN_FOUR_LEVEL_ALPHA" { modifiers= Shift+Mod5; map[Shift]= Level2; map[Mod5]= Level3; map[Shift+Mod5]= Level4; };` + "\n" +
		"};\nxkb_compatibility {};\nxkb_symbols {\n")

	// Implementation of the symbols generation part for testing
	length := 12
	offset := 0
	b.WriteString("  key <I59> {\n")

	// Re-run the fix logic
	numGroups := 4
	for g := 0; g < numGroups; g++ {
		base := g * 2
		altIdx := base + 4
		if altIdx < length && xkbSyms[offset+altIdx] != 0 {
			b.WriteString(fmt.Sprintf("    type[Group%d] = \"DYN_FOUR_LEVEL_ALPHA\",\n", g+1))
			b.WriteString(fmt.Sprintf("    symbols[Group%d] = [ 0x%X, 0x%X, 0x%X, 0x%X ]",
				g+1, uint32(xkbSyms[offset+base]), uint32(xkbSyms[offset+base+1]),
				uint32(xkbSyms[offset+altIdx]), uint32(xkbSyms[offset+altIdx+1])))
			if g < 3 { b.WriteString(",\n") } else { b.WriteString("\n") }
		}
	}
	b.WriteString("  };\n};\n};")

	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromString([]byte(b.String()), xkb.KeymapFormatTextV1)
	if err != nil {
		t.Fatalf("Reconstructed keymap failed to compile: %v\nGenerated map:\n%s", err, b.String())
	}

	state := keymap.NewState()

	// TEST: Group 2 (Russian) + AltGr (Mod5 = 0x80)
	state.UpdateMask(0x80, 0, 0, 0, 0, 1) // group index 1
	res := state.KeyGetOneSym(59)
	if res != 0xab {
		t.Errorf("AltGr on Russian layout failed. Expected 0xAB («), got 0x%X", res)
	}
}
func TestDynamicXkbTripleLayoutAltGr_NoLegacyAltGr(t *testing.T) {
	// Reconstructed keymap test for 3 groups where G1 and G2 do NOT have AltGr (compacted layout)

	// Create mock symbols for Keycode 52 (z -> y/z swap on QWERTZ)
	syms := make([]uint32, 12)
	syms[0], syms[1] = 0x7a, 0x5a    // G1 Base: z, Z
	syms[2], syms[3] = 0x6d1, 0x6f1  // G2 Base: ya, YA
	syms[4], syms[5] = 0x79, 0x59    // G3 Base: y, Y (offset 4 due to compression)
	syms[6], syms[7] = 0x100203a, 0  // G3 AltGr: › (offset 6 due to compression)

	xkbSyms := make([]xproto.Keysym, 12)
	for i, s := range syms { xkbSyms[i] = xproto.Keysym(s) }

	var b strings.Builder
	b.WriteString("xkb_keymap {\nxkb_keycodes { minimum=8; maximum=255; <I52>=52; };\n")
	b.WriteString("xkb_types {\n" +
		`type "DYN_FOUR_LEVEL_ALPHA" { modifiers= Shift+Mod5; map[Shift]= Level2; map[Mod5]= Level3; map[Shift+Mod5]= Level4; };` + "\n" +
		"};\nxkb_compatibility {};\nxkb_symbols {\n")

	length := 12
	offset := 0
	b.WriteString("  key <I52> {\n")

	numGroups := 3

	// Adaptive logic test
	hasLegacyAltGr := true
	sym0 := uint32(xkbSyms[0])
	sym4 := uint32(xkbSyms[4])
	if sym4 != 0 && sym4 != sym0 && isBaseLayoutLetter(sym4) && isBaseLayoutLetter(sym0) {
		hasLegacyAltGr = false
	}

	for g := 0; g < numGroups; g++ {
		base := 0
		altIdx := 0
		if hasLegacyAltGr {
			if g < 2 {
				base = g * 2
				altIdx = base + 4
			} else {
				base = 8 + (g-2)*4
				altIdx = base + 2
			}
		} else {
			if g < 2 {
				base = g * 2
				altIdx = 0
			} else {
				base = 4 + (g-2)*4
				altIdx = base + 2
			}
		}

		if base < length {
			b.WriteString(fmt.Sprintf("    type[Group%d] = \"DYN_FOUR_LEVEL_ALPHA\",\n", g+1))
			if altIdx < length && xkbSyms[offset+altIdx] != 0 {
				b.WriteString(fmt.Sprintf("    symbols[Group%d] = [ 0x%X, 0x%X, 0x%X, 0x%X ]",
					g+1, uint32(xkbSyms[offset+base]), uint32(xkbSyms[offset+base+1]),
					uint32(xkbSyms[offset+altIdx]), uint32(xkbSyms[offset+altIdx+1])))
			} else {
				b.WriteString(fmt.Sprintf("    symbols[Group%d] = [ 0x%X, 0x%X ]",
					g+1, uint32(xkbSyms[offset+base]), uint32(xkbSyms[offset+base+1])))
			}
			if g < 2 { b.WriteString(",\n") } else { b.WriteString("\n") }
		}
	}
	b.WriteString("  };\n};\n};")

	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromString([]byte(b.String()), xkb.KeymapFormatTextV1)
	if err != nil {
		t.Fatalf("Reconstructed compacted keymap failed to compile: %v\nGenerated map:\n%s", err, b.String())
	}

	state := keymap.NewState()

	// TEST 1: Group 3 (German) Base -> 'y' (0x79)
	state.UpdateMask(0, 0, 0, 0, 0, 2) // group index 2 (lockedGroup)
	res := state.KeyGetOneSym(52)
	if res != 0x79 {
		t.Errorf("Compacted layout Base on German failed. Expected 0x79 (y), got 0x%X", res)
	}

	// TEST 2: Group 3 (German) + AltGr (Mod5 = 0x80) -> '›' (0x100203a)
	state.UpdateMask(0x80, 0, 0, 0, 0, 2)
	res = state.KeyGetOneSym(52)
	if res != 0x100203a {
		t.Errorf("Compacted layout AltGr on German failed. Expected 0x100203A (›), got 0x%X", res)
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

func TestDynamicXkbKeypadNumLock(t *testing.T) {
	// Test compiling and running keypad keys with NumLock (Mod2) mapping
	keymapStr := `xkb_keymap {
		xkb_keycodes {
			minimum = 8;
			maximum = 255;
			<I79> = 79;
			<I106> = 106;
		};
		xkb_types {
			type "DYN_ONE_LEVEL" {
				modifiers = none;
				map[none] = Level1;
			};
			type "DYN_KEYPAD" {
				modifiers = Shift+Mod2;
				map[Shift] = Level2;
				map[Mod2] = Level2;
				map[Shift+Mod2] = Level1;
			};
		};
		xkb_compatibility {};
		xkb_symbols {
			key <I79> {
				type[Group1] = "DYN_KEYPAD",
				symbols[Group1] = [ KP_Home, KP_7 ]
			};
			key <I106> {
				type[Group1] = "DYN_ONE_LEVEL",
				symbols[Group1] = [ KP_Divide ]
			};
		};
	};`

	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromString([]byte(keymapStr), xkb.KeymapFormatTextV1)
	if err != nil {
		t.Fatalf("Failed to compile keypad test keymap: %v", err)
	}

	state := keymap.NewState()

	// 1. NumLock OFF (state = 0)
	state.UpdateMask(0, 0, 0, 0, 0, 0)
	if sym := state.KeyGetOneSym(79); sym != 0xFF95 { // KP_Home keysym
		t.Errorf("NumLock OFF: expected KP_Home (0xFF95), got 0x%X", sym)
	}
	if sym := state.KeyGetOneSym(106); sym != 0xFFAF { // KP_Divide keysym
		t.Errorf("NumLock OFF: expected KP_Divide (0xFFAF), got 0x%X", sym)
	}

	// 2. NumLock ON (Mod2 active = 0x10)
	state.UpdateMask(0, 0, 0x10, 0, 0, 0) // Mod2 locked/active
	if sym := state.KeyGetOneSym(79); sym != 0xFFB7 { // KP_7 keysym
		t.Errorf("NumLock ON: expected KP_7 (0xFFB7), got 0x%X", sym)
	}
	if sym := state.KeyGetOneSym(106); sym != 0xFFAF { // KP_Divide keysym (must NOT change)
		t.Errorf("NumLock ON: expected KP_Divide (0xFFAF), got 0x%X", sym)
	}
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
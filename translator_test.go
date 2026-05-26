package keytrans

import (
	"context"
	"testing"

	"github.com/jezek/xgb/xproto"
	"github.com/unxed/winkeys"
	"github.com/unxed/xkb-go"
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
	// 12 % 4 == 0, but Index 2 and 3 are letters (Cyrillic 'б' and 'Б'),
	// so the heuristic must correctly identify groupWidth as 2.
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
	// If the heuristic failed and used groupWidth=4, it would wrongly look at Index 2.
	if got := trans.lookup(8, 128, 0); got != 0x00AB {
		t.Errorf("GroupWidth heuristic failed (AltGr): got 0x%x, want 0x00AB", got)
	}
}

func TestCoreX11Lookup_AltGrPriority(t *testing.T) {
	// Verify that larger offsets (like 4) are checked before smaller ones (like 2).
	// This prevents grabbing a base letter from Group 1 when looking for AltGr in Group 0.
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

func TestIsXkbcompOutputValid(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{
			name: "Valid Map",
			data: "xkb_keycodes { <ESC> = 9; <RTRN> = 36; }; xkb_symbols { key <AD01> { [ q, Q ] }; key <AD02> { [ w, W ] }; key <AD03> { [ e, E ] }; key <AD04> { [ r, R ] }; key <AD05> { [ t, T ] }; key <AD06> { [ y, Y ] }; key <AD07> { [ u, U ] }; key <AD08> { [ i, I ] }; key <AD09> { [ o, O ] }; key <AD10> { [ p, P ] }; };",
			want: true,
		},
		{
			name: "Missing sections",
			data: "xkb_keycodes { <ESC> = 9; };",
			want: false,
		},
		{
			name: "macOS XQuartz empty key bug",
			data: "xkb_keycodes { <ESC> = 9; <RTRN> = 36; }; xkb_symbols { key < > { [ q, Q ] }; };",
			want: false,
		},
		{
			name: "Empty output",
			data: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isXkbcompOutputValid([]byte(tt.data))
			if got != tt.want {
				t.Errorf("isXkbcompOutputValid(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
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
	// Simulate keyboard map for QWERTY C key (keycode 54)
	syms := make([]xproto.Keysym, 512)
	offset := (54 - 8) * 4

	syms[offset+0] = 0x63  // Group 0, Level 0 = 'c'
	syms[offset+1] = 0x43  // Group 0, Level 1 = 'C'
	syms[offset+2] = 0x6d3 // Group 1, Level 0 = 'с' (Cyrillic es)
	syms[offset+3] = 0x6f3 // Group 1, Level 1 = 'С' (Cyrillic ES)

	trans := &coreX11Translator{
		minKeycode:     8,
		maxKeycode:     100,
		symsPerKey:     4,
		syms:           syms,
		modeSwitchMask: 32, // Mod3
	}

	// Simulate pressing 'с' (Russian layout active, Mod3/modeSwitch is set in state)
	event := trans.TranslateX11(54, 32, true)

	// 1. The literal character must be the Cyrillic 'с' (rune 0x0441 / 'с')
	if event.Char != 'с' {
		t.Errorf("Expected translated character to be 'с', got '%c'", event.Char)
	}

	// 2. The Virtual Key Code must fallback to English layout equivalent: VK_C (0x43)
	// This prevents shortcuts like Ctrl+C from breaking when alternative layout is on.
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

	// CapsLock On (state=2), Shift Off.
	// CapsLock must not affect non-letter keysyms.
	got := trans.lookup(10, 2, 0)
	if got != 0x31 {
		t.Errorf("Expected CapsLock to have no effect on '1' keysym, got 0x%x, want 0x31", got)
	}
}

func TestXkbcompTranslateWayland(t *testing.T) {
	// Parse a minimal valid keymap using xkb-go
	keymapStr := `xkb_keymap {
		xkb_keycodes {
			minimum = 8;
			maximum = 255;
			<AD01> = 24;
		};
		xkb_types {
			type "ONE_LEVEL" {
				modifiers = none;
				map[none] = 1;
			};
		};
		xkb_compatibility {};
		xkb_symbols {
			key <AD01> { [ q, Q ] };
		};
	};`

	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromString([]byte(keymapStr), xkb.KeymapFormatTextV1)
	if err != nil {
		t.Skip("xkb-go failed to parse minimal keymap in test context")
		return
	}

	trans := &xkbcompTranslator{
		xkbState: keymap.NewState(),
	}

	// Translate Wayland keycode 16 (evdev 16 -> X11 24 <AD01>)
	event := trans.TranslateWayland(16, true)

	if event.Char != 'q' {
		t.Errorf("Expected translated Wayland character to be 'q', got '%c'", event.Char)
	}

	if event.VirtualKeyCode != winkeys.VK_Q {
		t.Errorf("Expected Wayland VirtualKeyCode to be VK_Q (0x%X), got 0x%X", winkeys.VK_Q, event.VirtualKeyCode)
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
	// Create a mock coreX11Translator with groupWidth=2 (standard non-XKB width)
	syms := make([]xproto.Keysym, 256)
	offset := (24 - 8) * 4 // Keycode 24 (QWERTY 'q')
	
	// Group 0: Level 0 = 'a' (0x61), Level 1 = 'A' (0x41)
	syms[offset+0] = 0x61
	syms[offset+1] = 0x41
	// AltGr levels: Level 2 = 'æ' (0x00e6), Level 3 = 'Æ' (0x00c6)
	syms[offset+2] = 0x00e6
	syms[offset+3] = 0x00c6

	trans := &coreX11Translator{
		minKeycode: 8,
		maxKeycode: 100,
		symsPerKey: 4,
		syms:       syms,
		altGrMask:  128, // Mod5
	}

	// 1. Test AltGr On (state=128), Shift Off. Must resolve to 'æ' (0x00e6).
	got := trans.lookup(24, 128, 0)
	if got != 0x00e6 {
		t.Errorf("Expected AltGr to resolve to 'æ' (0x00e6), got 0x%x", got)
	}

	// 2. Test AltGr On + Shift On (state=129). Must resolve to 'Æ' (0x00c6).
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

	// Modifier keys must return correct VirtualKeyCode
	if event.VirtualKeyCode != winkeys.VK_LSHIFT {
		t.Errorf("Expected VK_LSHIFT (0x%X), got 0x%X", winkeys.VK_LSHIFT, event.VirtualKeyCode)
	}

	// Modifier keys must return 0 character (not a printable character)
	if event.Char != 0 {
		t.Errorf("Expected Char to be 0 for modifier key, got '%c'", event.Char)
	}
}

func TestXkbcompTranslate_PositionalVKFallback(t *testing.T) {
	// Simulate bilingual keyboard map with English and Russian layouts
	keymapStr := `xkb_keymap {
		xkb_keycodes {
			minimum = 8;
			maximum = 255;
			<AD01> = 24;
		};
		xkb_types {
			type "TWO_LEVEL" {
				modifiers = Shift;
				map[Shift] = 2;
			};
		};
		xkb_compatibility {};
		xkb_symbols {
			key <AD01> {
				symbols[Group1] = [ c, C ],
				symbols[Group2] = [ Cyrillic_es, Cyrillic_ES ]
			};
		};
	};`

	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromString([]byte(keymapStr), xkb.KeymapFormatTextV1)
	if err != nil {
		t.Skip("xkb-go failed to parse bilingual keymap in test context")
		return
	}

	trans := &xkbcompTranslator{
		xkbState: keymap.NewState(),
	}

	// Lock the active layout group to Group 1 (which represents the second group, i.e., Russian layout)
	trans.UpdateWaylandModifiers(0, 0, 0, 1)

	// Translate X11 keycode 24 (which is Cyrillic 'с' on the Russian layout)
	event := trans.TranslateX11(24, 0, true)

	// 1. The literal character must be Cyrillic 'с' (rune 0x0441)
	if event.Char != 'с' {
		t.Errorf("Expected translated character to be 'с', got '%c'", event.Char)
	}

	// 2. The Virtual Key Code must fallback to English layout equivalent: VK_C (0x43)
	if event.VirtualKeyCode != winkeys.VK_C {
		t.Errorf("Expected fallback VirtualKeyCode to be VK_C (0x43), got 0x%X", event.VirtualKeyCode)
	}
}

func TestCoreX11TranslateX11_PositionalVKFallback_GroupBased(t *testing.T) {
	// Simulate keyboard map for QWERTY C key (keycode 54)
	syms := make([]xproto.Keysym, 512)
	offset := (54 - 8) * 4

	syms[offset+0] = 0x63  // Group 0, Level 0 = 'c'
	syms[offset+2] = 0x6d3 // Group 1, Level 0 = 'с' (Cyrillic es)

	trans := &coreX11Translator{
		minKeycode: 8,
		maxKeycode: 100,
		symsPerKey: 4,
		syms:       syms,
	}

	// We test the lookup directly to simulate TranslateX11 behavior.
	// 1. Current char is Cyrillic 'с' (Group 1, State 0)
	sym := trans.lookup(54, 0, 1)
	if sym != 0x6d3 {
		t.Errorf("Expected Cyrillic 'с', got 0x%x", sym)
	}

	// 2. Trigger fallback logic: if vk is 0 and group > 0, lookup in Group 0
	vk := keysymToVK(sym)
	if vk != 0 {
		t.Errorf("Expected VK to be 0 for Cyrillic 'с', got 0x%x", vk)
	}

	// Simulation of: if vk == 0 && group > 0 { baseSym := t.lookup(kc, state, 0); vk = keysymToVK(baseSym) }
	baseSym := trans.lookup(54, 0, 0)
	fallbackVK := keysymToVK(baseSym)

	if fallbackVK != winkeys.VK_C {
		t.Errorf("Positional fallback failed: expected VK_C (0x43), got 0x%X", fallbackVK)
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

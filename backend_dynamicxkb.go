package keytrans

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
    "unicode"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/unxed/winkeys"
	"github.com/unxed/xkb-go"
)

type dynamicXkbTranslator struct {
	conn       *xgb.Conn
	xkbOpcode  byte
	xkbState   *xkb.State
	lastReload time.Time
}

func newDynamicXkbTranslator(info OSInfo) Translator {
	conn, ok := info.XgbConn.(*xgb.Conn)
	if !ok || conn == nil {
		return nil
	}

	initKeycodeScheme(conn)

	// Request XKEYBOARD extension to get the state dynamically
	var xkbOpcode byte
	extCookie := xproto.QueryExtension(conn, uint16(len("XKEYBOARD")), "XKEYBOARD")
	if extReply, err := extCookie.Reply(); err == nil && extReply.Present {
		xkbOpcode = extReply.MajorOpcode
		buf := make([]byte, 8)
		buf[0] = xkbOpcode
		xgb.Put16(buf[2:], 2)
		xgb.Put16(buf[4:], 1)
		cookie := conn.NewCookie(true, true)
		conn.NewRequest(buf, cookie)
		_, _ = cookie.Reply()
	}

	t := &dynamicXkbTranslator{
		conn:      conn,
		xkbOpcode: xkbOpcode,
	}

	if err := t.reloadKeymap(); err != nil {
		slog.Error("dynamicxkb: failed to compile dynamic keymap", "err", err)
		return nil
	}

	return t
}

func (t *dynamicXkbTranslator) Name() string {
	return "dynamicxkb"
}

func (t *dynamicXkbTranslator) reloadKeymap() error {
	setup := xproto.Setup(t.conn)

	minK, maxK, symsPerKey, syms, err := loadCoreKeymapData(t.conn, setup)
	if err != nil {
		return err
	}

	_, _, layout, _, options := getXKBRulesNames(t.conn)
	useShiftLock := false
	if strings.Contains(options, "caps:shiftlock") {
		useShiftLock = true
	} else if !strings.Contains(options, "caps:") {
		for _, lay := range strings.Split(layout, ",") {
			lay = strings.TrimSpace(lay)
			if lay == "de" || lay == "fr" || lay == "cz" {
				useShiftLock = true
				break
			}
		}
	}

	// 1. Find Modifiers Mapping (specifically looking for NumLock)
	numLockMod := "Mod2" // Default fallback
	if modReply, err := xproto.GetModifierMapping(t.conn).Reply(); err == nil && modReply != nil {
		kpm := int(modReply.KeycodesPerModifier)
		modNames := []string{"Shift", "Lock", "Control", "Mod1", "Mod2", "Mod3", "Mod4", "Mod5"}

		for modIndex := 0; modIndex < 8; modIndex++ {
			for i := 0; i < kpm; i++ {
				kc := int(modReply.Keycodes[modIndex*kpm+i])
				if kc >= minK && kc <= maxK {
					offset := (kc - minK) * symsPerKey
					if offset < len(syms) {
						if uint32(syms[offset]) == uint32(xkb.KeyNumLock) {
							numLockMod = modNames[modIndex]
						}
					}
				}
			}
		}
	}
	level3Mod := "Mod5" // Default fallback for AltGr
	if modReply, err := xproto.GetModifierMapping(t.conn).Reply(); err == nil && modReply != nil {
		kpm := int(modReply.KeycodesPerModifier)
		modNames := []string{"Shift", "Lock", "Control", "Mod1", "Mod2", "Mod3", "Mod4", "Mod5"}

		for modIndex := 0; modIndex < 8; modIndex++ {
			for i := 0; i < kpm; i++ {
				kc := int(modReply.Keycodes[modIndex*kpm+i])
				if kc >= minK && kc <= maxK {
					offset := (kc - minK) * symsPerKey
					if offset < len(syms) {
						sym := uint32(syms[offset])
						if sym == uint32(xkb.KeyISOLevel3Shift) || sym == uint32(xkb.KeyAltR) {
							level3Mod = modNames[modIndex]
						}
					}
				}
			}
		}
	}

	// 2. Generate the XKB Text representation
	var b strings.Builder
	b.WriteString("xkb_keymap {\n")

	// 2a. Keycodes
	b.WriteString("xkb_keycodes {\n")
	b.WriteString(fmt.Sprintf("  minimum = %d;\n  maximum = %d;\n", minK, maxK))
	for kc := minK; kc <= maxK; kc++ {
		b.WriteString(fmt.Sprintf("  <I%d> = %d;\n", kc, kc))
	}
	b.WriteString("};\n")

	// 2b. Types
	b.WriteString("xkb_types {\n")
	b.WriteString(`  type "DYN_ONE_LEVEL" { modifiers= none; map[none]= Level1; };` + "\n")
	if useShiftLock {
		b.WriteString(`  type "DYN_TWO_LEVEL" { modifiers= Shift+Lock; map[Shift]= Level2; map[Lock]= Level2; map[Shift+Lock]= Level1; };` + "\n")
	} else {
		b.WriteString(`  type "DYN_TWO_LEVEL" { modifiers= Shift; map[Shift]= Level2; };` + "\n")
	}
	b.WriteString(`  type "DYN_ALPHABETIC" { modifiers= Shift+Lock; map[Shift]= Level2; map[Lock]= Level2; map[Shift+Lock]= Level1; };` + "\n")
	b.WriteString(fmt.Sprintf(`  type "DYN_KEYPAD" { modifiers= Shift+%s; map[Shift]= Level2; map[%s]= Level2; map[Shift+%s]= Level1; };`+"\n", numLockMod, numLockMod, numLockMod))
	b.WriteString(fmt.Sprintf(`  type "DYN_FOUR_LEVEL" { modifiers= Shift+%s; map[Shift]= Level2; map[%s]= Level3; map[Shift+%s]= Level4; };`+"\n", level3Mod, level3Mod, level3Mod))
	b.WriteString(fmt.Sprintf(`  type "DYN_FOUR_LEVEL_ALPHA" { modifiers= Shift+Lock+%s; map[Shift]= Level2; map[Lock]= Level2; map[Shift+Lock]= Level1; map[%s]= Level3; map[Shift+%s]= Level4; map[Lock+%s]= Level4; map[Shift+Lock+%s]= Level3; };`+"\n", level3Mod, level3Mod, level3Mod, level3Mod, level3Mod))
	b.WriteString("};\n")

	// 2c. Compatibility (Empty minimal)
	b.WriteString("xkb_compatibility {};\n")

	// 2d. Symbols
	b.WriteString("xkb_symbols {\n")
	for kc := minK; kc <= maxK; kc++ {
		offset := (kc - minK) * symsPerKey

		// Find last non-zero sym
		length := symsPerKey
		for length > 0 && syms[offset+length-1] == 0 {
			length--
		}
		if length == 0 {
			continue
		}

		b.WriteString(fmt.Sprintf("  key <I%d> {\n", kc))

		type groupSymbols struct {
			syms     [4]uint32
			hasAltGr bool
		}
		var groups []groupSymbols
		
		if symsPerKey >= 4 {
			numGroups := 2
			if symsPerKey > 8 {
				numGroups = 2 + (symsPerKey-8)/4
			}
			if numGroups > 5 {
				numGroups = 5
			}

			// Adaptive geometry detection to handle per-keycode layout compression
			hasLegacyAltGr := true
			if length >= 8 {
				sym0 := uint32(syms[offset])
				sym4 := uint32(syms[offset+4])
				if sym4 != 0 && sym4 != sym0 && isBaseLayoutLetter(sym4) && isBaseLayoutLetter(sym0) {
					hasLegacyAltGr = false
				}
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
						altIdx = 0 // No AltGr on compacted G1/G2
					} else {
						base = 4 + (g-2)*4
						altIdx = base + 2
					}
				}

				if base >= length || (syms[offset+base] == 0 && (base+1 >= length || syms[offset+base+1] == 0)) {
					continue
				}

				var gs groupSymbols
				gs.syms[0] = uint32(syms[offset+base])
				if base+1 < length {
					gs.syms[1] = uint32(syms[offset+base+1])
				}

				if altIdx < length && syms[offset+altIdx] != 0 {
					gs.syms[2] = uint32(syms[offset+altIdx])
					gs.hasAltGr = true
					if altIdx+1 < length {
						gs.syms[3] = uint32(syms[offset+altIdx+1])
					}
				} else if length == 4 && g == 0 {
					// Fallback for interleaved 4-symbol single layout
					isG2 := isNonLatinLetterKeysym(uint32(syms[offset+2])) || isNonLatinLetterKeysym(uint32(syms[offset+3]))
					if !isG2 && syms[offset+2] != 0 {
						gs.syms[2] = uint32(syms[offset+2])
						gs.hasAltGr = true
						if offset+3 < length {
							gs.syms[3] = uint32(syms[offset+3])
						}
					}
				}

				// Skip empty padded groups
				if gs.syms[0] == 0 && gs.syms[1] == 0 {
					continue
				}
				groups = append(groups, gs)
			}
		} else {
			var gs groupSymbols
			if length > 0 { gs.syms[0] = uint32(syms[offset+0]) }
			if length > 1 { gs.syms[1] = uint32(syms[offset+1]) }
			groups = append(groups, gs)
		}

		for g, gs := range groups {
			typ := "DYN_TWO_LEVEL"
			if gs.hasAltGr {
				if useShiftLock || isLetterKeysym(gs.syms[0]) || isLetterKeysym(gs.syms[1]) {
					typ = "DYN_FOUR_LEVEL_ALPHA"
				} else {
					typ = "DYN_FOUR_LEVEL"
				}
			} else if isLetterKeysym(gs.syms[0]) || isLetterKeysym(gs.syms[1]) {
				typ = "DYN_ALPHABETIC"
			} else if (xkb.KeysymIsKeypad(xkb.Keysym(gs.syms[0])) || xkb.KeysymIsKeypad(xkb.Keysym(gs.syms[1]))) && gs.syms[1] != 0 && gs.syms[0] != gs.syms[1] {
				typ = "DYN_KEYPAD"
			} else if gs.syms[1] == 0 || gs.syms[0] == gs.syms[1] {
				typ = "DYN_ONE_LEVEL"
			}

			b.WriteString(fmt.Sprintf("    type[Group%d] = \"%s\",\n", g+1, typ))
			if typ == "DYN_FOUR_LEVEL" || typ == "DYN_FOUR_LEVEL_ALPHA" {
				b.WriteString(fmt.Sprintf("    symbols[Group%d] = [ %s, %s, %s, %s ]", g+1, symToStr(gs.syms[0]), symToStr(gs.syms[1]), symToStr(gs.syms[2]), symToStr(gs.syms[3])))
			} else {
				b.WriteString(fmt.Sprintf("    symbols[Group%d] = [ %s, %s ]", g+1, symToStr(gs.syms[0]), symToStr(gs.syms[1])))
			}
			if g < len(groups)-1 {
				b.WriteString(",\n")
			} else {
				b.WriteString("\n")
			}
		}
		b.WriteString("  };\n")
	}

	// 2e. Modifier Map
	if modReply, err := xproto.GetModifierMapping(t.conn).Reply(); err == nil && modReply != nil {
		kpm := int(modReply.KeycodesPerModifier)
		modNames := []string{"Shift", "Lock", "Control", "Mod1", "Mod2", "Mod3", "Mod4", "Mod5"}
		for modIndex := 0; modIndex < 8; modIndex++ {
			var keys []string
			for i := 0; i < kpm; i++ {
				kc := int(modReply.Keycodes[modIndex*kpm+i])
				if kc >= minK && kc <= maxK {
					keys = append(keys, fmt.Sprintf("<I%d>", kc))
				}
			}
			if len(keys) > 0 {
				b.WriteString(fmt.Sprintf("  modifier_map %s { %s };\n", modNames[modIndex], strings.Join(keys, ", ")))
			}
		}
	}
	b.WriteString("};\n") // end xkb_symbols
	b.WriteString("};\n") // end xkb_keymap

	// 3. Compile the generated keymap
	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromString([]byte(b.String()), xkb.KeymapFormatTextV1)
	if err != nil {
		return fmt.Errorf("xkb-go compilation failed: %v", err)
	}

	t.xkbState = keymap.NewState()
	return nil
}

func (t *dynamicXkbTranslator) TranslateX11(detail uint8, state uint16, isDown bool) winkeys.InputEvent {
	// Periodic/forced reload to bypass macOS XQuartz MappingNotify bugs
	if t.conn != nil {
		now := time.Now()
		if t.lastReload.IsZero() || now.Sub(t.lastReload) > 1 * time.Second {
			t.lastReload = now
			t.reloadKeymap()
		}
	}

	if t.conn != nil && t.xkbOpcode != 0 {
		buf := make([]byte, 8)
		buf[0] = t.xkbOpcode
		buf[1] = 4            // XkbGetState
		xgb.Put16(buf[2:], 2) // Length
		xgb.Put16(buf[4:], 0x0100) // XkbUseCoreKbd

		cookie := t.conn.NewCookie(true, true)
		t.conn.NewRequest(buf, cookie)
		if reply, err := cookie.Reply(); err == nil && len(reply) >= 18 {
			t.xkbState.UpdateMask(
				xkb.ModMask(reply[9]),
				xkb.ModMask(reply[10]),
				xkb.ModMask(reply[11]),
				xkb.Group(xgb.Get16(reply[14:])),
				xkb.Group(xgb.Get16(reply[16:])),
				xkb.Group(reply[13]),
			)
		}
	} else {
		// Fallback mask update using raw event state
		mods := xkb.ModMask(state & 0xFF)
		group := uint32((state >> 13) & 3)
		t.xkbState.UpdateMask(mods, 0, 0, 0, 0, xkb.Group(group))
	}

	kc := xkb.Keycode(detail)
	sym := t.xkbState.KeyGetOneSym(kc)
	char := t.xkbState.KeyGetUTF32(kc)
	vk := keysymToVK(uint32(sym))

	if vk == 0 {
		vk = keycodeToVKMap[detail]
	}

	return winkeys.InputEvent{
		Type:            winkeys.KeyEventType,
		VirtualKeyCode:  vk,
		Char:            char,
		KeyDown:         isDown,
		ControlKeyState: translateModifiers(state),
		InputSource:     "dynamicxkb",
		RepeatCount:     1,
	}
}

func (t *dynamicXkbTranslator) TranslateWayland(keycode uint32, isDown bool) winkeys.InputEvent {
	return winkeys.InputEvent{}
}

func (t *dynamicXkbTranslator) UpdateWaylandModifiers(modsDepressed, modsLatched, modsLocked, group uint32) {}
func (t *dynamicXkbTranslator) Close() {}

func symToStr(sym uint32) string {
	if sym == 0 {
		return "NoSymbol"
	}
	if sym >= 0x01000000 {
		return fmt.Sprintf("0x%X", sym)
	}
	name := xkb.KeysymGetName(xkb.Keysym(sym))
	if name == "" || containsNonAlphanumeric(name) {
		return fmt.Sprintf("0x%X", sym)
	}
	return name
}

func containsNonAlphanumeric(s string) bool {
	for _, r := range s {
		if r == '_' {
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return true
		}
	}
	return false
}


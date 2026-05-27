package keytrans

import (
	"unicode"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/unxed/winkeys"
	"github.com/unxed/xkb-go"
)

// coreX11Translator uses Xlib-style heuristics without CGO.
type coreX11Translator struct {
	conn           *xgb.Conn
	minKeycode     int
	maxKeycode     int
	symsPerKey     int
	syms           []xproto.Keysym
	numLockMask    uint16
	modeSwitchMask uint16
	altGrMask      uint16
	xkbOpcode      byte
}

func newCoreX11Translator(info OSInfo) Translator {
	conn, ok := info.XgbConn.(*xgb.Conn)
	if !ok || conn == nil {
		return nil
	}

	setup := xproto.Setup(conn)
	min := setup.MinKeycode
	max := setup.MaxKeycode
	count := byte(max - min + 1)

	reply, err := xproto.GetKeyboardMapping(conn, min, count).Reply()
	if err != nil {
		return nil
	}

	// Request XKEYBOARD extension to get the state dynamically (matching purex11_host.go)
	var xkbOpcode byte
	extCookie := xproto.QueryExtension(conn, uint16(len("XKEYBOARD")), "XKEYBOARD")
	if extReply, err := extCookie.Reply(); err == nil && extReply.Present {
		xkbOpcode = extReply.MajorOpcode
		// Init extension on server
		buf := make([]byte, 8)
		buf[0] = xkbOpcode
		xgb.Put16(buf[2:], 2) // length
		xgb.Put16(buf[4:], 1) // major
		cookie := conn.NewCookie(true, true)
		conn.NewRequest(buf, cookie)
		_, _ = cookie.Reply()
	}

	t := &coreX11Translator{
		conn:       conn,
		minKeycode: int(min),
		maxKeycode: int(max),
		symsPerKey: int(reply.KeysymsPerKeycode),
		syms:       reply.Keysyms,
		xkbOpcode:  xkbOpcode,
	}

	// ModMap Discovery
	if modReply, err := xproto.GetModifierMapping(conn).Reply(); err == nil && modReply != nil {
		kpm := int(modReply.KeycodesPerModifier)
		for modIndex := 0; modIndex < 8; modIndex++ {
			mask := uint16(1 << modIndex)
			for i := 0; i < kpm; i++ {
				kc := int(modReply.Keycodes[modIndex*kpm+i])
				if kc >= t.minKeycode && kc <= t.maxKeycode {
					offset := (kc - t.minKeycode) * t.symsPerKey
					if offset < len(t.syms) {
						sym := uint32(t.syms[offset])
						switch sym {
						case uint32(xkb.KeyNumLock):
							t.numLockMask |= mask
						case uint32(xkb.KeyModeSwitch):
							t.modeSwitchMask |= mask
						case uint32(xkb.KeyISOLevel3Shift), uint32(xkb.KeyAltR):
							t.altGrMask |= mask
						}
					}
				}
			}
		}
	}

	if t.numLockMask == 0 { t.numLockMask = 1 << 4 }
	if t.altGrMask == 0 { t.altGrMask = 1 << 7 }

	return t
}

func (t *coreX11Translator) Name() string {
	return "corex11"
}

func (t *coreX11Translator) TranslateX11(detail uint8, state uint16, isDown bool) winkeys.InputEvent {
	kc := int(detail)

	// Query active XKB group index dynamically from X server (matching purex11_host.go)
	group := 0
	if t.conn != nil && t.xkbOpcode != 0 {
		buf := make([]byte, 8)
		buf[0] = t.xkbOpcode
		buf[1] = 4            // XkbGetState
		xgb.Put16(buf[2:], 2) // Length
		xgb.Put16(buf[4:], 0x0100) // XkbUseCoreKbd

		cookie := t.conn.NewCookie(true, true)
		t.conn.NewRequest(buf, cookie)
		if reply, err := cookie.Reply(); err == nil && len(reply) >= 18 {
			group = int(int16(xgb.Get16(reply[14:]))) + int(reply[13]) // baseGroup + lockedGroup
			if group < 0 {
				group = 0
			}
			if group > 3 {
				group = group % 4
			}
		}
	}

	sym := t.lookup(kc, state, group)
	char := xkb.KeysymToUTF32(xkb.Keysym(sym))
	vk := keysymToVK(sym)

	// Positional VK fallback for alternate layouts
	isAlternateLayout := group > 0 || (state&t.modeSwitchMask) != 0
	if vk == 0 && isAlternateLayout {
		baseSym := t.lookup(kc, state&^t.modeSwitchMask, 0)
		vk = keysymToVK(baseSym)
	}

	return winkeys.InputEvent{
		Type:            winkeys.KeyEventType,
		VirtualKeyCode:  vk,
		Char:            char,
		KeyDown:         isDown,
		ControlKeyState: translateModifiers(state),
		InputSource:     "corex11",
		RepeatCount:     1,
	}
}

func (t *coreX11Translator) TranslateWayland(keycode uint32, isDown bool) winkeys.InputEvent {
	return winkeys.InputEvent{} // Not supported by this backend
}

func (t *coreX11Translator) UpdateWaylandModifiers(modsDepressed, modsLatched, modsLocked, group uint32) {}
func (t *coreX11Translator) Close() {}

func (t *coreX11Translator) lookup(kc int, state uint16, group int) uint32 {
	if kc < t.minKeycode || kc > t.maxKeycode {
		return 0
	}
	offset := (kc - t.minKeycode) * t.symsPerKey
	if offset+t.symsPerKey > len(t.syms) {
		return 0
	}
	syms := t.syms[offset : offset+t.symsPerKey]

	length := len(syms)
	for length > 0 && syms[length-1] == 0 {
		length--
	}
	if length == 0 {
		return 0
	}

	shift := (state & 1) != 0 // ShiftMask = 1
	capsLock := (state & 2) != 0 // LockMask = 2
	numLock := (state & t.numLockMask) != 0
	modeSwitch := (state & t.modeSwitchMask) != 0
	altGr := (state & t.altGrMask) != 0

	effectiveGroup := group
	if modeSwitch && effectiveGroup == 0 {
		effectiveGroup = 1
	}

	// Base groups are always pairs: G1=0,1; G2=2,3; G3=4,5; G4=6,7
	baseIdx := effectiveGroup * 2
	if baseIdx >= length {
		baseIdx = 0 // Fallback if requested group is missing
	}

	idx := baseIdx

	if altGr {
		// Standard Xorg flattening places AltGr (Level 3/4) at offset +4
		if idx+4 < length && syms[idx+4] != 0 {
			idx += 4
		} else if length == 4 && idx == 0 {
			// Ambiguity resolution for 4-element arrays:
			// [G1, G1_Shift, G2, G2_Shift] vs [G1, G1_Shift, G1_AltGr, G1_ShiftAltGr]

			// If index 2 or 3 is a non-Latin letter, they are almost certainly Group 2 Base/Shift (e.g. Cyrillic)
			isGroup2 := isNonLatinLetterKeysym(uint32(syms[2])) || isNonLatinLetterKeysym(uint32(syms[3]))

			if !isGroup2 && syms[2] != 0 {
				idx += 2
			}
		}
	}

	// Safety fallback if calculated index is empty or out of bounds
	if idx >= length || syms[idx] == 0 {
		idx = baseIdx
		if idx >= length || syms[idx] == 0 {
			idx = 0
		}
	}

	if idx >= length || syms[idx] == 0 {
		return 0 // Absolute fallback, nothing found
	}

	baseSym := uint32(syms[idx])
	shiftedSym := baseSym
	if idx+1 < length && syms[idx+1] != 0 {
		shiftedSym = uint32(syms[idx+1])
	}

	if xkb.KeysymIsKeypad(xkb.Keysym(baseSym)) || xkb.KeysymIsKeypad(xkb.Keysym(shiftedSym)) {
		if numLock {
			shift = !shift
		}
		if shift {
			return shiftedSym
		}
		return baseSym
	}

	resSym := baseSym
	if shift {
		resSym = shiftedSym
	}

	if capsLock {
		if isLetterKeysym(baseSym) || isLetterKeysym(shiftedSym) {
			if shift {
				resSym = baseSym
			} else {
				resSym = shiftedSym
			}
		}
	}

	if shiftedSym == baseSym && (shift || capsLock) {
		r := xkb.KeysymToUTF32(xkb.Keysym(resSym))
		if r != 0 && unicode.IsLower(r) {
			if synSym := xkb.UTF32ToKeysym(unicode.ToUpper(r)); synSym != xkb.KeyNoSymbol {
				return uint32(synSym)
			}
		}
	}

	return resSym
}

func isLetterKeysym(sym uint32) bool {
	// Standard Latin A-Z, a-z
	if (sym >= 0x41 && sym <= 0x5a) || (sym >= 0x61 && sym <= 0x7a) {
		return true
	}
	// Latin-1 Supplement accented letters (excluding division and multiplication signs)
	if sym >= 0xc0 && sym <= 0xff && sym != 0xd7 && sym != 0xf7 {
		return true
	}
	// Cyrillic keysym range
	if sym >= 0x06a0 && sym <= 0x06ff {
		return true
	}
	// Greek keysym range
	if sym >= 0x07a1 && sym <= 0x07f9 {
		return true
	}
	// Arabic keysym range
	if sym >= 0x05c1 && sym <= 0x05f2 {
		return true
	}
	// Armenian keysym range
	if sym >= 0x10000531 && sym <= 0x10000587 {
		return true
	}
	// Georgian keysym range
	if sym >= 0x10010d0 && sym <= 0x10010f6 {
		return true
	}
	// Hangul keysym range
	if sym >= 0x0ea1 && sym <= 0x0eff {
		return true
	}
	// Unicode keysyms (0x01000000 + codepoint)
	if sym >= 0x01000100 && sym <= 0x0100ffff {
		r := rune(sym - 0x01000000)
		return unicode.IsLetter(r)
	}
	return false
}

func isNonLatinLetterKeysym(sym uint32) bool {
	// Cyrillic keysym range
	if sym >= 0x06a0 && sym <= 0x06ff {
		return true
	}
	// Greek keysym range
	if sym >= 0x07a1 && sym <= 0x07f9 {
		return true
	}
	// Hebrew keysym range
	if sym >= 0x0ce0 && sym <= 0x0cfa {
		return true
	}
	// Arabic keysym range
	if sym >= 0x05c1 && sym <= 0x05f2 {
		return true
	}
	// Thai keysym range
	if sym >= 0x0da1 && sym <= 0x0df9 {
		return true
	}
	// Armenian keysym range
	if sym >= 0x10000531 && sym <= 0x10000587 {
		return true
	}
	// Georgian keysym range
	if sym >= 0x10010d0 && sym <= 0x10010f6 {
		return true
	}
	// Hangul keysym range
	if sym >= 0x0ea1 && sym <= 0x0eff {
		return true
	}
	// Unicode keysyms (0x01000000 + codepoint)
	if sym >= 0x01000100 && sym <= 0x0100ffff {
		r := rune(sym - 0x01000000)
		if unicode.IsLetter(r) {
			// Exclude Latin-1, Latin Extended-A, Latin Extended-B
			if r < 0x0250 {
				return false
			}
			return true
		}
	}
	return false
}

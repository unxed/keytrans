package keytrans

import (
	"unicode"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
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

	t := &coreX11Translator{
		conn:       conn,
		minKeycode: int(min),
		maxKeycode: int(max),
		symsPerKey: int(reply.KeysymsPerKeycode),
		syms:       reply.Keysyms,
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

func (t *coreX11Translator) TranslateX11(detail uint8, state uint16, isDown bool) KeyEvent {
	kc := int(detail)

	sym := t.lookup(kc, state, 0)
	char := xkb.KeysymToUTF32(xkb.Keysym(sym))
	vk := keysymToVK(sym)

	// Positional VK fallback for alternate layouts
	isAlternateLayout := (state & t.modeSwitchMask) != 0
	if vk == 0 && isAlternateLayout {
		baseSym := t.lookup(kc, state & ^t.modeSwitchMask, 0)
		vk = keysymToVK(baseSym)
	}

	return KeyEvent{
		VirtualKeyCode:  vk,
		Char:            char,
		ControlKeyState: translateModifiers(state),
	}
}

func (t *coreX11Translator) TranslateWayland(keycode uint32, isDown bool) KeyEvent {
	return KeyEvent{} // Not supported by this backend
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
	if modeSwitch {
		effectiveGroup++
	}

	groupWidth := 2
	if t.symsPerKey > 4 && t.symsPerKey%4 == 0 {
		groupWidth = 4
	}

	idx := effectiveGroup * groupWidth
	if idx >= length {
		idx = 0
	}

	if altGr {
		if groupWidth == 4 {
			if idx+2 < length && syms[idx+2] != 0 {
				idx += 2
			}
		} else {
			for _, o := range []int{2, 3, 4} {
				if idx+o < length && syms[idx+o] != 0 {
					idx += o
					break
				}
			}
		}
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
		rBase := xkb.KeysymToUTF32(xkb.Keysym(baseSym))
		rShifted := xkb.KeysymToUTF32(xkb.Keysym(shiftedSym))

		if (rBase != 0 && unicode.IsLetter(rBase)) || (rShifted != 0 && unicode.IsLetter(rShifted)) {
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
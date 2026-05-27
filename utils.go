package keytrans

import (
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/unxed/winkeys"
	"github.com/unxed/xkb-go"
)

// translateModifiers maps an X11 state mask to winkeys ControlKeyState.
func translateModifiers(state uint16) winkeys.ControlKeyState {
	var mods winkeys.ControlKeyState
	if state&1 != 0 {
		mods |= winkeys.ShiftPressed
	}
	if state&4 != 0 {
		mods |= winkeys.LeftCtrlPressed
	}
	if state&8 != 0 {
		mods |= winkeys.LeftAltPressed
	}
	if state&2 != 0 {
		mods |= winkeys.CapsLockOn
	}
	if state&16 != 0 {
		mods |= winkeys.NumLockOn
	}
	return mods
}

var usKeymapState *xkb.State

func init() {
	usKeymapState = xkb.TestKeymap().NewState()
}

func getLayoutIndependentVK(detail uint8) uint16 {
	// 1. Try standard evdev keycode mapping via xkb-go's TestKeymap
	if usKeymapState != nil {
		sym := usKeymapState.KeyGetOneSym(xkb.Keycode(detail))
		if vk := keysymToVK(uint32(sym)); vk != 0 {
			return vk
		}
	}
	// 2. Try macOS native keycode mapping (fallback for XQuartz with native keycodes)
	if sym, ok := macosNativeKeycodeToKeysym[detail]; ok {
		return keysymToVK(sym)
	}
	return 0
}

var macosNativeKeycodeToKeysym = map[uint8]uint32{
	8:  0x61, // A
	9:  0x73, // S
	10: 0x64, // D
	11: 0x66, // F
	12: 0x68, // H
	13: 0x67, // G
	14: 0x7a, // Z
	15: 0x78, // X
	16: 0x63, // C
	17: 0x76, // V
	19: 0x62, // B
	20: 0x71, // Q
	21: 0x77, // W
	22: 0x65, // E
	23: 0x72, // R
	24: 0x79, // Y
	25: 0x74, // T
	26: 0x31, // 1
	27: 0x32, // 2
	28: 0x33, // 3
	29: 0x34, // 4
	30: 0x36, // 6
	31: 0x35, // 5
	32: 0x3d, // =
	33: 0x39, // 9
	34: 0x37, // 7
	35: 0x2d, // -
	36: 0x38, // 8
	37: 0x30, // 0
	38: 0x5d, // ]
	39: 0x6f, // O
	40: 0x75, // U
	41: 0x5b, // [
	42: 0x69, // I
	43: 0x70, // P
	45: 0x6c, // L
	46: 0x6a, // J
	47: 0x27, // '
	48: 0x6b, // K
	49: 0x3b, // ;
	50: 0x5c, // \
	51: 0x2c, // ,
	52: 0x2f, // /
	53: 0x6e, // N
	54: 0x6d, // M
	55: 0x2e, // .
	58: 0x60, // `
}
var evdevToVK = [256]uint16{
	9:  winkeys.VK_ESCAPE,
	10: winkeys.VK_1, 11: winkeys.VK_2, 12: winkeys.VK_3, 13: winkeys.VK_4,
	14: winkeys.VK_5, 15: winkeys.VK_6, 16: winkeys.VK_7, 17: winkeys.VK_8,
	18: winkeys.VK_9, 19: winkeys.VK_0, 20: winkeys.VK_OEM_MINUS, 21: winkeys.VK_OEM_PLUS,
	22: winkeys.VK_BACK, 23: winkeys.VK_TAB,
	24: winkeys.VK_Q, 25: winkeys.VK_W, 26: winkeys.VK_E, 27: winkeys.VK_R,
	28: winkeys.VK_T, 29: winkeys.VK_Y, 30: winkeys.VK_U, 31: winkeys.VK_I,
	32: winkeys.VK_O, 33: winkeys.VK_P, 34: winkeys.VK_OEM_4, 35: winkeys.VK_OEM_6,
	36: winkeys.VK_RETURN, 37: winkeys.VK_LCONTROL,
	38: winkeys.VK_A, 39: winkeys.VK_S, 40: winkeys.VK_D, 41: winkeys.VK_F,
	42: winkeys.VK_G, 43: winkeys.VK_H, 44: winkeys.VK_J, 45: winkeys.VK_K,
	46: winkeys.VK_L, 47: winkeys.VK_OEM_1, 48: winkeys.VK_OEM_7, 49: winkeys.VK_OEM_3,
	50: winkeys.VK_LSHIFT, 51: winkeys.VK_OEM_5,
	52: winkeys.VK_Z, 53: winkeys.VK_X, 54: winkeys.VK_C, 55: winkeys.VK_V,
	56: winkeys.VK_B, 57: winkeys.VK_N, 58: winkeys.VK_M, 59: winkeys.VK_OEM_COMMA,
	60: winkeys.VK_OEM_PERIOD, 61: winkeys.VK_OEM_2, 62: winkeys.VK_RSHIFT,
	64: winkeys.VK_LMENU, 65: winkeys.VK_SPACE, 108: winkeys.VK_RMENU,
	133: winkeys.VK_LWIN, 134: winkeys.VK_RWIN, 135: winkeys.VK_APPS,
}

var macToVK = [256]uint16{
	8: winkeys.VK_A, 9: winkeys.VK_S, 10: winkeys.VK_D, 11: winkeys.VK_F,
	12: winkeys.VK_H, 13: winkeys.VK_G, 14: winkeys.VK_Z, 15: winkeys.VK_X,
	16: winkeys.VK_C, 17: winkeys.VK_V, 19: winkeys.VK_B, 20: winkeys.VK_Q,
	21: winkeys.VK_W, 22: winkeys.VK_E, 23: winkeys.VK_R, 24: winkeys.VK_Y,
	25: winkeys.VK_T, 26: winkeys.VK_1, 27: winkeys.VK_2, 28: winkeys.VK_3,
	29: winkeys.VK_4, 30: winkeys.VK_6, 31: winkeys.VK_5, 32: winkeys.VK_OEM_PLUS,
	33: winkeys.VK_9, 34: winkeys.VK_7, 35: winkeys.VK_OEM_MINUS, 36: winkeys.VK_8,
	37: winkeys.VK_0, 38: winkeys.VK_OEM_6, 39: winkeys.VK_O, 40: winkeys.VK_U,
	41: winkeys.VK_OEM_4, 42: winkeys.VK_I, 43: winkeys.VK_P, 44: winkeys.VK_RETURN,
	45: winkeys.VK_L, 46: winkeys.VK_J, 47: winkeys.VK_OEM_7, 48: winkeys.VK_K,
	49: winkeys.VK_OEM_1, 50: winkeys.VK_OEM_5, 51: winkeys.VK_OEM_COMMA, 52: winkeys.VK_OEM_2,
	53: winkeys.VK_N, 54: winkeys.VK_M, 55: winkeys.VK_OEM_PERIOD, 56: winkeys.VK_TAB,
	57: winkeys.VK_SPACE, 58: winkeys.VK_OEM_3, 59: winkeys.VK_BACK, 61: winkeys.VK_ESCAPE,
}

var (
	keycodeToVKMap [256]uint16
	schemeOnce     sync.Once
)

func initKeycodeScheme(conn *xgb.Conn) {
	schemeOnce.Do(func() {
		copy(keycodeToVKMap[:], evdevToVK[:]) // Default to evdev
		if conn == nil {
			return
		}
		setup := xproto.Setup(conn)
		if setup == nil {
			return
		}
		minK := setup.MinKeycode
		maxK := setup.MaxKeycode
		count := byte(maxK - minK + 1)
		reply, err := xproto.GetKeyboardMapping(conn, minK, count).Reply()
		if err != nil {
			return
		}
		symsPerKey := int(reply.KeysymsPerKeycode)
		for kc := int(minK); kc <= int(maxK); kc++ {
			if kc >= 256 {
				break
			}
			offset := (kc - int(minK)) * symsPerKey
			for i := 0; i < symsPerKey; i++ {
				if offset+i < len(reply.Keysyms) && reply.Keysyms[offset+i] == 0xff0d { // Keysym Return
					if kc == 44 { // macOS XQuartz mapped Return to 44
						copy(keycodeToVKMap[:], macToVK[:])
						return
					} else if kc == 36 { // Standard Linux evdev Return is 36
						return
					}
				}
			}
		}
	})
}
// keysymToVK maps a keysym to a VirtualKeyCode.
func keysymToVK(keysym uint32) uint16 {
	// 1. Direct mapping
	if vk, ok := keysymToVKMap[keysym]; ok {
		return vk
	}
	// 2. Digits
	if keysym >= 0x0030 && keysym <= 0x0039 {
		return uint16(keysym)
	}
	// 3. Letters (A-Z)
	if keysym >= 0x0061 && keysym <= 0x007a {
		return uint16(keysym - 0x20)
	}
	if keysym >= 0x0041 && keysym <= 0x005a {
		return uint16(keysym)
	}
	return 0
}

var keysymToVKMap = map[uint32]uint16{
	0xff08: winkeys.VK_BACK,
	0xff09: winkeys.VK_TAB,
	0xff0d: winkeys.VK_RETURN,
	0xff1b: winkeys.VK_ESCAPE,
	0xff50: winkeys.VK_HOME,
	0xff51: winkeys.VK_LEFT,
	0xff52: winkeys.VK_UP,
	0xff53: winkeys.VK_RIGHT,
	0xff54: winkeys.VK_DOWN,
	0xff55: winkeys.VK_PRIOR,
	0xff56: winkeys.VK_NEXT,
	0xff57: winkeys.VK_END,
	0xff63: winkeys.VK_INSERT,
	0xffff: winkeys.VK_DELETE,
	0xffbe: winkeys.VK_F1,
	0xffbf: winkeys.VK_F2,
	0xffc0: winkeys.VK_F3,
	0xffc1: winkeys.VK_F4,
	0xffc2: winkeys.VK_F5,
	0xffc3: winkeys.VK_F6,
	0xffc4: winkeys.VK_F7,
	0xffc5: winkeys.VK_F8,
	0xffc6: winkeys.VK_F9,
	0xffc7: winkeys.VK_F10,
	0xffc8: winkeys.VK_F11,
	0xffc9: winkeys.VK_F12,
	0xffeb: winkeys.VK_LWIN,
	0xffec: winkeys.VK_RWIN,
	0xff67: winkeys.VK_APPS,
	0xffe1: winkeys.VK_LSHIFT,
	0xffe2: winkeys.VK_RSHIFT,
	0xffe3: winkeys.VK_LCONTROL,
	0xffe4: winkeys.VK_RCONTROL,
	0xffe5: winkeys.VK_CAPITAL,
	0xffe9: winkeys.VK_LMENU,
	0xffea: winkeys.VK_RMENU,
	0xff7f: winkeys.VK_NUMLOCK,
	0xff14: winkeys.VK_SCROLL,
	0x0020: winkeys.VK_SPACE,
	0x002d: winkeys.VK_OEM_MINUS,
	0x005f: winkeys.VK_OEM_MINUS,
	0x003d: winkeys.VK_OEM_PLUS,
	0x002b: winkeys.VK_OEM_PLUS,
	0x005b: winkeys.VK_OEM_4,
	0x007b: winkeys.VK_OEM_4,
	0x005d: winkeys.VK_OEM_6,
	0x007d: winkeys.VK_OEM_6,
	0x003b: winkeys.VK_OEM_1,
	0x003a: winkeys.VK_OEM_1,
	0x0027: winkeys.VK_OEM_7,
	0x0022: winkeys.VK_OEM_7,
	0x002c: winkeys.VK_OEM_COMMA,
	0x003c: winkeys.VK_OEM_COMMA,
	0x002e: winkeys.VK_OEM_PERIOD,
	0x003e: winkeys.VK_OEM_PERIOD,
	0x002f: winkeys.VK_OEM_2,
	0x003f: winkeys.VK_OEM_2,
	0x005c: winkeys.VK_OEM_5,
	0x007c: winkeys.VK_OEM_5,
	0x0060: winkeys.VK_OEM_3,
	0x007e: winkeys.VK_OEM_3,
	0xff8d: winkeys.VK_RETURN,
	0xffaa: winkeys.VK_MULTIPLY,
	0xffab: winkeys.VK_ADD,
	0xffad: winkeys.VK_SUBTRACT,
	0xffae: winkeys.VK_DECIMAL,
	0xffaf: winkeys.VK_DIVIDE,
	0xffb0: winkeys.VK_NUMPAD0,
	0xffb1: winkeys.VK_NUMPAD1,
	0xffb2: winkeys.VK_NUMPAD2,
	0xffb3: winkeys.VK_NUMPAD3,
	0xffb4: winkeys.VK_NUMPAD4,
	0xffb5: winkeys.VK_NUMPAD5,
	0xffb6: winkeys.VK_NUMPAD6,
	0xffb7: winkeys.VK_NUMPAD7,
	0xffb8: winkeys.VK_NUMPAD8,
	0xffb9: winkeys.VK_NUMPAD9,
	0xff95: winkeys.VK_HOME,
	0xff96: winkeys.VK_LEFT,
	0xff97: winkeys.VK_UP,
	0xff98: winkeys.VK_RIGHT,
	0xff99: winkeys.VK_DOWN,
	0xff9a: winkeys.VK_PRIOR,
	0xff9b: winkeys.VK_NEXT,
	0xff9c: winkeys.VK_END,
	0xff9d: winkeys.VK_CLEAR,
	0xff9e: winkeys.VK_INSERT,
	0xff9f: winkeys.VK_DELETE,
}
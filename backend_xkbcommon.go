//go:build !noffi && (linux || darwin || freebsd)

package keytrans

import (
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/unxed/winkeys"
)

type xkbcommonTranslator struct {
	lib       uintptr
	context   uintptr
	keymap    uintptr
	state     uintptr
	conn      *xgb.Conn
	xkbOpcode byte

	// Symbols
	fnContextNew          uintptr
	fnContextUnref        uintptr
	fnKeymapNewFromNames  uintptr
	fnKeymapUnref         uintptr
	fnStateNew            uintptr
	fnStateUnref          uintptr
	fnStateKeyGetUtf8     uintptr
	fnStateUpdateMask     uintptr
	fnStateKeyGetOneSym   uintptr
}

func newXkbcommonTranslator(info OSInfo) Translator {
	conn, ok := info.XgbConn.(*xgb.Conn)
	if !ok || conn == nil {
		return nil
	}

	libNames := []string{
		"libxkbcommon.so.0",
		"libxkbcommon.so",
		"libxkbcommon.0.dylib",
		"/usr/local/lib/libxkbcommon.so.0",
		"/usr/local/lib/libxkbcommon.so",
	}
	var lib uintptr
	var err error
	for _, name := range libNames {
		lib, err = purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil
	}

	t := &xkbcommonTranslator{lib: lib}
	if err := t.resolveSymbols(); err != nil {
		purego.Dlclose(lib)
		return nil
	}

	// Request XKEYBOARD extension to get the state dynamically
	extCookie := xproto.QueryExtension(conn, uint16(len("XKEYBOARD")), "XKEYBOARD")
	extReply, err := extCookie.Reply()
	if err == nil && extReply.Present {
		t.xkbOpcode = extReply.MajorOpcode
		// Init extension on server
		buf := make([]byte, 8)
		buf[0] = t.xkbOpcode
		xgb.Put16(buf[2:], 2) // length
		xgb.Put16(buf[4:], 1) // major
		cookie := conn.NewCookie(true, true)
		conn.NewRequest(buf, cookie)
		_, _ = cookie.Reply()
		t.conn = conn
	}

	// 1. Create context
	ctx, _, _ := purego.SyscallN(t.fnContextNew, 0)
	if ctx == 0 {
		purego.Dlclose(lib)
		return nil
	}
	t.context = ctx

	// 2. Fetch RMLVO configuration from X server
	rules, model, layout, variant, options := getXKBRulesNames(conn)

	// Prepare C strings
	var rulesPtr, modelPtr, layoutPtr, variantPtr, optionsPtr uintptr
	if rules != "" {
		rulesC := append([]byte(rules), 0)
		rulesPtr = uintptr(unsafe.Pointer(&rulesC[0]))
	}
	if model != "" {
		modelC := append([]byte(model), 0)
		modelPtr = uintptr(unsafe.Pointer(&modelC[0]))
	}
	if layout != "" {
		layoutC := append([]byte(layout), 0)
		layoutPtr = uintptr(unsafe.Pointer(&layoutC[0]))
	}
	if variant != "" {
		variantC := append([]byte(variant), 0)
		variantPtr = uintptr(unsafe.Pointer(&variantC[0]))
	}
	if options != "" {
		optionsC := append([]byte(options), 0)
		optionsPtr = uintptr(unsafe.Pointer(&optionsC[0]))
	}

	names := [5]uintptr{rulesPtr, modelPtr, layoutPtr, variantPtr, optionsPtr}
	namesPtr := uintptr(unsafe.Pointer(&names[0]))

	// 3. Create keymap from names
	km, _, _ := purego.SyscallN(t.fnKeymapNewFromNames, t.context, namesPtr, 0)
	if km == 0 {
		purego.SyscallN(t.fnContextUnref, t.context)
		purego.Dlclose(lib)
		return nil
	}
	t.keymap = km

	// 4. Create state
	state, _, _ := purego.SyscallN(t.fnStateNew, t.keymap)
	if state == 0 {
		purego.SyscallN(t.fnKeymapUnref, t.keymap)
		purego.SyscallN(t.fnContextUnref, t.context)
		purego.Dlclose(lib)
		return nil
	}
	t.state = state

	return t
}

func (t *xkbcommonTranslator) Name() string {
	return "libxkbcommon"
}

func (t *xkbcommonTranslator) TranslateX11(detail uint8, state uint16, isDown bool) winkeys.InputEvent {
	// Sync state with X server (only if connection is available)
	if t.conn != nil {
		buf := make([]byte, 8)
		buf[0] = t.xkbOpcode
		buf[1] = 4            // XkbGetState
		xgb.Put16(buf[2:], 2) // Length
		xgb.Put16(buf[4:], 0x0100) // XkbUseCoreKbd

		cookie := t.conn.NewCookie(true, true)
		t.conn.NewRequest(buf, cookie)
		if reply, err := cookie.Reply(); err == nil && len(reply) >= 18 {
			depressed := uint32(reply[9])
			latched := uint32(reply[10])
			locked := uint32(reply[11])
			lockedGroup := uint32(reply[13])
			baseGroup := uint32(xgb.Get16(reply[14:]))
			latchedGroup := uint32(xgb.Get16(reply[16:]))

			purego.SyscallN(t.fnStateUpdateMask, t.state,
				uintptr(depressed), uintptr(latched), uintptr(locked),
				uintptr(baseGroup), uintptr(latchedGroup), uintptr(lockedGroup),
			)
		}
	}

	xkbKey := uint32(detail)

	// Fetch keysym and character
	var sym uint32
	if t.fnStateKeyGetOneSym != 0 {
		res, _, _ := purego.SyscallN(t.fnStateKeyGetOneSym, t.state, uintptr(xkbKey))
		sym = uint32(res)
	}

	char := rune(0)
	buf := make([]byte, 8)
	bufPtr := uintptr(unsafe.Pointer(&buf[0]))
	res, _, _ := purego.SyscallN(t.fnStateKeyGetUtf8, t.state, uintptr(xkbKey), bufPtr, uintptr(len(buf)))
	if res > 0 {
		for _, r := range string(buf[:res]) {
			char = r
			break
		}
	}

	vk := keysymToVK(sym)
	return winkeys.InputEvent{
		Type:            winkeys.KeyEventType,
		VirtualKeyCode:  vk,
		Char:            char,
		KeyDown:         isDown,
		ControlKeyState: translateModifiers(state),
		InputSource:     "libxkbcommon",
		RepeatCount:     1,
	}
}

func (t *xkbcommonTranslator) TranslateWayland(keycode uint32, isDown bool) winkeys.InputEvent {
	// Offset applied for wayland (evdev -> xkb)
	return t.TranslateX11(uint8(keycode+8), 0, isDown)
}

func (t *xkbcommonTranslator) UpdateWaylandModifiers(modsDepressed, modsLatched, modsLocked, group uint32) {
	purego.SyscallN(t.fnStateUpdateMask, t.state,
		uintptr(modsDepressed), uintptr(modsLatched), uintptr(modsLocked),
		0, 0, uintptr(group),
	)
}

func (t *xkbcommonTranslator) Close() {
	if t.state != 0 {
		purego.SyscallN(t.fnStateUnref, t.state)
	}
	if t.keymap != 0 {
		purego.SyscallN(t.fnKeymapUnref, t.keymap)
	}
	if t.context != 0 {
		purego.SyscallN(t.fnContextUnref, t.context)
	}
	if t.lib != 0 {
		purego.Dlclose(t.lib)
	}
}

func (t *xkbcommonTranslator) resolveSymbols() error {
	var err error
	resolve := func(name string) uintptr {
		sym, serr := purego.Dlsym(t.lib, name)
		if serr != nil {
			err = serr
		}
		return sym
	}

	t.fnContextNew = resolve("xkb_context_new")
	t.fnContextUnref = resolve("xkb_context_unref")
	t.fnKeymapNewFromNames = resolve("xkb_keymap_new_from_names")
	t.fnKeymapUnref = resolve("xkb_keymap_unref")
	t.fnStateNew = resolve("xkb_state_new")
	t.fnStateUnref = resolve("xkb_state_unref")
	t.fnStateKeyGetUtf8 = resolve("xkb_state_key_get_utf8")
	t.fnStateUpdateMask = resolve("xkb_state_update_mask")
	t.fnStateKeyGetOneSym = resolve("xkb_state_key_get_one_sym")

	return err
}

func getXKBRulesNames(conn *xgb.Conn) (string, string, string, string, string) {
	setup := xproto.Setup(conn)
	root := setup.DefaultScreen(conn).Root

	atomCookie := xproto.InternAtom(conn, true, uint16(len("_XKB_RULES_NAMES")), "_XKB_RULES_NAMES")
	atomReply, err := atomCookie.Reply()
	if err != nil || atomReply.Atom == 0 {
		return "", "", "", "", ""
	}

	propCookie := xproto.GetProperty(conn, false, root, atomReply.Atom, xproto.GetPropertyTypeAny, 0, 256)
	propReply, err := propCookie.Reply()
	if err != nil || len(propReply.Value) == 0 {
		return "", "", "", "", ""
	}

	var parts []string
	start := 0
	for i, b := range propReply.Value {
		if b == 0 {
			parts = append(parts, string(propReply.Value[start:i]))
			start = i + 1
			if len(parts) >= 5 {
				break
			}
		}
	}

	var rules, model, layout, variant, options string
	if len(parts) >= 1 { rules = parts[0] }
	if len(parts) >= 2 { model = parts[1] }
	if len(parts) >= 3 { layout = parts[2] }
	if len(parts) >= 4 { variant = parts[3] }
	if len(parts) >= 5 { options = parts[4] }

	return rules, model, layout, variant, options
}
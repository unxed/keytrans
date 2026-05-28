package keytrans

import (
	"context"
	"log/slog"
	"runtime"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/unxed/winkeys"
	"github.com/unxed/xkb-go"
)

type pureXKBTranslator struct {
	conn      *xgb.Conn
	xkbOpcode byte
	xkbState  *xkb.State
}

func newPureXKBTranslator(info OSInfo) Translator {
	// On macOS (Darwin) under XQuartz, compiled XKB state machines (like purexkb)
	// cannot track layout switching because XQuartz rewrites the core keymap dynamically
	// without updating the XKB rule names property. Thus, we must fail early on macOS
	// to allow the factory to fall back to XIM or corex11 which handle keymap rewriting.
	if runtime.GOOS == "darwin" {
		return nil
	}
	conn, ok := info.XgbConn.(*xgb.Conn)
	if !ok || conn == nil {
		return nil
	}
	initKeycodeScheme(conn)

	// Request XKEYBOARD extension to get the state dynamically
	extCookie := xproto.QueryExtension(conn, uint16(len("XKEYBOARD")), "XKEYBOARD")
	extReply, err := extCookie.Reply()
	if err != nil || !extReply.Present {
		return nil
	}
	xkbOpcode := extReply.MajorOpcode

	// Init extension on server
	buf := make([]byte, 8)
	buf[0] = xkbOpcode
	xgb.Put16(buf[2:], 2) // length
	xgb.Put16(buf[4:], 1) // major
	cookie := conn.NewCookie(true, true)
	conn.NewRequest(buf, cookie)
	_, err = cookie.Reply()
	if err != nil {
		return nil
	}

	// Fetch RMLVO configuration from X server dynamically
	rules, model, layout, variant, options := getXKBRulesNames(conn)
	if layout == "" {
		layout = "us" // Safe fallback
	}

	// Compile the keymap natively in Go memory using our updated include paths!
	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromNames(&xkb.RuleNames{
		Rules:   rules,
		Model:   model,
		Layout:  layout,
		Variant: variant,
		Options: options,
	})
	if err != nil {
		slog.Error("purexkb: NewKeymapFromNames failed to compile",
			"err", err,
			"rules", rules,
			"model", model,
			"layout", layout,
			"variant", variant,
			"options", options)
		return nil
	}

	return &pureXKBTranslator{
		conn:      conn,
		xkbOpcode: xkbOpcode,
		xkbState:  keymap.NewState(),
	}
}

func (t *pureXKBTranslator) Name() string {
	return "purexkb"
}

func (t *pureXKBTranslator) translateKeysym(detail uint8, isDown bool) winkeys.InputEvent {
	kc := xkb.Keycode(detail)
	sym := t.xkbState.KeyGetOneSym(kc)
	char := t.xkbState.KeyGetUTF32(kc)
	vk := keysymToVK(uint32(sym))

	if vk == 0 {
		bm, lam, lom := t.xkbState.BaseMods(), t.xkbState.LatchedMods(), t.xkbState.LockedMods()
		bg, lag, log := t.xkbState.BaseGroup(), t.xkbState.LatchedGroup(), t.xkbState.LockedGroup()

		t.xkbState.UpdateMask(0, 0, 0, 0, 0, 0)
		vkSym := t.xkbState.KeyGetOneSym(kc)
		vk = keysymToVK(uint32(vkSym))

		t.xkbState.UpdateMask(bm, lam, lom, bg, lag, log)
	}

	if vk == 0 {
		vk = keycodeToVKMap[detail]
	}

	return winkeys.InputEvent{
		Type:           winkeys.KeyEventType,
		VirtualKeyCode: vk,
		Char:           char,
		KeyDown:        isDown,
		RepeatCount:    1,
	}
}

func (t *pureXKBTranslator) TranslateX11(detail uint8, state uint16, isDown bool) winkeys.InputEvent {
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
		// Fallback for tests or headless environments: update mask using event state
		mods := xkb.ModMask(state & 0xFF)
		group := uint32((state >> 13) & 3)
		t.xkbState.UpdateMask(mods, 0, 0, 0, 0, xkb.Group(group))
	}

	event := t.translateKeysym(detail, isDown)
	event.ControlKeyState = translateModifiers(state)
	event.InputSource = "purexkb"
	return event
}

func (t *pureXKBTranslator) TranslateWayland(keycode uint32, isDown bool) winkeys.InputEvent {
	// Wayland does not pack modifier state into the keypress event;
	// it relies on the state previously set by UpdateWaylandModifiers.
	event := t.translateKeysym(uint8(keycode+8), isDown)
	event.InputSource = "purexkb"
	return event
}

func (t *pureXKBTranslator) UpdateWaylandModifiers(modsDepressed, modsLatched, modsLocked, group uint32) {
	t.xkbState.UpdateMask(xkb.ModMask(modsDepressed), xkb.ModMask(modsLatched), xkb.ModMask(modsLocked), 0, 0, xkb.Group(group))
}

func (t *pureXKBTranslator) Close() {}

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
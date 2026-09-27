package keytrans

import (
	"context"
	"log/slog"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/unxed/winkeys"
	"github.com/unxed/xkb-go"
	xkbx11 "github.com/unxed/xkb-go/x11"
)

// xkbgoX11Translator resolves the XKB keymap by speaking the XKB
// (XKEYBOARD) wire protocol directly to the X server via
// github.com/unxed/xkb-go/x11 (GetMap/GetNames/GetControls), the same
// requests libxkbcommon's xkb_x11_keymap_new_from_device issues. Unlike
// purexkb (which only guesses the layout from the _XKB_RULES_NAMES RMLVO
// property and recompiles it from xkeyboard-config), this backend reads the
// keymap the server actually resolved for the requested device, without
// CGO, FFI, or xkbcomp. It shares the caller-supplied *xgb.Conn with every
// other backend in this package; it never opens a second X11 connection.
type xkbgoX11Translator struct {
	conn      *xgb.Conn
	xkbOpcode byte
	xkbState  *xkb.State
}

func newXkbgoX11Translator(info OSInfo) Translator {
	conn, ok := info.XgbConn.(*xgb.Conn)
	if !ok || conn == nil {
		return nil
	}
	if isXWayland(conn) {
		return nil
	}
	initKeycodeScheme(conn)

	// Resolve the XKEYBOARD major opcode ourselves (in addition to the
	// negotiation NewKeymapFromX11Device performs internally) so that
	// TranslateX11 can issue its own XkbGetState requests later, exactly
	// like the purexkb and xkbcomp backends do.
	extCookie := xproto.QueryExtension(conn, uint16(len("XKEYBOARD")), "XKEYBOARD")
	extReply, err := extCookie.Reply()
	if err != nil || !extReply.Present {
		return nil
	}
	xkbOpcode := extReply.MajorOpcode

	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbx11.NewKeymapFromX11Device(xkbCtx, conn, xkbx11.UseCoreKbd)
	if err != nil {
		slog.Debug("xkbgo-x11: NewKeymapFromX11Device failed", "err", err)
		return nil
	}

	return &xkbgoX11Translator{
		conn:      conn,
		xkbOpcode: xkbOpcode,
		xkbState:  keymap.NewState(),
	}
}

func (t *xkbgoX11Translator) Name() string {
	return "xkbgo-x11"
}

func (t *xkbgoX11Translator) translateKeysym(detail uint8, isDown bool) winkeys.InputEvent {
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
		Type:            winkeys.KeyEventType,
		VirtualKeyCode:  vk,
		Char:            char,
		KeyDown:         isDown,
		ControlKeyState: enhancedKeyForKeysym(uint32(sym)),
		RepeatCount:     1,
	}
}

func (t *xkbgoX11Translator) TranslateX11(detail uint8, state uint16, isDown bool) winkeys.InputEvent {
	// Sync state with X server (only if connection is available)
	if t.conn != nil {
		buf := make([]byte, 8)
		buf[0] = t.xkbOpcode
		buf[1] = 4                 // XkbGetState
		xgb.Put16(buf[2:], 2)      // Length
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
	event.ControlKeyState |= translateModifiers(state)
	event.InputSource = "xkbgo-x11"
	return event
}

func (t *xkbgoX11Translator) TranslateWayland(keycode uint32, isDown bool) winkeys.InputEvent {
	// Wayland does not pack modifier state into the keypress event;
	// it relies on the state previously set by UpdateWaylandModifiers.
	event := t.translateKeysym(uint8(keycode+8), isDown)
	event.InputSource = "xkbgo-x11"
	return event
}

func (t *xkbgoX11Translator) UpdateWaylandModifiers(modsDepressed, modsLatched, modsLocked, group uint32) {
	t.xkbState.UpdateMask(xkb.ModMask(modsDepressed), xkb.ModMask(modsLatched), xkb.ModMask(modsLocked), 0, 0, xkb.Group(group))
}

func (t *xkbgoX11Translator) Close() {}

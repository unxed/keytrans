package keytrans

import (
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"strings"
    "os"
    "runtime"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/unxed/winkeys"
	"github.com/unxed/xkb-go"
)

var (
	keyDeclRegexp  = regexp.MustCompile(`(?i)key\s+<[^>]+>`)
	emptyKeyRegexp = regexp.MustCompile(`(?i)key\s+<\s*>`)
)

type xkbcompTranslator struct {
	conn      *xgb.Conn
	xkbOpcode byte
	xkbState  *xkb.State
}

func newXkbcompTranslator(info OSInfo) Translator {
	// Disable on macOS to prevent static layout lock issues with XQuartz
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

	// Try to dump map via xkbcomp
	path, err := exec.LookPath("xkbcomp")
	if err != nil {
		if runtime.GOOS == "windows" {
			commonPaths := []string{
				`C:\Program Files\VcXsrv\xkbcomp.exe`,
				`C:\Program Files (x86)\VcXsrv\xkbcomp.exe`,
				`C:\Program Files\Xming\xkbcomp.exe`,
				`C:\Program Files (x86)\Xming\xkbcomp.exe`,
				`C:\cygwin64\bin\xkbcomp.exe`,
				`C:\cygwin\bin\xkbcomp.exe`,
				`C:\msys64\usr\bin\xkbcomp.exe`,
				`C:\msys64\mingw64\bin\xkbcomp.exe`,
				`C:\msys64\mingw32\bin\xkbcomp.exe`,
			}
			for _, p := range commonPaths {
				if _, serr := os.Stat(p); serr == nil {
					path = p
					err = nil
					break
				}
			}
		}
	}

	if err != nil || path == "" {
		return nil
	}

	display := info.DisplayString
	if display == "" {
		display = ":0"
	}

	cmd := exec.Command(path, display, "-")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil || out.Len() == 0 {
		return nil
	}

	// Validate output
	if !isXkbcompOutputValid(out.Bytes()) {
		return nil
	}

	// Save the raw dumped keymap to a file for precise diagnostics
	// _ = os.WriteFile("keytrans-xkbcomp.dump", out.Bytes(), 0644)

	// Parse with xkb-go
	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromString(out.Bytes(), xkb.KeymapFormatTextV1)
	if err != nil {
		return nil
	}

	return &xkbcompTranslator{
		conn:      conn,
		xkbOpcode: xkbOpcode,
		xkbState:  keymap.NewState(),
	}
}

func (t *xkbcompTranslator) Name() string {
	return "xkbcomp"
}

func (t *xkbcompTranslator) TranslateX11(detail uint8, state uint16, isDown bool) winkeys.InputEvent {
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
	}

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
		ControlKeyState: translateModifiers(state),
		InputSource:     "xkbcomp",
		RepeatCount:     1,
	}
}

func (t *xkbcompTranslator) TranslateWayland(keycode uint32, isDown bool) winkeys.InputEvent {
	return t.TranslateX11(uint8(keycode+8), 0, isDown)
}

func (t *xkbcompTranslator) UpdateWaylandModifiers(modsDepressed, modsLatched, modsLocked, group uint32) {
	t.xkbState.UpdateMask(xkb.ModMask(modsDepressed), xkb.ModMask(modsLatched), xkb.ModMask(modsLocked), 0, 0, xkb.Group(group))
}

func (t *xkbcompTranslator) Close() {}

func isXkbcompOutputValid(output []byte) bool {
	if len(output) == 0 {
		return false
	}
	str := string(output)
	if !strings.Contains(str, "xkb_symbols") || !strings.Contains(str, "xkb_keycodes") {
		return false
	}
	if emptyKeyRegexp.MatchString(str) {
		return false
	}
	if !strings.Contains(str, "<ESC>") || !strings.Contains(str, "<RTRN>") {
		return false
	}
	return len(keyDeclRegexp.FindAllStringIndex(str, -1)) >= 10
}
//go:build !noffi && (linux || darwin)

package keytrans

import (
	"log/slog"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/unxed/winkeys"
)

type ximStyles struct {
	Count uint16
	_     [6]byte
	Style uintptr
}

type x11ximTranslator struct {
	lib                  uintptr
	display              uintptr
	im                   uintptr
	ic                   uintptr
	xutf8LookupStringPtr uintptr
	conn                 *xgb.Conn
	xkbOpcode            byte

	// Symbols
	fnInitThreads        uintptr
	fnOpenDisplay        uintptr
	fnCloseDisplay       uintptr
	fnOpenIM             uintptr
	fnCloseIM            uintptr
	fnCreateIC           uintptr
	fnDestroyIC          uintptr
	fnSetLocaleModifiers uintptr
	fnPending            uintptr
	fnNextEvent          uintptr
	xCreateIC            func(_ purego.Variadic, im uintptr, args ...any) uintptr
	xGetIMValues         func(_ purego.Variadic, im uintptr, args ...any) uintptr
}

type xKeyEvent struct {
	Type         int32
	_            [4]byte
	Serial       uint64
	SendEvent    int32
	_            [4]byte
	Display      uintptr
	Window       uint64
	Root         uint64
	Subwindow    uint64
	Time         uint64
	X, Y         int32
	XRoot, YRoot int32
	State        uint32
	Keycode      uint32
	SameScreen   int32
}

func newX11XIMTranslator(info OSInfo) Translator {
	// XIM strictly requires a valid Window ID to create the input context.
	// Falling back early prevents X-server BadWindow crashes.
	if info.WindowID == 0 {
		slog.Warn("keytrans: XIM initialization aborted - WindowID is 0")
		return nil
	}

	conn, ok := info.XgbConn.(*xgb.Conn)
	var xkbOpcode byte
	if ok && conn != nil {
		initKeycodeScheme(conn)
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
	}

	// Try multiple possible library names (matching vtui/x11_host.go)
	libNames := []string{
		"libX11.so.6",
		"libX11.so",
		"libX11.6.dylib",
		"/usr/lib/x86_64-linux-gnu/libX11.so.6",
		"/usr/local/lib/libX11.so",
		"/opt/X11/lib/libX11.6.dylib",
	}

	var lib uintptr
	var err error
	for _, name := range libNames {
		lib, err = purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			slog.Debug("keytrans: loaded libX11", "path", name)
			break
		}
	}

	if err != nil {
		slog.Warn("keytrans: failed to load libX11", "err", err)
		return nil
	}

	t := &x11ximTranslator{
		lib:       lib,
		conn:      conn,
		xkbOpcode: xkbOpcode,
	}
	if err := t.resolveSymbols(); err != nil {
		slog.Warn("keytrans: failed to resolve XIM symbols", "err", err)
		purego.Dlclose(lib)
		return nil
	}

	// 0. Enable X11 multithreading. CRITICAL for Go goroutine stability.
	if t.fnInitThreads != 0 {
		purego.SyscallN(t.fnInitThreads)
		slog.Debug("keytrans: XInitThreads called")
	}

	// Initialize locale (CRITICAL for XIM!)
	// Try to load libc to call setlocale
	libcNames := []string{
		"", // Current process
		"libc.so.6",
		"/lib/x86_64-linux-gnu/libc.so.6",
		"/lib/aarch64-linux-gnu/libc.so.6",
		"libc.so.7",
		"libc.so",
		"libSystem.B.dylib",
	}
	var libc uintptr
	for _, name := range libcNames {
		if h, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil && h != 0 {
			libc = h
			break
		}
	}
	if libc != 0 {
		if setlocaleSym, err := purego.Dlsym(libc, "setlocale"); err == nil && setlocaleSym != 0 {
			// setlocale(LC_ALL = 6, "")
			res, _, _ := purego.SyscallN(setlocaleSym, 6, uintptr(unsafe.Pointer(&([]byte("\x00")[0]))))
			if res == 0 {
				slog.Warn("keytrans: setlocale(LC_ALL, \"\") failed")
			} else {
				slog.Debug("keytrans: setlocale initialized")
			}
		}
		purego.Dlclose(libc)
	}

	// 1. Open Display
	var displayArg uintptr
	if info.DisplayString != "" {
		displayC := append([]byte(info.DisplayString), 0)
		displayArg = uintptr(unsafe.Pointer(&displayC[0]))
	}

	dpy, _, _ := purego.SyscallN(t.fnOpenDisplay, displayArg)
	if dpy == 0 {
		slog.Warn("keytrans: XOpenDisplay returned NULL", "display", info.DisplayString)
		purego.Dlclose(lib)
		return nil
	}
	t.display = dpy

	// 2. Open IM (matching x11_host.go)
	// First try with system modifiers (connects to IBus, Fcitx, etc.)
	emptyMods := []byte("\x00")
	purego.SyscallN(t.fnSetLocaleModifiers, uintptr(unsafe.Pointer(&emptyMods[0])))
	im, _, _ := purego.SyscallN(t.fnOpenIM, t.display, 0, 0, 0)

	if im == 0 {
		// Fallback to internal IM if system IM is not available (common on Wayland/XWayland)
		slog.Debug("keytrans: system IM not available, falling back to @im=none")
		noneMods := []byte("@im=none\x00")
		purego.SyscallN(t.fnSetLocaleModifiers, uintptr(unsafe.Pointer(&noneMods[0])))
		im, _, _ = purego.SyscallN(t.fnOpenIM, t.display, 0, 0, 0)
	}

	if im == 0 {
		slog.Warn("keytrans: XOpenIM returned NULL")
		purego.SyscallN(t.fnCloseDisplay, t.display)
		purego.Dlclose(lib)
		return nil
	}
	t.im = im

	// 3. Query supported input styles for diagnostics
	var stylesPtr uintptr
	nStyles := []byte("queryInputStyle\x00")
	if t.xGetIMValues != nil {
		t.xGetIMValues(purego.Variadic{}, im, uintptr(unsafe.Pointer(&nStyles[0])), uintptr(unsafe.Pointer(&stylesPtr)), uintptr(0))
	}

	bestStyle := uintptr(0x0010 | 0x0400) // XIMPreeditNothing | XIMStatusNothing
	if stylesPtr != 0 {
		styles := (*ximStyles)(unsafe.Pointer(stylesPtr))
		if styles.Count > 0 && styles.Style != 0 {
			styleSlice := unsafe.Slice((*uintptr)(unsafe.Pointer(styles.Style)), int(styles.Count))
			hasPreferred := false
			for _, s := range styleSlice {
				if s == (0x0010 | 0x0400) {
					hasPreferred = true
				}
			}
			if !hasPreferred {
				for _, s := range styleSlice {
					if s&0x0010 != 0 {
						bestStyle = s
						break
					}
				}
			}
		}
	}

	// 4. Create IC
	nInputStyle := []byte("inputStyle\x00")
	nClientWindow := []byte("clientWindow\x00")
	nFocusWindow := []byte("focusWindow\x00")

	var ic uintptr
	if t.xCreateIC != nil {
		ic = t.xCreateIC(purego.Variadic{}, t.im,
			uintptr(unsafe.Pointer(&nInputStyle[0])), bestStyle,
			uintptr(unsafe.Pointer(&nClientWindow[0])), uintptr(info.WindowID),
			uintptr(unsafe.Pointer(&nFocusWindow[0])), uintptr(info.WindowID),
			uintptr(0),
		)
	}

	if ic == 0 {
		slog.Warn("keytrans: XCreateIC returned NULL", "windowID", info.WindowID)
		purego.SyscallN(t.fnCloseIM, t.im)
		purego.SyscallN(t.fnCloseDisplay, t.display)
		purego.Dlclose(lib)
		return nil
	}
	t.ic = ic

	return t
}

func (t *x11ximTranslator) Name() string {
	return "libX11-XIM"
}

func (t *x11ximTranslator) TranslateX11(detail uint8, state uint16, isDown bool) winkeys.InputEvent {
	// Poll Xlib connection to process MappingNotify and other internal Xlib events
	if t.fnPending != 0 && t.fnNextEvent != 0 {
		for {
			n, _, _ := purego.SyscallN(t.fnPending, t.display)
			if n == 0 {
				break
			}
			var dummyEv [192]byte
			purego.SyscallN(t.fnNextEvent, t.display, uintptr(unsafe.Pointer(&dummyEv)))
		}
	}

	// Query active XKB group index dynamically from X server
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

	// Merge group into state (bits 13-14)
	state |= uint16(group << 13)

	ev := xKeyEvent{
		Type:    2, // KeyPress
		Display: t.display,
		State:   uint32(state),
		Keycode: uint32(detail),
	}
	if !isDown {
		ev.Type = 3 // KeyRelease
	}

	buf := make([]byte, 64)
	var keysym uintptr
	var status int32

	n, _, _ := purego.SyscallN(t.xutf8LookupStringPtr, t.ic, uintptr(unsafe.Pointer(&ev)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		uintptr(unsafe.Pointer(&keysym)), uintptr(unsafe.Pointer(&status)),
	)

	char := rune(0)
	if n > 0 {
		for _, r := range string(buf[:n]) {
			char = r
			break
		}
	}

	vk := keysymToVK(uint32(keysym))
	if vk == 0 {
		vk = keycodeToVKMap[detail]
	}
	return winkeys.InputEvent{
		Type:            winkeys.KeyEventType,
		VirtualKeyCode:  vk,
		Char:            char,
		KeyDown:         isDown,
		ControlKeyState: translateModifiers(state),
		InputSource:     "libX11-XIM",
		RepeatCount:     1,
	}
}

func (t *x11ximTranslator) TranslateWayland(keycode uint32, isDown bool) winkeys.InputEvent {
	return winkeys.InputEvent{} // XIM does not support Wayland
}

func (t *x11ximTranslator) UpdateWaylandModifiers(modsDepressed, modsLatched, modsLocked, group uint32) {}
func (t *x11ximTranslator) Close() {
	if t.ic != 0 {
		purego.SyscallN(t.fnDestroyIC, t.ic)
	}
	if t.im != 0 {
		purego.SyscallN(t.fnCloseIM, t.im)
	}
	if t.display != 0 {
		purego.SyscallN(t.fnCloseDisplay, t.display)
	}
	if t.lib != 0 {
		purego.Dlclose(t.lib)
	}
}

func (t *x11ximTranslator) resolveSymbols() error {
	var err error
	resolve := func(name string) uintptr {
		sym, serr := purego.Dlsym(t.lib, name)
		if serr != nil {
			err = serr
		}
		return sym
	}

	t.fnInitThreads = resolve("XInitThreads")
	t.fnOpenDisplay = resolve("XOpenDisplay")
	t.fnCloseDisplay = resolve("XCloseDisplay")
	t.fnOpenIM = resolve("XOpenIM")
	t.fnCloseIM = resolve("XCloseIM")
	t.fnCreateIC = resolve("XCreateIC")
	if t.fnCreateIC != 0 {
		purego.RegisterFunc(&t.xCreateIC, t.fnCreateIC)
	}
	t.fnDestroyIC = resolve("XDestroyIC")
	t.fnSetLocaleModifiers = resolve("XSetLocaleModifiers")
	t.xutf8LookupStringPtr = resolve("Xutf8LookupString")
	t.fnPending = resolve("XPending")
	t.fnNextEvent = resolve("XNextEvent")

	fnGetIMValues := resolve("XGetIMValues")
	if fnGetIMValues != 0 {
		purego.RegisterFunc(&t.xGetIMValues, fnGetIMValues)
	}

	return err
}
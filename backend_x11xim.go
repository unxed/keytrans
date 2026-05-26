//go:build !noffi && (linux || freebsd || openbsd || netbsd || dragonfly || darwin)

package keytrans

import (
	"unsafe"

	"github.com/ebitengine/purego"
)

type x11ximTranslator struct {
	lib                  uintptr
	display              uintptr
	im                   uintptr
	ic                   uintptr
	xutf8LookupStringPtr uintptr

	// Symbols
	fnOpenDisplay        uintptr
	fnCloseDisplay       uintptr
	fnOpenIM             uintptr
	fnCloseIM            uintptr
	fnCreateIC           uintptr
	fnDestroyIC          uintptr
	fnSetLocaleModifiers uintptr
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
	lib, err := purego.Dlopen("libX11.so.6", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil
	}

	t := &x11ximTranslator{lib: lib}
	if err := t.resolveSymbols(); err != nil {
		purego.Dlclose(lib)
		return nil
	}

	// 1. Open Display
	dpy, _, _ := purego.SyscallN(t.fnOpenDisplay, 0)
	if dpy == 0 {
		purego.Dlclose(lib)
		return nil
	}
	t.display = dpy

	// 2. Open IM
	purego.SyscallN(t.fnSetLocaleModifiers, uintptr(unsafe.Pointer(&([]byte("@im=none\x00")[0]))))
	im, _, _ := purego.SyscallN(t.fnOpenIM, t.display, 0, 0, 0)
	if im == 0 {
		purego.SyscallN(t.fnCloseDisplay, t.display)
		purego.Dlclose(lib)
		return nil
	}
	t.im = im

	// 3. Create IC
	nInputStyle := []byte("inputStyle\x00")
	nClientWindow := []byte("clientWindow\x00")
	nFocusWindow := []byte("focusWindow\x00")
	bestStyle := uintptr(0x0010 | 0x0400) // XIMPreeditNothing | XIMStatusNothing

	ic, _, _ := purego.SyscallN(t.fnCreateIC, t.im,
		uintptr(unsafe.Pointer(&nInputStyle[0])), bestStyle,
		uintptr(unsafe.Pointer(&nClientWindow[0])), 1, // Placeholder window ID
		uintptr(unsafe.Pointer(&nFocusWindow[0])), 1,
		0,
	)
	if ic == 0 {
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

func (t *x11ximTranslator) TranslateX11(detail uint8, state uint16, isDown bool) KeyEvent {
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
	return KeyEvent{
		VirtualKeyCode:  vk,
		Char:            char,
		ControlKeyState: translateModifiers(state),
	}
}

func (t *x11ximTranslator) TranslateWayland(keycode uint32, isDown bool) KeyEvent {
	return KeyEvent{} // XIM does not support Wayland
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

	t.fnOpenDisplay = resolve("XOpenDisplay")
	t.fnCloseDisplay = resolve("XCloseDisplay")
	t.fnOpenIM = resolve("XOpenIM")
	t.fnCloseIM = resolve("XCloseIM")
	t.fnCreateIC = resolve("XCreateIC")
	t.fnDestroyIC = resolve("XDestroyIC")
	t.fnSetLocaleModifiers = resolve("XSetLocaleModifiers")
	t.xutf8LookupStringPtr = resolve("Xutf8LookupString")

	return err
}
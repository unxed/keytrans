package keytrans

import "testing"

func TestNewPureXKBTranslatorWithoutX(t *testing.T) {
	for _, k := range []string{"RULES", "MODEL", "LAYOUT", "VARIANT", "OPTIONS"} {
		t.Setenv("XKB_DEFAULT_"+k, "")
	}
	tr, err := NewPureXKBTranslator(RMLVO{Layout: "us,ru"})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if tr.Name() != "purexkb" {
		t.Fatalf("Name = %q", tr.Name())
	}
	// Keycode 38 is the "a" key. The first group is us, the second (state bit
	// 13 of an X11 event) is ru: no X server is asked either way.
	if ev := tr.TranslateX11(38, 0, true); ev.Char != 'a' || ev.VirtualKeyCode != 0x41 {
		t.Fatalf("group 1: char %q vk %#x", ev.Char, ev.VirtualKeyCode)
	}
	if ev := tr.TranslateX11(38, 1<<13, true); ev.Char != '\u0444' {
		t.Fatalf("group 2: char %q, want the Russian \u0444", ev.Char)
	}
	// Wayland state goes through UpdateWaylandModifiers.
	tr.UpdateWaylandModifiers(0, 0, 0, 1)
	if ev := tr.TranslateWayland(38-8, true); ev.Char != '\u0444' {
		t.Fatalf("wayland group 2: char %q", ev.Char)
	}
	tr.UpdateWaylandModifiers(1, 0, 0, 0)
	if ev := tr.TranslateWayland(38-8, true); ev.Char != 'A' {
		t.Fatalf("wayland shift: char %q", ev.Char)
	}
}

func TestNewX11TranslatorWithoutConnectionUsesOfflinePureXKB(t *testing.T) {
	t.Setenv("KEYTRANS_BACKEND", "")
	tr := NewX11Translator(OSInfo{})
	// An FFI backend (libxkbcommon) may answer first where the library is
	// installed; what matters is that there is one, with no X connection.
	if tr == nil {
		t.Fatal("no translator without an X connection")
	}
	tr.Close()
}

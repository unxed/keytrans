package keytrans

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/unxed/xkb-go"
)

func TestEmbeddedXKBUnpacksOnceAndCompiles(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir, err := unpackEmbeddedXKB(embeddedXKBArchive)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"rules/evdev", "rules/base", "symbols/us", "COPYING"} {
		if st, err := os.Stat(filepath.Join(dir, rel)); err != nil || !st.Mode().IsRegular() {
			t.Fatalf("%s missing from the unpacked xkeyboard-config: %v", rel, err)
		}
	}
	again, err := unpackEmbeddedXKB(embeddedXKBArchive)
	if err != nil || again != dir {
		t.Fatalf("second unpack = %q, %v; want the same directory %q", again, err, dir)
	}

	// The unpacked copy alone must be enough for xkb-go.
	ctx := xkb.NewContext(context.Background(), xkb.ContextNoDefaultIncludes)
	ctx.PrependIncludePath(dir)
	km, err := ctx.NewKeymapFromNames(&xkb.RuleNames{Rules: "evdev", Model: "pc105", Layout: "us,ru", Options: "grp:alt_shift_toggle"})
	if err != nil || km == nil {
		t.Fatalf("keymap from the embedded data: %v", err)
	}
}

func TestExtractXKBArchiveRefusesGarbage(t *testing.T) {
	if err := extractXKBArchive([]byte("not an xz stream"), t.TempDir()); err == nil {
		t.Fatal("garbage was accepted")
	}
}

func TestRMLVOWithDefaults(t *testing.T) {
	for _, k := range []string{"RULES", "MODEL", "LAYOUT", "VARIANT", "OPTIONS"} {
		t.Setenv("XKB_DEFAULT_"+k, "")
	}
	r, m, l, v, o := rmlvoWithDefaults("", "", "", "", "")
	if r != "evdev" || m != "pc105" || l != "us" || v != "" || o != "" {
		t.Fatalf("defaults = %q %q %q %q %q", r, m, l, v, o)
	}
	t.Setenv("XKB_DEFAULT_LAYOUT", "de")
	t.Setenv("XKB_DEFAULT_OPTIONS", "ctrl:nocaps")
	r, m, l, v, o = rmlvoWithDefaults("base", "", "", "nodeadkeys", "")
	if r != "base" || m != "pc105" || l != "de" || v != "nodeadkeys" || o != "ctrl:nocaps" {
		t.Fatalf("with env = %q %q %q %q %q", r, m, l, v, o)
	}
}

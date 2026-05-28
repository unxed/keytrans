package keytrans

import (
	"context"
	"testing"

	"github.com/unxed/winkeys"
	"github.com/unxed/xkb-go"
)

func TestIsXkbcompOutputValid(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{
			name: "Valid Map",
			data: "xkb_keycodes { <ESC> = 9; <RTRN> = 36; }; xkb_symbols { key <AD01> { [ q, Q ] }; key <AD02> { [ w, W ] }; key <AD03> { [ e, E ] }; key <AD04> { [ r, R ] }; key <AD05> { [ t, T ] }; key <AD06> { [ y, Y ] }; key <AD07> { [ u, U ] }; key <AD08> { [ i, I ] }; key <AD09> { [ o, O ] }; key <AD10> { [ p, P ] }; };",
			want: true,
		},
		{
			name: "Missing sections",
			data: "xkb_keycodes { <ESC> = 9; };",
			want: false,
		},
		{
			name: "macOS XQuartz empty key bug",
			data: "xkb_keycodes { <ESC> = 9; <RTRN> = 36; }; xkb_symbols { key < > { [ q, Q ] }; };",
			want: false,
		},
		{
			name: "Empty output",
			data: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isXkbcompOutputValid([]byte(tt.data))
			if got != tt.want {
				t.Errorf("isXkbcompOutputValid(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestXkbcompTranslateWayland(t *testing.T) {
	// Parse a minimal valid keymap using xkb-go
	keymapStr := `xkb_keymap {
		xkb_keycodes {
			minimum = 8;
			maximum = 255;
			<AD01> = 24;
		};
		xkb_types {
			type "ONE_LEVEL" {
				modifiers = none;
				map[none] = 1;
			};
		};
		xkb_compatibility {};
		xkb_symbols {
			key <AD01> { [ q, Q ] };
		};
	};`

	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromString([]byte(keymapStr), xkb.KeymapFormatTextV1)
	if err != nil {
		t.Skip("xkb-go failed to parse minimal keymap in test context")
		return
	}

	trans := &xkbcompTranslator{
		xkbState: keymap.NewState(),
	}

	// Translate Wayland keycode 16 (evdev 16 -> X11 24 <AD01>)
	event := trans.TranslateWayland(16, true)

	if event.Char != 'q' {
		t.Errorf("Expected translated Wayland character to be 'q', got '%c'", event.Char)
	}

	if event.VirtualKeyCode != winkeys.VK_Q {
		t.Errorf("Expected Wayland VirtualKeyCode to be VK_Q (0x%X), got 0x%X", winkeys.VK_Q, event.VirtualKeyCode)
	}
}

func TestXkbcompTranslate_PositionalVKFallback(t *testing.T) {
	// Simulate bilingual keyboard map with English and Russian layouts
	keymapStr := `xkb_keymap {
		xkb_keycodes {
			minimum = 8;
			maximum = 255;
			<AD01> = 24;
		};
		xkb_types {
			type "TWO_LEVEL" {
				modifiers = Shift;
				map[Shift] = 2;
			};
		};
		xkb_compatibility {};
		xkb_symbols {
			key <AD01> {
				symbols[Group1] = [ c, C ],
				symbols[Group2] = [ Cyrillic_es, Cyrillic_ES ]
			};
		};
	};`

	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromString([]byte(keymapStr), xkb.KeymapFormatTextV1)
	if err != nil {
		t.Skip("xkb-go failed to parse bilingual keymap in test context")
		return
	}

	trans := &xkbcompTranslator{
		xkbState: keymap.NewState(),
	}

	// Lock the active layout group to Group 1 (which represents the second group, i.e., Russian layout)
	trans.UpdateWaylandModifiers(0, 0, 0, 1)

	// Translate X11 keycode 24 (which is Cyrillic 'с' on the Russian layout)
	event := trans.TranslateX11(24, 0, true)

	// 1. The literal character must be Cyrillic 'с' (rune 0x0441)
	if event.Char != 'с' {
		t.Errorf("Expected translated character to be 'с', got '%c'", event.Char)
	}

	// 2. The Virtual Key Code must fallback to English layout equivalent: VK_C (0x43)
	if event.VirtualKeyCode != winkeys.VK_C {
		t.Errorf("Expected fallback VirtualKeyCode to be VK_C (0x43), got 0x%X", event.VirtualKeyCode)
	}
}
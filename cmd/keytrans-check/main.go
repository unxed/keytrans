package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/unxed/keytrans"
	"github.com/unxed/winkeys"
	"github.com/unxed/xkb-go"
)

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

func main() {
	// 1. Open log file
	logFile, err := os.OpenFile("keytrans-debug.log", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Warning: Failed to create log file: %v\n", err)
	} else {
		defer logFile.Close()
	}

	logWrite := func(format string, a ...interface{}) {
		msg := fmt.Sprintf(format, a...)
		fmt.Print(msg) // Output to console
		if logFile != nil {
			logFile.WriteString(msg) // Output to file
		}
	}

	logWrite("--- keytrans Diagnostic Tool ---\n")
	displayStr := os.Getenv("DISPLAY")
	logWrite("Environment DISPLAY: %q\n", displayStr)

	// 2. Connect to X11
	conn, err := xgb.NewConn()
	if err != nil {
		logWrite("CRITICAL: Failed to connect to X11 via XGB: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	logWrite("X11 Screen Dimensions: %dx%d pixels\n", screen.WidthInPixels, screen.HeightInPixels)

	// 3. Create simple window
	winID, err := xproto.NewWindowId(conn)
	if err != nil {
		logWrite("CRITICAL: Failed to allocate window ID: %v\n", err)
		os.Exit(1)
	}

	// Listen for key events and structure/delete notifications
	eventMask := uint32(
		xproto.EventMaskKeyPress |
			xproto.EventMaskKeyRelease |
			xproto.EventMaskExposure |
			xproto.EventMaskStructureNotify,
	)

	xproto.CreateWindow(conn, screen.RootDepth, winID, screen.Root,
		100, 100, 400, 300, 0,
		xproto.WindowClassInputOutput, screen.RootVisual,
		xproto.CwBackPixel|xproto.CwEventMask,
		[]uint32{screen.BlackPixel, eventMask})

	// Set window title
	title := "keytrans Diagnostic Tool"
	xproto.ChangeProperty(conn, xproto.PropModeReplace, winID, xproto.AtomWmName,
		xproto.AtomString, 8, uint32(len(title)), []byte(title))

	// Set WM_DELETE_WINDOW protocol to handle window close button
	protocolsAtom, _ := xproto.InternAtom(conn, false, 12, "WM_PROTOCOLS").Reply()
	deleteAtom, _ := xproto.InternAtom(conn, false, 16, "WM_DELETE_WINDOW").Reply()
	if protocolsAtom != nil && deleteAtom != nil {
		data := make([]byte, 4)
		xgb.Put32(data, uint32(deleteAtom.Atom))
		xproto.ChangeProperty(conn, xproto.PropModeReplace, winID, protocolsAtom.Atom, xproto.AtomAtom, 32, 1, data)
	}

	// Map window to screen
	xproto.MapWindow(conn, winID)

	// Log System XKB Rules Names
	rules, model, layout, variant, options := getXKBRulesNames(conn)
	logWrite("[System XKB Rules Names]\n")
	logWrite("  Rules:    %q\n", rules)
	logWrite("  Model:    %q\n", model)
	logWrite("  Layout:   %q\n", layout)
	logWrite("  Variant:  %q\n", variant)
	logWrite("  Options:  %q\n\n", options)

	// Log Keycodes range and SymsPerKeycode
	if reply, err := xproto.GetKeyboardMapping(conn, setup.MinKeycode, 1).Reply(); err == nil {
		logWrite("[X11 Keyboard Map Geometry]\n")
		logWrite("  Min Keycode: %d\n", setup.MinKeycode)
		logWrite("  Max Keycode: %d\n", setup.MaxKeycode)
		logWrite("  Keysyms Per Keycode: %d\n\n", reply.KeysymsPerKeycode)
	}

	// Log Modifier Mapping
	if modReply, err := xproto.GetModifierMapping(conn).Reply(); err == nil && modReply != nil {
		logWrite("[X11 Modifier Mapping]\n")
		kpm := int(modReply.KeycodesPerModifier)
		modNames := []string{"Shift", "Lock", "Control", "Mod1", "Mod2", "Mod3", "Mod4", "Mod5"}
		for modIndex := 0; modIndex < 8; modIndex++ {
			var kcList []string
			for i := 0; i < kpm; i++ {
				kc := modReply.Keycodes[modIndex*kpm+i]
				if kc != 0 {
					kcList = append(kcList, fmt.Sprintf("%d", kc))
				}
			}
			logWrite("  %s (Index %d): keycodes [%s]\n", modNames[modIndex], modIndex, strings.Join(kcList, ", "))
		}
		logWrite("\n")
	}

	// 4. Initialize keytrans Translator
	info := keytrans.OSInfo{
		DisplayString: displayStr,
		XgbConn:       conn,
		WindowID:      uint32(winID),
	}
	translator := keytrans.NewX11Translator(info)
	defer translator.Close()

	logWrite("Active Translator Backend: %s\n", translator.Name())
	logWrite("Window created successfully (ID: 0x%X). Focus it and type keys.\n\n", winID)

	// 5. Event Loop
	for {
		ev, err := conn.WaitForEvent()
		if err != nil {
			logWrite("Warning: Error waiting for event: %v\n", err)
			continue
		}
		if ev == nil {
			break
		}

		switch e := ev.(type) {
		case xproto.KeyPressEvent:
			handleKey(logWrite, conn, translator, e.Detail, e.State, true)
		case xproto.KeyReleaseEvent:
			handleKey(logWrite, conn, translator, e.Detail, e.State, false)

		case xproto.MappingNotifyEvent:
			logWrite("--- MAPPING NOTIFY (Keyboard layout/mapping changed) ---\n")
			translator.Close()
			translator = keytrans.NewX11Translator(info)
			logWrite("  Recreated translator backend: %s\n\n", translator.Name())

		case xproto.ClientMessageEvent:
			if deleteAtom != nil && e.Data.Data32[0] == uint32(deleteAtom.Atom) {
				logWrite("Window close requested. Exiting.\n")
				return
			}
		}
	}
}

func handleKey(logWrite func(string, ...interface{}), conn *xgb.Conn, trans keytrans.Translator, detail xproto.Keycode, state uint16, isDown bool) {
	direction := "KeyPress"
	if !isDown {
		direction = "KeyRelease"
	}

	// Translate event
	event := trans.TranslateX11(uint8(detail), state, isDown)

	// Query XKB extension state directly from server
	xkbGroupBase := 0
	xkbGroupLocked := 0
	xkbGroupLatched := 0
	xkbModsDepressed := 0
	xkbModsLatched := 0
	xkbModsLocked := 0
	xkbPresent := false

	extCookie := xproto.QueryExtension(conn, uint16(len("XKEYBOARD")), "XKEYBOARD")
	if extReply, err := extCookie.Reply(); err == nil && extReply.Present {
		xkbOpcode := extReply.MajorOpcode
		buf := make([]byte, 8)
		buf[0] = xkbOpcode
		buf[1] = 4                 // XkbGetState
		xgb.Put16(buf[2:], 2)      // Length
		xgb.Put16(buf[4:], 0x0100) // XkbUseCoreKbd

		cookie := conn.NewCookie(true, true)
		conn.NewRequest(buf, cookie)
		if reply, err := cookie.Reply(); err == nil && len(reply) >= 18 {
			xkbPresent = true
			xkbModsDepressed = int(reply[9])
			xkbModsLatched = int(reply[10])
			xkbModsLocked = int(reply[11])
			xkbGroupLocked = int(reply[13])
			xkbGroupBase = int(int16(xgb.Get16(reply[14:])))
			xkbGroupLatched = int(int16(xgb.Get16(reply[16:])))
		}
	}

	// Query raw keysym mapping directly from X server for this specific keycode (with full indices)
	var keysymList []string
	if reply, err := xproto.GetKeyboardMapping(conn, detail, 1).Reply(); err == nil && reply != nil {
		for i, sym := range reply.Keysyms {
			name := ""
			if sym != 0 {
				name = xkb.KeysymGetName(xkb.Keysym(sym))
			}
			if name == "" {
				name = "NoSymbol"
			}
			keysymList = append(keysymList, fmt.Sprintf("    Index %2d: 0x%04X (%s)", i, sym, name))
		}
	}

	logWrite("--- KEY EVENT (%s) %s ---\n", direction, time.Now().Format("15:04:05.000"))
	logWrite("  [X11 Raw Data]\n")
	logWrite("    Keycode: %d\n", detail)
	logWrite("    State:   0x%04X (binary: %016b)\n", state, state)
	if xkbPresent {
		logWrite("  [XKB Extension State]\n")
		logWrite("    Mods Depressed: 0x%02X\n", xkbModsDepressed)
		logWrite("    Mods Latched:   0x%02X\n", xkbModsLatched)
		logWrite("    Mods Locked:    0x%02X\n", xkbModsLocked)
		logWrite("    Group Base:     %d\n", xkbGroupBase)
		logWrite("    Group Latched:  %d\n", xkbGroupLatched)
		logWrite("    Group Locked:   %d\n", xkbGroupLocked)
	}
	if len(keysymList) > 0 {
		logWrite("  [X11 Server Keysym Map for Keycode %d (Total Keysyms: %d)]\n", detail, len(keysymList))
		logWrite("%s\n", strings.Join(keysymList, "\n"))
	}
	logWrite("  [Translated Win32 InputEvent]\n")
	logWrite("    Virtual Key Code: %s (0x%02X)\n", winkeys.VKString(event.VirtualKeyCode), event.VirtualKeyCode)
	if event.Char != 0 {
		logWrite("    Character:        '%c' (Unicode: 0x%04X)\n", event.Char, event.Char)
	} else {
		logWrite("    Character:        None (0)\n")
	}
	logWrite("    Modifiers:        %s\n", event.ControlKeyState.String())
	logWrite("    Backend Source:   %s\n", trans.Name())
	logWrite("-----------------------------------------\n\n")
}
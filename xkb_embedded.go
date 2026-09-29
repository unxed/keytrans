package keytrans

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/unxed/xkb-go"
	"github.com/unxed/xz"
)

// xkeyboard-config embedded in the binary (unxed/keytrans#1), so that a keymap
// can be compiled on a machine that has no xkeyboard-config installed and no X
// server to ask. The archive is built by scripts/build-xkb-archive.sh (see
// docs/embed-xkeyboard-config.md for the numbers and the choice of xz).
//
// xkb-go reads its data through real file system paths, so the archive is
// unpacked once into a per-user cache directory named after the archive's hash
// and handed to xkb-go with Context.PrependIncludePath. The system's own data
// is tried first (compileKeymapFromNames): it may carry the user's or the
// distribution's changes, and the embedded copy is what is left when there is
// none.

//go:embed xkbdata/xkeyboard-config.tar.xz
var embeddedXKBArchive []byte

// maxEmbeddedXKBBytes bounds the unpacked size: the real tree is about 3.6 MB,
// and an archive that claims much more is not this one.
const maxEmbeddedXKBBytes = 64 << 20

var (
	embeddedXKBOnce sync.Once
	embeddedXKBPath string
	embeddedXKBErr  error
)

// embeddedXKBDir unpacks the embedded xkeyboard-config (once per process, and
// once per archive version on disk) and returns the directory holding it.
func embeddedXKBDir() (string, error) {
	embeddedXKBOnce.Do(func() {
		embeddedXKBPath, embeddedXKBErr = unpackEmbeddedXKB(embeddedXKBArchive)
	})
	return embeddedXKBPath, embeddedXKBErr
}

func unpackEmbeddedXKB(archive []byte) (string, error) {
	sum := sha256.Sum256(archive)
	name := "xkb-" + hex.EncodeToString(sum[:8])

	base, err := os.UserCacheDir()
	if err == nil {
		base = filepath.Join(base, "keytrans")
		err = os.MkdirAll(base, 0o700)
	}
	if err != nil {
		// No cache directory (no $HOME, read-only home): unpack for this
		// process alone into a private temporary directory.
		tmp, terr := os.MkdirTemp("", "keytrans-xkb-")
		if terr != nil {
			return "", fmt.Errorf("keytrans: no place to unpack the embedded xkeyboard-config: %w", terr)
		}
		if uerr := extractXKBArchive(archive, tmp); uerr != nil {
			return "", uerr
		}
		return tmp, nil
	}

	final := filepath.Join(base, name)
	if st, err := os.Stat(filepath.Join(final, "rules", "evdev")); err == nil && st.Mode().IsRegular() {
		return final, nil
	}
	// Unpack beside the final name and rename: the directory appears whole or
	// not at all, so two processes starting together cannot see half of it.
	tmp, err := os.MkdirTemp(base, ".unpack-")
	if err != nil {
		return "", fmt.Errorf("keytrans: cannot unpack the embedded xkeyboard-config: %w", err)
	}
	if err := extractXKBArchive(archive, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.RemoveAll(tmp)
		// Lost the race to another process that finished first.
		if st, serr := os.Stat(filepath.Join(final, "rules", "evdev")); serr == nil && st.Mode().IsRegular() {
			return final, nil
		}
		return "", fmt.Errorf("keytrans: cannot unpack the embedded xkeyboard-config: %w", err)
	}
	return final, nil
}

// extractXKBArchive writes the archive's directories and regular files under
// dir. Anything else, and any name that would leave dir, is refused: the data
// is our own, but an unpacker that trusts names is a bug waiting for an input.
func extractXKBArchive(archive []byte, dir string) error {
	xr, err := xz.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("keytrans: embedded xkeyboard-config: %w", err)
	}
	tr := tar.NewReader(xr)
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("keytrans: embedded xkeyboard-config: %w", err)
		}
		clean := path.Clean(hdr.Name)
		if clean == "." {
			continue
		}
		if !fs.ValidPath(clean) {
			return fmt.Errorf("keytrans: embedded xkeyboard-config: bad name %q", hdr.Name)
		}
		target := filepath.Join(dir, filepath.FromSlash(clean))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += hdr.Size
			if hdr.Size < 0 || total > maxEmbeddedXKBBytes {
				return errors.New("keytrans: embedded xkeyboard-config is larger than expected")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, hdr.Size)); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("keytrans: embedded xkeyboard-config: unexpected entry %q", hdr.Name)
		}
	}
}

// compileKeymapFromNames compiles names with the system's xkeyboard-config
// and, when that cannot be done (no data installed, or it lacks what names
// ask for), with the embedded copy. The first error is returned when both fail.
func compileKeymapFromNames(names *xkb.RuleNames) (*xkb.Keymap, error) {
	ctx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := ctx.NewKeymapFromNames(names)
	if err == nil {
		return keymap, nil
	}
	dir, derr := embeddedXKBDir()
	if derr != nil {
		return nil, err
	}
	ctx = xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	ctx.PrependIncludePath(dir)
	keymap, eerr := ctx.NewKeymapFromNames(names)
	if eerr != nil {
		return nil, fmt.Errorf("%w (embedded xkeyboard-config: %v)", err, eerr)
	}
	return keymap, nil
}

// rmlvoWithDefaults fills the RMLVO names the caller could not get (an X server
// that publishes none, or no X server at all): first the XKB_DEFAULT_* variables
// libxkbcommon reads, then evdev / pc105 / us. Names that are already set win.
func rmlvoWithDefaults(rules, model, layout, variant, options string) (string, string, string, string, string) {
	fill := func(value, env, fallback string) string {
		if value != "" {
			return value
		}
		if v := os.Getenv(env); v != "" {
			return v
		}
		return fallback
	}
	return fill(rules, "XKB_DEFAULT_RULES", "evdev"),
		fill(model, "XKB_DEFAULT_MODEL", "pc105"),
		fill(layout, "XKB_DEFAULT_LAYOUT", "us"),
		fill(variant, "XKB_DEFAULT_VARIANT", ""),
		fill(options, "XKB_DEFAULT_OPTIONS", "")
}

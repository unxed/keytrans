//go:build noffi || (!linux && !freebsd && !openbsd && !netbsd && !dragonfly && !solaris && !illumos && !darwin)

package keytrans

func newX11XIMTranslator(info OSInfo) Translator {
	return nil
}
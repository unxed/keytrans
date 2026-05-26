//go:build noffi || (!linux && !darwin && !freebsd)

package keytrans

func newX11XIMTranslator(info OSInfo) Translator {
	return nil
}
//go:build noffi || (!linux && !darwin && !freebsd) || arm

package keytrans

func newX11XIMTranslator(info OSInfo) Translator {
	return nil
}
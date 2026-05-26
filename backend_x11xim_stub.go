//go:build noffi || (!linux && !darwin)

package keytrans

func newX11XIMTranslator(info OSInfo) Translator {
	return nil
}
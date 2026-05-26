//go:build noffi || (!linux && !darwin)

package keytrans

func newXkbcommonTranslator(info OSInfo) Translator {
	return nil
}
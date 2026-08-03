//go:build noffi || (!linux && !darwin && !freebsd) || arm

package keytrans

func newXkbcommonTranslator(info OSInfo) Translator {
	return nil
}
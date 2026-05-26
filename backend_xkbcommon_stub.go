//go:build noffi || (!linux && !darwin && !freebsd)

package keytrans

func newXkbcommonTranslator(info OSInfo) Translator {
	return nil
}
//go:build noffi

package keytrans

func newXkbcommonTranslator(info OSInfo) Translator {
	return nil
}
//go:build noffi || (!linux && !darwin && !freebsd) || (!amd64 && !arm64)

package keytrans

func newXkbcommonTranslator(info OSInfo) Translator {
	return nil
}

//go:build noffi || (!linux && !darwin && !freebsd) || (!amd64 && !arm64)

package keytrans

func newX11XIMTranslator(info OSInfo) Translator {
	return nil
}

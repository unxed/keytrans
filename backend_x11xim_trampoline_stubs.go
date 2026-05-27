//go:build !darwin || !arm64

package keytrans

var (
	trampolineXGetIMValuesAddr uintptr = 0
	trampolineXCreateICAddr    uintptr = 0
)
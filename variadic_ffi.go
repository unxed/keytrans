//go:build pureffi && !noffi && (linux || darwin || freebsd) && (amd64 || arm64)

package keytrans

// This file is only compiled with -tags pureffi. It routes C variadic calls
// through the purego.Variadic marker, which is an extension provided by
// github.com/unxed/pureffi (a drop-in replacement for ebitengine/purego
// backed by libffi). Use it together with:
//
//	replace github.com/ebitengine/purego => github.com/unxed/pureffi v0.1.12
//
// Without the tag, keytrans builds against vanilla purego and handles the
// Apple arm64 variadic ABI itself; see variadic_syscall.go.

import (
	"sync"

	"github.com/ebitengine/purego"
)

type cVariadicFn func(_ purego.Variadic, fixed uintptr, args ...any) uintptr

var (
	variadicCacheMu sync.Mutex
	variadicCache   = map[uintptr]cVariadicFn{}
)

func resolveCVariadic(fn uintptr) cVariadicFn {
	variadicCacheMu.Lock()
	defer variadicCacheMu.Unlock()
	if f, ok := variadicCache[fn]; ok {
		return f
	}
	var f cVariadicFn
	purego.RegisterFunc(&f, fn)
	variadicCache[fn] = f
	return f
}

// callCVariadic calls a C function of the form `ret fn(fixed, ...)`, passing
// varargs as the variadic tail. It returns the first return register.
//
//go:uintptrescapes
func callCVariadic(fn uintptr, fixed uintptr, varargs ...uintptr) uintptr {
	if fn == 0 {
		return 0
	}
	f := resolveCVariadic(fn)
	if f == nil {
		return 0
	}
	boxed := make([]any, len(varargs))
	for i, v := range varargs {
		boxed[i] = v
	}
	return f(purego.Variadic{}, fixed, boxed...)
}

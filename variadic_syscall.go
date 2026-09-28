//go:build !pureffi && !noffi && (linux || darwin || freebsd) && (amd64 || arm64)

package keytrans

// This file implements C variadic calls (XCreateIC, XGetIMValues, ...) using
// only the vanilla ebitengine/purego API, i.e. plain purego.SyscallN.
//
// It is the default. Build with -tags pureffi to use the richer
// purego.Variadic marker provided by github.com/unxed/pureffi instead
// (see variadic_ffi.go).
//
// ABI background
// --------------
// On SysV x86-64, AAPCS64 (Linux/FreeBSD arm64) and 32-bit x86, variadic
// arguments are passed exactly like normal arguments, so purego.SyscallN
// already produces a correct call.
//
// Apple's arm64 ABI is the exception: named parameters are passed in
// registers as usual, but *every* variadic argument must be passed on the
// stack, in 8-byte slots starting at SP at the moment of the call.
// Passing them in x1..x7 makes the callee read garbage and crash.
// See https://github.com/ebitengine/purego/issues/446
//
// purego.SyscallN on arm64 puts args 1..8 into x0..x7 and args 9..15 onto
// the stack at [SP+0], [SP+8], ... That is precisely the layout Apple
// expects for variadic arguments, so we do not need any assembly: we just
// pad the register window with dummy zeroes until it is full, and let the
// real variadic arguments spill onto the stack.
//
//	callCVariadic(fn, im, a, b, c)   ->   SyscallN(fn, im, 0,0,0,0,0,0,0, a, b, c)
//	                                              x0  x1..x7 (ignored)  stack
//
// The same padding is harmless-but-unnecessary elsewhere, so it is applied
// only on darwin/arm64.

import (
	"log/slog"
	"runtime"

	"github.com/ebitengine/purego"
)

// maxSyscallArgs mirrors purego's internal limit for SyscallN. Vanilla purego
// panics above this; pureffi allows more, but we stay within the strictest
// bound so the same code works on both.
const maxSyscallArgs = 15

// variadicRegPadFor returns the number of dummy register arguments that must
// follow the fixed parameter on the given target so that the variadic tail
// is spilled onto the stack. It is a pure function of GOOS/GOARCH so the
// arm64 Apple-Silicon case can be unit-tested on any host, including hosts
// that are neither darwin nor arm64.
func variadicRegPadFor(goos, goarch string) int {
	if goos == "darwin" && goarch == "arm64" {
		// x0 holds the single fixed parameter, x1..x7 get padded out.
		return 7
	}
	return 0
}

// variadicRegPad is variadicRegPadFor for the running target.
//
// Computed at runtime rather than via build tags so that this single file
// covers every supported target.
var variadicRegPad = variadicRegPadFor(runtime.GOOS, runtime.GOARCH)

// buildVariadicArgs assembles the argument list passed to purego.SyscallN
// for a call of the form `ret fn(fixed, ...)`: the fixed parameter, then
// regPad dummy zero registers, then the real variadic arguments. It reports
// ok=false when the resulting argument count would exceed maxSyscallArgs,
// mirroring purego's own limit instead of letting it panic.
func buildVariadicArgs(regPad int, fixed uintptr, varargs []uintptr) (args []uintptr, ok bool) {
	total := 1 + regPad + len(varargs)
	if total > maxSyscallArgs {
		return nil, false
	}

	args = make([]uintptr, 0, total)
	args = append(args, fixed)
	for i := 0; i < regPad; i++ {
		args = append(args, 0)
	}
	args = append(args, varargs...)
	return args, true
}

// callCVariadic calls a C function of the form `ret fn(fixed, ...)`, passing
// varargs as the variadic tail. It returns the first return register.
//
// The go:uintptrescapes pragma is required: callers convert Go pointers to
// uintptr at the call site, and this pragma keeps the referenced objects
// alive and unmoved for the whole duration of the call.
//
//go:uintptrescapes
func callCVariadic(fn uintptr, fixed uintptr, varargs ...uintptr) uintptr {
	if fn == 0 {
		return 0
	}

	args, ok := buildVariadicArgs(variadicRegPad, fixed, varargs)
	if !ok {
		// Only reachable on darwin/arm64 with a very long argument list.
		// Bail out instead of letting purego panic; the caller treats a
		// zero result as "backend unavailable" and falls back.
		slog.Warn("keytrans: variadic call has too many arguments for purego.SyscallN",
			"args", len(varargs), "limit", maxSyscallArgs-1-variadicRegPad)
		return 0
	}

	r1, _, _ := purego.SyscallN(fn, args...)
	return r1
}

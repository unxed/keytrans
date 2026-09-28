//go:build !pureffi && !noffi && (linux || darwin || freebsd) && (amd64 || arm64)

package keytrans

import "testing"

// TestVariadicRegPadFor covers the arm64/Apple-Silicon ABI adjustment that
// replaced the old assembly trampoline (see the package comment in
// variadic_syscall.go): every variadic argument on darwin/arm64 must be
// spilled onto the stack, which purego.SyscallN already does once the
// register window x1..x7 is padded out. This must keep working without any
// darwin or arm64 build/runtime available, which is why the padding amount
// is a pure function of (GOOS, GOARCH) instead of a runtime.GOOS/GOARCH
// check buried inside callCVariadic.
func TestVariadicRegPadFor(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         int
	}{
		{"darwin", "arm64", 7}, // the actual trampoline replacement
		{"darwin", "amd64", 0},
		{"linux", "arm64", 0}, // AAPCS64: variadics behave like normal args
		{"linux", "amd64", 0},
		{"freebsd", "arm64", 0},
		{"freebsd", "amd64", 0},
		{"windows", "arm64", 0}, // unsupported target, must default safely
	}
	for _, c := range cases {
		if got := variadicRegPadFor(c.goos, c.goarch); got != c.want {
			t.Errorf("variadicRegPadFor(%q, %q) = %d, want %d", c.goos, c.goarch, got, c.want)
		}
	}
}

// TestBuildVariadicArgs_DarwinArm64Padding checks the exact argument layout
// documented at the top of variadic_syscall.go: the fixed parameter in
// slot 0, seven padding zeroes filling the rest of the register window,
// then the real variadic arguments spilling past it (onto the stack, from
// purego.SyscallN's point of view).
func TestBuildVariadicArgs_DarwinArm64Padding(t *testing.T) {
	args, ok := buildVariadicArgs(7, 0x1000, []uintptr{0xA, 0xB, 0xC})
	if !ok {
		t.Fatal("buildVariadicArgs reported failure for a well-formed call")
	}

	want := []uintptr{0x1000, 0, 0, 0, 0, 0, 0, 0, 0xA, 0xB, 0xC}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %#x, want %#x (full: %v)", i, args[i], want[i], args)
		}
	}
}

// TestBuildVariadicArgs_NoPadding checks the non-Apple-arm64 path, where
// purego.SyscallN's normal argument placement is already ABI-correct.
func TestBuildVariadicArgs_NoPadding(t *testing.T) {
	args, ok := buildVariadicArgs(0, 0x42, []uintptr{1, 2, 3})
	if !ok {
		t.Fatal("buildVariadicArgs reported failure for a well-formed call")
	}
	want := []uintptr{0x42, 1, 2, 3}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %#x, want %#x (full: %v)", i, args[i], want[i], args)
		}
	}
}

// TestBuildVariadicArgs_TooManyArgs checks the guard that stands in for
// purego's own panic on argument lists longer than it supports; this is the
// case that shrinks the usable variadic tail on darwin/arm64, since the
// padding itself eats into the same budget.
func TestBuildVariadicArgs_TooManyArgs(t *testing.T) {
	long := make([]uintptr, maxSyscallArgs) // guaranteed too many regardless of regPad
	if _, ok := buildVariadicArgs(7, 0, long); ok {
		t.Fatal("buildVariadicArgs should reject an argument list over the limit")
	}
	if _, ok := buildVariadicArgs(0, 0, long); ok {
		t.Fatal("buildVariadicArgs should reject an argument list over the limit")
	}
}

// TestCallCVariadic_NilFn checks the "backend unavailable" fallback path:
// a zero function pointer must never reach purego.SyscallN.
func TestCallCVariadic_NilFn(t *testing.T) {
	if got := callCVariadic(0, 1, 2, 3); got != 0 {
		t.Errorf("callCVariadic(0, ...) = %d, want 0", got)
	}
}

// TestCallCVariadic_TooManyArgs checks that an oversized argument list is
// rejected before purego.SyscallN would ever be invoked. fn is a
// deliberately fake, non-zero, never-dereferenced pointer: the guard inside
// buildVariadicArgs must trigger first, so this must stay safe to run on
// any host.
func TestCallCVariadic_TooManyArgs(t *testing.T) {
	long := make([]uintptr, maxSyscallArgs)
	if got := callCVariadic(1, 0, long...); got != 0 {
		t.Errorf("callCVariadic with an oversized argument list = %d, want 0", got)
	}
}

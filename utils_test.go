package keytrans

import (
	"testing"

	"github.com/unxed/winkeys"
)

func TestGetLayoutIndependentVK(t *testing.T) {
	tests := []struct {
		name   string
		detail uint8
		want   uint16
	}{
		{name: "evdev keycode", detail: 38, want: winkeys.VK_A},
		{name: "macOS native keycode fallback", detail: 8, want: winkeys.VK_A},
		{name: "unknown keycode", detail: 255, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getLayoutIndependentVK(tt.detail); got != tt.want {
				t.Errorf("getLayoutIndependentVK(%d) = %#x, want %#x", tt.detail, got, tt.want)
			}
		})
	}
}

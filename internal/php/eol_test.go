package php

import (
	"runtime"
	"testing"
)

func TestEOL(t *testing.T) {
	if got := nativeEOL("windows"); got != "\r\n" {
		t.Errorf("PHP_EOL on Windows = %q", got)
	}

	for _, goos := range []string{"linux", "darwin", "freebsd"} {
		if got := nativeEOL(goos); got != "\n" {
			t.Errorf("PHP_EOL on %s = %q", goos, got)
		}
	}

	if EOL != nativeEOL(runtime.GOOS) {
		t.Errorf("EOL = %q on %s", EOL, runtime.GOOS)
	}
}

func TestSetEOLForTest(t *testing.T) {
	prev := EOL

	t.Run("windows", func(t *testing.T) {
		SetEOLForTest(t, "\r\n")

		if EOL != "\r\n" {
			t.Fatalf("EOL = %q", EOL)
		}

		// str_replace(PHP_EOL, "\n", ...): a lone "\n" or "\r" stays.
		if got := NormalizeEOL("a\r\nb\nc\rd\r\n"); got != "a\nb\nc\rd\n" {
			t.Errorf("NormalizeEOL = %q", got)
		}
	})

	if EOL != prev {
		t.Errorf("EOL = %q after the test, want %q", EOL, prev)
	}

	t.Run("unix", func(t *testing.T) {
		SetEOLForTest(t, "\n")

		if got := NormalizeEOL("a\r\nb\n"); got != "a\r\nb\n" {
			t.Errorf("NormalizeEOL = %q", got)
		}
	})
}

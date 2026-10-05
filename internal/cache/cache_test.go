package cache

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestTheCacheDirectory(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", "/tmp/explicit")

	if Dir() != "/tmp/explicit" {
		t.Fatalf("expected MAESTRO_CACHE_DIR to win, got %s", Dir())
	}

	if Store() != filepath.Join("/tmp/explicit", "store", "v1") {
		t.Fatalf("expected the store under the cache directory, got %s", Store())
	}

	t.Setenv("MAESTRO_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg")

	if Dir() != filepath.Join("/tmp/xdg", "maestro") {
		t.Fatalf("expected the XDG cache home to be used, got %s", Dir())
	}

	if runtime.GOOS != "linux" {
		return
	}

	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/home/someone")

	if want := filepath.Join("/home/someone", ".cache", "maestro"); Dir() != want {
		t.Fatalf("expected %s, got %s", want, Dir())
	}
}

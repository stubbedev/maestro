package util

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// Ports tests/Composer/Test/Util/IniHelperTest.php.

func setIniEnv(t *testing.T, paths []string) {
	t.Helper()
	t.Setenv("COMPOSER_ORIGINAL_INIS", strings.Join(paths, string(os.PathListSeparator)))
}

func noLoadedInis() []string {
	panic("COMPOSER_ORIGINAL_INIS is set")
}

func TestIniHelper_WithNoIni(t *testing.T) {
	paths := []string{""}
	setIniEnv(t, paths)

	if msg := IniGetMessage(noLoadedInis); !strings.Contains(msg, "does not exist") {
		t.Errorf("IniGetMessage = %q", msg)
	}

	if got := IniGetAll(noLoadedInis); !slices.Equal(got, paths) {
		t.Errorf("IniGetAll = %q", got)
	}
}

func TestIniHelper_WithLoadedIniOnly(t *testing.T) {
	setIniEnv(t, []string{"loaded.ini"})

	if msg := IniGetMessage(noLoadedInis); !strings.Contains(msg, "loaded.ini") {
		t.Errorf("IniGetMessage = %q", msg)
	}
}

func TestIniHelper_WithLoadedIniAndAdditional(t *testing.T) {
	paths := []string{"loaded.ini", "one.ini", "two.ini"}
	setIniEnv(t, paths)

	if msg := IniGetMessage(noLoadedInis); !strings.Contains(msg, "multiple ini files") {
		t.Errorf("IniGetMessage = %q", msg)
	}

	if got := IniGetAll(noLoadedInis); !slices.Equal(got, paths) {
		t.Errorf("IniGetAll = %q", got)
	}
}

func TestIniHelper_WithoutLoadedIniAndAdditional(t *testing.T) {
	paths := []string{"", "one.ini", "two.ini"}
	setIniEnv(t, paths)

	if msg := IniGetMessage(noLoadedInis); !strings.Contains(msg, "multiple ini files") {
		t.Errorf("IniGetMessage = %q", msg)
	}

	if got := IniGetAll(noLoadedInis); !slices.Equal(got, paths) {
		t.Errorf("IniGetAll = %q", got)
	}
}

func TestIniHelper_WithoutEnv(t *testing.T) {
	t.Setenv("COMPOSER_ORIGINAL_INIS", "")
	os.Unsetenv("COMPOSER_ORIGINAL_INIS")

	loaded := func() []string { return []string{"/etc/php.ini"} }
	if msg := IniGetMessage(loaded); msg != "The php.ini used by your command-line PHP is: /etc/php.ini" {
		t.Errorf("IniGetMessage = %q", msg)
	}
}

package util

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

// Ports tests/Composer/Test/Util/PlatformTest.php. The
// assertPharMetadataSafe tests are not ported: the check only applies to
// PHP < 8.0 and has no Go counterpart.

func TestPlatform_ExpandPath(t *testing.T) {
	t.Setenv("TESTENV", "/home/test")

	for _, c := range []struct{ in, want string }{
		{"%TESTENV%/myPath", "/home/test/myPath"},
		{"$TESTENV/myPath", "/home/test/myPath"},
	} {
		if got, err := ExpandPath(c.in); err != nil || got != c.want {
			t.Errorf("ExpandPath(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}

	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}

	if got, err := ExpandPath("~/test"); err != nil || got != home+"/test" {
		t.Errorf("ExpandPath(~/test) = %q, %v; want %q", got, err, home+"/test")
	}
}

func TestPlatform_IsWindows(t *testing.T) {
	if IsWindows() != (runtime.GOOS == "windows") || IsWindows() != (os.PathSeparator == '\\') {
		t.Error("IsWindows disagrees with the platform")
	}
}

func TestPlatform_GetBoolEnvReturnsDefaultWhenUnset(t *testing.T) {
	t.Setenv("COMPOSER_TEST_BOOL_ENV", "")
	os.Unsetenv("COMPOSER_TEST_BOOL_ENV")

	// PHP returns $default; set reports that it applies.
	if value, set, err := GetBoolEnv("COMPOSER_TEST_BOOL_ENV"); value || set || err != nil {
		t.Errorf("GetBoolEnv = %v, %v, %v", value, set, err)
	}
}

func TestPlatform_GetBoolEnvReturnsExpectedValue(t *testing.T) {
	for _, c := range []struct {
		value    string
		expected bool
	}{{"true", true}, {"false", false}, {"1", true}, {"0", false}, {"on", true}, {"off", false}} {
		t.Setenv("COMPOSER_TEST_BOOL_ENV", c.value)

		if value, set, err := GetBoolEnv("COMPOSER_TEST_BOOL_ENV"); value != c.expected || !set || err != nil {
			t.Errorf("GetBoolEnv(%q) = %v, %v, %v; want %v", c.value, value, set, err, c.expected)
		}
	}
}

func TestPlatform_GetBoolEnvThrowsForInvalidValue(t *testing.T) {
	for _, value := range []string{"2", "-1", "abc", " 1 "} {
		t.Setenv("COMPOSER_TEST_BOOL_ENV", value)

		_, _, err := GetBoolEnv("COMPOSER_TEST_BOOL_ENV")

		var runtimeErr *RuntimeError
		if !errors.As(err, &runtimeErr) || !strings.Contains(err.Error(), "Invalid value for COMPOSER_TEST_BOOL_ENV") {
			t.Errorf("GetBoolEnv(%q) error = %v", value, err)
		}
	}
}

// The tests below cover behaviour PlatformTest does not.

func TestPlatform_PutClearEnv(t *testing.T) {
	t.Setenv("COMPOSER_TEST_ENV", "")

	PutEnv("COMPOSER_TEST_ENV", "value")

	if v, ok := GetEnv("COMPOSER_TEST_ENV"); !ok || v != "value" {
		t.Errorf("GetEnv = %q, %v", v, ok)
	}

	ClearEnv("COMPOSER_TEST_ENV")

	if _, ok := GetEnv("COMPOSER_TEST_ENV"); ok {
		t.Error("ClearEnv left the variable set")
	}
}

func TestPlatform_GetCwdAndRealpath(t *testing.T) {
	cwd, err := GetCwd(false)
	if err != nil || cwd == "" {
		t.Fatalf("GetCwd = %q, %v", cwd, err)
	}

	if got := Realpath(""); got != cwd {
		t.Errorf("Realpath(\"\") = %q, want %q", got, cwd)
	}

	if got := Realpath("/does/not/exist"); got != "/does/not/exist" {
		t.Errorf("Realpath(missing) = %q", got)
	}

	dir := t.TempDir()
	mustMkdir(t, dir+"/real")

	if err := os.Symlink(dir+"/real", dir+"/link"); err != nil {
		t.Skip(err)
	}

	want := Realpath(dir + "/real")
	if got := Realpath(dir + "/link/../link/."); got != want {
		t.Errorf("Realpath(link) = %q, want %q", got, want)
	}
}

func TestPlatform_GetUserDirectory(t *testing.T) {
	t.Setenv("HOME", "/home/someone")

	if got, err := GetUserDirectory(); err != nil || got != "/home/someone" {
		t.Errorf("GetUserDirectory = %q, %v", got, err)
	}
}

func TestPlatform_Misc(t *testing.T) {
	if Strlen("é") != 2 {
		t.Error("Strlen counts characters")
	}

	if (GetDevNull() == "NUL") != IsWindows() {
		t.Error("GetDevNull disagrees with the platform")
	}

	t.Setenv("MSYSTEM", "mingw64")

	if !IsTty(nil) {
		t.Error("IsTty ignores MSYSTEM")
	}
}

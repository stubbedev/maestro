// Ports tests/Composer/Test/Command/ClearCacheCommandTest.php.

package command_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/util"
)

// clearCacheTester isolates the caches: Composer's test clears the real
// ones of the user running it.
func clearCacheTester(t *testing.T) *commandtest.ApplicationTester {
	t.Helper()
	home := commandtest.UniqueTmpDirectory(t)
	util.PutEnv("COMPOSER_HOME", home)
	util.PutEnv("COMPOSER_CACHE_DIR", home+"/cache")
	util.PutEnv("MAESTRO_CACHE_DIR", home+"/maestro")
	t.Cleanup(func() {
		// --no-cache triggers the env to change so make sure the env is cleaned up after these tests run
		util.ClearEnv("COMPOSER_CACHE_DIR")
		util.ClearEnv("COMPOSER_HOME")
		util.ClearEnv("MAESTRO_CACHE_DIR")
	})

	return commandtest.GetApplicationTester(t)
}

func runCommandSuccessfully(t *testing.T, appTester *commandtest.ApplicationTester, kv ...any) string {
	t.Helper()
	code, err := appTester.RunArgs(commandtest.Options{}, kv...)
	if err != nil || code != 0 {
		t.Fatalf("command failed: %d %v\n%s", code, err, appTester.Display(true))
	}

	return appTester.Display(true)
}

func TestClearCacheCommand_Success(t *testing.T) {
	output := runCommandSuccessfully(t, clearCacheTester(t), "command", "clear-cache")
	if !strings.Contains(output, "All caches cleared.") {
		t.Errorf("output %q", output)
	}
}

func TestClearCacheCommand_WithOptionGarbageCollection(t *testing.T) {
	output := runCommandSuccessfully(t, clearCacheTester(t), "command", "clear-cache", "--gc", true)
	if !strings.Contains(output, "All caches garbage-collected.") {
		t.Errorf("output %q", output)
	}
}

func TestClearCacheCommand_WithOptionNoCache(t *testing.T) {
	output := runCommandSuccessfully(t, clearCacheTester(t), "command", "clear-cache", "--no-cache", true)
	if !strings.Contains(output, "Cache is not enabled") {
		t.Errorf("output %q", output)
	}
}

// clear-cache removes maestro's decoded repository metadata with the
// repository cache, and --gc ages it with cache-ttl, without output of its
// own.
func TestClearCacheCommand_DecodedMetadata(t *testing.T) {
	for _, gc := range []bool{false, true} {
		appTester := clearCacheTester(t)
		home, _ := util.GetEnv("COMPOSER_HOME")
		if err := os.MkdirAll(home+"/cache/repo", 0o777); err != nil {
			t.Fatal(err)
		}
		slots := home + "/maestro/p2/v1"
		if err := os.MkdirAll(slots, 0o777); err != nil {
			t.Fatal(err)
		}
		past := time.Now().AddDate(-1, 0, 0)
		for _, name := range []string{"old.bin", "new.bin"} {
			if err := os.WriteFile(slots+"/"+name, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chtimes(slots+"/old.bin", past, past); err != nil {
			t.Fatal(err)
		}

		var output string
		if gc {
			output = runCommandSuccessfully(t, appTester, "command", "clear-cache", "--gc", true)
		} else {
			output = runCommandSuccessfully(t, appTester, "command", "clear-cache")
		}
		if strings.Contains(output, "p2") || strings.Contains(output, home+"/maestro") {
			t.Errorf("gc=%v: output %q", gc, output)
		}
		_, errOld := os.Stat(slots + "/old.bin")
		_, errNew := os.Stat(slots + "/new.bin")
		if !os.IsNotExist(errOld) || gc == os.IsNotExist(errNew) {
			t.Errorf("gc=%v: old %v, new %v", gc, errOld, errNew)
		}
	}
}

// A full clear-cache removes every one of maestro's own caches but the
// store (pruned, not removed) with the Composer cache directory it
// follows, without output of its own; --gc leaves the fresh ones.
func TestClearCacheCommand_OwnCaches(t *testing.T) {
	for _, gc := range []bool{false, true} {
		appTester := clearCacheTester(t)
		home, _ := util.GetEnv("COMPOSER_HOME")
		for _, dir := range []string{"/cache/repo", "/cache/files", "/cache/vcs"} {
			if err := os.MkdirAll(home+dir, 0o777); err != nil {
				t.Fatal(err)
			}
		}
		for _, o := range cache.Owned() {
			if o.Path == "store/v1" {
				continue
			}
			p := filepath.Join(home, "maestro", filepath.FromSlash(o.Path), "entry")
			if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		args := []any{"command", "clear-cache"}
		if gc {
			args = append(args, "--gc", true)
		}
		output := runCommandSuccessfully(t, appTester, args...)
		if strings.Contains(output, home+"/maestro") {
			t.Errorf("gc=%v: output %q", gc, output)
		}
		for _, o := range cache.Owned() {
			if o.Path == "store/v1" {
				continue
			}
			_, err := os.Stat(filepath.Join(home, "maestro", filepath.FromSlash(o.Path)))
			if gone := os.IsNotExist(err); gone == gc {
				t.Errorf("gc=%v: %s removed %v", gc, o.Path, gone)
			}
		}
	}
}

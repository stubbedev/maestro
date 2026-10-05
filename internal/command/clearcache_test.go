// Ports tests/Composer/Test/Command/ClearCacheCommandTest.php.

package command_test

import (
	"strings"
	"testing"

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

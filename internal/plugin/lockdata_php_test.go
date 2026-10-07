package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
)

// TestShimAPI_SetLockDataNullDevPackages: Locker::setLockData's ?array
// $devPackages reaches the lock file as PHP passed it (#67): null writes
// "packages-dev": null, [] an empty list.
func TestShimAPI_SetLockDataNullDevPackages(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	for devPackages, want := range map[string]string{"null": `"packages-dev": null,`, "[]": `"packages-dev": [],`} {
		t.Run(devPackages, func(t *testing.T) {
			lockPath := filepath.Join(t.TempDir(), "composer.lock")
			lockFile, err := json.NewFile(lockPath, nil, io.NewNullIO())
			if err != nil {
				t.Fatal(err)
			}
			l, err := locker.New(io.NewNullIO(), lockFile, &fixtureIM{}, "{}", nil)
			if err != nil {
				t.Fatal(err)
			}

			if got := evalPHP(t, rt, `return $vars['l']->setLockData([], `+devPackages+`, [], [], [], 'stable', [], false, false, []);`, php.ArrayOf("l", rt.value(l))); got != true {
				t.Fatalf("setLockData returned %v", got)
			}
			data, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "\n    "+want+"\n") {
				t.Errorf("lock file:\n%s\nwant it to hold %s", data, want)
			}
		})
	}
}

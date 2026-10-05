package plugin

import (
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// TestHandlers_CoverShim (docs/PLUGINS.md §9.1 "Handler coverage"): the
// methods the shim calls in maestro (`Rpc::call('<area>.<method>', ...)`
// in its source) are exactly the handlers maestro registers.
func TestHandlers_CoverShim(t *testing.T) {
	called := map[string]bool{}
	pattern := php.MustCompile(`{Rpc::call\('([a-z]+\.[A-Za-z0-9]+)'}`)
	err := fs.WalkDir(shimFS(), "src", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".php") {
			return err
		}
		data, err := fs.ReadFile(shimFS(), path)
		if err != nil {
			return err
		}
		matches, err := pattern.MatchAll(string(data))
		if err != nil {
			return err
		}
		for _, m := range matches {
			called[m.Get(1)] = true
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	rt := New(Options{CacheDir: t.TempDir()})
	registered := map[string]bool{}
	for name := range rt.handlers {
		registered[name] = true
	}

	for _, name := range slices.Sorted(maps.Keys(called)) {
		if !registered[name] {
			t.Errorf("the shim calls %s, which maestro does not handle", name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(registered)) {
		if !called[name] {
			t.Errorf("maestro handles %s, which the shim never calls", name)
		}
	}
}

// Differential tests against the goldens tools/oracle/pkg/comparer.php
// records by running Composer 2.10.3's Comparer on the same trees.

package comparer_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg/comparer"
)

type entry [3]string // path, kind, content

func build(t *testing.T, dir string, entries []entry) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		full := filepath.Join(dir, e[0])
		if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
			t.Fatal(err)
		}

		var err error

		switch e[1] {
		case "dir":
			err = os.MkdirAll(full, 0o777)
		case "link":
			err = os.Symlink(e[2], full)
		default:
			err = os.WriteFile(full, []byte(e[2]), 0o666)
		}

		if err != nil {
			t.Fatal(err)
		}
	}
}

// pairs flattens getChanged() into sorted "section path" lines: readdir
// order differs between file systems, so only the contents are compared.
func pairs(changed map[string][]string) []string {
	var out []string

	for section, paths := range changed {
		for _, p := range paths {
			out = append(out, section+" "+p)
		}
	}

	slices.Sort(out)

	return out
}

func TestOracle_Comparer(t *testing.T) {
	data, err := os.ReadFile("testdata/oracle/comparer.json")
	if err != nil {
		t.Fatal(err)
	}

	var golden map[string][5]json.RawMessage
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}

	for k, c := range golden {
		var source, update []entry

		_ = json.Unmarshal(c[0], &source)
		_ = json.Unmarshal(c[1], &update)

		decodeChanged := func(raw json.RawMessage) map[string][]string {
			m := map[string][]string{}
			if string(raw) != "false" {
				_ = json.Unmarshal(raw, &m)
			}

			return m
		}

		explicated, plain := decodeChanged(c[2]), decodeChanged(c[3])

		var asString string
		_ = json.Unmarshal(c[4], &asString)

		base := t.TempDir()
		build(t, filepath.Join(base, "source"), source)
		build(t, filepath.Join(base, "update"), update)

		var cmp comparer.Comparer
		cmp.SetSource(filepath.Join(base, "source"))
		cmp.SetUpdate(filepath.Join(base, "update"))

		if err := cmp.DoCompare(); err != nil {
			t.Fatal(err)
		}

		toMap := func(changed []comparer.Changed) map[string][]string {
			m := map[string][]string{}
			for _, s := range changed {
				m[s.Section] = s.Paths
			}

			return m
		}

		if got, want := pairs(toMap(cmp.GetChanged(true))), pairs(explicated); !slices.Equal(got, want) {
			t.Errorf("#%s explicated: got %q, want %q", k, got, want)
		}

		if got, want := pairs(toMap(cmp.GetChanged(false))), pairs(plain); !slices.Equal(got, want) {
			t.Errorf("#%s: got %q, want %q", k, got, want)
		}

		lines := func(s string) []string {
			l := strings.Split(s, "\r\n")
			slices.Sort(l)

			return l
		}

		if got := cmp.GetChangedAsString(true, false); !slices.Equal(lines(got), lines(asString)) {
			t.Errorf("#%s string: got %q, want %q", k, got, asString)
		}
	}
}

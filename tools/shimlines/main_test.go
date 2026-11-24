package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAnnotatedCall(t *testing.T) {
	tests := []struct {
		code string
		want call
	}{
		{`trigger_error('A is deprecated.', E_USER_DEPRECATED); `, call{"trigger_error", `'A is deprecated.'`}},
		{`@trigger_error("B", E_USER_DEPRECATED); `, call{"trigger_error", `"B"`}},
		{`trigger_error('Package::'.$source.' must be called'); `, call{"trigger_error", `'Package::'`}},
		{`trigger_error('it\'s (not) a call(', E_USER_DEPRECATED); `, call{"trigger_error", `'it\'s (not) a call('`}},
		{`$plugin->activate($this->composer, $this->io); `, call{"activate", ""}},
		{`$this->composer->getEventDispatcher()->addSubscriber($plugin); `, call{"addSubscriber", ""}},
		{`eval($code); `, call{"eval", ""}},
		{`if (foo('x')) { bar(baz('y')); } `, call{"bar", ""}},
		{`$f('x'); /* g('y') */ h('z'); `, call{"h", `'z'`}},
	}
	for _, tt := range tests {
		got, ok := annotatedCall(tt.code)
		if !ok || got != tt.want {
			t.Errorf("annotatedCall(%q) = %v, %v; want %v", tt.code, got, ok, tt.want)
		}
	}

	for _, code := range []string{`$a = $b; `, `if ($x) { `, `$f('x'); `, `// foo('x') `} {
		if got, ok := annotatedCall(code); ok {
			t.Errorf("annotatedCall(%q) = %v; want no call", code, got)
		}
	}
}

// fixture writes files (path relative to dir: content) under dir.
func fixture(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		file := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const composerFoo = `<?php
class Foo
{
    public function a()
    {
        trigger_error('a is deprecated', E_USER_DEPRECATED);
        $plugin->activate($this->composer, $this->io);
    }

    public function b()
    {
        trigger_error('b is deprecated', E_USER_DEPRECATED);
        $plugin->activate($this->composer, $this->io);
    }
}
`

func checkFixture(t *testing.T, shim map[string]string) []string {
	t.Helper()
	ref, src := t.TempDir(), t.TempDir()
	fixture(t, ref, map[string]string{
		"src/Composer/Foo.php":    composerFoo,
		"src/Composer/Plugin.php": "<?php\n\n    eval($code);\n",
	})
	fixture(t, src, shim)

	drifts, err := check(ref, src)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(drifts))
	for i, d := range drifts {
		got[i] = strings.TrimPrefix(d.String(), src+string(filepath.Separator))
	}

	return got
}

func TestCheck_Matching(t *testing.T) {
	got := checkFixture(t, map[string]string{
		"Composer/Foo.php": "<?php\n" +
			"trigger_error('a is deprecated', E_USER_DEPRECATED); // @line 6\n" +
			"$plugin->activate($this->composer, $this->io); // @line 13\n",
		"Maestro/Shim/Plugins.php": "<?php\neval($code); // @line src/Composer/Plugin.php:3\n",
	})
	if len(got) != 0 {
		t.Errorf("drifts = %q; want none", got)
	}
}

func TestCheck_Drifted(t *testing.T) {
	got := checkFixture(t, map[string]string{
		"Composer/Foo.php": "<?php\n" +
			"trigger_error('b is deprecated', E_USER_DEPRECATED); // @line 6\n" +
			"$plugin->activate($this->composer, $this->io); // @line 4\n" +
			"trigger_error('a is deprecated', E_USER_DEPRECATED); // @line 60\n",
		"Maestro/Shim/Plugins.php": "<?php\neval($code); // @line src/Composer/Plugin.php:2\n",
	})
	want := []string{
		filepath.FromSlash("Composer/Foo.php") + ":2: @line 6: src/Composer/Foo.php:6 does not call trigger_error('b is deprecated'); it is at line 12",
		filepath.FromSlash("Composer/Foo.php") + ":3: @line 4: src/Composer/Foo.php:4 does not call activate(); it is at lines 7, 13",
		filepath.FromSlash("Composer/Foo.php") + ":4: @line 60: src/Composer/Foo.php:60 does not call trigger_error('a is deprecated'); it is at line 6",
		filepath.FromSlash("Maestro/Shim/Plugins.php") + ":2: @line src/Composer/Plugin.php:2: src/Composer/Plugin.php:2 does not call eval(); it is at line 3",
	}
	if !slices.Equal(got, want) {
		t.Errorf("drifts =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestCheck_Missing(t *testing.T) {
	got := checkFixture(t, map[string]string{
		"Composer/Foo.php": "<?php\ntrigger_error('c is deprecated', E_USER_DEPRECATED); // @line 6\n",
	})
	want := []string{
		filepath.FromSlash("Composer/Foo.php") + ":2: @line 6: src/Composer/Foo.php:6 does not call trigger_error('c is deprecated'), and no line of src/Composer/Foo.php does",
	}
	if !slices.Equal(got, want) {
		t.Errorf("drifts = %q; want %q", got, want)
	}
}

func TestCheck_Errors(t *testing.T) {
	tests := map[string]struct {
		shim string
		want string
	}{
		"no Composer file": {"trigger_error('a', E_USER_DEPRECATED); // @line src/Composer/Gone.php:1\n", "Gone.php"},
		"no call":          {"$a = $b; // @line 6\n", "annotates a line that makes no call"},
		"no line":          {"eval($code); // @line 0\n", "names no line"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ref, src := t.TempDir(), t.TempDir()
			fixture(t, ref, map[string]string{"src/Composer/Foo.php": composerFoo})
			fixture(t, src, map[string]string{"Composer/Foo.php": "<?php\n" + tt.shim})
			if _, err := check(ref, src); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("check() error = %v; want one naming %q", err, tt.want)
			}
		})
	}
}

// TestCheck_Composer: every @line annotation of the shim names the line
// of Composer's source that makes its call, in the release maestro ports.
// It needs .ref/composer (`just ref`).
func TestCheck_Composer(t *testing.T) {
	const ref = "../../.ref/composer"
	if _, err := os.Stat(ref + "/src"); err != nil {
		t.Skip("no .ref/composer; run ref-sync")
	}

	drifts, err := check(ref, "../../internal/plugin/php/src")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range drifts {
		t.Error(d)
	}
}

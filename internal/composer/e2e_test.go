package composer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFactory_CreateComposerAndInstall wires the real Factory (no mocks)
// and installs a path repository package into a fresh project.
func TestFactory_CreateComposerAndInstall(t *testing.T) {
	home := tempDir(t)
	t.Setenv("COMPOSER_HOME", home)
	t.Setenv("COMPOSER_CACHE_DIR", filepath.Join(home, "cache"))
	project := tempDir(t)
	write := func(path, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(project, "lib/composer.json"), `{"name": "acme/lib", "version": "1.2.3", "autoload": {"psr-4": {"Acme\\": "src/"}}, "bin": ["bin/tool"]}`)
	write(filepath.Join(project, "lib/src/Foo.php"), "<?php namespace Acme; class Foo {}\n")
	write(filepath.Join(project, "lib/bin/tool"), "#!/usr/bin/env php\n<?php echo 'hi';\n")
	write(filepath.Join(project, "composer.json"), `{
    "repositories": [{"type": "path", "url": "lib", "options": {"symlink": false}}],
    "require": {"acme/lib": "^1.0"}
}`)
	t.Chdir(project)

	out := newBufferIO(t)
	f := &Factory{Runtime: testRuntime(t, 0)}
	c, err := f.CreateComposer(out, nil, PluginsEnabled, "", false)
	if err != nil {
		t.Fatal(err)
	}
	installer, err := CreateInstaller(out, c)
	if err != nil {
		t.Fatal(err)
	}
	installer.SetDevMode(true).SetAudit(false)
	code, err := installer.Run()
	output := bufferOutput(out)
	if err != nil || code != 0 {
		t.Fatalf("run = %d, %v\n%s", code, err, output)
	}

	for _, want := range []string{
		"<warning>No composer.lock file present. Updating dependencies to latest instead of installing from lock file. See https://getcomposer.org/install for more information.</warning>",
		"Lock file operations: 1 install, 0 updates, 0 removals",
		"  - Locking acme/lib (1.2.3)",
		"Writing lock file",
		"Installing dependencies from lock file (including require-dev)",
		"Package operations: 1 install, 0 updates, 0 removals",
		"  - Installing acme/lib (1.2.3): Mirroring from lib",
		"Generating autoload files",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}
	for _, file := range []string{"composer.lock", "vendor/autoload.php", "vendor/composer/installed.json", "vendor/composer/installed.php", "vendor/acme/lib/src/Foo.php", "vendor/bin/tool"} {
		if _, err := os.Stat(filepath.Join(project, file)); err != nil {
			t.Errorf("%s: %v", file, err)
		}
	}
}

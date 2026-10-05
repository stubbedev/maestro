package config

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/util"
)

// Ports the tests of tests/Composer/Test/FactoryTest.php covering the
// configuration functions; testDefaultValuesAreAsExpected covers
// createHttpDownloader, which internal/composer ports.

func TestFactory_GetComposerJsonPath(t *testing.T) {
	unsetenv(t, "COMPOSER")
	got, err := ComposerFile()
	if err != nil || got != "./composer.json" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestFactory_GetComposerJsonPathFailsIfDir(t *testing.T) {
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPOSER", dir)
	_, err = ComposerFile()
	var re *util.RuntimeError
	if !errors.As(err, &re) || re.Message != "The COMPOSER environment variable is set to "+dir+" which is a directory, this variable should point to a composer.json or be left unset." {
		t.Errorf("got %v", err)
	}
}

func TestFactory_GetComposerJsonPathFromEnv(t *testing.T) {
	t.Setenv("COMPOSER", " foo.json ")
	got, err := ComposerFile()
	if err != nil || got != "foo.json" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestFactory_GetLockFile(t *testing.T) {
	for in, want := range map[string]string{
		"composer.json":         "composer.lock",
		"./composer.json":       "./composer.lock",
		"foo.json":              "foo.lock",
		"composer.jsonc":        "composer.jsonc.lock",
		"dir.json/composer":     "dir.json/composer.lock",
		"/a/b/project.JSON":     "/a/b/project.JSON.lock",
		"composer-prod.json":    "composer-prod.lock",
		"no-extension":          "no-extension.lock",
		"/path/with.dot/c.json": "/path/with.dot/c.lock",
	} {
		if got := LockFile(in); got != want {
			t.Errorf("LockFile(%q) = %q, want %q", in, got, want)
		}
	}
}

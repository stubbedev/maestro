package composer

// Ports tests/Composer/Test/FactoryTest.php.

import (
	"errors"
	"os"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

func TestFactory_DefaultValuesAreAsExpected(t *testing.T) {
	http.ResetCreateHttpDownloaderWarning()
	t.Cleanup(http.ResetCreateHttpDownloaderWarning)

	out := newBufferIO(t)
	cfg := config.New(false, "")
	if err := cfg.Merge(php.ArrayOf("config", php.ArrayOf("disable-tls", true)), config.SourceUnknown); err != nil {
		t.Fatal(err)
	}

	f := &Factory{Runtime: testRuntime(t, 0)}
	if _, err := f.CreateHttpDownloader(out, cfg, nil); err != nil {
		t.Fatal(err)
	}
	if got := bufferOutput(out); got != "<warning>You are running Composer with SSL/TLS protection disabled.</warning>\n" {
		t.Errorf("output = %q", got)
	}
}

func TestFactory_GetComposerJsonPath(t *testing.T) {
	t.Setenv("COMPOSER", "")
	_ = os.Unsetenv("COMPOSER")
	if got, err := GetComposerFile(); err != nil || got != "./composer.json" {
		t.Errorf("GetComposerFile() = %q, %v", got, err)
	}
}

func TestFactory_GetComposerJsonPathFailsIfDir(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPOSER", dir)
	_, err = GetComposerFile()
	var re *util.RuntimeError
	if !errors.As(err, &re) || re.Message != "The COMPOSER environment variable is set to "+dir+" which is a directory, this variable should point to a composer.json or be left unset." {
		t.Errorf("err = %v", err)
	}
}

func TestFactory_GetComposerJsonPathFromEnv(t *testing.T) {
	t.Setenv("COMPOSER", " foo.json ")
	if got, err := GetComposerFile(); err != nil || got != "foo.json" {
		t.Errorf("GetComposerFile() = %q, %v", got, err)
	}
}

func TestFactory_GetLockFile(t *testing.T) {
	for in, want := range map[string]string{
		"composer.json":      "composer.lock",
		"./composer.json":    "./composer.lock",
		"foo/bar.json":       "foo/bar.lock",
		"composer.json.dist": "composer.json.dist.lock",
	} {
		if got := GetLockFile(in); got != want {
			t.Errorf("GetLockFile(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFactory_CreateComposerFailsWithoutComposerJSON(t *testing.T) {
	dir := tempDir(t)
	t.Chdir(dir)
	f := newFactoryMock(t, testRuntime(t, 0))
	_, err := f.CreateComposer(newBufferIO(t), "./composer.json", PluginsEnabled, "", false)
	var iae *util.InvalidArgumentError
	if !errors.As(err, &iae) || iae.Message != "Composer could not find a composer.json file in "+dir+"\nTo initialize a project, please create a composer.json file. See https://getcomposer.org/basic-usage" {
		t.Errorf("err = %v", err)
	}

	_, err = f.CreateComposer(newBufferIO(t), "other.json", PluginsEnabled, "", false)
	if !errors.As(err, &iae) || iae.Message != "Composer could not find the config file: other.json\nTo initialize a project, please create a composer.json file. See https://getcomposer.org/basic-usage" {
		t.Errorf("err = %v", err)
	}

	_, err = f.CreatePartialComposer(newBufferIO(t), "other.json", PluginsEnabled, "", false)
	if !errors.As(err, &iae) || iae.Message != "Composer could not find the config file: other.json\n" {
		t.Errorf("err = %v", err)
	}
}

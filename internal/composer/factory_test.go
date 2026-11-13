package composer

// Ports tests/Composer/Test/FactoryTest.php.

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
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
	if !errors.As(err, &iae) || iae.Message != "Composer could not find a composer.json file in "+dir+php.EOL+"To initialize a project, please create a composer.json file. See https://getcomposer.org/basic-usage" {
		t.Errorf("err = %v", err)
	}

	_, err = f.CreateComposer(newBufferIO(t), "other.json", PluginsEnabled, "", false)
	if !errors.As(err, &iae) || iae.Message != "Composer could not find the config file: other.json"+php.EOL+"To initialize a project, please create a composer.json file. See https://getcomposer.org/basic-usage" {
		t.Errorf("err = %v", err)
	}

	_, err = f.CreatePartialComposer(newBufferIO(t), "other.json", PluginsEnabled, "", false)
	if !errors.As(err, &iae) || iae.Message != "Composer could not find the config file: other.json"+php.EOL {
		t.Errorf("err = %v", err)
	}
}

// Factory::createConfig joins its messages with PHP_EOL, "\r\n" on
// Windows.
func TestFactory_CreateComposerMessagesUseWindowsEOL(t *testing.T) {
	php.SetEOLForTest(t, "\r\n")
	dir := tempDir(t)
	t.Chdir(dir)
	f := newFactoryMock(t, testRuntime(t, 0))

	_, err := f.CreateComposer(newBufferIO(t), "other.json", PluginsEnabled, "", false)
	var iae *util.InvalidArgumentError
	if !errors.As(err, &iae) || iae.Message != "Composer could not find the config file: other.json\r\nTo initialize a project, please create a composer.json file. See https://getcomposer.org/basic-usage" {
		t.Errorf("err = %v", err)
	}

	if err := os.WriteFile("composer.json", []byte(`{"require": "nope"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = f.CreateComposer(newBufferIO(t), "composer.json", PluginsEnabled, "", false)
	var ve *json.ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Message, " does not match the expected JSON schema:\r\n - ") {
		t.Errorf("err = %v", err)
	}
}

// TestPluginAPIBefore22 checks parseAllowedPlugins' version_compare() of
// the lock file's plugin-api-version, which strict_types refuses unless it
// is a string.
func TestPluginAPIBefore22(t *testing.T) {
	for api, want := range map[string]bool{"1.1.0": true, "2.1.9": true, "2.2.0": false, "2.9.0": false} {
		if got, err := PluginAPIBefore22(api); err != nil || got != want {
			t.Errorf("%s: %v, %v", api, got, err)
		}
	}
	for _, tc := range []struct {
		api   any
		given string
	}{{int64(220), "int"}, {2.2, "float"}, {true, "true"}, {php.ListOf("2.2.0"), "array"}} {
		_, err := PluginAPIBefore22(tc.api)
		var e *php.EngineError
		if !errors.As(err, &e) || e.Class != "TypeError" || e.Message != "version_compare(): Argument #1 ($version1) must be of type string, "+tc.given+" given" {
			t.Errorf("%v: %v", tc.api, err)
		}
	}
}

// Every downloader the factory registers names its own class
// (get_class($downloader)), which DownloadManager's messages print and
// plugins read: one of Composer's, and a class no other type has.
func TestFactory_DownloadersNameTheirOwnPHPClass(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())
	out := newBufferIO(t)
	cfg := config.New(false, "")
	if err := cfg.Merge(php.ArrayOf("config", php.ArrayOf("cache-dir", t.TempDir())), config.SourceUnknown); err != nil {
		t.Fatal(err)
	}
	f := &Factory{Runtime: testRuntime(t, 0)}
	httpDownloader, err := f.CreateHttpDownloader(out, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := f.CreateDownloadManager(out, cfg, httpDownloader, util.NewProcessExecutor(out), nil)
	if err != nil {
		t.Fatal(err)
	}

	types := map[string]string{}
	// Factory::createDownloadManager's types
	for _, typ := range []string{"git", "svn", "fossil", "hg", "perforce", "zip", "rar", "tar", "gzip", "xz", "phar", "file", "path"} {
		d, err := dm.Downloader(typ)
		if err != nil {
			t.Fatal(err)
		}
		class := d.PHPClass()
		if !strings.HasPrefix(class, `Composer\Downloader\`) {
			t.Errorf("the %s downloader is of class %q", typ, class)
		}
		if other, ok := types[class]; ok {
			t.Errorf("the %s and %s downloaders are both of class %q", other, typ, class)
		}
		types[class] = typ
	}
}

func factoryTempComposer(t *testing.T, composerJSON, authJSON string) string {
	t.Helper()
	dir := tempDir(t)
	for name, contents := range map[string]string{"composer.json": composerJSON, "auth.json": authJSON} {
		if err := os.WriteFile(dir+"/"+name, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("COMPOSER_HOME", dir+"/composer-home")
	t.Setenv("COMPOSER_AUTH", "")
	t.Setenv("COMPOSER", "")
	_ = os.Unsetenv("COMPOSER")
	t.Chdir(dir)

	return dir
}

func factoryPartialConfig(t *testing.T, out *io.BufferIO) *config.Config {
	t.Helper()
	c, err := (&Factory{Runtime: testRuntime(t, 0)}).CreatePartialComposer(out, nil, PluginsDisabled, "", false)
	if err != nil {
		t.Fatal(err)
	}

	return c.Config()
}

func factoryConfigValue(t *testing.T, cfg *config.Config, key string) any {
	t.Helper()
	v, err := cfg.Get(key, 0)
	if err != nil {
		t.Fatal(err)
	}

	return v
}

func TestFactory_HomeCannotBeSetByComposerJson(t *testing.T) {
	dir := factoryTempComposer(t, `{"config": {"home": "custom-home"}}`, `{}`)
	out := newBufferIO(t)

	cfg := factoryPartialConfig(t, out)

	if got := factoryConfigValue(t, cfg, "home"); got != dir+"/composer-home" {
		t.Errorf("home = %v, want %s", got, dir+"/composer-home")
	}
	if output := bufferOutput(out); !strings.Contains(output, `Ignoring the "home" config set in ./composer.json`) {
		t.Errorf("output lacks the home warning:\n%s", output)
	}
}

func TestFactory_AuthJsonOnlyAppliesAuthConfig(t *testing.T) {
	dir := factoryTempComposer(t, `{}`, `{
		"http-basic": {"example.org": {"username": "foo", "password": "bar"}},
		"gitlab-domains": ["gitlab.example.org"],
		"vendor-dir": "custom-vendor"
	}`)
	out := newBufferIO(t)

	cfg := factoryPartialConfig(t, out)

	want := php.ArrayOf("example.org", php.ArrayOf("username", "foo", "password", "bar"))
	if got := factoryConfigValue(t, cfg, "http-basic"); !php.StrictEquals(got, want) {
		t.Errorf("http-basic = %v, want %v", got, want)
	}
	domains, _ := factoryConfigValue(t, cfg, "gitlab-domains").(*php.Array)
	if domains == nil || !slices.Contains(domains.Values(), any("gitlab.example.org")) {
		t.Errorf("gitlab-domains = %v, want gitlab.example.org among them", domains)
	}
	if got := factoryConfigValue(t, cfg, "vendor-dir"); got != dir+"/vendor" {
		t.Errorf("vendor-dir = %v, want %s", got, dir+"/vendor")
	}
	if output := bufferOutput(out); !strings.Contains(output, `Ignoring the "vendor-dir" config set in `+dir+`/auth.json`) {
		t.Errorf("output lacks the vendor-dir warning:\n%s", output)
	}
}

func TestFactory_ComposerAuthEnvOnlyAppliesAuthConfig(t *testing.T) {
	dir := factoryTempComposer(t, `{}`, `{}`)
	t.Setenv("COMPOSER_AUTH", `{"bearer":{"example.org":"token"},"vendor-dir":"custom-vendor"}`)
	out := newBufferIO(t)

	cfg := factoryPartialConfig(t, out)

	if got := factoryConfigValue(t, cfg, "bearer"); !php.StrictEquals(got, php.ArrayOf("example.org", "token")) {
		t.Errorf("bearer = %v", got)
	}
	if got := factoryConfigValue(t, cfg, "vendor-dir"); got != dir+"/vendor" {
		t.Errorf("vendor-dir = %v, want %s", got, dir+"/vendor")
	}
	if output := bufferOutput(out); !strings.Contains(output, `Ignoring the "vendor-dir" config set in COMPOSER_AUTH`) {
		t.Errorf("output lacks the vendor-dir warning:\n%s", output)
	}
}

package config

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/util"
)

// Ports tests/Composer/Test/ConfigTest.php.

// arr decodes a JSON object or list into a PHP array (json_decode assoc).
func arr(t *testing.T, json string) *php.Array {
	t.Helper()
	v, err := php.JSONDecode(json, true)
	if err != nil {
		t.Fatalf("bad JSON %s: %v", json, err)
	}
	a, ok := v.(*php.Array)
	if !ok {
		t.Fatalf("not an array: %s", json)
	}

	return a
}

func cfg(t *testing.T, json string) *php.Array {
	t.Helper()

	return php.ArrayOf("config", arr(t, json))
}

func mustGet(t *testing.T, c *Config, key string, flags int) any {
	t.Helper()
	v, err := c.Get(key, flags)
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}

	return v
}

func merge(t *testing.T, c *Config, config *php.Array, source string) {
	t.Helper()
	if err := c.Merge(config, source); err != nil {
		t.Fatalf("Merge: %v", err)
	}
}

// assertSame compares like PHPUnit's assertSame: ===, so array order and
// types matter.
func assertSame(t *testing.T, want, got any) {
	t.Helper()
	if !php.StrictEquals(want, got) {
		t.Errorf("got %s, want %s", php.VarExport(got), php.VarExport(want))
	}
}

// unsetenv clears an environment variable for the test, restoring it after.
func unsetenv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

func TestConfig_AddPackagistRepository(t *testing.T) {
	cases := []struct {
		name                    string
		expected, local, system string
	}{
		{"local config inherits system defaults", `{"packagist.org": {"type": "composer", "url": "https://repo.packagist.org"}}`, `[]`, ``},
		{"local config can disable system config by name", `[]`, `[{"packagist.org": false}]`, ``},
		{"local config can disable system config by name bc", `[]`, `[{"packagist": false}]`, ``},
		{
			"local config adds above defaults",
			`{"0": {"type": "vcs", "url": "git://github.com/composer/composer.git"}, "1": {"type": "pear", "url": "http://pear.composer.org"}, "packagist.org": {"type": "composer", "url": "https://repo.packagist.org"}}`,
			`[{"type": "vcs", "url": "git://github.com/composer/composer.git"}, {"type": "pear", "url": "http://pear.composer.org"}]`,
			``,
		},
		{
			"system config adds above core defaults",
			`{"example.com": {"type": "composer", "url": "http://example.com"}, "packagist.org": {"type": "composer", "url": "https://repo.packagist.org"}}`,
			`[]`,
			`{"example.com": {"type": "composer", "url": "http://example.com"}}`,
		},
		{
			"local config can disable repos by name and re-add them anonymously to bring them above system config",
			`{"1": {"type": "composer", "url": "http://packagist.org"}, "example.com": {"type": "composer", "url": "http://example.com"}}`,
			`[{"packagist.org": false}, {"type": "composer", "url": "http://packagist.org"}]`,
			`{"example.com": {"type": "composer", "url": "http://example.com"}}`,
		},
		{
			"local config can override by name to bring a repo above system config",
			`{"packagist.org": {"type": "composer", "url": "http://packagistnew.org"}, "example.com": {"type": "composer", "url": "http://example.com"}}`,
			`{"packagist.org": {"type": "composer", "url": "http://packagistnew.org"}}`,
			`{"example.com": {"type": "composer", "url": "http://example.com"}}`,
		},
		{
			"local config redefining packagist.org by URL override it if no named keys are used",
			`[{"type": "composer", "url": "https://repo.packagist.org"}]`,
			`[{"type": "composer", "url": "https://repo.packagist.org"}]`,
			``,
		},
		{
			"local config redefining packagist.org by URL override it also with named keys",
			`{"example": {"type": "composer", "url": "https://repo.packagist.org"}}`,
			`{"example": {"type": "composer", "url": "https://repo.packagist.org"}}`,
			``,
		},
		{
			"incorrect local config does not cause ErrorException",
			`{"packagist.org": {"type": "composer", "url": "https://repo.packagist.org"}, "type": "vcs", "url": "http://example.com"}`,
			`{"type": "vcs", "url": "http://example.com"}`,
			``,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config := New(false, "")
			if c.system != "" {
				merge(t, config, php.ArrayOf("repositories", arr(t, c.system)), SourceUnknown)
			}
			merge(t, config, php.ArrayOf("repositories", arr(t, c.local)), SourceUnknown)

			// assertEquals: key order is not compared (the oracle checks it)
			if !php.LooseEquals(arr(t, c.expected), config.Repositories()) {
				t.Errorf("got %s, want %s", php.VarExport(config.Repositories()), c.expected)
			}
		})
	}
}

func TestConfig_PreferredInstallAsString(t *testing.T) {
	config := New(false, "")
	merge(t, config, cfg(t, `{"preferred-install": "source"}`), SourceUnknown)
	merge(t, config, cfg(t, `{"preferred-install": "dist"}`), SourceUnknown)

	assertSame(t, "dist", mustGet(t, config, "preferred-install", 0))
}

func TestConfig_MergePreferredInstall(t *testing.T) {
	config := New(false, "")
	merge(t, config, cfg(t, `{"preferred-install": "dist"}`), SourceUnknown)
	merge(t, config, cfg(t, `{"preferred-install": {"foo/*": "source"}}`), SourceUnknown)

	// This assertion needs to make sure full wildcard preferences are placed last
	assertSame(t, arr(t, `{"foo/*": "source", "*": "dist"}`), mustGet(t, config, "preferred-install", 0))
}

func TestConfig_MergeGithubOauth(t *testing.T) {
	config := New(false, "")
	merge(t, config, cfg(t, `{"github-oauth": {"foo": "bar"}}`), SourceUnknown)
	merge(t, config, cfg(t, `{"github-oauth": {"bar": "baz"}}`), SourceUnknown)

	assertSame(t, arr(t, `{"foo": "bar", "bar": "baz"}`), mustGet(t, config, "github-oauth", 0))
}

func userHome() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}

	return strings.TrimRight(home, "\\/")
}

func TestConfig_VarReplacement(t *testing.T) {
	config := New(false, "")
	merge(t, config, cfg(t, `{"a": "b", "c": "{$a}"}`), SourceUnknown)
	merge(t, config, cfg(t, `{"bin-dir": "$HOME", "cache-dir": "~/foo/"}`), SourceUnknown)

	home := userHome()
	assertSame(t, "b", mustGet(t, config, "c", 0))
	assertSame(t, home, mustGet(t, config, "bin-dir", 0))
	assertSame(t, home+"/foo", mustGet(t, config, "cache-dir", 0))
}

func TestConfig_RealpathReplacement(t *testing.T) {
	config := New(false, "/foo/bar")
	merge(t, config, cfg(t, `{"bin-dir": "$HOME/foo", "cache-dir": "/baz/", "vendor-dir": "vendor"}`), SourceUnknown)

	home := userHome()
	assertSame(t, "/foo/bar/vendor", mustGet(t, config, "vendor-dir", 0))
	assertSame(t, home+"/foo", mustGet(t, config, "bin-dir", 0))
	assertSame(t, "/baz", mustGet(t, config, "cache-dir", 0))
}

func TestConfig_StreamWrapperDirs(t *testing.T) {
	config := New(false, "/foo/bar")
	merge(t, config, cfg(t, `{"cache-dir": "s3://baz/"}`), SourceUnknown)

	assertSame(t, "s3://baz", mustGet(t, config, "cache-dir", 0))
}

func TestConfig_FetchingRelativePaths(t *testing.T) {
	config := New(false, "/foo/bar")
	merge(t, config, cfg(t, `{"bin-dir": "{$vendor-dir}/foo", "vendor-dir": "vendor"}`), SourceUnknown)

	assertSame(t, "/foo/bar/vendor", mustGet(t, config, "vendor-dir", 0))
	assertSame(t, "/foo/bar/vendor/foo", mustGet(t, config, "bin-dir", 0))
	assertSame(t, "vendor", mustGet(t, config, "vendor-dir", RelativePaths))
	assertSame(t, "vendor/foo", mustGet(t, config, "bin-dir", RelativePaths))
}

func TestConfig_OverrideGithubProtocols(t *testing.T) {
	config := New(false, "")
	merge(t, config, cfg(t, `{"github-protocols": ["https", "ssh"]}`), SourceUnknown)
	merge(t, config, cfg(t, `{"github-protocols": ["https"]}`), SourceUnknown)

	assertSame(t, arr(t, `["https"]`), mustGet(t, config, "github-protocols", 0))
}

func TestConfig_GitDisabledByDefaultInGithubProtocols(t *testing.T) {
	config := New(false, "")
	merge(t, config, cfg(t, `{"github-protocols": ["https", "git"]}`), SourceUnknown)
	assertSame(t, arr(t, `["https"]`), mustGet(t, config, "github-protocols", 0))

	merge(t, config, cfg(t, `{"secure-http": false}`), SourceUnknown)
	assertSame(t, arr(t, `["https", "git"]`), mustGet(t, config, "github-protocols", 0))
}

func TestConfig_AllowedUrlsPass(t *testing.T) {
	for _, url := range []string{
		"https://packagist.org",
		"git@github.com:composer/composer.git",
		"hg://user:pass@my.satis/satis",
		`\myserver\myplace.git`,
		"file://myserver.localhost/mygit.git",
		"file://example.org/mygit.git",
		"git:Department/Repo.git",
		"ssh://[user@]host.xz[:port]/path/to/repo.git/",
	} {
		if err := New(false, "").ProhibitURLByConfig(url, nil, nil); err != nil {
			t.Errorf("%s: %v", url, err)
		}
	}
}

func TestConfig_ProhibitedUrlsThrowException(t *testing.T) {
	for _, url := range []string{
		"http://packagist.org",
		"http://10.1.0.1/satis",
		"http://127.0.0.1/satis",
		"http://\xf0\x9f\x92\x9b@example.org",
		"svn://localhost/trunk",
		"svn://will.not.resolve/trunk",
		"svn://192.168.0.1/trunk",
		"svn://1.2.3.4/trunk",
		"git://5.6.7.8/git.git",
	} {
		err := New(false, "").ProhibitURLByConfig(url, nil, nil)
		var te *util.TransportError
		if !errors.As(err, &te) {
			t.Errorf("%s: got %v, want a TransportError", url, err)

			continue
		}
		if !strings.Contains(te.Message, "Your configuration does not allow connections to "+url) {
			t.Errorf("%s: message %q", url, te.Message)
		}
	}
}

// recordingIO records WriteError messages.
type recordingIO struct {
	*io.NullIO
	errors []string
}

func (r *recordingIO) WriteError(message string, _ bool, _ io.Verbosity) {
	r.errors = append(r.errors, message)
}

func TestConfig_ProhibitedUrlsWarningVerifyPeer(t *testing.T) {
	rec := &recordingIO{NullIO: io.NewNullIO()}

	config := New(false, "")
	err := config.ProhibitURLByConfig("https://example.org", rec, php.ArrayOf("ssl", php.ArrayOf(
		"verify_peer", false,
		"verify_peer_name", false,
	)))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"<warning>Warning: Accessing example.org with verify_peer and verify_peer_name disabled.</warning>"}
	if strings.Join(rec.errors, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q, want %q", rec.errors, want)
	}
}

func TestConfig_DisableTlsCanBeOverridden(t *testing.T) {
	config := New(true, "")
	merge(t, config, cfg(t, `{"disable-tls": "false"}`), SourceUnknown)
	assertSame(t, false, mustGet(t, config, "disable-tls", 0))
	merge(t, config, cfg(t, `{"disable-tls": "true"}`), SourceUnknown)
	assertSame(t, true, mustGet(t, config, "disable-tls", 0))
}

func TestConfig_ProcessTimeout(t *testing.T) {
	t.Setenv("COMPOSER_PROCESS_TIMEOUT", "0")
	config := New(true, "")

	assertSame(t, int64(0), mustGet(t, config, "process-timeout", 0))
}

func TestConfig_HtaccessProtect(t *testing.T) {
	t.Setenv("COMPOSER_HTACCESS_PROTECT", "0")
	config := New(true, "")

	assertSame(t, false, mustGet(t, config, "htaccess-protect", 0))
}

func sourceOf(t *testing.T, c *Config, key string) string {
	t.Helper()
	s, err := c.SourceOfValue(key)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func TestConfig_GetSourceOfValue(t *testing.T) {
	unsetenv(t, "COMPOSER_PROCESS_TIMEOUT")

	config := New(true, "")

	assertSame(t, SourceDefault, sourceOf(t, config, "process-timeout"))

	merge(t, config, cfg(t, `{"process-timeout": 1}`), "phpunit-test")

	assertSame(t, "phpunit-test", sourceOf(t, config, "process-timeout"))
}

func TestConfig_GetSourceOfValueEnvVariables(t *testing.T) {
	t.Setenv("COMPOSER_HTACCESS_PROTECT", "0")
	config := New(true, "")

	assertSame(t, "COMPOSER_HTACCESS_PROTECT", sourceOf(t, config, "htaccess-protect"))
}

func getArray(t *testing.T, v any) *php.Array {
	t.Helper()
	a, ok := v.(*php.Array)
	if !ok {
		t.Fatalf("not an array: %s", php.VarExport(v))
	}

	return a
}

func entry(t *testing.T, a *php.Array, key string) any {
	t.Helper()
	v, ok := a.Get(key)
	if !ok {
		t.Fatalf("no key %q in %s", key, php.VarExport(a))
	}

	return v
}

func TestConfig_Audit(t *testing.T) {
	unsetenv(t, "COMPOSER_AUDIT_ABANDONED")
	unsetenv(t, "COMPOSER_SECURITY_BLOCKING_ABANDONED")

	config := New(true, "")
	result := getArray(t, mustGet(t, config, "audit", 0))
	assertSame(t, policy.AuditFail, entry(t, result, "abandoned"))
	assertSame(t, php.NewArray(), entry(t, result, "ignore"))

	t.Setenv("COMPOSER_AUDIT_ABANDONED", policy.AuditIgnore)
	result = getArray(t, mustGet(t, config, "audit", 0))
	unsetenv(t, "COMPOSER_AUDIT_ABANDONED")
	assertSame(t, policy.AuditIgnore, entry(t, result, "abandoned"))
	assertSame(t, php.NewArray(), entry(t, result, "ignore"))

	merge(t, config, cfg(t, `{"audit": {"ignore": ["A", "B"]}}`), SourceUnknown)
	merge(t, config, cfg(t, `{"audit": {"ignore": ["A", "C"]}}`), SourceUnknown)
	result = getArray(t, mustGet(t, config, "audit", 0))
	assertSame(t, arr(t, `["A", "B", "A", "C"]`), entry(t, result, "ignore"))

	// Test COMPOSER_SECURITY_BLOCKING_ABANDONED env var
	t.Setenv("COMPOSER_SECURITY_BLOCKING_ABANDONED", "1")
	result = getArray(t, mustGet(t, config, "audit", 0))
	assertSame(t, true, entry(t, result, "block-abandoned"))

	t.Setenv("COMPOSER_SECURITY_BLOCKING_ABANDONED", "0")
	result = getArray(t, mustGet(t, config, "audit", 0))
	assertSame(t, false, entry(t, result, "block-abandoned"))
}

func TestConfig_Policy(t *testing.T) {
	unsetenv(t, "COMPOSER_POLICY")

	config := New(true, "")
	assertSame(t, true, mustGet(t, config, "policy", 0))

	merge(t, config, cfg(t, `{"policy": {"advisories": {"ignore": ["acme/package"]}}}`), SourceUnknown)
	merge(t, config, cfg(t, `{"policy": {"advisories": {"ignore-severities": ["low"]}}}`), SourceUnknown)
	result := getArray(t, mustGet(t, config, "policy", 0))
	assertSame(t, arr(t, `{"ignore": ["acme/package"], "ignore-severities": ["low"]}`), entry(t, result, "advisories"))

	// COMPOSER_POLICY=1 is a no-op when policy is already enabled — the existing
	// array config is preserved, not flattened back to the `true` shorthand.
	t.Setenv("COMPOSER_POLICY", "1")
	resultWithEnvOn := getArray(t, mustGet(t, config, "policy", 0))
	unsetenv(t, "COMPOSER_POLICY")
	assertSame(t, arr(t, `{"ignore": ["acme/package"], "ignore-severities": ["low"]}`), entry(t, resultWithEnvOn, "advisories"))

	merge(t, config, cfg(t, `{"policy": true}`), SourceUnknown)
	getArray(t, mustGet(t, config, "policy", 0))

	merge(t, config, cfg(t, `{"policy": false}`), SourceUnknown)
	assertSame(t, false, mustGet(t, config, "policy", 0))

	// COMPOSER_POLICY=1 re-enables policy when the config has it disabled.
	t.Setenv("COMPOSER_POLICY", "1")
	assertSame(t, true, mustGet(t, config, "policy", 0))
	unsetenv(t, "COMPOSER_POLICY")

	// The disable path still wins — env var of 0 forces policy off regardless
	// of any prior array config.
	t.Setenv("COMPOSER_POLICY", "0")
	assertSame(t, false, mustGet(t, config, "policy", 0))
	unsetenv(t, "COMPOSER_POLICY")

	merge(t, config, cfg(t, `{"policy": true}`), SourceUnknown)
	assertSame(t, php.NewArray(), mustGet(t, config, "policy", 0))
}

func TestConfig_PolicyListBoolTrueAndEmptyObjectAreEquivalentInLayering(t *testing.T) {
	configBoolTrue := New(false, "")
	merge(t, configBoolTrue, cfg(t, `{"policy": {"advisories": true}}`), SourceUnknown)
	merge(t, configBoolTrue, cfg(t, `{"policy": {"advisories": {"audit": "report"}}}`), SourceUnknown)

	configEmptyObj := New(false, "")
	merge(t, configEmptyObj, cfg(t, `{"policy": {"advisories": []}}`), SourceUnknown)
	merge(t, configEmptyObj, cfg(t, `{"policy": {"advisories": {"audit": "report"}}}`), SourceUnknown)

	assertSame(t, mustGet(t, configBoolTrue, "policy", 0), mustGet(t, configEmptyObj, "policy", 0))
}

func TestConfig_PolicyListFalseOverridesPriorTrueOrEmptyEquallyAcrossLayers(t *testing.T) {
	configFromTrue := New(false, "")
	merge(t, configFromTrue, cfg(t, `{"policy": {"advisories": true}}`), SourceUnknown)
	merge(t, configFromTrue, cfg(t, `{"policy": {"advisories": false}}`), SourceUnknown)

	configFromEmpty := New(false, "")
	merge(t, configFromEmpty, cfg(t, `{"policy": {"advisories": []}}`), SourceUnknown)
	merge(t, configFromEmpty, cfg(t, `{"policy": {"advisories": false}}`), SourceUnknown)

	assertSame(t, mustGet(t, configFromTrue, "policy", 0), mustGet(t, configFromEmpty, "policy", 0))
	assertSame(t, false, entry(t, getArray(t, mustGet(t, configFromTrue, "policy", 0)), "advisories"))
}

func TestConfig_PolicyMasterTrueAfterDetailedConfigDoesNotEraseDetail(t *testing.T) {
	config := New(false, "")
	merge(t, config, cfg(t, `{"policy": {"advisories": {"block": false}}}`), SourceUnknown)
	merge(t, config, cfg(t, `{"policy": true}`), SourceUnknown)

	result := getArray(t, mustGet(t, config, "policy", 0))
	assertSame(t, arr(t, `{"block": false}`), entry(t, result, "advisories"))
}

func TestConfig_PolicyDeepMergesIgnoreAcrossSources(t *testing.T) {
	unsetenv(t, "COMPOSER_POLICY")
	config := New(true, "")

	merge(t, config, cfg(t, `{"policy": {"advisories": {
		"ignore": ["vendor/global-1", "vendor/global-2"],
		"ignore-id": ["CVE-1111"],
		"ignore-severity": ["low"],
		"block": true}}}`), SourceUnknown)
	merge(t, config, cfg(t, `{"policy": {"advisories": {
		"ignore": ["vendor/project-1"],
		"ignore-id": ["CVE-2222"],
		"ignore-severity": ["medium"],
		"audit": "report"}}}`), SourceUnknown)

	advisories := getArray(t, entry(t, getArray(t, mustGet(t, config, "policy", 0)), "advisories"))

	// Deep-merged inner arrays (mirrors audit.ignore behaviour)
	assertSame(t, arr(t, `["vendor/global-1", "vendor/global-2", "vendor/project-1"]`), entry(t, advisories, "ignore"))
	assertSame(t, arr(t, `["CVE-1111", "CVE-2222"]`), entry(t, advisories, "ignore-id"))
	assertSame(t, arr(t, `["low", "medium"]`), entry(t, advisories, "ignore-severity"))

	// Sibling scalar keys still merge top-level (later wins, but both retained)
	assertSame(t, true, entry(t, advisories, "block"))
	assertSame(t, "report", entry(t, advisories, "audit"))
}

func TestConfig_PolicyDeepMergesIgnoreForMalwareAndAbandonedAndCustomList(t *testing.T) {
	unsetenv(t, "COMPOSER_POLICY")
	config := New(true, "")

	merge(t, config, cfg(t, `{"policy": {
		"malware": {"ignore": ["vendor/global-malware"], "ignore-source": ["source-global"]},
		"abandoned": {"ignore": ["vendor/global-abandoned"]},
		"custom-list": {"ignore": ["vendor/global-custom"]}}}`), SourceUnknown)
	merge(t, config, cfg(t, `{"policy": {
		"malware": {"ignore": ["vendor/project-malware"], "ignore-source": ["source-project"]},
		"abandoned": {"ignore": ["vendor/project-abandoned"]},
		"custom-list": {"ignore": ["vendor/project-custom"]}}}`), SourceUnknown)

	result := getArray(t, mustGet(t, config, "policy", 0))
	list := func(name string) *php.Array { return getArray(t, entry(t, result, name)) }
	assertSame(t, arr(t, `["vendor/global-malware", "vendor/project-malware"]`), entry(t, list("malware"), "ignore"))
	assertSame(t, arr(t, `["source-global", "source-project"]`), entry(t, list("malware"), "ignore-source"))
	assertSame(t, arr(t, `["vendor/global-abandoned", "vendor/project-abandoned"]`), entry(t, list("abandoned"), "ignore"))
	assertSame(t, arr(t, `["vendor/global-custom", "vendor/project-custom"]`), entry(t, list("custom-list"), "ignore"))
}

func TestConfig_GetDefaultsToAnEmptyArray(t *testing.T) {
	config := New(true, "")
	for _, key := range []string{"bitbucket-oauth", "github-oauth", "gitlab-oauth", "gitlab-token", "forgejo-token", "http-basic", "bearer"} {
		if a := getArray(t, mustGet(t, config, key, 0)); a.Len() != 0 {
			t.Errorf("%s: %s", key, php.VarExport(a))
		}
	}
}

func TestConfig_MergesPluginConfig(t *testing.T) {
	config := New(false, "")
	merge(t, config, cfg(t, `{"allow-plugins": {"some/plugin": true}}`), SourceUnknown)
	assertSame(t, arr(t, `{"some/plugin": true}`), mustGet(t, config, "allow-plugins", 0))

	merge(t, config, cfg(t, `{"allow-plugins": {"another/plugin": true}}`), SourceUnknown)
	assertSame(t, arr(t, `{"another/plugin": true, "some/plugin": true}`), mustGet(t, config, "allow-plugins", 0))
}

func TestConfig_OverridesGlobalBooleanPluginsConfig(t *testing.T) {
	config := New(false, "")
	merge(t, config, cfg(t, `{"allow-plugins": true}`), SourceUnknown)
	assertSame(t, true, mustGet(t, config, "allow-plugins", 0))

	merge(t, config, cfg(t, `{"allow-plugins": {"another/plugin": true}}`), SourceUnknown)
	assertSame(t, arr(t, `{"another/plugin": true}`), mustGet(t, config, "allow-plugins", 0))
}

func TestConfig_AllowsAllPluginsFromLocalBoolean(t *testing.T) {
	config := New(false, "")
	merge(t, config, cfg(t, `{"allow-plugins": {"some/plugin": true}}`), SourceUnknown)
	assertSame(t, arr(t, `{"some/plugin": true}`), mustGet(t, config, "allow-plugins", 0))

	merge(t, config, cfg(t, `{"allow-plugins": true}`), SourceUnknown)
	assertSame(t, true, mustGet(t, config, "allow-plugins", 0))
}

func TestConfig_SourceFallbackDefaultsToFalse(t *testing.T) {
	assertSame(t, false, mustGet(t, New(false, ""), "source-fallback", 0))
}

func TestConfig_SourceFallbackCanBeDisabled(t *testing.T) {
	config := New(false, "")
	merge(t, config, cfg(t, `{"source-fallback": false}`), SourceUnknown)
	assertSame(t, false, mustGet(t, config, "source-fallback", 0))
}

func TestConfig_SourceFallbackCanBeSetFromString(t *testing.T) {
	config := New(false, "")
	merge(t, config, cfg(t, `{"source-fallback": "false"}`), SourceUnknown)
	assertSame(t, false, mustGet(t, config, "source-fallback", 0))

	merge(t, config, cfg(t, `{"source-fallback": "true"}`), SourceUnknown)
	assertSame(t, true, mustGet(t, config, "source-fallback", 0))
}

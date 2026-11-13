package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
)

// Ports tests/Composer/Test/Config/JsonConfigSourceTest.php.

func fixturePath(name string) string { return filepath.Join("testdata", "Fixtures", name) }

// jcsCopy copies a fixture to a fresh composer.json and returns its path.
func jcsCopy(t *testing.T, fixture string) string {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}

	return jcsWrite(t, string(data))
}

func jcsWrite(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "composer.json")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func jcsSource(t *testing.T, path string) *JSONConfigSource {
	t.Helper()
	file, err := json.NewFile(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	return NewJSONConfigSource(file, false)
}

func assertFileEquals(t *testing.T, expectedFile, actualFile string) {
	t.Helper()
	expected, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, string(expected), actualFile)
}

func assertFileContents(t *testing.T, expected, actualFile string) {
	t.Helper()
	actual, err := os.ReadFile(actualFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != expected {
		t.Errorf("got:\n%s\nwant:\n%s", actual, expected)
	}
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestJsonConfigSource_AddRepository(t *testing.T) {
	config := jcsCopy(t, fixturePath("composer-repositories.json"))
	check(t, jcsSource(t, config).AddRepository("example_tld", php.ArrayOf("type", "git", "url", "example.tld"), true))

	assertFileEquals(t, fixturePath("config/config-with-exampletld-repository.json"), config)
}

func TestJsonConfigSource_AddRepositoryAsList(t *testing.T) {
	config := jcsCopy(t, fixturePath("composer-repositories.json"))
	check(t, jcsSource(t, config).AddRepository("", php.ArrayOf("type", "git", "url", "example.tld"), true))

	assertFileEquals(t, fixturePath("config/config-with-exampletld-repository-as-list.json"), config)
}

func TestJsonConfigSource_AddRepositoryWithOptions(t *testing.T) {
	config := jcsCopy(t, fixturePath("composer-repositories.json"))
	check(t, jcsSource(t, config).AddRepository("example_tld", php.ArrayOf(
		"type", "composer",
		"url", "https://example.tld",
		"options", php.ArrayOf("ssl", php.ArrayOf("local_cert", "/home/composer/.ssl/composer.pem")),
	), true))

	assertFileEquals(t, fixturePath("config/config-with-exampletld-repository-and-options.json"), config)
}

func TestJsonConfigSource_RemoveRepository(t *testing.T) {
	config := jcsCopy(t, fixturePath("config/config-with-exampletld-repository.json"))
	check(t, jcsSource(t, config).RemoveRepository("example_tld"))

	assertFileEquals(t, fixturePath("composer-empty.json"), config)
}

func TestJsonConfigSource_AddPackagistRepositoryWithFalseValue(t *testing.T) {
	config := jcsCopy(t, fixturePath("composer-repositories.json"))
	check(t, jcsSource(t, config).AddRepository("packagist", false, true))

	assertFileEquals(t, fixturePath("config/config-with-packagist-false.json"), config)
}

func TestJsonConfigSource_RemovePackagist(t *testing.T) {
	config := jcsCopy(t, fixturePath("config/config-with-packagist-false.json"))
	check(t, jcsSource(t, config).RemoveRepository("packagist"))

	assertFileEquals(t, fixturePath("composer-empty.json"), config)
}

func TestJsonConfigSource_AddPolicyListFieldPreservesFormatting(t *testing.T) {
	config := jcsWrite(t, `{
    "name": "vendor/pkg", "config": {
        "vendor-dir": "vendor", "sort-packages": true,
        "policy": {
            "advisories": {
                "audit": "report"
            },
            "abandoned": {
                "block": true
            }
        }
    }
}
`)
	check(t, jcsSource(t, config).AddConfigSetting("policy.advisories.block", true))

	assertFileContents(t, `{
    "name": "vendor/pkg", "config": {
        "vendor-dir": "vendor", "sort-packages": true,
        "policy": {
            "advisories": {
                "audit": "report",
                "block": true
            },
            "abandoned": {
                "block": true
            }
        }
    }
}
`, config)
}

func TestJsonConfigSource_RemovePolicyListFieldPreservesFormatting(t *testing.T) {
	config := jcsWrite(t, `{
    "name": "vendor/pkg", "config": {
        "vendor-dir": "vendor", "sort-packages": true,
        "policy": {
            "advisories": {
                "audit": "report",
                "block": true
            }
        }
    }
}
`)
	check(t, jcsSource(t, config).RemoveConfigSetting("policy.advisories.audit"))

	assertFileContents(t, `{
    "name": "vendor/pkg", "config": {
        "vendor-dir": "vendor", "sort-packages": true,
        "policy": {
            "advisories": {
                "block": true
            }
        }
    }
}
`, config)
}

func TestJsonConfigSource_RemovePolicyListFieldCascadesEmptyAncestors(t *testing.T) {
	config := jcsWrite(t, `{
    "name": "vendor/pkg", "config": {
        "vendor-dir": "vendor", "sort-packages": true,
        "policy": {
            "advisories": {
                "block": true
            }
        }
    }
}
`)
	check(t, jcsSource(t, config).RemoveConfigSetting("policy.advisories.block"))

	assertFileContents(t, `{
    "name": "vendor/pkg", "config": {
        "vendor-dir": "vendor",
        "sort-packages": true,
        "policy": {
        }
    }
}
`, config)
}

func TestJsonConfigSource_AddPropertyToArrayValuedExtraDoesNotDoubleNest(t *testing.T) {
	config := jcsWrite(t, `{
    "name": "vendor/pkg",
    "extra": [
        "hello"
    ]
}
`)
	check(t, jcsSource(t, config).AddProperty("extra.1", "world"))

	// The value must land at extra.1, not at a phantom extra.extra.1 (GH-13165).
	assertFileContents(t, `{
    "name": "vendor/pkg",
    "extra": [
        "hello",
        "world"
    ]
}
`, config)
}

func TestJsonConfigSource_RemovePropertyFromArrayValuedExtraDoesNotDoubleNest(t *testing.T) {
	config := jcsWrite(t, `{
    "name": "vendor/pkg",
    "extra": [
        "hello",
        "world"
    ]
}
`)
	check(t, jcsSource(t, config).RemoveProperty("extra.0"))

	assertFileContents(t, `{
    "name": "vendor/pkg",
    "extra": [
        "world"
    ]
}
`, config)
}

// assertCompactJSON compares a file with json_encode(json_decode($file)).
func assertCompactJSON(t *testing.T, expected, actualFile string) {
	t.Helper()
	data, err := os.ReadFile(actualFile)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := php.JSONDecode(string(data), false)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := php.JSONEncode(decoded, 0)
	if err != nil {
		t.Fatal(err)
	}
	if encoded != expected {
		t.Errorf("got %s, want %s", encoded, expected)
	}
}

func TestJsonConfigSource_PropertyFallbackMatchesManipulator(t *testing.T) {
	type operation struct {
		run      func(*JSONConfigSource, []any) error
		fallback fallback
	}
	addProperty := operation{func(s *JSONConfigSource, a []any) error { return s.AddProperty(a[0].(string), a[1]) }, addPropertyFallback}
	removeProperty := operation{func(s *JSONConfigSource, a []any) error { return s.RemoveProperty(a[0].(string)) }, removePropertyFallback}
	removeLink := operation{func(s *JSONConfigSource, a []any) error { return s.RemoveLink(a[0].(string), a[1].(string)) }, removeLinkFallback}
	removeRepository := operation{func(s *JSONConfigSource, a []any) error { return s.RemoveRepository(a[0].(string)) }, removeRepositoryFallback}
	cases := []struct {
		name     string
		original string
		op       operation
		args     []any
		want     string
	}{
		{"add nested extra key keeps the remainder literal", `{"extra": {"foo": "bar"}}`, addProperty, []any{"extra.a.b.c", "x"}, `{"extra":{"foo":"bar","a":{"b.c":"x"}}}`},
		{"add replaces a scalar parent", `{"extra": {"foo": "bar"}}`, addProperty, []any{"extra.foo.baz", "x"}, `{"extra":{"foo":{"baz":"x"}}}`},
		{"add script", `{"scripts": {"test": "phpunit"}}`, addProperty, []any{"scripts.lint", "phpcs"}, `{"scripts":{"test":"phpunit","lint":"phpcs"}}`},
		{"add suggest", `{"suggest": {"a/b": "c"}}`, addProperty, []any{"suggest.foo/b.ar", "desc"}, `{"suggest":{"a\/b":"c","foo\/b.ar":"desc"}}`},
		{"remove nested extra key with literal remainder", `{"extra": {"a": {"b.c": "x", "d": 1}}}`, removeProperty, []any{"extra.a.b.c"}, `{"extra":{"a":{"d":1}}}`},
		{"remove last nested key keeps an object", `{"extra": {"foo": {"bar": 1}, "z": 2}}`, removeProperty, []any{"extra.foo.bar"}, `{"extra":{"foo":{},"z":2}}`},
		{"remove last extra key keeps an object", `{"extra": {"foo": 1}}`, removeProperty, []any{"extra.foo"}, `{"extra":{}}`},
		{"remove below a scalar is a no-op", `{"extra": {"foo": "bar", "z": 1}}`, removeProperty, []any{"extra.foo.0.x"}, `{"extra":{"foo":"bar","z":1}}`},
		{"remove suggest", `{"suggest": {"foo/bar": "x", "a/b": "y"}}`, removeProperty, []any{"suggest.foo/bar"}, `{"suggest":{"a\/b":"y"}}`},
		{"remove autoload type", `{"autoload": {"psr-4": {"A\\\\": "src/"}, "files": ["x.php"]}}`, removeProperty, []any{"autoload.psr-4"}, `{"autoload":{"files":["x.php"]}}`},
		{"remove last link removes the key", `{"require": {"a/b": "1"}, "require-dev": {"c/d": "1"}}`, removeLink, []any{"require-dev", "c/d"}, `{"require":{"a\/b":"1"}}`},
		{"remove link keeps the other links", `{"require-dev": {"a/b": "1", "c/d": "1"}}`, removeLink, []any{"require-dev", "c/d"}, `{"require-dev":{"a\/b":"1"}}`},
		{"remove missing link keeps the key", `{"require-dev": {"a/b": "1"}}`, removeLink, []any{"require-dev", "c/d"}, `{"require-dev":{"a\/b":"1"}}`},
		{"remove extra key set to null", `{"extra": {"foo": null, "z": 1}}`, removeProperty, []any{"extra.foo"}, `{"extra":{"z":1}}`},
		{"remove nested extra key set to null", `{"extra": {"foo": {"bar": null, "z": 1}}}`, removeProperty, []any{"extra.foo.bar"}, `{"extra":{"foo":{"z":1}}}`},
		{"remove link set to null", `{"require-dev": {"a/b": "1", "c/d": null}}`, removeLink, []any{"require-dev", "c/d"}, `{"require-dev":{"a\/b":"1"}}`},
		{"remove repository set to null", `{"repositories": {"foo": null, "bar": {"type": "vcs", "url": "x"}}}`, removeRepository, []any{"foo"}, `{"repositories":{"bar":{"type":"vcs","url":"x"}}}`},
		{"remove repository from list", `{"repositories": [{"name": "foo", "type": "vcs", "url": "x"}, {"name": "bar", "type": "vcs", "url": "y"}]}`, removeRepository, []any{"foo"}, `{"repositories":[{"name":"bar","type":"vcs","url":"y"}]}`},
		{"remove disabled packagist from list", `{"repositories": [{"packagist.org": false}, {"name": "bar", "type": "vcs", "url": "y"}]}`, removeRepository, []any{"packagist.org"}, `{"repositories":[{"name":"bar","type":"vcs","url":"y"}]}`},
		{"remove repository with a numeric name from list", `{"repositories": [{"name": "a", "type": "vcs", "url": "x"}, {"name": "b", "type": "vcs", "url": "y"}, {"name": "1", "type": "vcs", "url": "z"}]}`, removeRepository, []any{"1"}, `{"repositories":[{"name":"a","type":"vcs","url":"x"},{"name":"b","type":"vcs","url":"y"}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config := jcsWrite(t, c.original)
			check(t, c.op.run(jcsSource(t, config), c.args))
			assertCompactJSON(t, c.want, config)

			config = jcsWrite(t, c.original)
			check(t, jcsSource(t, config).rewrite(c.op.fallback, c.args))
			assertCompactJSON(t, c.want, config)
		})
	}
}

var linkCases = []struct{ typ, name, value, fixture string }{
	{"require", "my-vend/my-lib", "1.*", "require"},
	{"require-dev", "my-vend/my-lib-tests", "1.*", "require-dev"},
	{"provide", "my-vend/my-lib-interface", "1.*", "provide"},
	{"suggest", "my-vend/my-optional-extension", "1.*", "suggest"},
	{"replace", "my-vend/other-app", "1.*", "replace"},
	{"conflict", "my-vend/my-old-app", "1.*", "conflict"},
}

func TestJsonConfigSource_AddLink(t *testing.T) {
	befores := []struct{ suffix, file string }{
		{"empty", "composer-empty.json"},
		{"oneOfEverything", "composer-one-of-everything.json"},
		{"twoOfEverything", "composer-two-of-everything.json"},
	}
	for _, c := range linkCases {
		for _, before := range befores {
			t.Run(c.fixture+"-from-"+before.suffix, func(t *testing.T) {
				composerJSON := jcsCopy(t, fixturePath(before.file))
				check(t, jcsSource(t, composerJSON).AddLink(c.typ, c.name, c.value))

				assertFileEquals(t, fixturePath("addLink/"+c.fixture+"-from-"+before.suffix+".json"), composerJSON)
			})
		}
	}
}

func TestJsonConfigSource_RemoveLink(t *testing.T) {
	afters := []struct{ suffix, after string }{
		{"empty", ""},
		{"oneOfEverything", "composer-one-of-everything.json"},
		{"twoOfEverything", "composer-two-of-everything.json"},
	}
	for _, c := range linkCases {
		for _, a := range afters {
			base := "removeLink/" + c.fixture + "-to-" + a.suffix
			t.Run(base, func(t *testing.T) {
				after := fixturePath(base + "-after.json")
				if a.after != "" {
					after = fixturePath(a.after)
				}
				composerJSON := jcsCopy(t, fixturePath(base+".json"))
				check(t, jcsSource(t, composerJSON).RemoveLink(c.typ, c.name))

				assertFileEquals(t, after, composerJSON)
			})
		}
	}
}

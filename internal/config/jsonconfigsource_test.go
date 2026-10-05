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

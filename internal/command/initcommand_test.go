// Ports tests/Composer/Test/Command/InitCommandTest.php.

package command_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/util"
)

const defaultAuthors = `{"name": "John Smith", "email": "john@example.com"}`

// initSetUp ports setUp: $_SERVER['COMPOSER_DEFAULT_AUTHOR'/'_EMAIL'].
func initSetUp(t *testing.T) {
	t.Helper()
	t.Setenv("COMPOSER_DEFAULT_AUTHOR", "John Smith")
	t.Setenv("COMPOSER_DEFAULT_EMAIL", "john@example.com")
}

func TestInitCommand_ParseValidAuthorString(t *testing.T) {
	initSetUp(t)
	email := func(s string) *string { return &s }
	cases := []struct {
		name, author string
		email        *string
		input        string
	}{
		{"simple", "John Smith", email("john@example.com"), "John Smith <john@example.com>"},
		{"without email", "John Smith", nil, "John Smith"},
		{"UTF-8", "Matti Meikäläinen", email("matti@example.com"), "Matti Meikäläinen <matti@example.com>"},
		// \xCC\x88 is UTF-8 for U+0308 diaeresis (umlaut) combining mark
		{"UTF-8 with non-spacing marks", "Matti Meika\xCC\x88la\xCC\x88inen", email("matti@example.com"), "Matti Meika\xCC\x88la\xCC\x88inen <matti@example.com>"},
		{"numeric author name", "h4x0r", email("h4x@example.com"), "h4x0r <h4x@example.com>"},
		// https://github.com/composer/composer/issues/5631 Issue #5631
		{"alias 1", `Johnathon "Johnny" Smith`, email("john@example.com"), `Johnathon "Johnny" Smith <john@example.com>`},
		// https://github.com/composer/composer/issues/5631 Issue #5631
		{"alias 2", "Johnathon (Johnny) Smith", email("john@example.com"), "Johnathon (Johnny) Smith <john@example.com>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, mail, err := command.NewInitCommand().ParseAuthorString(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if name != tc.author || !reflect.DeepEqual(mail, tc.email) {
				t.Fatalf("got %q %v, want %q %v", name, mail, tc.author, tc.email)
			}
		})
	}
}

func initExpectInvalidArgument(t *testing.T, err error) {
	t.Helper()
	if _, ok := errors.AsType[*util.InvalidArgumentError](err); !ok {
		t.Fatalf("expected InvalidArgumentException, got %v", err)
	}
}

func TestInitCommand_ParseEmptyAuthorString(t *testing.T) {
	initSetUp(t)
	_, _, err := command.NewInitCommand().ParseAuthorString("")
	initExpectInvalidArgument(t, err)
}

func TestInitCommand_ParseAuthorStringWithInvalidEmail(t *testing.T) {
	initSetUp(t)
	_, _, err := command.NewInitCommand().ParseAuthorString("John Smith <john>")
	initExpectInvalidArgument(t, err)
}

func TestInitCommand_NamespaceFromValidPackageName(t *testing.T) {
	initSetUp(t)
	if ns := command.NewInitCommand().NamespaceFromPackageName("new_projects.acme-extra/package-name"); ns != `NewProjectsAcmeExtra\PackageName` {
		t.Fatalf("got %q", ns)
	}
}

func TestInitCommand_NamespaceFromInvalidPackageName(t *testing.T) {
	initSetUp(t)
	if ns := command.NewInitCommand().NamespaceFromPackageName("invalid-package-name"); ns != "" {
		t.Fatalf("got %q", ns)
	}
}

func TestInitCommand_NamespaceFromMissingPackageName(t *testing.T) {
	initSetUp(t)
	if ns := command.NewInitCommand().NamespaceFromPackageName(""); ns != "" {
		t.Fatalf("got %q", ns)
	}
}

// initDecodeJSON decodes JSON into plain Go values for an order-insensitive
// comparison (PHPUnit's assertEquals on arrays).
func initDecodeJSON(t *testing.T, data string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		t.Fatalf("%v: %s", err, data)
	}

	return v
}

func assertInitComposerJSON(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := initDecodeJSON(t, string(data)), initDecodeJSON(t, expected); !reflect.DeepEqual(got, want) {
		t.Fatalf("composer.json:\n%s\nwant:\n%s", data, expected)
	}
}

// initTempDirWithoutFiles is initTempComposer() minus composer.json and
// auth.json.
func initTempDirWithoutFiles(t *testing.T) string {
	t.Helper()
	dir := commandtest.InitTempComposer(t, nil, nil, nil, true)
	if err := os.Remove(dir + "/composer.json"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir + "/auth.json"); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestInitCommand_RunCommand(t *testing.T) {
	cases := []struct {
		name     string
		expected string
		args     []console.Param
	}{
		{
			"name argument", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {}}`,
			[]console.Param{console.P("--name", "test/pkg")},
		},
		{
			"name and author arguments", `{"name": "test/pkg", "require": {}, "authors": [{"name": "Mr. Test", "email": "test@example.org"}]}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--author", "Mr. Test <test@example.org>")},
		},
		{
			"name and author arguments without email", `{"name": "test/pkg", "require": {}, "authors": [{"name": "Mr. Test"}]}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--author", "Mr. Test")},
		},
		{
			"single repository argument", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {}, "repositories": [{"type": "vcs", "url": "http://packages.example.com"}]}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--repository", []any{`{"type":"vcs","url":"http://packages.example.com"}`})},
		},
		{
			"multiple repository arguments", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {}, "repositories": [
			{"type": "vcs", "url": "http://vcs.example.com"},
			{"type": "composer", "url": "http://composer.example.com"},
			{"type": "composer", "url": "http://composer2.example.com", "options": {"ssl": {"verify_peer": "true"}}}]}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--repository", []any{
				`{"type":"vcs","url":"http://vcs.example.com"}`,
				`{"type":"composer","url":"http://composer.example.com"}`,
				`{"type":"composer","url":"http://composer2.example.com","options":{"ssl":{"verify_peer":"true"}}}`,
			})},
		},
		{
			"stability argument", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {}, "minimum-stability": "dev"}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--stability", "dev")},
		},
		{
			"require one argument", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {"first/pkg": "1.0.0"}}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--require", []any{"first/pkg:1.0.0"})},
		},
		{
			"require multiple arguments", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {"first/pkg": "1.0.0", "second/pkg": "^3.4"}}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--require", []any{"first/pkg:1.0.0", "second/pkg:^3.4"})},
		},
		{
			"require-dev one argument", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {}, "require-dev": {"first/pkg": "1.0.0"}}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--require-dev", []any{"first/pkg:1.0.0"})},
		},
		{
			"require-dev multiple arguments", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {}, "require-dev": {"first/pkg": "1.0.0", "second/pkg": "^3.4"}}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--require-dev", []any{"first/pkg:1.0.0", "second/pkg:^3.4"})},
		},
		{
			"autoload argument", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {}, "autoload": {"psr-4": {"Test\\Pkg\\": "testMapping/"}}}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--autoload", "testMapping/")},
		},
		{
			"homepage argument", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {}, "homepage": "https://example.org/"}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--homepage", "https://example.org/")},
		},
		{
			"description argument", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {}, "description": "My first example package"}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--description", "My first example package")},
		},
		{
			"type argument", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {}, "type": "project"}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--type", "project")},
		},
		{
			"license argument", `{"name": "test/pkg", "authors": [` + defaultAuthors + `], "require": {}, "license": "MIT"}`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--license", "MIT")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			initSetUp(t)
			dir := initTempDirWithoutFiles(t)

			appTester := commandtest.GetApplicationTester(t)
			params := append([]console.Param{console.P("command", "init"), console.P("--no-interaction", true)}, tc.args...)
			if _, err := appTester.Run(params, commandtest.Options{}); err != nil {
				t.Fatal(err)
			}

			if code := appTester.StatusCode(); code != 0 {
				t.Fatalf("status %d: %s", code, appTester.Display(true))
			}
			assertInitComposerJSON(t, dir+"/composer.json", tc.expected)
		})
	}
}

func TestInitCommand_RunCommandInvalid(t *testing.T) {
	cases := []struct {
		name      string
		exception string // "" is none
		message   string // "" is none; a regexp when exception is ""
		args      []console.Param
	}{
		{
			"invalid name argument", command.ClassInvalidArgument, "",
			[]console.Param{console.P("--name", "test")},
		},
		{
			"invalid author argument", command.ClassInvalidArgument, "",
			[]console.Param{console.P("--name", "test/pkg"), console.P("--author", "Mr. Test <test>")},
		},
		{
			"invalid stability argument", "", `minimum-stability\s+:\s+Does not have a value in the enumeration`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--stability", "bogus")},
		},
		{
			"invalid require argument", command.ClassUnexpectedValue, "Option first is missing a version constraint, use e.g. first:^1.0",
			[]console.Param{console.P("--name", "test/pkg"), console.P("--require", []any{"first"})},
		},
		{
			"invalid require-dev argument", command.ClassUnexpectedValue, "Option first is missing a version constraint, use e.g. first:^1.0",
			[]console.Param{console.P("--name", "test/pkg"), console.P("--require-dev", []any{"first"})},
		},
		{
			"invalid homepage argument", "", `homepage\s*:\s*Invalid URL format`,
			[]console.Param{console.P("--name", "test/pkg"), console.P("--homepage", "not-a-url")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			initSetUp(t)
			initTempDirWithoutFiles(t)

			appTester := commandtest.GetApplicationTester(t)
			params := append([]console.Param{console.P("command", "init"), console.P("--no-interaction", true)}, tc.args...)
			_, err := appTester.Run(params, commandtest.Options{CaptureStderrSeparately: true})

			if tc.exception != "" {
				e, ok := errors.AsType[*command.Error](err)
				if !ok || e.Class != tc.exception {
					t.Fatalf("expected %s, got %v", tc.exception, err)
				}
				if tc.message != "" && !strings.Contains(e.Message, tc.message) {
					t.Fatalf("message %q does not contain %q", e.Message, tc.message)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if code := appTester.StatusCode(); code != 1 {
				t.Fatalf("status %d", code)
			}
			if out := appTester.ErrorOutput(false); !regexp.MustCompile(tc.message).MatchString(out) {
				t.Fatalf("error output %q does not match %s", out, tc.message)
			}
		})
	}
}

func TestInitCommand_RunGuessNameFromDirSanitizesDir(t *testing.T) {
	initSetUp(t)
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	dirName := "_foo_--bar__baz.--..qux__"
	if err := os.Mkdir(dirName, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dirName); err != nil {
		t.Fatal(err)
	}

	t.Setenv("COMPOSER_DEFAULT_VENDOR", ".vendorName")

	appTester := commandtest.GetApplicationTester(t)
	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "init", "--no-interaction", true); err != nil {
		t.Fatal(err)
	}

	if code := appTester.StatusCode(); code != 0 {
		t.Fatalf("status %d: %s", code, appTester.Display(true))
	}

	assertInitComposerJSON(t, "./composer.json", `{"name": "vendor-name/foo-bar_baz.qux", "authors": [`+defaultAuthors+`], "require": {}}`)
}

func TestInitCommand_InteractiveRun(t *testing.T) {
	initSetUp(t)
	dir := initTempDirWithoutFiles(t)

	appTester := commandtest.GetApplicationTester(t)

	appTester.SetInputs(
		"vendor/pkg",                  // Pkg name
		"my description",              // Description
		"Mr. Test <test@example.org>", // Author
		"stable",                      // Minimum stability
		"library",                     // Type
		"AGPL-3.0-only",               // License
		"no",                          // Define dependencies
		"no",                          // Define dev dependencies
		"n",                           // Add PSR-4 autoload mapping
		"",                            // Confirm generation
	)

	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "init"); err != nil {
		t.Fatal(err)
	}

	if code := appTester.StatusCode(); code != 0 {
		t.Fatalf("status %d: %s", code, appTester.Display(true))
	}

	assertInitComposerJSON(t, dir+"/composer.json", `{
		"name": "vendor/pkg",
		"description": "my description",
		"type": "library",
		"license": "AGPL-3.0-only",
		"authors": [{"name": "Mr. Test", "email": "test@example.org"}],
		"minimum-stability": "stable",
		"require": {}
	}`)
}

func TestInitCommand_FormatAuthors(t *testing.T) {
	initSetUp(t)
	cmd := command.NewInitCommand()
	authors, err := command.FormatAuthors(cmd, "John Smith <john@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := authors.GetArray(0); got == nil || got.Len() != 2 || initMustString(got, "name") != "John Smith" || initMustString(got, "email") != "john@example.com" {
		t.Fatalf("got %v", authors)
	}
	authors, err = command.FormatAuthors(cmd, "John Smith")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := authors.GetArray(0); got == nil || got.Len() != 1 || initMustString(got, "name") != "John Smith" {
		t.Fatalf("got %v", authors)
	}
}

func initMustString(a interface{ GetString(k any) (string, bool) }, key string) string {
	s, _ := a.GetString(key)

	return s
}

func TestInitCommand_GetGitConfig(t *testing.T) {
	initSetUp(t)
	// Composer's test relies on the developer's git configuration; give
	// git a deterministic global one.
	gitConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(gitConfig, []byte("[user]\n\tname = John Smith\n\temail = john@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)

	config := command.GitConfig(command.NewInitCommand())
	if _, ok := config["user.name"]; !ok {
		t.Fatalf("user.name missing from %v", config)
	}
	if _, ok := config["user.email"]; !ok {
		t.Fatalf("user.email missing from %v", config)
	}
}

func TestInitCommand_AddVendorIgnore(t *testing.T) {
	initSetUp(t)
	ignoreFile := commandtest.UniqueTmpDirectory(t) + "/ignore"
	command.AddVendorIgnore(ignoreFile, "/vendor/")
	content, err := os.ReadFile(ignoreFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "/vendor/") {
		t.Fatalf("got %q", content)
	}
}

func TestInitCommand_HasVendorIgnore(t *testing.T) {
	initSetUp(t)
	ignoreFile := commandtest.UniqueTmpDirectory(t) + "/ignore"
	if command.HasVendorIgnore(ignoreFile, "vendor") {
		t.Fatal("expected false")
	}
	command.AddVendorIgnore(ignoreFile, "/vendor/")
	if !command.HasVendorIgnore(ignoreFile, "vendor") {
		t.Fatal("expected true")
	}
}

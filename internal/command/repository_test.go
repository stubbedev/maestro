// Ports tests/Composer/Test/Command/RepositoryCommandTest.php.

package command_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
)

// jsonFileEquals is assertSame($expected, json_decode(file_get_contents($file), true)):
// expected is JSON decoded the same way; both are compared re-encoded.
func jsonFileEquals(t *testing.T, file, expected string) {
	t.Helper()
	data, err := os.ReadFile(file) //nolint:gosec // test file
	if err != nil {
		t.Fatal(err)
	}
	got := canonicalJSON(t, string(data))
	want := canonicalJSON(t, expected)
	if got != want {
		t.Errorf("%s:\n got  %s\n want %s", file, got, want)
	}
}

func canonicalJSON(t *testing.T, s string) string {
	t.Helper()
	v, err := php.JSONDecode(s, true)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	out, err := php.JSONEncode(v, php.JSONUnescapedSlashes)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func decodeJSONFile(t *testing.T, file string) *php.Array {
	t.Helper()
	data, err := os.ReadFile(file) //nolint:gosec // test file
	if err != nil {
		t.Fatal(err)
	}
	v, err := php.JSONDecode(string(data), true)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := v.(*php.Array)

	return a
}

// runOK runs a command and asserts it succeeded (assertCommandIsSuccessful).
func runOK(t *testing.T, kv ...any) string {
	t.Helper()

	return runCommandSuccessfully(t, commandtest.GetApplicationTester(t), kv...)
}

func TestRepositoryCommand_ListWithNoRepositories(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	display := runOK(t, "command", "repo", "action", "list")
	if got := strings.TrimSpace(display); got != "[packagist.org] composer https://repo.packagist.org" {
		t.Errorf("got %q", got)
	}
	// composer.json should remain unchanged
	jsonFileEquals(t, "composer.json", `[]`)
}

func TestRepositoryCommand_ListWithRepositoriesAsList(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": [
		{"type": "composer", "url": "https://first.test"},
		{"name": "foo", "type": "vcs", "url": "https://old.example.org"},
		{"name": "bar", "type": "vcs", "url": "https://other.example.org"}
	]}`, nil, nil, true)

	display := runOK(t, "command", "repo", "action", "list")
	want := `[0] composer https://first.test
[foo] vcs https://old.example.org
[bar] vcs https://other.example.org
[packagist.org] disabled`
	if got := strings.TrimSpace(display); got != want {
		t.Errorf("got %q", got)
	}
}

func TestRepositoryCommand_ListWithRepositoriesAsAssoc(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": {
		"0": {"type": "composer", "url": "https://first.test"},
		"foo": {"type": "vcs", "url": "https://old.example.org"},
		"bar": {"type": "vcs", "url": "https://other.example.org"}
	}}`, nil, nil, true)

	display := runOK(t, "command", "repo", "action", "list")
	want := `[0] composer https://first.test
[foo] vcs https://old.example.org
[bar] vcs https://other.example.org
[packagist.org] disabled`
	if got := strings.TrimSpace(display); got != want {
		t.Errorf("got %q", got)
	}
}

func TestRepositoryCommand_AddRepositoryWithTypeAndUrl(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	runOK(t, "command", "repo", "action", "add", "name", "foo", "arg1", "vcs", "arg2", "https://example.org/foo.git")
	jsonFileEquals(t, "composer.json", `{"repositories": [{"name": "foo", "type": "vcs", "url": "https://example.org/foo.git"}]}`)
}

func TestRepositoryCommand_AddRepositoryWithJson(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	runOK(t, "command", "repo", "action", "add", "name", "bar", "arg1", `{"type":"composer","url":"https://repo.example.org"}`)
	jsonFileEquals(t, "composer.json", `{"repositories": [{"name": "bar", "type": "composer", "url": "https://repo.example.org"}]}`)
}

func TestRepositoryCommand_RemoveRepository(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": {"foo": {"type": "vcs", "url": "https://example.org"}}}`, nil, nil, false)

	runOK(t, "command", "repo", "action", "remove", "name", "foo")

	json := decodeJSONFile(t, "composer.json")
	// repositories key may still exist as empty array depending on manipulator, accept either
	if repos, ok := json.Get("repositories"); ok {
		if a, ok := repos.(*php.Array); !ok || a.Len() != 0 {
			t.Errorf("repositories = %v", repos)
		}
	} else if json.Len() != 0 {
		t.Errorf("composer.json = %v", json)
	}
}

func TestRepositoryCommand_SetAndGetUrlInRepositoryAssoc(t *testing.T) {
	repositories := `{
		"first": {"type": "composer", "url": "https://first.test"},
		"foo": {"type": "vcs", "url": "https://old.example.org"},
		"bar": {"type": "vcs", "url": "https://other.example.org"}
	}`
	for _, tc := range []struct{ desc, name, index, newURL string }{
		{"change first of three", "first", "first", "https://new.example.org"},
		{"change middle of three", "foo", "foo", "https://new.example.org"},
		{"change last of three", "bar", "bar", "https://new.example.org"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			commandtest.InitTempComposer(t, `{"repositories": `+repositories+`}`, nil, nil, false)

			runOK(t, "command", "repo", "action", "set-url", "name", tc.name, "arg1", tc.newURL)

			// calling it still in assoc means, the repository has not been converted, which is good
			json := decodeJSONFile(t, "composer.json")
			repos, _ := json.GetArray("repositories")
			repo, _ := repos.GetArray(tc.index)
			if url, _ := repo.GetString("url"); url != tc.newURL {
				t.Errorf("url = %q", url)
			}

			if got := strings.TrimSpace(runOK(t, "command", "repo", "action", "get-url", "name", tc.name)); got != tc.newURL {
				t.Errorf("get-url = %q", got)
			}
		})
	}
}

func TestRepositoryCommand_SetAndGetUrlInRepositoryList(t *testing.T) {
	repositories := `[
		{"name": "first", "type": "composer", "url": "https://first.test"},
		{"name": "foo", "type": "vcs", "url": "https://old.example.org"},
		{"name": "bar", "type": "vcs", "url": "https://other.example.org"}
	]`
	for _, tc := range []struct {
		desc, name string
		index      int
		newURL     string
	}{
		{"change first of three", "first", 0, "https://new.example.org"},
		{"change middle of three", "foo", 1, "https://new.example.org"},
		{"change last of three", "bar", 2, "https://new.example.org"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			commandtest.InitTempComposer(t, `{"repositories": `+repositories+`}`, nil, nil, false)

			runOK(t, "command", "repo", "action", "set-url", "name", tc.name, "arg1", tc.newURL)

			json := decodeJSONFile(t, "composer.json")
			repos, _ := json.GetArray("repositories")
			repo, _ := repos.GetArray(tc.index)
			if name, _ := repo.GetString("name"); name != tc.name {
				t.Errorf("name = %q", name)
			}
			if url, _ := repo.GetString("url"); url != tc.newURL {
				t.Errorf("url = %q", url)
			}

			if got := strings.TrimSpace(runOK(t, "command", "repo", "action", "get-url", "name", tc.name)); got != tc.newURL {
				t.Errorf("get-url = %q", got)
			}
		})
	}
}

func TestRepositoryCommand_DisableAndEnablePackagist(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	runOK(t, "command", "repo", "action", "disable", "name", "packagist")
	jsonFileEquals(t, "composer.json", `{"repositories": [{"packagist.org": false}]}`)

	// enable packagist should remove the override
	runOK(t, "command", "repo", "action", "enable", "name", "packagist")
	jsonFileEquals(t, "composer.json", `[]`)
}

func TestRepositoryCommand_InvalidArgCombinationThrows(t *testing.T) {
	appTester := commandtest.GetApplicationTester(t)
	_, err := appTester.RunArgs(commandtest.Options{}, "command", "repo", "--file", "alt.composer.json", "--global", true)
	if err == nil || err.Error() != "--file and --global can not be combined" || phpClassOf(err) != "RuntimeException" {
		t.Fatalf("got %v", err)
	}
}

func TestRepositoryCommand_PrependRepositoryByNameListToAssoc(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": [{"type": "git", "url": "example.tld"}]}`, nil, nil, false)

	runOK(t, "command", "repo", "action", "add", "name", "foo", "arg1", "path", "arg2", "foo/bar")
	jsonFileEquals(t, "composer.json", `{"repositories": [
		{"name": "foo", "type": "path", "url": "foo/bar"},
		{"type": "git", "url": "example.tld"}
	]}`)
}

func TestRepositoryCommand_AppendRepositoryByNameListToAssoc(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": [{"type": "git", "url": "example.tld"}]}`, nil, nil, false)

	runOK(t, "command", "repo", "action", "add", "name", "foo", "arg1", "path", "arg2", "foo/bar", "--append", true)
	jsonFileEquals(t, "composer.json", `{"repositories": [
		{"type": "git", "url": "example.tld"},
		{"name": "foo", "type": "path", "url": "foo/bar"}
	]}`)
}

func TestRepositoryCommand_PrependRepositoryAssocWithPackagistDisabled(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": {"0": {"type": "git", "url": "example.tld"}, "packagist.org": false}}`, nil, nil, true)

	runOK(t, "command", "repo", "action", "add", "name", "foo", "arg1", "path", "arg2", "foo/bar")
	jsonFileEquals(t, "composer.json", `{"repositories": [
		{"name": "foo", "type": "path", "url": "foo/bar"},
		{"type": "git", "url": "example.tld"},
		{"packagist.org": false}
	]}`)
}

func TestRepositoryCommand_AppendRepositoryAssocWithPackagistDisabled(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": {"0": {"type": "git", "url": "example.tld"}, "packagist.org": false}}`, nil, nil, true)

	runOK(t, "command", "repo", "action", "add", "name", "foo", "arg1", "path", "arg2", "foo/bar", "--append", true)
	jsonFileEquals(t, "composer.json", `{"repositories": [
		{"type": "git", "url": "example.tld"},
		{"packagist.org": false},
		{"name": "foo", "type": "path", "url": "foo/bar"}
	]}`)
}

func TestRepositoryCommand_AddBeforeAndAfterByName(t *testing.T) {
	// Start with two repos as named-list and a disabled packagist boolean
	commandtest.InitTempComposer(t, `{"repositories": {
		"0": {"name": "alpha", "type": "vcs", "url": "https://example.org/a"},
		"1": {"name": "omega", "type": "vcs", "url": "https://example.org/o"},
		"packagist.org": false
	}}`, nil, nil, true)

	// Insert before omega
	runOK(t, "command", "repo", "action", "add", "name", "beta", "arg1", "vcs", "arg2", "https://example.org/b", "--before", "omega")
	// Insert after alpha
	runOK(t, "command", "repo", "action", "add", "name", "gamma", "arg1", "vcs", "arg2", "https://example.org/g", "--after", "alpha")

	// Expect order: alpha, gamma, beta, omega, then packagist.org boolean preserved
	jsonFileEquals(t, "composer.json", `{"repositories": [
		{"name": "alpha", "type": "vcs", "url": "https://example.org/a"},
		{"name": "gamma", "type": "vcs", "url": "https://example.org/g"},
		{"name": "beta", "type": "vcs", "url": "https://example.org/b"},
		{"name": "omega", "type": "vcs", "url": "https://example.org/o"},
		{"packagist.org": false}
	]}`)
}

func TestRepositoryCommand_AddSameNameReplacesExisting(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	// first add
	runOK(t, "command", "repo", "action", "add", "name", "foo", "arg1", "vcs", "arg2", "https://example.org/old")
	// second add with same name but different url
	runOK(t, "command", "repo", "action", "add", "name", "foo", "arg1", "vcs", "arg2", "https://example.org/new", "--append", true)

	json := decodeJSONFile(t, "composer.json")
	repos, _ := json.GetArray("repositories")

	// repositories can be stored as assoc or named-list depending on manipulator fallbacks
	// Validate there is only one "foo" and its url is the latest
	countFoo := 0
	var url string
	for k, repo := range repos.All() {
		r, isArray := repo.(*php.Array)
		if k.IsString() && k.String() == "foo" && isArray {
			countFoo++
			url, _ = r.GetString("url")
		} else if isArray {
			if n, _ := r.GetString("name"); n == "foo" {
				countFoo++
				url, _ = r.GetString("url")
			}
		}
	}
	if countFoo != 1 {
		t.Errorf("Exactly one repository entry with name foo should exist, got %d", countFoo)
	}
	if url != "https://example.org/new" {
		t.Errorf("The foo repository should have been updated to the new URL, got %q", url)
	}
}

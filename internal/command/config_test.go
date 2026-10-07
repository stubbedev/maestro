// Ports tests/Composer/Test/Command/ConfigCommandTest.php.

package command_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
)

// normalizedJSON decodes data as json_decode($data, true) does and
// re-encodes it, so two documents compare like PHP's assertSame on the
// decoded arrays (key order included).
func normalizedJSON(t *testing.T, data string) string {
	t.Helper()
	v, err := php.JSONDecode(data, true)
	if err != nil {
		t.Fatalf("invalid JSON %q: %v", data, err)
	}
	s, err := php.JSONEncode(v, 0)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func readComposerJSON(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("composer.json")
	if err != nil {
		t.Fatal(err)
	}

	return normalizedJSON(t, string(data))
}

func runConfig(t *testing.T, params []console.Param) (*commandtest.ApplicationTester, error) {
	t.Helper()
	appTester := commandtest.GetApplicationTester(t)
	_, err := appTester.Run(append([]console.Param{console.P("command", "config")}, params...), commandtest.Options{})

	return appTester, err
}

func TestConfigCommand_ConfigUpdates(t *testing.T) {
	cases := []struct {
		name     string
		before   string
		command  []console.Param
		expected string
	}{
		{"set scripts", `[]`, []console.Param{console.P("setting-key", "scripts.test"), console.P("setting-value", []string{"foo bar"})}, `{"scripts":{"test":"foo bar"}}`},
		{"unset scripts", `{"scripts":{"test":"foo bar","lala":"baz"}}`, []console.Param{console.P("setting-key", "scripts.lala"), console.P("--unset", true)}, `{"scripts":{"test":"foo bar"}}`},
		{"set single config with bool normalizer", `[]`, []console.Param{console.P("setting-key", "use-github-api"), console.P("setting-value", []string{"1"})}, `{"config":{"use-github-api":true}}`},
		{"set multi config", `[]`, []console.Param{console.P("setting-key", "github-protocols"), console.P("setting-value", []string{"https", "git"})}, `{"config":{"github-protocols":["https","git"]}}`},
		{"set version", `[]`, []console.Param{console.P("setting-key", "version"), console.P("setting-value", []string{"1.0.0"})}, `{"version":"1.0.0"}`},
		{"unset version", `{"version":"1.0.0"}`, []console.Param{console.P("setting-key", "version"), console.P("--unset", true)}, `[]`},
		{"unset arbitrary property", `{"random-prop":"1.0.0"}`, []console.Param{console.P("setting-key", "random-prop"), console.P("--unset", true)}, `[]`},
		{"set preferred-install", `[]`, []console.Param{console.P("setting-key", "preferred-install.foo/*"), console.P("setting-value", []string{"source"})}, `{"config":{"preferred-install":{"foo/*":"source"}}}`},
		{"unset preferred-install", `{"config":{"preferred-install":{"foo/*":"source"}}}`, []console.Param{console.P("setting-key", "preferred-install.foo/*"), console.P("--unset", true)}, `{"config":{"preferred-install":[]}}`},
		{"unset platform", `{"config":{"platform":{"php":"7.2.5"},"platform-check":false}}`, []console.Param{console.P("setting-key", "platform.php"), console.P("--unset", true)}, `{"config":{"platform":[],"platform-check":false}}`},
		{"set extra with merge", `[]`, []console.Param{console.P("setting-key", "extra.patches.foo/bar"), console.P("setting-value", []string{"{\"123\":\"value\"}"}), console.P("--json", true), console.P("--merge", true)}, `{"extra":{"patches":{"foo/bar":{"123":"value"}}}}`},
		{"combine extra with merge", `{"extra":{"patches":{"foo/bar":{"5":"oldvalue"}}}}`, []console.Param{console.P("setting-key", "extra.patches.foo/bar"), console.P("setting-value", []string{"{\"123\":\"value\"}"}), console.P("--json", true), console.P("--merge", true)}, `{"extra":{"patches":{"foo/bar":{"123":"value","5":"oldvalue"}}}}`},
		{"combine extra with list", `{"extra":{"patches":{"foo/bar":["oldvalue"]}}}`, []console.Param{console.P("setting-key", "extra.patches.foo/bar"), console.P("setting-value", []string{"{\"123\":\"value\"}"}), console.P("--json", true), console.P("--merge", true)}, `{"extra":{"patches":{"foo/bar":{"123":"value","0":"oldvalue"}}}}`},
		{"overwrite extra with merge", `{"extra":{"patches":{"foo/bar":{"123":"oldvalue"}}}}`, []console.Param{console.P("setting-key", "extra.patches.foo/bar"), console.P("setting-value", []string{"{\"123\":\"value\"}"}), console.P("--json", true), console.P("--merge", true)}, `{"extra":{"patches":{"foo/bar":{"123":"value"}}}}`},
		{"unset autoload", `{"autoload":{"psr-4":["test"],"classmap":["test"]}}`, []console.Param{console.P("setting-key", "autoload.psr-4"), console.P("--unset", true)}, `{"autoload":{"classmap":["test"]}}`},
		{"unset autoload-dev", `{"autoload-dev":{"psr-4":["test"],"classmap":["test"]}}`, []console.Param{console.P("setting-key", "autoload-dev.psr-4"), console.P("--unset", true)}, `{"autoload-dev":{"classmap":["test"]}}`},
		{"set audit.ignore-unreachable", `[]`, []console.Param{console.P("setting-key", "audit.ignore-unreachable"), console.P("setting-value", []string{"true"})}, `{"config":{"audit":{"ignore-unreachable":true}}}`},
		{"set audit.block-insecure", `[]`, []console.Param{console.P("setting-key", "audit.block-insecure"), console.P("setting-value", []string{"false"})}, `{"config":{"audit":{"block-insecure":false}}}`},
		{"set audit.block-abandoned", `[]`, []console.Param{console.P("setting-key", "audit.block-abandoned"), console.P("setting-value", []string{"true"})}, `{"config":{"audit":{"block-abandoned":true}}}`},
		{"unset audit.ignore-unreachable", `{"config":{"audit":{"ignore-unreachable":true}}}`, []console.Param{console.P("setting-key", "audit.ignore-unreachable"), console.P("--unset", true)}, `{"config":{"audit":[]}}`},
		{"set audit.ignore-severity", `[]`, []console.Param{console.P("setting-key", "audit.ignore-severity"), console.P("setting-value", []string{"low", "medium"})}, `{"config":{"audit":{"ignore-severity":["low","medium"]}}}`},
		{"set audit.ignore as array", `[]`, []console.Param{console.P("setting-key", "audit.ignore"), console.P("setting-value", []string{"[\"CVE-2024-1234\",\"GHSA-xxxx-yyyy\"]"}), console.P("--json", true)}, `{"config":{"audit":{"ignore":["CVE-2024-1234","GHSA-xxxx-yyyy"]}}}`},
		{"set audit.ignore as object", `[]`, []console.Param{console.P("setting-key", "audit.ignore"), console.P("setting-value", []string{"{\"CVE-2024-1234\":\"False positive\",\"GHSA-xxxx-yyyy\":\"Not applicable\"}"}), console.P("--json", true)}, `{"config":{"audit":{"ignore":{"CVE-2024-1234":"False positive","GHSA-xxxx-yyyy":"Not applicable"}}}}`},
		{"merge audit.ignore array", `{"config":{"audit":{"ignore":["CVE-2024-1234"]}}}`, []console.Param{console.P("setting-key", "audit.ignore"), console.P("setting-value", []string{"[\"CVE-2024-5678\"]"}), console.P("--json", true), console.P("--merge", true)}, `{"config":{"audit":{"ignore":["CVE-2024-1234","CVE-2024-5678"]}}}`},
		{"merge audit.ignore object", `{"config":{"audit":{"ignore":{"CVE-2024-1234":"Old reason"}}}}`, []console.Param{console.P("setting-key", "audit.ignore"), console.P("setting-value", []string{"{\"CVE-2024-5678\":\"New advisory\"}"}), console.P("--json", true), console.P("--merge", true)}, `{"config":{"audit":{"ignore":{"CVE-2024-5678":"New advisory","CVE-2024-1234":"Old reason"}}}}`},
		{"overwrite audit.ignore key with merge", `{"config":{"audit":{"ignore":{"CVE-2024-1234":"Old reason"}}}}`, []console.Param{console.P("setting-key", "audit.ignore"), console.P("setting-value", []string{"{\"CVE-2024-1234\":\"New reason\"}"}), console.P("--json", true), console.P("--merge", true)}, `{"config":{"audit":{"ignore":{"CVE-2024-1234":"New reason"}}}}`},
		{"set audit.ignore-abandoned as array", `[]`, []console.Param{console.P("setting-key", "audit.ignore-abandoned"), console.P("setting-value", []string{"[\"vendor/package1\",\"vendor/package2\"]"}), console.P("--json", true)}, `{"config":{"audit":{"ignore-abandoned":["vendor/package1","vendor/package2"]}}}`},
		{"set audit.ignore-abandoned as object", `[]`, []console.Param{console.P("setting-key", "audit.ignore-abandoned"), console.P("setting-value", []string{"{\"vendor/package1\":\"Still maintained\",\"vendor/package2\":\"Fork available\"}"}), console.P("--json", true)}, `{"config":{"audit":{"ignore-abandoned":{"vendor/package1":"Still maintained","vendor/package2":"Fork available"}}}}`},
		{"merge audit.ignore-abandoned array", `{"config":{"audit":{"ignore-abandoned":["vendor/package1"]}}}`, []console.Param{console.P("setting-key", "audit.ignore-abandoned"), console.P("setting-value", []string{"[\"vendor/package2\"]"}), console.P("--json", true), console.P("--merge", true)}, `{"config":{"audit":{"ignore-abandoned":["vendor/package1","vendor/package2"]}}}`},
		{"merge audit.ignore-abandoned object", `{"config":{"audit":{"ignore-abandoned":{"vendor/package1":"Old reason"}}}}`, []console.Param{console.P("setting-key", "audit.ignore-abandoned"), console.P("setting-value", []string{"{\"vendor/package2\":\"New reason\"}"}), console.P("--json", true), console.P("--merge", true)}, `{"config":{"audit":{"ignore-abandoned":{"vendor/package2":"New reason","vendor/package1":"Old reason"}}}}`},
		{"unset audit.ignore", `{"config":{"audit":{"ignore":["CVE-2024-1234"]}}}`, []console.Param{console.P("setting-key", "audit.ignore"), console.P("--unset", true)}, `{"config":{"audit":[]}}`},
		{"unset audit.ignore-abandoned", `{"config":{"audit":{"ignore-abandoned":["vendor/package1"]}}}`, []console.Param{console.P("setting-key", "audit.ignore-abandoned"), console.P("--unset", true)}, `{"config":{"audit":[]}}`},
		{"unset policy", `{"config":{"policy":{"advisories":false}}}`, []console.Param{console.P("setting-key", "policy"), console.P("--unset", true)}, `{"config":[]}`},
		{"set policy.advisories.block false via 0", `[]`, []console.Param{console.P("setting-key", "policy.advisories.block"), console.P("setting-value", []string{"0"})}, `{"config":{"policy":{"advisories":{"block":false}}}}`},
		{"set policy.advisories.block true via 1", `[]`, []console.Param{console.P("setting-key", "policy.advisories.block"), console.P("setting-value", []string{"1"})}, `{"config":{"policy":{"advisories":{"block":true}}}}`},
		{"set policy.advisories.audit", `[]`, []console.Param{console.P("setting-key", "policy.advisories.audit"), console.P("setting-value", []string{"report"})}, `{"config":{"policy":{"advisories":{"audit":"report"}}}}`},
		{"set policy.malware.block false", `[]`, []console.Param{console.P("setting-key", "policy.malware.block"), console.P("setting-value", []string{"false"})}, `{"config":{"policy":{"malware":{"block":false}}}}`},
		{"set policy.malware.block-scope", `[]`, []console.Param{console.P("setting-key", "policy.malware.block-scope"), console.P("setting-value", []string{"install"})}, `{"config":{"policy":{"malware":{"block-scope":"install"}}}}`},
		{"set policy.malware.audit", `[]`, []console.Param{console.P("setting-key", "policy.malware.audit"), console.P("setting-value", []string{"ignore"})}, `{"config":{"policy":{"malware":{"audit":"ignore"}}}}`},
		{"set policy.abandoned.block true", `[]`, []console.Param{console.P("setting-key", "policy.abandoned.block"), console.P("setting-value", []string{"true"})}, `{"config":{"policy":{"abandoned":{"block":true}}}}`},
		{"set policy.abandoned.audit", `[]`, []console.Param{console.P("setting-key", "policy.abandoned.audit"), console.P("setting-value", []string{"fail"})}, `{"config":{"policy":{"abandoned":{"audit":"fail"}}}}`},
		{"set policy.ignore-unreachable bool true", `[]`, []console.Param{console.P("setting-key", "policy.ignore-unreachable"), console.P("setting-value", []string{"true"})}, `{"config":{"policy":{"ignore-unreachable":true}}}`},
		{"set policy.ignore-unreachable bool false", `[]`, []console.Param{console.P("setting-key", "policy.ignore-unreachable"), console.P("setting-value", []string{"false"})}, `{"config":{"policy":{"ignore-unreachable":false}}}`},
		{"set policy.ignore-unreachable positional single enum value", `[]`, []console.Param{console.P("setting-key", "policy.ignore-unreachable"), console.P("setting-value", []string{"update"})}, `{"config":{"policy":{"ignore-unreachable":["update"]}}}`},
		{"set policy.ignore-unreachable positional multi enum values", `[]`, []console.Param{console.P("setting-key", "policy.ignore-unreachable"), console.P("setting-value", []string{"update", "install"})}, `{"config":{"policy":{"ignore-unreachable":["update","install"]}}}`},
		{"set policy.ignore-unreachable via --json array form", `[]`, []console.Param{console.P("setting-key", "policy.ignore-unreachable"), console.P("setting-value", []string{"[\"update\",\"install\"]"}), console.P("--json", true)}, `{"config":{"policy":{"ignore-unreachable":["update","install"]}}}`},
		{"set policy.advisories.block alongside existing audit setting", `{"config":{"policy":{"advisories":{"audit":"report"}}}}`, []console.Param{console.P("setting-key", "policy.advisories.block"), console.P("setting-value", []string{"false"})}, `{"config":{"policy":{"advisories":{"audit":"report","block":false}}}}`},
		{"set policy.malware.block alongside existing advisories", `{"config":{"policy":{"advisories":{"block":false}}}}`, []console.Param{console.P("setting-key", "policy.malware.block"), console.P("setting-value", []string{"true"})}, `{"config":{"policy":{"advisories":{"block":false},"malware":{"block":true}}}}`},
		{"set custom policy list block", `[]`, []console.Param{console.P("setting-key", "policy.my-list.block"), console.P("setting-value", []string{"true"})}, `{"config":{"policy":{"my-list":{"block":true}}}}`},
		{"set custom policy list audit", `[]`, []console.Param{console.P("setting-key", "policy.my-list.audit"), console.P("setting-value", []string{"report"})}, `{"config":{"policy":{"my-list":{"audit":"report"}}}}`},
		{"unset policy.advisories.block leaves siblings", `{"config":{"policy":{"advisories":{"block":false,"audit":"fail"}}}}`, []console.Param{console.P("setting-key", "policy.advisories.block"), console.P("--unset", true)}, `{"config":{"policy":{"advisories":{"audit":"fail"}}}}`},
		{"unset policy.ignore-unreachable leaves siblings", `{"config":{"policy":{"ignore-unreachable":true,"advisories":{"block":true}}}}`, []console.Param{console.P("setting-key", "policy.ignore-unreachable"), console.P("--unset", true)}, `{"config":{"policy":{"advisories":{"block":true}}}}`},
		{"unset last policy list sub-key removes the list", `{"config":{"policy":{"advisories":{"block":false}}}}`, []console.Param{console.P("setting-key", "policy.advisories.block"), console.P("--unset", true)}, `{"config":{"policy":[]}}`},
		{"unset last sub-key of list keeps sibling lists", `{"config":{"policy":{"advisories":{"block":false},"malware":{"block":true}}}}`, []console.Param{console.P("setting-key", "policy.advisories.block"), console.P("--unset", true)}, `{"config":{"policy":{"malware":{"block":true}}}}`},
		{"unset only policy.ignore-unreachable", `{"config":{"policy":{"ignore-unreachable":true}}}`, []console.Param{console.P("setting-key", "policy.ignore-unreachable"), console.P("--unset", true)}, `{"config":{"policy":[]}}`},
		{"set policy.advisories.ignore as array", `[]`, []console.Param{console.P("setting-key", "policy.advisories.ignore"), console.P("setting-value", []string{"[\"CVE-2024-1234\"]"}), console.P("--json", true)}, `{"config":{"policy":{"advisories":{"ignore":["CVE-2024-1234"]}}}}`},
		{"set policy.advisories.ignore as object", `[]`, []console.Param{console.P("setting-key", "policy.advisories.ignore"), console.P("setting-value", []string{"{\"CVE-2024-1234\":\"False positive\"}"}), console.P("--json", true)}, `{"config":{"policy":{"advisories":{"ignore":{"CVE-2024-1234":"False positive"}}}}}`},
		{"merge policy.advisories.ignore array", `{"config":{"policy":{"advisories":{"ignore":["CVE-2024-1234"]}}}}`, []console.Param{console.P("setting-key", "policy.advisories.ignore"), console.P("setting-value", []string{"[\"CVE-2024-5678\"]"}), console.P("--json", true), console.P("--merge", true)}, `{"config":{"policy":{"advisories":{"ignore":["CVE-2024-1234","CVE-2024-5678"]}}}}`},
		{"merge policy.advisories.ignore object", `{"config":{"policy":{"advisories":{"ignore":{"CVE-2024-1234":"Old reason"}}}}}`, []console.Param{console.P("setting-key", "policy.advisories.ignore"), console.P("setting-value", []string{"{\"CVE-2024-5678\":\"New advisory\"}"}), console.P("--json", true), console.P("--merge", true)}, `{"config":{"policy":{"advisories":{"ignore":{"CVE-2024-5678":"New advisory","CVE-2024-1234":"Old reason"}}}}}`},
		{"set policy.advisories.ignore-severity", `[]`, []console.Param{console.P("setting-key", "policy.advisories.ignore-severity"), console.P("setting-value", []string{"low", "medium"})}, `{"config":{"policy":{"advisories":{"ignore-severity":["low","medium"]}}}}`},
		{"set policy.advisories.ignore-id as array", `[]`, []console.Param{console.P("setting-key", "policy.advisories.ignore-id"), console.P("setting-value", []string{"[\"CVE-2024-1234\",\"GHSA-xxxx-yyyy\"]"}), console.P("--json", true)}, `{"config":{"policy":{"advisories":{"ignore-id":["CVE-2024-1234","GHSA-xxxx-yyyy"]}}}}`},
		{"set policy.malware.ignore as array", `[]`, []console.Param{console.P("setting-key", "policy.malware.ignore"), console.P("setting-value", []string{"[\"vendor/pkg\"]"}), console.P("--json", true)}, `{"config":{"policy":{"malware":{"ignore":["vendor/pkg"]}}}}`},
		{"set policy.malware.ignore-source", `[]`, []console.Param{console.P("setting-key", "policy.malware.ignore-source"), console.P("setting-value", []string{"source-a", "source-b"})}, `{"config":{"policy":{"malware":{"ignore-source":["source-a","source-b"]}}}}`},
		{"set policy.abandoned.ignore as array", `[]`, []console.Param{console.P("setting-key", "policy.abandoned.ignore"), console.P("setting-value", []string{"[\"vendor/pkg\"]"}), console.P("--json", true)}, `{"config":{"policy":{"abandoned":{"ignore":["vendor/pkg"]}}}}`},
		{"set policy.ignore-unreachable as array via json", `[]`, []console.Param{console.P("setting-key", "policy.ignore-unreachable"), console.P("setting-value", []string{"[\"install\",\"update\"]"}), console.P("--json", true)}, `{"config":{"policy":{"ignore-unreachable":["install","update"]}}}`},
		{"set custom policy list ignore", `[]`, []console.Param{console.P("setting-key", "policy.my-list.ignore"), console.P("setting-value", []string{"[\"vendor/pkg\"]"}), console.P("--json", true)}, `{"config":{"policy":{"my-list":{"ignore":["vendor/pkg"]}}}}`},
		{"set policy.malware false disables whole list", `[]`, []console.Param{console.P("setting-key", "policy.malware"), console.P("setting-value", []string{"false"})}, `{"config":{"policy":{"malware":false}}}`},
		{"set policy.advisories true via 1 enables whole list", `[]`, []console.Param{console.P("setting-key", "policy.advisories"), console.P("setting-value", []string{"1"})}, `{"config":{"policy":{"advisories":true}}}`},
		{"set policy.abandoned false", `[]`, []console.Param{console.P("setting-key", "policy.abandoned"), console.P("setting-value", []string{"false"})}, `{"config":{"policy":{"abandoned":false}}}`},
		{"set policy.<list> false overwrites existing object", `{"config":{"policy":{"malware":{"block":true,"audit":"fail"}}}}`, []console.Param{console.P("setting-key", "policy.malware"), console.P("setting-value", []string{"false"})}, `{"config":{"policy":{"malware":false}}}`},
		{"set policy.<builtin> false preserves sibling lists", `{"config":{"policy":{"advisories":{"block":true,"audit":"fail"},"malware":{"block":true},"ignore-unreachable":true}}}`, []console.Param{console.P("setting-key", "policy.advisories"), console.P("setting-value", []string{"false"})}, `{"config":{"policy":{"advisories":false,"malware":{"block":true},"ignore-unreachable":true}}}`},
		{"set custom policy list false", `[]`, []console.Param{console.P("setting-key", "policy.my-custom"), console.P("setting-value", []string{"false"})}, `{"config":{"policy":{"my-custom":false}}}`},
		{"unset policy.<list> removes the list entry", `{"config":{"policy":{"malware":false,"advisories":{"block":true}}}}`, []console.Param{console.P("setting-key", "policy.malware"), console.P("--unset", true)}, `{"config":{"policy":{"advisories":{"block":true}}}}`},
		{"unset only policy.<list>", `{"config":{"policy":{"malware":false}}}`, []console.Param{console.P("setting-key", "policy.malware"), console.P("--unset", true)}, `{"config":{"policy":[]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, tc.before, nil, nil, true)

			appTester, err := runConfig(t, tc.command)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if appTester.StatusCode() != 0 {
				t.Fatalf("status %d: %s", appTester.StatusCode(), appTester.Display(false))
			}

			if got, want := readComposerJSON(t), normalizedJSON(t, tc.expected); got != want {
				t.Errorf("composer.json\n got %s\nwant %s", got, want)
			}
		})
	}
}

func TestConfigCommand_ConfigReads(t *testing.T) {
	cases := []struct {
		name         string
		composerJSON string
		command      []console.Param
		expected     string
	}{
		{"read description", `{"description":"foo bar"}`, []console.Param{console.P("setting-key", "description")}, "foo bar"},
		{"read vendor-dir with source", `{"config":{"vendor-dir":"lala"}}`, []console.Param{console.P("setting-key", "vendor-dir"), console.P("--source", true)}, "lala (./composer.json)"},
		{"read default vendor-dir", `[]`, []console.Param{console.P("setting-key", "vendor-dir")}, "vendor"},
		{"read repos by named key", `{"repositories":{"foo":{"type":"vcs","url":"https://example.org"},"packagist.org":{"type":"composer","url":"https://repo.packagist.org"}}}`, []console.Param{console.P("setting-key", "repositories.foo")}, "{\"type\":\"vcs\",\"url\":\"https://example.org\"}"},
		{"read repos by numeric index", `{"repositories":{"0":{"type":"vcs","url":"https://example.org"},"packagist.org":{"type":"composer","url":"https://repo.packagist.org"}}}`, []console.Param{console.P("setting-key", "repos.0")}, "{\"type\":\"vcs\",\"url\":\"https://example.org\"}"},
		{"read all repos includes the default packagist", `{"repositories":{"foo":{"type":"vcs","url":"https://example.org"},"packagist.org":{"type":"composer","url":"https://repo.packagist.org"}}}`, []console.Param{console.P("setting-key", "repos")}, "{\"foo\":{\"type\":\"vcs\",\"url\":\"https://example.org\"},\"packagist.org\":{\"type\":\"composer\",\"url\":\"https://repo.packagist.org\"}}"},
		{"read all repos does not include the disabled packagist", `{"repositories":{"foo":{"type":"vcs","url":"https://example.org"},"packagist.org":false}}`, []console.Param{console.P("setting-key", "repos")}, "{\"foo\":{\"type\":\"vcs\",\"url\":\"https://example.org\"}}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, tc.composerJSON, nil, nil, true)

			appTester, err := runConfig(t, tc.command)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if appTester.StatusCode() != 0 {
				t.Fatalf("status %d: %s", appTester.StatusCode(), appTester.Display(false))
			}

			if got := php.Trim(appTester.Display(true)); got != tc.expected {
				t.Errorf("display %q, want %q", got, tc.expected)
			}
			if got, want := readComposerJSON(t), normalizedJSON(t, tc.composerJSON); got != want {
				t.Errorf("The composer.json should not be modified by config reads: %s", got)
			}
		})
	}
}

func expectRuntimeError(t *testing.T, err error, message string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an exception")
	}
	if !strings.Contains(err.Error(), message) {
		t.Fatalf("exception %q does not contain %q", err.Error(), message)
	}
}

func TestConfigCommand_ConfigThrowsForInvalidArgCombination(t *testing.T) {
	_, err := runConfig(t, []console.Param{console.P("--file", "alt.composer.json"), console.P("--global", true)})
	expectRuntimeError(t, err, "--file and --global can not be combined")
}

func TestConfigCommand_ConfigThrowsForInvalidSeverity(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	_, err := runConfig(t, []console.Param{console.P("setting-key", "audit.ignore-severity"), console.P("setting-value", []string{"low", "invalid"})})
	expectRuntimeError(t, err, "valid severities include: low, medium, high, critical")
}

func TestConfigCommand_ConfigThrowsWhenMergingArrayWithObject(t *testing.T) {
	commandtest.InitTempComposer(t, `{"config":{"audit":{"ignore":["CVE-2024-1234"]}}}`, nil, nil, true)
	_, err := runConfig(t, []console.Param{console.P("setting-key", "audit.ignore"), console.P("setting-value", []string{`{"CVE-2024-5678":"reason"}`}), console.P("--json", true), console.P("--merge", true)})
	expectRuntimeError(t, err, "Cannot merge array and object")
}

func TestConfigCommand_ConfigThrowsWhenMergingPolicyArrayWithObject(t *testing.T) {
	commandtest.InitTempComposer(t, `{"config":{"policy":{"advisories":{"ignore":["CVE-2024-1234"]}}}}`, nil, nil, true)
	_, err := runConfig(t, []console.Param{console.P("setting-key", "policy.advisories.ignore"), console.P("setting-value", []string{`{"CVE-2024-5678":"reason"}`}), console.P("--json", true), console.P("--merge", true)})
	expectRuntimeError(t, err, "Cannot merge array and object")
}

func TestConfigCommand_ConfigThrowsForInvalidPolicyAuditMode(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	_, err := runConfig(t, []console.Param{console.P("setting-key", "policy.advisories.audit"), console.P("setting-value", []string{"bogus"})})
	expectRuntimeError(t, err, "")
}

func TestConfigCommand_ConfigThrowsForInvalidPolicyBlockScope(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	_, err := runConfig(t, []console.Param{console.P("setting-key", "policy.malware.block-scope"), console.P("setting-value", []string{"bogus"})})
	expectRuntimeError(t, err, "")
}

func TestConfigCommand_ConfigThrowsForInvalidPolicyIgnoreSeverity(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	_, err := runConfig(t, []console.Param{console.P("setting-key", "policy.advisories.ignore-severity"), console.P("setting-value", []string{"low", "bogus"})})
	expectRuntimeError(t, err, "valid severities include: low, medium, high, critical")
}

func TestConfigCommand_ConfigThrowsForInvalidPolicyIgnoreUnreachableValue(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	_, err := runConfig(t, []console.Param{console.P("setting-key", "policy.ignore-unreachable"), console.P("setting-value", []string{`["bogus"]`}), console.P("--json", true)})
	expectRuntimeError(t, err, "")
}

func TestConfigCommand_ConfigThrowsForInvalidPolicyListBoolValue(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	_, err := runConfig(t, []console.Param{console.P("setting-key", "policy.malware"), console.P("setting-value", []string{"bogus"})})
	expectRuntimeError(t, err, "expected a boolean")
}

func TestConfigCommand_ConfigThrowsPolicyListReserved(t *testing.T) {
	cases := []struct{ expectedMessage, settingKey, settingValue string }{
		{`reserved prefix "ignore"`, "policy.ignore-foo", "true"},
		{`reserved prefix "ignore"`, "policy.ignore-foo.block", "true"},
		{"reserved for future use", "policy.support.audit", "fail"},
		{`reserved prefix "ignore"`, "policy.ignore-foo.ignore", `["CVE-2024-1234"]`},
	}
	for _, tc := range cases {
		t.Run(tc.settingKey, func(t *testing.T) {
			commandtest.InitTempComposer(t, nil, nil, nil, true)
			_, err := runConfig(t, []console.Param{console.P("setting-key", tc.settingKey), console.P("setting-value", []string{tc.settingValue})})
			expectRuntimeError(t, err, tc.expectedMessage)
		})
	}
}

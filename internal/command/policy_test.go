// Ports tests/Composer/Test/Command/PolicyCommandTest.php.

package command_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

const oneSourceJSON = `{"config": {"policy": {"my-list": {"sources": [{"type": "url", "url": "https://example.org/list.json"}]}}}}`

// expectRunError runs a command expecting an exception of class whose
// message contains message.
func expectRunError(t *testing.T, class, message string, kv ...any) {
	t.Helper()
	_, err := commandtest.GetApplicationTester(t).RunArgs(commandtest.Options{}, kv...)
	if err == nil {
		t.Fatalf("expected %s %q, got no error", class, message)
	}
	if !strings.Contains(err.Error(), message) {
		t.Errorf("message %q does not contain %q", err.Error(), message)
	}
	if !isPHPInstance(err, class) {
		t.Errorf("exception %s is not a %s", phpClassOf(err), class)
	}
}

// isPHPInstance is $e instanceof $class for the SPL classes the tests expect.
func isPHPInstance(err error, class string) bool {
	switch class {
	case "RuntimeException":
		return util.IsRuntimeException(err)
	case "InvalidArgumentException":
		got := phpClassOf(err)
		return got == "InvalidArgumentException" || strings.HasSuffix(got, `\InvalidArgumentException`) || strings.HasSuffix(got, "NotFoundException")
	}

	return phpClassOf(err) == class
}

func TestPolicyCommand_AddSourceCreatesNewList(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	runOK(t, "command", "policy", "action", "add-source", "name", "my-list", "arg1", "url", "arg2", "https://example.org/list.json")
	jsonFileEquals(t, "composer.json", oneSourceJSON)
}

func TestPolicyCommand_AddSourceAppendsToExistingList(t *testing.T) {
	commandtest.InitTempComposer(t, `{"config": {"policy": {"my-list": {"block": true, "sources": [{"type": "url", "url": "https://first.example.org/list.json"}]}}}}`, nil, nil, true)
	runOK(t, "command", "policy", "action", "add-source", "name", "my-list", "arg1", "url", "arg2", "https://second.example.org/list.json")
	jsonFileEquals(t, "composer.json", `{"config": {"policy": {"my-list": {"block": true, "sources": [
		{"type": "url", "url": "https://first.example.org/list.json"},
		{"type": "url", "url": "https://second.example.org/list.json"}
	]}}}}`)
}

func TestPolicyCommand_AddSourceIsNoopWhenUrlAlreadyPresent(t *testing.T) {
	commandtest.InitTempComposer(t, oneSourceJSON, nil, nil, true)
	display := runOK(t, "command", "policy", "action", "add-source", "name", "my-list", "arg1", "url", "arg2", "https://example.org/list.json")
	if !strings.Contains(display, "already present") {
		t.Errorf("display %q", display)
	}
	sources, _ := nestedArray(decodeJSONFile(t, "composer.json"), "config", "policy", "my-list", "sources")
	if sources.Len() != 1 {
		t.Errorf("sources = %d", sources.Len())
	}
}

func nestedArray(a *php.Array, keys ...string) (*php.Array, bool) {
	for _, k := range keys {
		var ok bool
		if a, ok = a.GetArray(k); !ok {
			return php.NewArray(), false
		}
	}

	return a, true
}

func TestPolicyCommand_AddSourceWithJson(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	runOK(t, "command", "policy", "action", "add-source", "name", "my-list", "arg1", `{"type":"url","url":"https://example.org/list.json"}`)
	jsonFileEquals(t, "composer.json", oneSourceJSON)
}

func TestPolicyCommand_AddSourceWithGlobalFlagWritesToHomeConfigJson(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	runOK(t, "command", "policy", "action", "add-source", "name", "my-list", "arg1", "url", "arg2", "https://example.org/list.json", "--global", true)

	home, _ := util.GetEnv("COMPOSER_HOME")
	globalConfigPath := home + "/config.json"
	if _, err := os.Stat(globalConfigPath); err != nil {
		t.Fatal(err)
	}
	jsonFileEquals(t, globalConfigPath, oneSourceJSON)

	// local composer.json should be untouched
	jsonFileEquals(t, "composer.json", `[]`)
}

func TestPolicyCommand_AddSourceWithFileFlagWritesToCustomFile(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	if err := os.WriteFile("alt.composer.json", []byte("{\n}\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	runOK(t, "command", "policy", "action", "add-source", "name", "my-list", "arg1", "url", "arg2", "https://example.org/list.json", "--file", "alt.composer.json")
	jsonFileEquals(t, "alt.composer.json", oneSourceJSON)

	// primary composer.json should be untouched
	jsonFileEquals(t, "composer.json", `[]`)
}

func TestPolicyCommand_AddSourceRejectsBuiltInListName(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	expectRunError(t, "RuntimeException", `Built-in dependency policy "advisories" does not support sources`,
		"command", "policy", "action", "add-source", "name", "advisories", "arg1", "url", "arg2", "https://example.org/list.json")
}

func TestPolicyCommand_AddSourceRejectsIgnoreUnreachableName(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	expectRunError(t, "RuntimeException", `reserved prefix "ignore"`,
		"command", "policy", "action", "add-source", "name", "ignore-unreachable", "arg1", "url", "arg2", "https://example.org/list.json")
}

func TestPolicyCommand_AddSourceRejectsNonHttpsUrl(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	expectRunError(t, "RuntimeException", `must start with "https://"`,
		"command", "policy", "action", "add-source", "name", "my-list", "arg1", "url", "arg2", "http://insecure.example.org/list.json")
}

func TestPolicyCommand_AddSourceRejectsUnsupportedType(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	expectRunError(t, "RuntimeException", "Unsupported source type",
		"command", "policy", "action", "add-source", "name", "my-list", "arg1", "file", "arg2", "https://example.org/list.json")
}

func TestPolicyCommand_AddSourceRejectsNameContainingDot(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	expectRunError(t, "RuntimeException", `Invalid dependency policy name "bad.name"`,
		"command", "policy", "action", "add-source", "name", "bad.name", "arg1", "url", "arg2", "https://example.org/list.json")
}

func TestPolicyCommand_AddSourceRejectsJsonMissingUrl(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	expectRunError(t, "RuntimeException", `missing a string "url"`,
		"command", "policy", "action", "add-source", "name", "my-list", "arg1", `{"type":"url"}`)
}

func TestPolicyCommand_UnknownActionThrows(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	expectRunError(t, "InvalidArgumentException", "Unknown action", "command", "policy", "action", "bogus")
}

package util

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// Ports tests/Composer/Test/Util/PlatformTest.php. The
// assertPharMetadataSafe tests are not ported: the check only applies to
// PHP < 8.0 and has no Go counterpart.

func TestPlatform_ExpandPath(t *testing.T) {
	t.Setenv("TESTENV", "/home/test")

	for _, c := range []struct{ in, want string }{
		{"%TESTENV%/myPath", "/home/test/myPath"},
		{"$TESTENV/myPath", "/home/test/myPath"},
	} {
		if got, err := ExpandPath(c.in); err != nil || got != c.want {
			t.Errorf("ExpandPath(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}

	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}

	if got, err := ExpandPath("~/test"); err != nil || got != home+"/test" {
		t.Errorf("ExpandPath(~/test) = %q, %v; want %q", got, err, home+"/test")
	}
}

func TestPlatform_IsWindows(t *testing.T) {
	if IsWindows() != (runtime.GOOS == "windows") || IsWindows() != (os.PathSeparator == '\\') {
		t.Error("IsWindows disagrees with the platform")
	}
}

func TestPlatform_GetBoolEnvReturnsDefaultWhenUnset(t *testing.T) {
	t.Setenv("COMPOSER_TEST_BOOL_ENV", "")
	os.Unsetenv("COMPOSER_TEST_BOOL_ENV")

	// PHP returns $default; set reports that it applies.
	if value, set, err := GetBoolEnv("COMPOSER_TEST_BOOL_ENV"); value || set || err != nil {
		t.Errorf("GetBoolEnv = %v, %v, %v", value, set, err)
	}
}

func TestPlatform_GetBoolEnvReturnsExpectedValue(t *testing.T) {
	for _, c := range []struct {
		value    string
		expected bool
	}{{"true", true}, {"false", false}, {"1", true}, {"0", false}, {"on", true}, {"off", false}} {
		t.Setenv("COMPOSER_TEST_BOOL_ENV", c.value)

		if value, set, err := GetBoolEnv("COMPOSER_TEST_BOOL_ENV"); value != c.expected || !set || err != nil {
			t.Errorf("GetBoolEnv(%q) = %v, %v, %v; want %v", c.value, value, set, err, c.expected)
		}
	}
}

func TestPlatform_GetBoolEnvThrowsForInvalidValue(t *testing.T) {
	for _, value := range []string{"2", "-1", "abc", " 1 "} {
		t.Setenv("COMPOSER_TEST_BOOL_ENV", value)

		_, _, err := GetBoolEnv("COMPOSER_TEST_BOOL_ENV")

		var runtimeErr *RuntimeError
		if !errors.As(err, &runtimeErr) || !strings.Contains(err.Error(), "Invalid value for COMPOSER_TEST_BOOL_ENV") {
			t.Errorf("GetBoolEnv(%q) error = %v", value, err)
		}
	}
}

// clearAgentEnv is PlatformTest::setUp and tearDown: no agent marker is
// set, and the memo of GetAIAgent starts and ends empty.
func clearAgentEnv(t *testing.T) {
	t.Helper()

	t.Cleanup(resetAIAgentCache)
	resetAIAgentCache()

	t.Setenv("AI_AGENT", "")

	for _, marker := range codingAgentEnvVars {
		t.Setenv(marker.Name, "")
	}
}

func TestPlatform_GetAiAgentWithoutAnyAgentEnvVar(t *testing.T) {
	clearAgentEnv(t)

	if php.FileExists("/opt/.devin") {
		t.Skip("Tests are running inside Devin")
	}

	if agent, ok := GetAIAgent(); ok || agent != "" {
		t.Errorf("GetAIAgent = %q, %v; want none", agent, ok)
	}
}

func TestPlatform_GetAiAgentDetectsKnownEnvVars(t *testing.T) {
	for _, marker := range codingAgentEnvVars {
		t.Run(marker.Name, func(t *testing.T) {
			clearAgentEnv(t)

			PutEnv(marker.Name, "1")

			if agent, ok := GetAIAgent(); !ok || agent != marker.Agent {
				t.Errorf("GetAIAgent = %q, %v; want %q", agent, ok, marker.Agent)
			}
		})
	}
}

func TestPlatform_GetAiAgentIgnoresEmptyEnvVar(t *testing.T) {
	clearAgentEnv(t)

	if php.FileExists("/opt/.devin") {
		t.Skip("Tests are running inside Devin")
	}

	PutEnv("CLAUDECODE", "")

	if agent, ok := GetAIAgent(); ok || agent != "" {
		t.Errorf("GetAIAgent = %q, %v; want none", agent, ok)
	}
}

func TestPlatform_GetAiAgentPrecedence(t *testing.T) {
	for _, c := range []struct {
		name  string
		env   map[string]string
		agent string
	}{
		{"known AI_AGENT name wins over env vars", map[string]string{"CLAUDECODE": "1", "AI_AGENT": "cursor"}, "cursor"},
		{"known AI_AGENT name after cleanup wins over env vars", map[string]string{"CLAUDECODE": "1", "AI_AGENT": "Codex@1.0"}, "codex"},
		{"unknown AI_AGENT name loses to env vars", map[string]string{"CLAUDECODE": "1", "AI_AGENT": "whatever"}, "claude-code"},
		{"bare true AI_AGENT loses to env vars", map[string]string{"CLAUDECODE": "1", "AI_AGENT": "1"}, "claude-code"},
		{"falsy AI_AGENT does not disable env vars", map[string]string{"CLAUDECODE": "1", "AI_AGENT": "false"}, "claude-code"},
		{"blank AI_AGENT does not disable env vars", map[string]string{"CLAUDECODE": "1", "AI_AGENT": " "}, "claude-code"},
		{"first matching env var wins", map[string]string{"CODEX_THREAD_ID": "abc", "CURSOR_AGENT": "1"}, "cursor"},
		{"empty env var is skipped", map[string]string{"CURSOR_AGENT": "", "CODEX_THREAD_ID": "abc"}, "codex"},
		{"amp over claude-code", map[string]string{"CLAUDECODE": "1", "AMP_CURRENT_THREAD_ID": "T-123"}, "amp"},
		{"codebuddy over claude-code", map[string]string{"CLAUDECODE": "1", "CODEBUDDY": "1"}, "codebuddy"},
		{"cowork over claude-code", map[string]string{"CLAUDECODE": "1", "CLAUDE_CODE_IS_COWORK": "1"}, "cowork"},
		{"qwen-code over gemini-cli", map[string]string{"GEMINI_CLI": "1", "QWEN_CODE": "1"}, "qwen-code"},
		{"vecli over gemini-cli", map[string]string{"GEMINI_CLI": "1", "VECLI_DIR": "/tmp"}, "vecli"},
		{"kilo-code over opencode", map[string]string{"OPENCODE": "1", "OPENCODE_CLIENT": "cli", "KILO": "1"}, "kilo-code"},
		{"known AI_AGENT name still wins over forks", map[string]string{"GEMINI_CLI": "1", "QWEN_CODE": "1", "AI_AGENT": "gemini"}, "gemini-cli"},
	} {
		t.Run(c.name, func(t *testing.T) {
			clearAgentEnv(t)

			for name, value := range c.env {
				PutEnv(name, value)
			}

			if agent, ok := GetAIAgent(); !ok || agent != c.agent {
				t.Errorf("GetAIAgent = %q, %v; want %q", agent, ok, c.agent)
			}
		})
	}
}

func TestPlatform_GetAiAgentUnknownAiAgentNameIsUsedWithoutOtherMarkers(t *testing.T) {
	clearAgentEnv(t)

	if php.FileExists("/opt/.devin") {
		t.Skip("Tests are running inside Devin")
	}

	PutEnv("AI_AGENT", "whatever")

	if agent, ok := GetAIAgent(); !ok || agent != "whatever" {
		t.Errorf("GetAIAgent = %q, %v; want %q", agent, ok, "whatever")
	}
}

func TestPlatform_GetAiAgentCacheIsResetWhenEnvChanges(t *testing.T) {
	clearAgentEnv(t)

	PutEnv("CLAUDECODE", "1")

	if agent, _ := GetAIAgent(); agent != "claude-code" {
		t.Errorf("GetAIAgent = %q; want claude-code", agent)
	}

	PutEnv("AI_AGENT", "cursor")

	if agent, _ := GetAIAgent(); agent != "cursor" {
		t.Errorf("GetAIAgent = %q; want cursor", agent)
	}

	ClearEnv("AI_AGENT")
	PutEnv("AMP_CURRENT_THREAD_ID", "T-123")

	if agent, _ := GetAIAgent(); agent != "amp" {
		t.Errorf("GetAIAgent = %q; want amp", agent)
	}
}

func TestPlatform_GetAiAgentFromAiAgentEnvVar(t *testing.T) {
	for _, c := range []struct {
		name     string
		value    string
		agent    string
		agentSet bool
	}{
		{"known name", "claude-code", "claude-code", true},
		{"alias and case", " Claude ", "claude-code", true},
		{"exact name with underscores", "github_copilot_vscode_agent", "github-copilot", true},
		{"at version", "codex@0.5.1", "codex", true},
		{"space and version", "gemini-cli 1.2.3", "gemini-cli", true},
		{"slash version", "cursor/2.0", "cursor", true},
		{"dash version", "opencode-1.2", "opencode", true},
		{"v-prefixed version", "goose v1.0", "goose", true},
		{"short name that looks like a version", "v0", "v0", true},
		{"bare true value", "1", "unknown", true},
		{"unknown name is preserved", "New-Agent", "new-agent", true},
		{"unknown name is sanitized", "New_Agent2.0 (beta)", "new-agent", true},
		{"underscore in known name", "roo_code", "roo-code", true},
		{"underscore suffix after known name", "claude-code_2-1-201_agent", "claude-code", true},
		{"repeated dashes are collapsed", "foo--9-bar", "foo-bar", true},
		{"unknown name with version", "foo-bar-1.2.3", "foo-bar", true},
		{"unknown name strips invalid chars", "foo.bar!", "foobar", true},
		{"unknown name is truncated", strings.Repeat("a", 30), strings.Repeat("a", 20), true},
		{"unknown name with only invalid chars", "42!", "unknown", true},
		{"long known name", "dsh", "deepseek-harness", true},
		{"falsy value", "false", "", false},
		{"off value", "OFF", "", false},
		{"zero value", "0", "", false},
		{"blank value", "  ", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			clearAgentEnv(t)

			if php.FileExists("/opt/.devin") {
				t.Skip("Tests are running inside Devin")
			}

			PutEnv("AI_AGENT", c.value)

			if agent, ok := GetAIAgent(); agent != c.agent || ok != c.agentSet {
				t.Errorf("GetAIAgent(%q) = %q, %v; want %q, %v", c.value, agent, ok, c.agent, c.agentSet)
			}
		})
	}
}

func TestPlatform_GetAiAgentNamesAreAtMost20Chars(t *testing.T) {
	re := php.MustCompile(`{^[a-z0-9-]{1,20}$}`)

	names := make([]string, 0, len(codingAgentEnvVars)+len(knownAIAgentNames))
	for _, marker := range codingAgentEnvVars {
		names = append(names, marker.Agent)
	}

	for _, name := range knownAIAgentNames {
		names = append(names, name)
	}

	for _, name := range names {
		if matched, err := re.IsMatch(name); err != nil || !matched {
			t.Errorf("agent name %q does not match {^[a-z0-9-]{1,20}$}", name)
		}
	}
}

func TestPlatform_IsMetadataPath(t *testing.T) {
	for _, c := range []struct {
		path string
		want bool
	}{
		{".DS_Store", true},
		{"__MACOSX", true},
		{"__MACOSX/foo/._bar", true},
		{"._composer.json", true},
		// only the top-level segment decides; "folder" is a real dir
		{"folder/._nested", false},
		{"Thumbs.db", true},
		{"desktop.ini", true},
		{"Desktop.ini", true},
		{"thumbs.db", true},
		{".ds_store", false},
		{".AppleDouble", true},
		{".fseventsd/0001", true},
		{"composer.json", false},
		{"src/Client.php", false},
		{"folder", false},
	} {
		if got := IsMetadataPath(c.path); got != c.want {
			t.Errorf("IsMetadataPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// The tests below cover behaviour PlatformTest does not.

func TestPlatform_PutClearEnv(t *testing.T) {
	t.Setenv("COMPOSER_TEST_ENV", "")

	PutEnv("COMPOSER_TEST_ENV", "value")

	if v, ok := GetEnv("COMPOSER_TEST_ENV"); !ok || v != "value" {
		t.Errorf("GetEnv = %q, %v", v, ok)
	}

	ClearEnv("COMPOSER_TEST_ENV")

	if _, ok := GetEnv("COMPOSER_TEST_ENV"); ok {
		t.Error("ClearEnv left the variable set")
	}
}

func TestPlatform_GetCwdAndRealpath(t *testing.T) {
	cwd, err := GetCwd(false)
	if err != nil || cwd == "" {
		t.Fatalf("GetCwd = %q, %v", cwd, err)
	}

	if got := Realpath(""); got != cwd {
		t.Errorf("Realpath(\"\") = %q, want %q", got, cwd)
	}

	if got := Realpath("/does/not/exist"); got != "/does/not/exist" {
		t.Errorf("Realpath(missing) = %q", got)
	}

	dir := t.TempDir()
	mustMkdir(t, dir+"/real")

	if err := os.Symlink(dir+"/real", dir+"/link"); err != nil {
		t.Skip(err)
	}

	want := Realpath(dir + "/real")
	if got := Realpath(dir + "/link/../link/."); got != want {
		t.Errorf("Realpath(link) = %q, want %q", got, want)
	}
}

func TestPlatform_GetUserDirectory(t *testing.T) {
	t.Setenv("HOME", "/home/someone")

	if got, err := GetUserDirectory(); err != nil || got != "/home/someone" {
		t.Errorf("GetUserDirectory = %q, %v", got, err)
	}
}

func TestPlatform_Misc(t *testing.T) {
	if Strlen("é") != 2 {
		t.Error("Strlen counts characters")
	}

	if (GetDevNull() == "NUL") != IsWindows() {
		t.Error("GetDevNull disagrees with the platform")
	}

	t.Setenv("MSYSTEM", "mingw64")

	if !IsTty(nil) {
		t.Error("IsTty ignores MSYSTEM")
	}
}

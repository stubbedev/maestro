// Ports src/Composer/Util/Platform.php.

package util

import (
	"os"
	"os/user"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/stubbedev/maestro/internal/php"
)

// MetadataNameRegex is Platform::METADATA_NAME_REGEX: OS metadata file
// names, the Windows ones case-insensitively.
const MetadataNameRegex = `{^(?:\._.*|\.DS_Store|__MACOSX|\.AppleDouble|\.fseventsd|\.Spotlight-V100|\.Trashes|(?i:thumbs\.db|desktop\.ini))$}`

// IsMetadataPath ports Platform::isMetadataPath: whether the top-level
// segment of an archive entry path is OS metadata (resource forks,
// thumbnail caches, ...).
func IsMetadataPath(name string) bool {
	path := strings.TrimLeft(php.Strtr(name, `\`, "/"), "/")
	top, _, _ := strings.Cut(path, "/")

	matched, err := php.PregIsMatch(MetadataNameRegex, top)

	return err == nil && matched
}

// codingAgentEnvVars is Platform::CODING_AGENT_ENV_VARS: env vars that
// indicate Composer runs under an AI coding agent, mapped to the agent's
// name. Order matters, as the first match wins.
var codingAgentEnvVars = []struct{ Name, Agent string }{
	// Amp, CodeBuddy and Cowork set/mirror Claude Code vars, so they must precede them.
	{"AMP_CURRENT_THREAD_ID", "amp"},
	{"CODEBUDDY", "codebuddy"},
	{"CLAUDE_CODE_IS_COWORK", "cowork"},
	{"CLAUDECODE", "claude-code"},
	{"CLAUDE_CODE", "claude-code"},
	{"CURSOR_AGENT", "cursor"},
	{"CURSOR_SANDBOX", "cursor-cli"},
	// Gemini CLI forks, must precede GEMINI_CLI.
	{"QWEN_CODE", "qwen-code"},
	{"VECLI_DIR", "vecli"},
	{"GEMINI_CLI", "gemini-cli"},
	{"CODEX_SANDBOX", "codex"},
	{"CODEX_CI", "codex"},
	{"CODEX_THREAD_ID", "codex"},
	{"AUGMENT_AGENT", "augment"},
	// Kilo inherits the OPENCODE* vars, so it must precede them.
	{"KILO", "kilo-code"},
	{"OPENCODE_CLIENT", "opencode"},
	{"OPENCODE", "opencode"},
	{"CLINE_ACTIVE", "cline"},
	{"CLINE_TASK_ID", "cline"},
	{"ROO_CODE_TASK_ID", "roo-code"},
	{"CRUSH", "crush"},
	{"IFLOW_CLI", "iflow-cli"},
	{"TRAE_AI_SHELL_ID", "trae"},
	{"GOOSE_TERMINAL", "goose"},
	{"OZ_RUN_ID", "warp"},
	{"FIREBENDER_TERMINAL", "firebender"},
	{"GROK_SESSION_ID", "grok-build"},
	{"JUNIE_SHIM_PATH", "junie"},
	{"REPL_ID", "replit"},
	{"COPILOT_MODEL", "github-copilot"},
	{"COPILOT_ALLOW_ALL", "github-copilot"},
	{"COPILOT_GITHUB_TOKEN", "github-copilot"},
	{"COPILOT_CLI", "github-copilot"},
	{"ANTIGRAVITY_AGENT", "antigravity"},
	{"PI_CODING_AGENT", "pi"},
	{"MATTERHORN_SESSION_ID", "junie"},
	{"KIRO_AGENT_PATH", "kiro"},
}

// knownAIAgentNames is Platform::KNOWN_AI_AGENT_NAMES: known AI_AGENT
// values and aliases mapped to agent names.
var knownAIAgentNames = map[string]string{
	"amp":                         "amp",
	"amazonq":                     "amazon-q",
	"amazon-q":                    "amazon-q",
	"amazon-q-cli":                "amazon-q",
	"antigravity":                 "antigravity",
	"augment":                     "augment",
	"augment-cli":                 "augment",
	"claude":                      "claude-code",
	"claude-code":                 "claude-code",
	"claudecode":                  "claude-code",
	"cline":                       "cline",
	"codebuddy":                   "codebuddy",
	"codex":                       "codex",
	"cowork":                      "cowork",
	"crush":                       "crush",
	"cursor":                      "cursor",
	"cursor-cli":                  "cursor-cli",
	"deepseek-harness":            "deepseek-harness",
	"dsh":                         "deepseek-harness",
	"devin":                       "devin",
	"firebender":                  "firebender",
	"gemini":                      "gemini-cli",
	"gemini-cli":                  "gemini-cli",
	"github-copilot":              "github-copilot",
	"github-copilot-cli":          "github-copilot",
	"github_copilot_vscode_agent": "github-copilot",
	"github_copilot_app_agent":    "github-copilot",
	"goose":                       "goose",
	"grok":                        "grok-cli",
	"grok-cli":                    "grok-cli",
	"grok-build":                  "grok-build",
	"hermes":                      "hermes",
	"hermes-agent":                "hermes",
	"iflow":                       "iflow-cli",
	"iflow-cli":                   "iflow-cli",
	"junie":                       "junie",
	"kilo":                        "kilo-code",
	"kilo-code":                   "kilo-code",
	"kilocode":                    "kilo-code",
	"kiro":                        "kiro",
	"kiro-cli":                    "kiro",
	"openclaw":                    "openclaw",
	"opencode":                    "opencode",
	"openhands":                   "openhands",
	"pi":                          "pi",
	"qwen":                        "qwen-code",
	"qwen-code":                   "qwen-code",
	"qwencode":                    "qwen-code",
	"replit":                      "replit",
	"roo":                         "roo-code",
	"roo-code":                    "roo-code",
	"roocode":                     "roo-code",
	"trae":                        "trae",
	"v0":                          "v0",
	"vecli":                       "vecli",
	"vtcode":                      "vtcode",
	"warp":                        "warp",
	"oz":                          "warp",
}

// aiAgentCache is Platform::$aiAgent: nil when not computed yet, and
// otherwise the agent ("" for none).
var (
	aiAgentMu    sync.Mutex
	aiAgentCache *string
)

func resetAIAgentCache() {
	aiAgentMu.Lock()
	aiAgentCache = nil
	aiAgentMu.Unlock()
}

// GetAIAgent ports Platform::getAiAgent: the name of the AI coding agent
// running Composer, "unknown" when one cannot be identified, and false
// when a human runs it.
func GetAIAgent() (string, bool) {
	aiAgentMu.Lock()
	cached := aiAgentCache
	aiAgentMu.Unlock()

	if cached != nil {
		return *cached, *cached != ""
	}

	agent := detectAIAgent()

	aiAgentMu.Lock()
	aiAgentCache = &agent
	aiAgentMu.Unlock()

	return agent, agent != ""
}

func detectAIAgent() string {
	fallback := ""

	raw, _ := GetEnv("AI_AGENT")
	value := php.Strtolower(php.Trim(raw))
	if !slices.Contains([]string{"", "0", "false", "no", "off"}, value) {
		name, known := knownAIAgentNames[value]
		if !known {
			// Strip anything after a separator (e.g. "codex@1.2" or
			// "claude-code v2") and a trailing version, then also try the
			// part before an underscore ("claude-code_2-1-201_agent").
			value = stripAIAgentSeparator(value)
			normalized := stripAIAgentVersion(php.Strtr(value, "_", "-"))
			name, known = knownAIAgentNames[normalized]
			if !known {
				name, known = knownAIAgentNames[stripAIAgentVersion(strings.SplitN(value, "_", 2)[0])]
			}
			value = normalized
		}

		if known {
			return name
		}

		sanitized, _, _ := php.PregReplace(`{[^a-z-]+}`, "", value, -1)
		sanitized, _, _ = php.PregReplace(`{-{2,}}`, "-", sanitized, -1)
		value = php.SubstrLen(php.TrimSet(sanitized, "-"), 0, 20)
		if value != "" {
			fallback = value
		} else {
			fallback = "unknown"
		}
	}

	for _, marker := range codingAgentEnvVars {
		if v, ok := GetEnv(marker.Name); ok && v != "" {
			return marker.Agent
		}
	}

	// devin exposes no env var but mounts this directory. PHP skips the
	// check under open_basedir, which Go has no equivalent of.
	if php.FileExists("/opt/.devin") {
		return "devin"
	}

	return fallback
}

func stripAIAgentSeparator(value string) string {
	out, _, err := php.PregReplace(`{[\s@/:].*$}s`, "", value, -1)
	if err != nil {
		return value
	}

	return out
}

func stripAIAgentVersion(value string) string {
	out, _, err := php.PregReplace(`{-?v?\d+(\.\d+)*$}`, "", value, -1)
	if err != nil {
		return value
	}

	return out
}

// GetCwd ports Platform::getCwd: the physical working directory (getcwd(3),
// never $PWD), or "" when allowEmpty and it cannot be determined.
func GetCwd(allowEmpty bool) (string, error) {
	cwd, err := php.Getcwd()
	if err == nil {
		return cwd, nil
	}

	if allowEmpty {
		return "", nil
	}

	return "", &RuntimeError{Message: "Could not determine the current working directory"}
}

// Realpath ports Platform::realpath: realpath(3), falling back on path.
func Realpath(path string) string {
	if real, ok := php.Realpath(path); ok {
		return real
	}

	return path
}

// GetEnv ports Platform::getEnv. PHP reads $_SERVER and $_ENV before
// getenv(); PutEnv and ClearEnv keep all three in step, so the process
// environment is the single source here.
func GetEnv(name string) (string, bool) {
	return os.LookupEnv(name)
}

// GetEnvTruthy is Platform::getEnv($name) used as a boolean: the value,
// and whether it is set to neither "" nor "0".
func GetEnvTruthy(name string) (string, bool) {
	v, ok := GetEnv(name)

	return v, ok && php.Truthy(v)
}

// EnvTruthy is (bool) Platform::getEnv($name).
func EnvTruthy(name string) bool {
	_, ok := GetEnvTruthy(name)

	return ok
}

// GetBoolEnv ports Platform::getBoolEnv: set reports whether the variable
// holds a non-empty value; only 0, 1, false, true, off and on are accepted.
func GetBoolEnv(name string) (value, set bool, err error) {
	v, ok := GetEnv(name)
	if !ok || v == "" {
		return false, false, nil
	}

	switch v {
	case "1", "true", "on":
		return true, true, nil
	case "0", "false", "off":
		return false, true, nil
	}

	return false, false, &RuntimeError{Message: "Invalid value for " + name + ": " + v + ". Expected 0, 1, false, true, off, or on."}
}

// PutEnv ports Platform::putEnv.
func PutEnv(name, value string) {
	_ = os.Setenv(name, value)

	if name == "AI_AGENT" || slices.ContainsFunc(codingAgentEnvVars, func(m struct{ Name, Agent string }) bool { return m.Name == name }) {
		resetAIAgentCache()
	}
}

// ClearEnv ports Platform::clearEnv.
func ClearEnv(name string) {
	_ = os.Unsetenv(name)

	if name == "AI_AGENT" || slices.ContainsFunc(codingAgentEnvVars, func(m struct{ Name, Agent string }) bool { return m.Name == name }) {
		resetAIAgentCache()
	}
}

// ExpandPath ports Platform::expandPath: a leading "~/" becomes the user
// directory, a leading $VAR or %VAR% the variable's value.
func ExpandPath(path string) (string, error) {
	return expandPath(path, IsWindows())
}

func expandPath(path string, windows bool) (string, error) {
	// '#^~[\\/]#' in PHP source is the class [\/], which only holds "/".
	if strings.HasPrefix(path, "~/") {
		home, err := GetUserDirectory()
		if err != nil {
			return "", err
		}

		return home + path[1:], nil
	}

	// #^(\$|(?P<percent>%))(?P<var>\w++)(?(percent)%)(?P<path>.*)#
	if path == "" || (path[0] != '$' && path[0] != '%') {
		return path, nil
	}

	percent := path[0] == '%'
	end := 1

	for end < len(path) && isWordByte(path[end]) {
		end++
	}

	if end == 1 {
		return path, nil
	}

	name := path[1:end]
	if percent {
		if end == len(path) || path[end] != '%' {
			return path, nil
		}

		end++
	}

	// .* stops at the first newline; what follows is not part of the match.
	rest := path[end:]
	tail := ""

	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest, tail = rest[:i], rest[i:]
	}

	// Treat HOME as an alias for USERPROFILE on Windows for legacy reasons.
	if windows && name == "HOME" {
		if home, _ := GetEnv("HOME"); php.Truthy(home) {
			return home + rest + tail, nil
		}

		profile, _ := GetEnv("USERPROFILE")

		return profile + rest + tail, nil
	}

	value, _ := GetEnv(name)

	return value + rest + tail, nil
}

// isWordByte reports whether c matches PCRE's \w without UCP.
func isWordByte(c byte) bool {
	return isASCIIAlnum(c) || c == '_'
}

// GetUserDirectory ports Platform::getUserDirectory.
func GetUserDirectory() (string, error) {
	if home, ok := GetEnv("HOME"); ok {
		return home, nil
	}

	if IsWindows() {
		if home, ok := GetEnv("USERPROFILE"); ok {
			return home, nil
		}
	} else if u, err := user.LookupId(currentUID()); err == nil {
		return u.HomeDir, nil
	}

	return "", &RuntimeError{Message: "Could not determine user directory"}
}

var (
	wslOnce sync.Once
	isWSL   bool
)

// IsWindowsSubsystemForLinux ports Platform::isWindowsSubsystemForLinux.
func IsWindowsSubsystemForLinux() bool {
	wslOnce.Do(func() {
		if IsWindows() {
			return
		}

		data, err := os.ReadFile("/proc/version")
		isWSL = err == nil && strings.Contains(php.Strtolower(string(data)), "microsoft") && !IsDocker()
	})

	return isWSL
}

// IsWindows ports Platform::isWindows.
func IsWindows() bool {
	return php.IsWindows()
}

var (
	dockerOnce sync.Once
	isDocker   bool
)

// IsDocker ports Platform::isDocker.
func IsDocker() bool {
	dockerOnce.Do(func() { isDocker = detectDocker() })

	return isDocker
}

func detectDocker() bool {
	// .dockerenv and .containerenv are present in some cases but not reliably.
	if slices.ContainsFunc([]string{"/.dockerenv", "/run/.containerenv", "/var/run/.containerenv"}, php.FileExists) {
		return true
	}

	// cgroup v2, then cgroup v1.
	for _, cgroup := range []string{"/proc/self/mountinfo", "/proc/1/cgroup"} {
		data, err := os.ReadFile(cgroup)
		if err != nil {
			continue
		}

		// Detect default mount points created by Docker/containerd.
		s := string(data)
		if strings.Contains(s, "/var/lib/docker/") || strings.Contains(s, "/io.containerd.snapshotter") {
			return true
		}
	}

	return false
}

// Strlen ports Platform::strlen: the byte length.
func Strlen(s string) int {
	return len(s)
}

// IsTty ports Platform::isTty for f (os.Stdout when nil).
func IsTty(f *os.File) bool {
	if f == nil {
		f = os.Stdout
	}

	// Detect msysgit/mingw and assume this is a tty because detection does
	// not work correctly, see https://github.com/composer/composer/issues/9690
	if msystem, _ := GetEnv("MSYSTEM"); php.Strcasecmp(msystem, "MINGW32") == 0 || php.Strcasecmp(msystem, "MINGW64") == 0 {
		return true
	}

	return term.IsTerminal(int(f.Fd()))
}

// IsInputCompletionProcess ports Platform::isInputCompletionProcess.
func IsInputCompletionProcess() bool {
	return len(os.Args) > 1 && os.Args[1] == "_complete"
}

// WorkaroundFilesystemIssues ports Platform::workaroundFilesystemIssues.
func WorkaroundFilesystemIssues() {
	if isVirtualBoxGuest() {
		time.Sleep(200 * time.Millisecond)
	}
}

var (
	vboxOnce sync.Once
	isVBox   bool
)

// isVirtualBoxGuest ports Platform::isVirtualBoxGuest: the process user is
// "vagrant", COMPOSER_RUNTIME_ENV is "virtualbox", or lsmod lists the
// VirtualBox guest additions.
func isVirtualBoxGuest() bool {
	vboxOnce.Do(func() {
		if IsWindows() {
			return
		}

		if u, err := user.LookupId(currentEUID()); err == nil && u.Username == "vagrant" {
			isVBox = true

			return
		}

		if env, _ := GetEnv("COMPOSER_RUNTIME_ENV"); env == "virtualbox" {
			isVBox = true

			return
		}

		if runtime.GOOS == "linux" {
			// lsmod prints /proc/modules: reading it gives the same answer
			// without starting a process (deliberate deviation 3); lsmod
			// still answers where it cannot be read.
			if modules, err := os.ReadFile("/proc/modules"); err == nil {
				isVBox = strings.Contains(string(modules), "vboxguest")

				return
			}

			var output string

			code, err := NewProcessExecutor(nil).Execute(Cmd("lsmod"), &output, "")
			isVBox = err == nil && code == 0 && strings.Contains(output, "vboxguest")
		}
	})

	return isVBox
}

// GetDevNull ports Platform::getDevNull.
func GetDevNull() string {
	if IsWindows() {
		return "NUL"
	}

	return "/dev/null"
}

// isLink is PHP's is_link.
func isLink(path string) bool {
	fi, err := os.Lstat(path)

	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

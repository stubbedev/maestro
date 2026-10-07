// Ports the restart of composer/xdebug-handler 3.0.5
// (vendor/composer/xdebug-handler/src/XdebugHandler.php: prepareRestart,
// writeTmpIni, mergeLoadedConfig, getAllIniFiles, setEnvironment, and
// check()'s restarted branch with setEnvRestartSettings), which
// bin/composer runs before Composer (docs/PLUGINS.md §5.2 "Xdebug").
//
// Composer restarts php without xdebug unless COMPOSER_ALLOW_XDEBUG
// allows it. maestro starts the plugin child the way that restart starts
// the restarted process, so the child is the process Composer's code
// would run in: `php -n -c <tmp.ini>` with the ini files' settings minus
// the xdebug zend_extension lines, and the environment the restarted
// process has.

package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/platform"
)

// xdebugRestart is how the child is started instead of plainly.
type xdebugRestart struct {
	// args go before the script: -n -c <tmp.ini>.
	args []string
	// env holds the variables to set ("" with unset true to remove).
	env    map[string]string
	unset  []string
	tmpIni string
}

var (
	iniSectionPattern = php.MustCompile(`/^\s*\[(?:PATH|HOST)\s*=/mi`)
	iniXdebugPattern  = php.MustCompile(`/^\s*(zend_extension\s*=.*xdebug.*)$/mi`)
)

// planXdebugRestart returns the restart bin/composer would do for the php
// raw describes (its probe, before Composer's view), or nil when none
// happens: xdebug is not active, COMPOSER_ALLOW_XDEBUG allows it, or the
// restart is not possible. It writes the temporary ini file. When that
// fails XdebugHandler reports an error (only with its debug output) and
// Composer goes on with xdebug, and so does the child.
func planXdebugRestart(raw *platform.Snapshot, getenv func(string) (string, bool)) *xdebugRestart {
	view, skipped := raw.ComposerView(getenv)
	if !raw.Xdebug.Loaded || view.Xdebug.Loaded {
		return nil
	}

	eol := "\n"
	if runtime.GOOS == "windows" {
		eol = "\r\n"
	}

	iniFiles := allIniFiles(raw, getenv)
	scannedInis := len(iniFiles) > 1

	content, ok := tmpIniContent(raw, iniFiles, eol)
	if !ok {
		return nil
	}

	f, err := os.CreateTemp(os.TempDir(), "")
	if err != nil {
		return nil
	}
	tmpIni := f.Name()
	_, err = f.WriteString(content)
	if cerr := f.Close(); err != nil || cerr != nil {
		_ = os.Remove(tmpIni)

		return nil
	}

	// setEnvironment, then what check() does in the restarted process:
	// COMPOSER_ALLOW_XDEBUG is removed again and XDEBUG_HANDLER_SETTINGS
	// records the restart (php_ini_loaded_file() is the temporary ini).
	originalInis := strings.Join(iniFiles, string(os.PathListSeparator))
	scanDir, ok := getenv("PHP_INI_SCAN_DIR")
	if !ok {
		scanDir = "*"
	}
	phprc, ok := getenv("PHPRC")
	if !ok {
		phprc = "*"
	}
	scanned := "0"
	if scannedInis {
		scanned = "1"
	}

	return &xdebugRestart{
		args: []string{"-n", "-c", tmpIni},
		env: map[string]string{
			"COMPOSER_ORIGINAL_INIS":  originalInis,
			"XDEBUG_HANDLER_SETTINGS": strings.Join([]string{tmpIni, scanned, scanDir, phprc, originalInis, skipped}, "|"),
		},
		unset:  []string{"COMPOSER_ALLOW_XDEBUG"},
		tmpIni: tmpIni,
	}
}

// allIniFiles is XdebugHandler::getAllIniFiles for Composer.
func allIniFiles(raw *platform.Snapshot, getenv func(string) (string, bool)) []string {
	if env, ok := getenv("COMPOSER_ORIGINAL_INIS"); ok {
		return strings.Split(env, string(os.PathListSeparator))
	}

	return raw.IniFiles()
}

// tmpIniContent is writeTmpIni's content; ok is false where it fails.
func tmpIniContent(raw *platform.Snapshot, iniFiles []string, eol string) (string, bool) {
	// iniFiles has at least one item and it may be empty
	if len(iniFiles) > 0 && iniFiles[0] == "" {
		iniFiles = iniFiles[1:]
	}

	var content strings.Builder
	for _, file := range iniFiles {
		// Check for inaccessible ini files
		data, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			return "", false
		}
		text := string(data)
		// Check and remove directives after HOST and PATH sections
		m, err := iniSectionPattern.Match(text)
		if err != nil {
			return "", false
		}
		if m != nil {
			text = text[:m.Offset(0)]
		}
		replaced, _, err := iniXdebugPattern.Replace(text, ";$1", -1)
		if err != nil {
			return "", false
		}
		content.WriteString(replaced + eol)
	}

	content.WriteString(mergeLoadedConfig(raw.IniGetAll(), parseIni(content.String()), eol))

	// Work-around for https://bugs.php.net/bug.php?id=75932
	content.WriteString("opcache.enable_cli=0" + eol)

	return content.String(), true
}

// mergeLoadedConfig returns the settings of the running php that the ini
// content does not already give (default, changed and command-line
// settings), double-quoted.
func mergeLoadedConfig(loaded *php.Array, iniConfig map[string]string, eol string) string {
	if loaded == nil {
		return ""
	}

	escape := strings.NewReplacer(`\`, `\\`, `"`, `\"`)

	var b strings.Builder
	for k, v := range loaded.All() {
		name := k.String()
		value, ok := v.(string)
		// Value will either be null, string or array (HHVM only)
		if !ok || strings.HasPrefix(name, "xdebug") || name == "apc.mmap_file_mask" {
			continue
		}

		if ini, ok := iniConfig[name]; !ok || ini != value {
			// Double-quote escape each value
			b.WriteString(name + `="` + escape.Replace(value) + `"` + eol)
		}
	}

	return b.String()
}

// parseIni reads the settings of ini content whose value
// parse_ini_string() would certainly give as written. XdebugHandler
// compares them with the loaded settings to leave out the ones the files
// already set; a setting this leaves out of the map is written again,
// with the value php has loaded, which changes nothing.
func parseIni(content string) map[string]string {
	out := map[string]string{}

	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || line[0] == ';' || line[0] == '[' {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:eq])
		raw := strings.TrimSpace(line[eq+1:])

		value, ok := plainIniValue(raw)
		if !ok || strings.ContainsAny(name, "[]{}$\"'") {
			delete(out, name)

			continue
		}
		out[name] = value
	}

	return out
}

// plainIniValue is the value of an ini setting when it needs no constant,
// variable or expression evaluation.
func plainIniValue(raw string) (string, bool) {
	if strings.HasPrefix(raw, `"`) {
		end := strings.IndexByte(raw[1:], '"')
		if end < 0 {
			return "", false
		}
		value, rest := raw[1:end+1], strings.TrimSpace(raw[end+2:])
		if strings.ContainsAny(value, `\$`) || (rest != "" && rest[0] != ';') {
			return "", false
		}

		return value, true
	}

	if i := strings.IndexByte(raw, ';'); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	switch php.Strtolower(raw) {
	case "on", "yes", "true":
		return "1", true
	case "off", "no", "false", "none", "null", "":
		return "", true
	}
	for _, c := range raw {
		isWord := c == '.' || c == '/' || c == '-' || c == '_' || c == ':' || c == '@' || c == '+' || c == ',' ||
			(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')
		// Upper case words may be constants (E_ALL), and other characters
		// expressions.
		if !isWord {
			return "", false
		}
	}

	return raw, true
}

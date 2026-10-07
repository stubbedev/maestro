// Ports what bin/composer (with XdebugHandler::check() from
// vendor/composer/xdebug-handler/src/XdebugHandler.php) and
// Application::__construct do to the PHP Composer runs on.

package platform

import (
	"maps"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// ComposerView returns the platform as Composer's code sees it, and
// XdebugHandler::getSkippedVersion(). getenv is os.LookupEnv or a
// replacement.
//
// When xdebug is active and COMPOSER_ALLOW_XDEBUG does not allow it,
// bin/composer restarts php without xdebug, so its Runtime then has no
// xdebug and PlatformRepository adds ext-xdebug with the skipped version
// instead. The restart's other effects (the temporary ini, the
// environment it sets for child processes) concern whoever starts php
// for Composer's code (internal/plugin); IniFiles keeps reporting the
// original ini files, as IniHelper does through COMPOSER_ORIGINAL_INIS.
//
// bin/composer and Application::__construct then change some settings
// with ini_set, which ini_get (and so the -d options of `@php` scripts)
// reports: display_errors, memory_limit (raised to 1536M, or
// COMPOSER_MEMORY_LIMIT), xdebug.show_exception_trace and xdebug.scream.
func (s *Snapshot) ComposerView(getenv func(string) (string, bool)) (view *Snapshot, skippedVersion string) {
	view, skippedVersion = s.xdebugCheck(getenv)

	return view.withComposerIni(getenv), skippedVersion
}

// xdebugCheck ports XdebugHandler::check().
func (s *Snapshot) xdebugCheck(getenv func(string) (string, bool)) (*Snapshot, string) {
	allow, _ := getenv("COMPOSER_ALLOW_XDEBUG")
	envArgs := strings.Split(allow, "|")

	if !php.ToBool(envArgs[0]) && s.Xdebug.Active {
		// Restart required
		if !s.canRestart() {
			return s, ""
		}

		return s.WithoutXdebug(), s.Xdebug.Version
	}

	if envArgs[0] == "internal" && len(envArgs) == 5 {
		// Restarted, so use the saved values; the skipped version is only
		// set if Xdebug is not loaded.
		if !s.Xdebug.Loaded {
			return s, envArgs[1]
		}

		return s, ""
	}

	// getRestartSettings: called with an existing restart's settings.
	settings, _ := getenv("XDEBUG_HANDLER_SETTINGS")
	parts := strings.Split(settings, "|")

	if loaded, ok := s.LoadedIniFile(); len(parts) == 6 && ok && loaded == parts[0] {
		return s, parts[5]
	}

	return s, ""
}

// withComposerIni applies the ini_set calls of bin/composer and
// Application::__construct.
func (s *Snapshot) withComposerIni(getenv func(string) (string, bool)) *Snapshot {
	r := NewRuntime(s)
	if !r.HasFunction("ini_set") {
		return s
	}

	c := *s
	c.ini = s.ini.Clone()

	set := func(name, value string) {
		if c.ini.Has(name) {
			c.ini.Set(name, value)
		}
	}

	// On the CLI SAPI, ensure errors are displayed on stderr, either via
	// display_errors or via error_log.
	if sapi, _ := s.Constant("PHP_SAPI"); sapi == "cli" {
		errorLog, _ := s.IniGet("error_log")
		logErrors, _ := s.IniGet("log_errors")

		if errorLog == "" && php.ToBool(logErrors) {
			set("display_errors", "0")
		} else {
			set("display_errors", "stderr")
		}
	}

	if limit, _ := getenv("COMPOSER_MEMORY_LIMIT"); php.ToBool(limit) {
		set("memory_limit", limit)
	} else {
		limit, _ := s.IniGet("memory_limit")
		limit = php.Trim(limit)

		// Increase memory_limit if it is lower than 1.5GB
		if !php.LooseEquals(limit, int64(-1)) && memoryInBytes(limit) < 1024*1024*1536 {
			set("memory_limit", "1536M")
		}
	}

	if _, ok := r.extension("xdebug"); ok {
		set("xdebug.show_exception_trace", "0")
		set("xdebug.scream", "0")
	}

	return &c
}

// memoryInBytes is bin/composer's closure of that name.
func memoryInBytes(value string) int64 {
	unit := php.Strtolower(php.Substr(value, -1))
	n := php.ToInt(value)

	switch unit {
	case "g":
		n *= 1024

		fallthrough
	case "m":
		n *= 1024

		fallthrough
	case "k":
		n *= 1024
	}

	return n
}

// canRestart is whether prepareRestart succeeds (checkConfiguration): the
// CLI SAPI, proc_open, an existing PHP_BINARY and no uopz that would
// swallow the exit. The temporary ini is assumed to be writable.
func (s *Snapshot) canRestart() bool {
	r := NewRuntime(s)

	if sapi, _ := s.Constant("PHP_SAPI"); sapi != "cli" {
		return false
	}

	if !r.HasFunction("proc_open") {
		return false
	}

	binary, _ := s.Constant("PHP_BINARY")
	if path, _ := binary.(string); path == "" || !php.FileExists(path) {
		return false
	}

	if _, ok := r.extension("uopz"); ok && !s.IniBool("uopz.disable") && !r.HasFunction("uopz_allow_exit") {
		return false
	}

	// cmd.exe does not support UNC paths, before PHP 7.4.
	if _, windows := s.Constant("PHP_WINDOWS_VERSION_BUILD"); windows && s.VersionID < 70400 {
		wd, err := php.Getcwd()
		if err != nil || strings.HasPrefix(wd, `\\`) {
			return false
		}
	}

	return true
}

// WithoutXdebug returns the snapshot of the php XdebugHandler restarts:
// the same php without the xdebug extension, and with
// opcache.enable_cli=0, which the restart's ini adds.
func (s *Snapshot) WithoutXdebug() *Snapshot {
	if !s.Xdebug.Loaded {
		return s
	}

	c := *s
	c.Xdebug = Xdebug{}

	c.Extensions = slices.DeleteFunc(slices.Clone(s.Extensions), func(e Extension) bool { return php.Strcasecmp(e.Name, "xdebug") == 0 })
	c.ZendExtensions = slices.DeleteFunc(slices.Clone(s.ZendExtensions), func(n string) bool { return php.Strcasecmp(n, "xdebug") == 0 })

	c.extIndex = make(map[string]int, len(c.Extensions))
	for i, e := range c.Extensions {
		c.extIndex[php.Strtolower(e.Name)] = i
	}

	c.functions = without(s.functions, s.Xdebug.Functions)
	c.classes = without(s.classes, s.Xdebug.Classes)

	c.constants = maps.Clone(s.constants)
	for _, name := range s.Xdebug.Constants {
		delete(c.constants, name)
	}

	c.ini = s.ini.Clone()
	for _, name := range s.Xdebug.Ini {
		c.ini.Delete(name)
	}

	if c.ini.Has("opcache.enable_cli") {
		c.ini.Set("opcache.enable_cli", "0")
	}

	return &c
}

func without(set map[string]struct{}, names []string) map[string]struct{} {
	out := make(map[string]struct{}, len(set))
	for k := range set {
		out[k] = struct{}{}
	}

	for _, n := range names {
		delete(out, php.Strtolower(n))
	}

	return out
}

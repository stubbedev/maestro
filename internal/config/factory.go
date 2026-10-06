// Ports the configuration half of src/Composer/Factory.php: getHomeDir,
// getCacheDir, getDataDir, createConfig, getComposerFile, getLockFile,
// loadComposerAuthEnv, useXdg, getUserDir and validateJsonSchema.
// internal/composer builds the rest of Factory on these.

package config

import (
	"errors"
	"os"
	"runtime"
	"strings"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// getEnvTruthy is Platform::getEnv($name) used as a boolean: set, and
// neither "" nor "0".
func getEnvTruthy(name string) (string, bool) {
	v, ok := util.GetEnv(name)

	return v, ok && php.ToBool(v)
}

func isDir(dir string) bool {
	fi, err := os.Stat(dir)

	return err == nil && fi.IsDir()
}

// HomeDir ports Factory::getHomeDir: COMPOSER_HOME, else the XDG config
// directory or ~/.composer (whichever exists, the first otherwise).
func HomeDir() (string, error) {
	if home, ok := getEnvTruthy("COMPOSER_HOME"); ok {
		return home, nil
	}

	if util.IsWindows() {
		appData, ok := getEnvTruthy("APPDATA")
		if !ok {
			return "", &util.RuntimeError{Site: phperr.At("Factory.php", 67), Message: "The APPDATA or COMPOSER_HOME environment variable must be set for composer to run correctly"}
		}

		return strings.TrimRight(strings.ReplaceAll(appData, `\`, "/"), "/") + "/Composer", nil
	}

	userDir, err := userDir()
	if err != nil {
		return "", err
	}
	dirs := make([]string, 0, 2)

	if useXdg() {
		// XDG Base Directory Specifications
		xdgConfig, ok := getEnvTruthy("XDG_CONFIG_HOME")
		if !ok {
			xdgConfig = userDir + "/.config"
		}

		dirs = append(dirs, xdgConfig+"/composer")
	}

	dirs = append(dirs, userDir+"/.composer")

	// select first dir which exists of: $XDG_CONFIG_HOME/composer or ~/.composer
	for _, dir := range dirs {
		if isDir(dir) {
			return dir, nil
		}
	}

	// if none exists, we default to first defined one (XDG one if system uses it, or ~/.composer otherwise)
	return dirs[0], nil
}

// CacheDir ports Factory::getCacheDir.
func CacheDir(home string) (string, error) {
	if cacheDir, ok := getEnvTruthy("COMPOSER_CACHE_DIR"); ok {
		return cacheDir, nil
	}

	if homeEnv, ok := getEnvTruthy("COMPOSER_HOME"); ok {
		return homeEnv + "/cache", nil
	}

	if util.IsWindows() {
		cacheDir, ok := getEnvTruthy("LOCALAPPDATA")
		if ok {
			cacheDir += "/Composer"
		} else {
			cacheDir = home + "/cache"
		}

		return strings.TrimRight(strings.ReplaceAll(cacheDir, `\`, "/"), "/"), nil
	}

	userDir, err := userDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		// Migrate existing cache dir in old location if present
		if isDir(home+"/cache") && !isDir(userDir+"/Library/Caches/composer") {
			_ = os.Rename(home+"/cache", userDir+"/Library/Caches/composer")
		}

		return userDir + "/Library/Caches/composer", nil
	}

	if home == userDir+"/.composer" && isDir(home+"/cache") {
		return home + "/cache", nil
	}

	if useXdg() {
		xdgCache, ok := getEnvTruthy("XDG_CACHE_HOME")
		if !ok {
			xdgCache = userDir + "/.cache"
		}

		return xdgCache + "/composer", nil
	}

	return home + "/cache", nil
}

// DataDir ports Factory::getDataDir.
func DataDir(home string) (string, error) {
	if homeEnv, ok := getEnvTruthy("COMPOSER_HOME"); ok {
		return homeEnv, nil
	}

	if util.IsWindows() {
		return strings.ReplaceAll(home, `\`, "/"), nil
	}

	userDir, err := userDir()
	if err != nil {
		return "", err
	}
	if home != userDir+"/.composer" && useXdg() {
		xdgData, ok := getEnvTruthy("XDG_DATA_HOME")
		if !ok {
			xdgData = userDir + "/.local/share"
		}

		return xdgData + "/composer", nil
	}

	return home, nil
}

// getString is Get for the string-valued settings (home and the dirs).
func (c *Config) getString(key string) (string, error) {
	v, err := c.Get(key, 0)

	return php.ToString(v), err
}

// CreateConfig ports Factory::createConfig: the global configuration
// (defaults, COMPOSER_HOME/config.json, auth.json and COMPOSER_AUTH), with
// cwd ("" for null: the working directory) as base directory. out may be
// nil.
func CreateConfig(out io.IO, cwd string) (*Config, error) {
	if cwd == "" {
		var err error
		if cwd, err = util.GetCwd(true); err != nil {
			return nil, err
		}
	}

	config := New(true, cwd)

	// determine and add main dirs to the config
	home, err := HomeDir()
	if err != nil {
		return nil, err
	}
	cacheDir, err := CacheDir(home)
	if err != nil {
		return nil, err
	}
	dataDir, err := DataDir(home)
	if err != nil {
		return nil, err
	}
	if err := config.Merge(php.ArrayOf("config", php.ArrayOf(
		"home", home,
		"cache-dir", cacheDir,
		"data-dir", dataDir,
	)), SourceDefault); err != nil {
		return nil, err
	}

	// load global config
	homeDir, err := config.getString("home")
	if err != nil {
		return nil, err
	}
	file, err := json.NewFile(homeDir+"/config.json", nil, nil)
	if err != nil {
		return nil, err
	}
	if file.Exists() {
		if out != nil {
			out.WriteError("Loading config file "+file.Path(), true, io.Debug)
		}
		if err := ValidateJSONSchema(out, file, json.LaxSchema, ""); err != nil {
			return nil, phperr.Call(err, `Composer\Factory::validateJsonSchema`, "Factory.php", 187)
		}
		data, err := file.Read()
		if err != nil {
			return nil, phperr.Call(err, `Composer\Json\JsonFile->read`, "Factory.php", 188)
		}
		a, ok := data.(*php.Array)
		if !ok {
			return nil, (&php.EngineError{Class: "TypeError", Message: "Composer\\Config::merge(): Argument #1 ($config) must be of type array, " + php.ZvalValueName(data) + " given"}).
				Called(`Composer\Config->merge`, phperr.At("Config.php", 199), "Factory.php", 188)
		}
		if err := config.Merge(a, file.Path()); err != nil {
			return nil, phperr.Call(err, `Composer\Config->merge`, "Factory.php", 188)
		}
	}
	config.SetConfigSource(NewJSONConfigSource(file, false))

	htaccessProtect, err := config.Get("htaccess-protect", 0)
	if err != nil {
		return nil, phperr.Call(err, `Composer\Config->get`, "Factory.php", 192)
	}
	if php.ToBool(htaccessProtect) {
		// Protect directory against web access. Since HOME could be
		// the www-data's user home and be web-accessible it is a
		// potential security risk
		for _, key := range [...]string{"home", "cache-dir", "data-dir"} {
			dir, err := config.getString(key)
			if err != nil {
				return nil, err
			}
			if _, err := os.Stat(dir + "/.htaccess"); err != nil {
				if !isDir(dir) {
					_ = os.MkdirAll(dir, 0o777)
				}
				_ = os.WriteFile(dir+"/.htaccess", []byte("Deny from all"), 0o666)
			}
		}
	}

	// load global auth file
	homeDir, err = config.getString("home")
	if err != nil {
		return nil, err
	}
	file, err = json.NewFile(homeDir+"/auth.json", nil, nil)
	if err != nil {
		return nil, err
	}
	if file.Exists() {
		if out != nil {
			out.WriteError("Loading config file "+file.Path(), true, io.Debug)
		}
		if err := ValidateJSONSchema(out, file, json.AuthSchema, ""); err != nil {
			return nil, phperr.Call(err, `Composer\Factory::validateJsonSchema`, "Factory.php", 214)
		}
		data, err := file.Read()
		if err != nil {
			return nil, phperr.Call(err, `Composer\Json\JsonFile->read`, "Factory.php", 215)
		}
		if err := config.Merge(php.ArrayOf("config", data), file.Path()); err != nil {
			return nil, phperr.Call(err, `Composer\Config->merge`, "Factory.php", 215)
		}
	}
	config.SetAuthConfigSource(NewJSONConfigSource(file, true))

	if err := LoadComposerAuthEnv(config, out); err != nil {
		return nil, phperr.Call(err, `Composer\Factory::loadComposerAuthEnv`, "Factory.php", 219)
	}

	return config, nil
}

// ComposerFile ports Factory::getComposerFile: the COMPOSER environment
// variable, trimmed, or ./composer.json.
func ComposerFile() (string, error) {
	if env, ok := util.GetEnv("COMPOSER"); ok {
		env = php.Trim(env)
		if env != "" {
			if isDir(env) {
				return "", &util.RuntimeError{Site: phperr.At("Factory.php", 231), Message: "The COMPOSER environment variable is set to " + env + " which is a directory, this variable should point to a composer.json or be left unset."}
			}

			return env, nil
		}
	}

	return "./composer.json", nil
}

// LockFile ports Factory::getLockFile: composer.json's lock file.
func LockFile(composerFile string) string {
	if pathinfoExtension(composerFile) == "json" {
		return composerFile[:len(composerFile)-4] + "lock"
	}

	return composerFile + ".lock"
}

// pathinfoExtension is pathinfo($path, PATHINFO_EXTENSION): the part of
// the basename after its last dot.
func pathinfoExtension(p string) string {
	base := php.Basename(p, "")
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		return base[i+1:]
	}

	return ""
}

// LoadComposerAuthEnv ports Factory::loadComposerAuthEnv: merges the JSON
// of the COMPOSER_AUTH environment variable into config.
func LoadComposerAuthEnv(config *Config, out io.IO) error {
	composerAuthEnv, ok := util.GetEnv("COMPOSER_AUTH")
	if !ok || composerAuthEnv == "" {
		return nil
	}

	authData, _ := php.JSONDecode(composerAuthEnv, false)
	if authData == nil {
		return &util.UnexpectedValueError{Site: phperr.At("Factory.php", 692), Message: "COMPOSER_AUTH environment variable is malformed, should be a valid JSON object"}
	}

	if out != nil {
		out.WriteError("Loading auth config from COMPOSER_AUTH", true, io.Debug)
	}
	if err := ValidateJSONSchema(out, authData, json.AuthSchema, "COMPOSER_AUTH"); err != nil {
		return phperr.Call(err, `Composer\Factory::validateJsonSchema`, "Factory.php", 698)
	}
	if authData, _ = php.JSONDecode(composerAuthEnv, true); authData != nil {
		return phperr.Call(config.Merge(php.ArrayOf("config", authData), "COMPOSER_AUTH"), `Composer\Config->merge`, "Factory.php", 701)
	}

	return nil
}

// useXdg ports Factory::useXdg: whether any XDG_ variable is set or
// /etc/xdg exists.
func useXdg() bool {
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "XDG_") {
			return true
		}
	}

	return isDir("/etc/xdg")
}

// userDir ports Factory::getUserDir: $HOME with forward slashes and no
// trailing slash.
func userDir() (string, error) {
	home, ok := getEnvTruthy("HOME")
	if !ok {
		return "", &util.RuntimeError{Site: phperr.At("Factory.php", 727), Message: "The HOME or COMPOSER_HOME environment variable must be set for composer to run correctly"}
	}

	return strings.TrimRight(strings.ReplaceAll(home, `\`, "/"), "/"), nil
}

// ValidateJSONSchema ports Factory::validateJsonSchema: validation errors
// become a warning on out, or an UnexpectedValueException without out.
// fileOrData is a *json.File or decoded data (then source names it).
func ValidateJSONSchema(out io.IO, fileOrData any, schema int, source string) error {
	if util.IsInputCompletionProcess() {
		return nil
	}

	var err error
	if file, ok := fileOrData.(*json.File); ok {
		err = phperr.Call(file.ValidateSchema(schema, ""), `Composer\Json\JsonFile->validateSchema`, "Factory.php", 745)
	} else {
		if source == "" {
			return &util.InvalidArgumentError{Site: phperr.At("Factory.php", 748), Message: "$source is required to be provided if $fileOrData is arbitrary data"}
		}
		err = phperr.Call(json.ValidateJSONSchema(source, fileOrData, schema, ""), `Composer\Json\JsonFile::validateJsonSchema`, "Factory.php", 750)
	}

	var ve *json.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	msg := ve.Message + ", this may result in errors and should be resolved:\n - " + strings.Join(ve.Errors, "\n - ")
	if out != nil {
		out.WriteError("<warning>"+msg+"</>", true, io.Normal)

		return nil
	}

	return &util.UnexpectedValueError{Site: phperr.At("Factory.php", 757), Message: msg}
}

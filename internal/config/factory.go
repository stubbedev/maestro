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
			return "", &util.RuntimeError{Message: "The APPDATA or COMPOSER_HOME environment variable must be set for composer to run correctly"}
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
// nil. With htaccess-protect on, the home, cache and data directories are
// created and each is given a .htaccess, as Composer does.
func CreateConfig(out io.IO, cwd string) (*Config, error) {
	return createConfig(out, cwd, true)
}

// ReadConfig is CreateConfig without its side effects on the file system:
// it reads the same configuration but creates no directory and writes no
// .htaccess. It is for maestro's own lookups at startup, where Composer has
// not called Factory::createConfig yet.
func ReadConfig(out io.IO, cwd string) (*Config, error) {
	return createConfig(out, cwd, false)
}

func createConfig(out io.IO, cwd string, protect bool) (*Config, error) {
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
			return nil, err
		}
		data, err := file.Read()
		if err != nil {
			return nil, err
		}
		a, ok := data.(*php.Array)
		if !ok {
			return nil, &php.EngineError{Class: "TypeError", Message: "Composer\\Config::merge(): Argument #1 ($config) must be of type array, " + php.ZvalValueName(data) + " given"}
		}
		if err := config.Merge(a, file.Path()); err != nil {
			return nil, err
		}
	}
	config.SetConfigSource(NewJSONConfigSource(file, false))

	if protect {
		if err := protectDirs(config); err != nil {
			return nil, err
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
			return nil, err
		}
		data, err := file.Read()
		if err != nil {
			return nil, err
		}
		if err := config.Merge(php.ArrayOf("config", data), file.Path()); err != nil {
			return nil, err
		}
	}
	config.SetAuthConfigSource(NewJSONConfigSource(file, true))

	if err := LoadComposerAuthEnv(config, out); err != nil {
		return nil, err
	}

	return config, nil
}

// protectDirs is Factory::createConfig's htaccess-protect step: with it on,
// the home, cache and data directories are created and get a .htaccess.
func protectDirs(config *Config) error {
	htaccessProtect, err := config.Get("htaccess-protect", 0)
	if err != nil {
		return err
	}
	if !php.ToBool(htaccessProtect) {
		return nil
	}

	// Protect directory against web access. Since HOME could be
	// the www-data's user home and be web-accessible it is a
	// potential security risk
	for _, key := range [...]string{"home", "cache-dir", "data-dir"} {
		dir, err := config.getString(key)
		if err != nil {
			return err
		}
		if _, err := os.Stat(dir + "/.htaccess"); err != nil {
			if !isDir(dir) {
				_ = os.MkdirAll(dir, 0o777)
			}
			_ = os.WriteFile(dir+"/.htaccess", []byte("Deny from all"), 0o666)
		}
	}

	return nil
}

// ComposerFile ports Factory::getComposerFile: the COMPOSER environment
// variable, trimmed, or ./composer.json.
func ComposerFile() (string, error) {
	if env, ok := util.GetEnv("COMPOSER"); ok {
		env = php.Trim(env)
		if env != "" {
			if isDir(env) {
				return "", &util.RuntimeError{Message: "The COMPOSER environment variable is set to " + env + " which is a directory, this variable should point to a composer.json or be left unset."}
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
		return &util.UnexpectedValueError{Message: "COMPOSER_AUTH environment variable is malformed, should be a valid JSON object"}
	}

	if out != nil {
		out.WriteError("Loading auth config from COMPOSER_AUTH", true, io.Debug)
	}
	if err := ValidateJSONSchema(out, authData, json.AuthSchema, "COMPOSER_AUTH"); err != nil {
		return err
	}
	if authData, _ = php.JSONDecode(composerAuthEnv, true); authData != nil {
		return config.Merge(php.ArrayOf("config", authData), "COMPOSER_AUTH")
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
		return "", &util.RuntimeError{Message: "The HOME or COMPOSER_HOME environment variable must be set for composer to run correctly"}
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
		err = file.ValidateSchema(schema, "")
	} else {
		if source == "" {
			return &util.InvalidArgumentError{Message: "$source is required to be provided if $fileOrData is arbitrary data"}
		}
		err = json.ValidateJSONSchema(source, fileOrData, schema, "")
	}

	var ve *json.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	msg := ve.Message + ", this may result in errors and should be resolved:" + php.EOL + " - " + strings.Join(ve.Errors, php.EOL+" - ")
	if out != nil {
		out.WriteError("<warning>"+msg+"</>", true, io.Normal)

		return nil
	}

	return &util.UnexpectedValueError{Message: msg}
}

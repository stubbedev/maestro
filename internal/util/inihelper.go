// Ports src/Composer/Util/IniHelper.php.

package util

import (
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// IniGetAll ports IniHelper::getAll: the php.ini locations, the loaded one
// first (possibly empty). XdebugHandler keeps the original locations in
// COMPOSER_ORIGINAL_INIS across a restart; otherwise they are those of the
// PHP binary, which loaded supplies (php_ini_loaded_file, then
// php_ini_scanned_files).
func IniGetAll(loaded func() []string) []string {
	if env, ok := os.LookupEnv("COMPOSER_ORIGINAL_INIS"); ok {
		return strings.Split(env, string(os.PathListSeparator))
	}

	return loaded()
}

// IniGetMessage ports IniHelper::getMessage: describes the location of the
// loaded php.ini file(s).
func IniGetMessage(loaded func() []string) string {
	paths := IniGetAll(loaded)

	if len(paths) > 0 && phpEmpty(paths[0]) {
		paths = paths[1:]
	}

	if len(paths) == 0 || phpEmpty(paths[0]) {
		return "A php.ini file does not exist. You will have to create one."
	}

	if len(paths) > 1 {
		return "Your command-line PHP is using multiple ini files. Run `php --ini` to show them."
	}

	return "The php.ini used by your command-line PHP is: " + paths[0]
}

// phpEmpty is PHP's empty() for a string.
func phpEmpty(s string) bool {
	return !php.Truthy(s)
}

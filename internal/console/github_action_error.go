// Ports src/Composer/Console/GithubActionError.php (Composer).

package console

import (
	"os"
	"strconv"
	"strings"
)

// GithubActionError emits GitHub Actions "::error" workflow commands. The
// IO it writes through is given as a function (console cannot import
// internal/io): it receives each command like IOInterface::write($message)
// with its default newline and verbosity.
type GithubActionError struct {
	write func(message string)
}

// NewGithubActionError mirrors new GithubActionError($io).
func NewGithubActionError(write func(message string)) *GithubActionError {
	return &GithubActionError{write: write}
}

// envTruthy is the truthiness of Platform::getEnv($name).
func envTruthy(name string) bool {
	v := os.Getenv(name)

	return v != "" && v != "0"
}

// Emit writes message as an error annotation when running in GitHub
// Actions. An empty file or a zero line means none (PHP null).
func (g *GithubActionError) Emit(message, file string, line int) {
	if !envTruthy("GITHUB_ACTIONS") || envTruthy("COMPOSER_TESTS_ARE_RUNNING") {
		return
	}

	message = escapeData(message)
	hasFile := file != "" && file != "0"

	switch {
	case hasFile && line != 0:
		g.write("::error file=" + escapeProperty(file) + ",line=" + strconv.Itoa(line) + "::" + message)
	case hasFile:
		g.write("::error file=" + escapeProperty(file) + "::" + message)
	default:
		g.write("::error ::" + message)
	}
}

// see https://github.com/actions/toolkit/blob/4f7fb6513a355689f69f0849edeb369a4dc81729/packages/core/src/command.ts#L80-L85
var dataEscaper = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")

// see https://github.com/actions/toolkit/blob/4f7fb6513a355689f69f0849edeb369a4dc81729/packages/core/src/command.ts#L87-L94
var propertyEscaper = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")

func escapeData(data string) string { return dataEscaper.Replace(data) }

func escapeProperty(property string) string { return propertyEscaper.Replace(property) }

// Ports AutoloadGenerator::buildExclusionRegex
// (src/Composer/Autoload/AutoloadGenerator.php).

package autoload

import (
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/fspath"
)

// buildExclusionRegex ports buildExclusionRegex: the exclude-from-classmap
// patterns that can apply below dir, as one regex (nil for none). The
// regex of a pattern list is compiled once per process (php.Compile's
// cache, as PHP's).
func buildExclusionRegex(dir string, excluded []string) classmap.Matcher {
	if len(excluded) == 0 {
		return nil
	}

	// filter excluded patterns here to only use those matching $dir
	// exclude-from-classmap patterns are all realpath'd so we can only
	// filter them if $dir exists so that realpath($dir) will work
	// if $dir does not exist, it should anyway not find anything there so
	// no trouble
	if _, err := os.Stat(dir); err == nil {
		// transform $dir in the same way that exclude-from-classmap
		// patterns are transformed so we can match them against each other
		real, _ := php.Realpath(dir)
		dirMatch := php.PregQuote(strings.ReplaceAll(real, `\`, "/"), "")
		// also match against the non-realpath version for symlinks
		absDir := dir
		if !fspath.IsAbsolutePath(dir) {
			cwd, _ := util.GetCwd(false)
			absDir = util.Realpath(cwd) + "/" + dir
		}
		dirMatchNormalized := php.PregQuote(strings.ReplaceAll(fspath.NormalizePath(absDir), `\`, "/"), "")
		isSymlink := dirMatch != dirMatchNormalized

		related := func(pattern, dir string) bool {
			return strings.HasPrefix(pattern, dir) || strings.HasPrefix(dir, pattern)
		}
		kept := make([]string, 0, len(excluded))
		for _, pattern := range excluded {
			// extract the constant string prefix of the pattern here, until
			// we reach a non-escaped regex special character
			prefix := constantPrefix(pattern)
			// if the pattern is not a subset or superset of $dir, it is
			// unrelated and we skip it
			if related(prefix, dirMatch) || (isSymlink && related(prefix, dirMatchNormalized)) {
				kept = append(kept, pattern)
			}
		}
		excluded = kept
	}

	if len(excluded) == 0 {
		return nil
	}

	re, err := php.Compile("{(" + strings.Join(excluded, "|") + ")}")
	if err != nil {
		// preg_match() warns about a pattern that does not compile, which
		// Composer's error handler turns into an exception at the first
		// match (in composer/pcre's Preg::pregMatch).
		return errMatcher{&util.ErrorException{Message: "preg_match(): " + err.Error()}}
	}

	return re
}

// constantPrefix is what Composer's
//
//	Preg::replace('{^(([^.+*?\[^\]$(){}=!<>|:\\\\#-]+|\\\\[.+*?\[^\]$(){}=!<>|:#-])*).*}', '$1', $pattern)
//
// leaves of pattern, without running the regex (it cost a script
// dispatch's class loader ~10 ms): the run of bytes that are not special
// and of escaped special characters that pattern starts with, then what
// follows the first newline after that run, which `.*` stops at.
func constantPrefix(pattern string) string {
	i := 0
	for i < len(pattern) {
		if c := pattern[i]; c == '\\' {
			if i+1 == len(pattern) || !isPrefixSpecial(pattern[i+1]) {
				break
			}
			i += 2
		} else if isPrefixSpecial(c) {
			break
		} else {
			i++
		}
	}
	if nl := strings.IndexByte(pattern[i:], '\n'); nl >= 0 {
		return pattern[:i] + pattern[i+nl:]
	}

	return pattern[:i]
}

// isPrefixSpecial reports whether c ends constantPrefix's run unescaped:
// the regex's class [.+*?\[^\]$(){}=!<>|:#-].
func isPrefixSpecial(c byte) bool {
	return strings.IndexByte(`.+*?[^]$(){}=!<>|:#-`, c) >= 0
}

// errMatcher is a matcher that fails.
type errMatcher struct{ err error }

func (m errMatcher) IsMatch(string) (bool, error) { return false, m.err }

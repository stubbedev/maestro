// Ports AutoloadGenerator::buildExclusionRegex
// (src/Composer/Autoload/AutoloadGenerator.php).

package autoload

import (
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// {^(([^.+*?\[^\]$(){}=!<>|:\\#-]+|\\[.+*?\[^\]$(){}=!<>|:#-])*).*}: the
// constant prefix of a pattern, up to its first unescaped special character.
var reConstantPrefix = php.MustCompile(`{^(([^.+*?\[^\]$(){}=!<>|:\\#-]+|\\[.+*?\[^\]$(){}=!<>|:#-])*).*}`)

// buildExclusionRegex ports buildExclusionRegex: the exclude-from-classmap
// patterns that can apply below dir, as one regex (nil for none).
func buildExclusionRegex(dir string, excluded []string) classmap.Matcher {
	return (&exclusionRegexes{}).build(dir, excluded)
}

// exclusionRegexes builds the exclusion regexes of one dump: what the
// rules share (the patterns' constant prefixes, the regexes compiled from
// the same patterns) is worked out once (deliberate deviation 3, speed).
type exclusionRegexes struct {
	prefixes map[string]prefixResult
	compiled map[string]classmap.Matcher
}

type prefixResult struct {
	prefix string
	err    error
}

// build is buildExclusionRegex.
func (c *exclusionRegexes) build(dir string, excluded []string) classmap.Matcher {
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
		real, _ := util.RealpathOK(dir)
		dirMatch := php.PregQuote(strings.ReplaceAll(real, `\`, "/"), "")
		// also match against the non-realpath version for symlinks
		absDir := dir
		if !util.IsAbsolutePath(dir) {
			cwd, _ := util.GetCwd(false)
			absDir = util.Realpath(cwd) + "/" + dir
		}
		dirMatchNormalized := php.PregQuote(strings.ReplaceAll(util.NormalizePath(absDir), `\`, "/"), "")
		isSymlink := dirMatch != dirMatchNormalized

		related := func(pattern, dir string) bool {
			return strings.HasPrefix(pattern, dir) || strings.HasPrefix(dir, pattern)
		}
		kept := make([]string, 0, len(excluded))
		for _, pattern := range excluded {
			// extract the constant string prefix of the pattern here, until
			// we reach a non-escaped regex special character
			prefix, err := c.constantPrefix(pattern)
			if err != nil {
				return errMatcher{err}
			}
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

	pattern := "{(" + strings.Join(excluded, "|") + ")}"
	if m, ok := c.compiled[pattern]; ok {
		return m
	}
	var m classmap.Matcher
	re, err := php.Compile(pattern)
	if err != nil {
		// preg_match() warns about a pattern that does not compile, which
		// Composer's error handler turns into an exception at the first
		// match (in composer/pcre's Preg::pregMatch).
		m = errMatcher{&util.ErrorException{Message: "preg_match(): " + err.Error(), Site: phperr.At("Preg.php", 430)}}
	} else {
		m = re
	}
	if c.compiled == nil {
		c.compiled = map[string]classmap.Matcher{}
	}
	c.compiled[pattern] = m

	return m
}

// constantPrefix is a pattern's constant prefix (reConstantPrefix).
func (c *exclusionRegexes) constantPrefix(pattern string) (string, error) {
	if r, ok := c.prefixes[pattern]; ok {
		return r.prefix, r.err
	}
	prefix, _, err := reConstantPrefix.Replace(pattern, "$1", -1)
	if c.prefixes == nil {
		c.prefixes = map[string]prefixResult{}
	}
	c.prefixes[pattern] = prefixResult{prefix, err}

	return prefix, err
}

// errMatcher is a matcher that fails.
type errMatcher struct{ err error }

func (m errMatcher) IsMatch(string) (bool, error) { return false, m.err }

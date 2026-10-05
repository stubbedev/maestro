// Ports src/Composer/Package/Archiver/BaseExcludeFilter.php,
// ComposerExcludeFilter.php and GitExcludeFilter.php.

package archiver

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"syscall"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// ExcludePattern is one of BaseExcludeFilter's [$pattern, $negate,
// $stripLeadingSlash] arrays.
type ExcludePattern struct {
	Pattern           string
	Negate            bool
	StripLeadingSlash bool
}

// excludeFilter is what ArchivableFilesFinder calls on each filter.
type excludeFilter interface {
	Filter(relativePath string, exclude bool) (bool, error)
}

// BaseExcludeFilter ports BaseExcludeFilter.
type BaseExcludeFilter struct {
	sourcePath      string
	excludePatterns []ExcludePattern
}

// Filter ports BaseExcludeFilter::filter: whether the file at relativePath
// (relative to the source path, with a leading slash) is excluded, given
// that a previous filter decided exclude. Negated patterns overwrite the
// decisions of previous filters.
//
// A PcreException is suppressed, as in PHP; a pattern that does not
// compile makes preg_match() emit a warning first, which Composer's
// ErrorHandler turns into the returned *util.ErrorException.
func (f *BaseExcludeFilter) Filter(relativePath string, exclude bool) (bool, error) {
	for _, p := range f.excludePatterns {
		path := relativePath
		if p.StripLeadingSlash {
			path = php.Substr(relativePath, 1)
		}

		ok, err := php.PregIsMatch(p.Pattern, path)
		if pcreErr, isPcre := errors.AsType[*php.PcreError](err); isPcre && pcreErr.Warning != "" {
			return exclude, &util.ErrorException{Message: pcreErr.Warning, Site: phperr.At("Preg.php", 430)}
		}

		if err == nil && ok {
			exclude = !p.Negate
		}
	}

	return exclude, nil
}

// parseLines is BaseExcludeFilter::parseLines: the patterns lineParser
// makes of the lines that are neither blank nor comments.
func parseLines(lines []string, lineParser func(line string) (ExcludePattern, bool)) []ExcludePattern {
	var patterns []ExcludePattern

	for _, line := range lines {
		line = php.Trim(line)

		if !php.ToBool(line) || strings.HasPrefix(line, "#") {
			continue
		}

		if p, ok := lineParser(line); ok {
			patterns = append(patterns, p)
		}
	}

	return patterns
}

// generatePatterns is BaseExcludeFilter::generatePatterns.
func generatePatterns(rules []string) []ExcludePattern {
	patterns := make([]ExcludePattern, 0, len(rules))
	for _, rule := range rules {
		patterns = append(patterns, generatePattern(rule))
	}

	return patterns
}

// generatePattern is BaseExcludeFilter::generatePattern: the exclude
// pattern of a gitignore rule.
func generatePattern(rule string) ExcludePattern {
	negate := false
	pattern := ""

	if rule != "" && rule[0] == '!' {
		negate = true
		rule = strings.TrimLeft(rule, "!")
	}

	if firstSlashPosition := strings.IndexByte(rule, '/'); firstSlashPosition == 0 {
		pattern = "^/"
	} else if firstSlashPosition < 0 || len(rule)-1 == firstSlashPosition {
		pattern = "/"
	}

	rule = strings.Trim(rule, "/")

	// remove delimiters as well as caret (^) and dollar sign ($) from the
	// regex
	regex := globToRegex(rule)
	rule = regex[2 : len(regex)-2]

	return ExcludePattern{Pattern: "{" + pattern + rule + "(?=$|/)}", Negate: negate}
}

// ComposerExcludeFilter ports ComposerExcludeFilter: the exclude rules of
// composer.json's archive.exclude.
type ComposerExcludeFilter struct{ BaseExcludeFilter }

// NewComposerExcludeFilter ports ComposerExcludeFilter::__construct.
func NewComposerExcludeFilter(sourcePath string, excludeRules []string) *ComposerExcludeFilter {
	return &ComposerExcludeFilter{BaseExcludeFilter{sourcePath: sourcePath, excludePatterns: generatePatterns(excludeRules)}}
}

// GitExcludeFilter ports GitExcludeFilter: the export-ignore attributes of
// the source path's .gitattributes.
type GitExcludeFilter struct{ BaseExcludeFilter }

// NewGitExcludeFilter ports GitExcludeFilter::__construct. Reading the
// file fails with the *util.ErrorException Composer's ErrorHandler makes
// of file()'s warning.
func NewGitExcludeFilter(sourcePath string) (*GitExcludeFilter, error) {
	f := &GitExcludeFilter{BaseExcludeFilter{sourcePath: sourcePath}}

	path := sourcePath + "/.gitattributes"
	if _, err := os.Stat(path); err != nil {
		return f, nil
	}

	lines, err := phpFile(path)
	if err != nil {
		return nil, err
	}

	f.excludePatterns = append(f.excludePatterns, parseLines(lines, f.ParseGitAttributesLine)...)

	return f, nil
}

// ParseGitAttributesLine ports GitExcludeFilter::parseGitAttributesLine:
// the exclude pattern of an export-ignore line, or false (PHP's null).
func (f *GitExcludeFilter) ParseGitAttributesLine(line string) (ExcludePattern, bool) {
	parts, err := php.PregSplit(`#\s+#`, line, -1, 0)
	if err != nil || len(parts) != 2 {
		return ExcludePattern{}, false
	}

	switch parts[1] {
	case "export-ignore":
		return generatePattern(parts[0]), true
	case "-export-ignore":
		return generatePattern("!" + parts[0]), true
	}

	return ExcludePattern{}, false
}

// phpFile is file($path): the lines of a file, each with its "\n". Its
// failures are the warnings PHP emits, as ErrorExceptions (only
// GitExcludeFilter calls it).
func phpFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)

	switch {
	case errors.Is(err, syscall.EISDIR):
		// PHP opens directories, then fails to read them
		return nil, &util.ErrorException{Message: "file(): Read of 8192 bytes failed with errno=21 Is a directory", Site: phperr.At("GitExcludeFilter.php", 37)}
	case err != nil:
		return nil, &util.ErrorException{Message: "file(" + path + "): Failed to open stream: " + util.Strerror(err), Site: phperr.At("GitExcludeFilter.php", 37)}
	}

	var lines []string
	for line := range bytes.Lines(data) {
		lines = append(lines, string(line))
	}

	return lines, nil
}

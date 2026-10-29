// Ports src/ClassMap.php.

package classmap

import (
	"iter"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// ClassMap is the result of a scan: class names mapped to the file they
// were found in, in insertion order (a PHP array), plus the ambiguous
// classes and PSR violations seen along the way. The zero value is empty.
type ClassMap struct {
	classes []string // keys of $map in order
	paths   []string // values of $map, parallel to classes
	index   map[string]int

	ambiguous      []AmbiguousClass
	ambiguousIndex map[string]int

	// psrViolations is $psrViolations: an ordered map from path to the
	// violations found in it. Entries removed by unset() are marked dead
	// (nil list) so that a later insert of the same path goes to the end,
	// as in PHP.
	psrViolations []psrViolationsOfPath
	psrIndex      map[string]int
	psrLive       int
}

// AmbiguousClass is a class found in more than one file: Paths are the
// files other than the one it is mapped to, in the order they were found.
type AmbiguousClass struct {
	Class string
	Paths []string
}

// PsrViolation is one entry of getRawPsrViolations().
type PsrViolation struct {
	Warning   string
	ClassName string
}

type psrViolationsOfPath struct {
	path       string
	violations []PsrViolation // nil once removed
}

// Map returns the class map, which is a list of paths indexed by class name
// (getMap()), in order.
func (m *ClassMap) Map() iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for i, class := range m.classes {
			if !yield(class, m.paths[i]) {
				return
			}
		}
	}
}

// Classes returns the mapped class names in order (array_keys(getMap())).
// The slice is owned by the ClassMap.
func (m *ClassMap) Classes() []string { return m.classes }

// PsrViolations returns the warning strings of the PSR-0/4 violations that
// were detected (getPsrViolations()).
//
// Violations are for ex a class which is in the wrong file/directory and
// thus should not be found using psr-0/psr-4 autoloading but was found by
// the ClassMapGenerator as it scans all files. This only happens when
// scanning paths using the psr-0/psr-4 autoload type.
func (m *ClassMap) PsrViolations() []string {
	var warnings []string
	for _, p := range m.psrViolations {
		for _, v := range p.violations {
			warnings = append(warnings, v.Warning)
		}
	}

	return warnings
}

// RawPsrViolations returns the violations by file path
// (getRawPsrViolations()), in order.
func (m *ClassMap) RawPsrViolations() iter.Seq2[string, []PsrViolation] {
	return func(yield func(string, []PsrViolation) bool) {
		for _, p := range m.psrViolations {
			if p.violations != nil && !yield(p.path, p.violations) {
				return
			}
		}
	}
}

// AmbiguousClasses returns the classes that were found in several files
// (getAmbiguousClasses()). To get the path a class is mapped to, call
// ClassPath.
//
// Paths matching duplicatesFilter are left out, and classes left without
// paths are dropped; a nil filter returns everything (PHP's false). PHP's
// default filter, which ignores paths containing test(s), fixture(s),
// example(s) or stub(s) as those are typically dummy classes, is
// DefaultDuplicatesFilter.
func (m *ClassMap) AmbiguousClasses(duplicatesFilter Matcher) ([]AmbiguousClass, error) {
	if duplicatesFilter == nil {
		return m.ambiguous, nil
	}
	var result []AmbiguousClass
	for _, a := range m.ambiguous {
		var paths []string
		for _, path := range a.Paths {
			matched, err := duplicatesFilter.IsMatch(strings.ReplaceAll(path, `\`, "/"))
			if err != nil {
				return nil, err
			}
			if !matched {
				paths = append(paths, path)
			}
		}
		if len(paths) > 0 {
			result = append(result, AmbiguousClass{Class: a.Class, Paths: paths})
		}
	}

	return result, nil
}

// AmbiguousFolders returns the sets of paths which are identical except
// for their casing (getAmbiguousFolders()). Case-insensitive filesystems
// fold each such set into a single file or directory, so a project which
// autoloads correctly on a case-sensitive filesystem can behave
// differently there.
//
// Every folder or file whose own name differs in casing is reported, while
// differences a path only inherits from a parent folder are reported once
// at that parent's level. A nil filter returns everything (PHP's false);
// DefaultDuplicatesFilter ignores the paths of tests, fixtures, examples and
// stubs, as those are typically dummy classes, but only leaves a set out
// when every path below it is ignored.
func (m *ClassMap) AmbiguousFolders(duplicatesFilter Matcher) ([][]string, error) {
	paths := make([]string, 0, len(m.classes)+len(m.ambiguous))
	for _, path := range m.paths {
		paths = append(paths, strings.ReplaceAll(path, `\`, "/"))
	}
	// a class found in several files only has one of them in the map, but the
	// others are just as relevant here, and typically are the renamed
	// folder's stale copies
	for _, a := range m.ambiguous {
		for _, path := range a.Paths {
			paths = append(paths, strings.ReplaceAll(path, `\`, "/"))
		}
	}
	// sorted so the result does not depend on scan order, and so that each
	// path can skip the folders it shares with the previous one as those
	// were already registered
	slices.Sort(paths)

	// first maps a folded prefix to the first path seen for it, which has the
	// lowest casing as paths are sorted
	first := make(map[string]string, len(paths))
	// variants maps a folded prefix to the name of its last segment as
	// written, to the first path seen with that name
	variants := make(map[string][]string)
	variantNames := make(map[string]map[string]bool)
	previous := ""
	for _, path := range paths {
		// the common prefix with the previous path: only whole folders were
		// registered already, so a path can skip those it shares with it
		common := commonPrefix(path, previous)
		skipUntil := 0
		if common > 0 {
			if i := strings.LastIndexByte(path[:common], '/'); i >= 0 {
				skipUntil = i
			}
		}
		previous = path

		// walk every prefix of the path, the full path included so that
		// files which only differ in casing are caught as well as the
		// folders above them
		foldedPath := foldCase(path)
		// drive letters and stream wrapper schemes are never case sensitive
		// so they are skipped, as ClassMapGenerator::normalizePath does
		rootLength := pathRootLength(path)
		offset := 0
		for name := range strings.SplitSeq(path, "/") {
			end := offset + len(name)
			if name != "" && end > rootLength && end > skipUntil {
				foldedPrefix := foldedPath[:end]
				if f, ok := first[foldedPrefix]; !ok {
					first[foldedPrefix] = path[:offset] + name
				} else if f[offset:] != name && !variantNames[foldedPrefix][name] {
					// a name written the same way as the first one only differs
					// by its parents, which get reported at their own level
					if variantNames[foldedPrefix] == nil {
						variantNames[foldedPrefix] = make(map[string]bool)
					}
					variantNames[foldedPrefix][name] = true
					variants[foldedPrefix] = append(variants[foldedPrefix], path[:offset]+name)
				}
			}
			offset = end + 1
		}
	}

	if len(variants) == 0 {
		return nil, nil
	}

	// only look for paths which are not filtered out once something was found
	var relevant map[string]bool
	if duplicatesFilter != nil {
		relevant = make(map[string]bool, len(paths))
		for _, path := range paths {
			matched, err := duplicatesFilter.IsMatch(path)
			if err != nil {
				return nil, err
			}
			if matched {
				continue
			}
			foldedPath := foldCase(path)
			offset := 0
			for {
				i := strings.IndexByte(foldedPath[offset:], '/')
				if i < 0 {
					break
				}
				relevant[foldedPath[:offset+i]] = true
				offset += i + 1
			}
			relevant[foldedPath] = true
		}
	}

	foldedPrefixes := make([]string, 0, len(variants))
	for foldedPrefix := range variants {
		foldedPrefixes = append(foldedPrefixes, foldedPrefix)
	}
	slices.Sort(foldedPrefixes)

	var ambiguous [][]string
	for _, foldedPrefix := range foldedPrefixes {
		if relevant != nil && !relevant[foldedPrefix] {
			continue
		}
		// already sorted as the paths were walked in order
		ambiguous = append(ambiguous, append([]string{first[foldedPrefix]}, variants[foldedPrefix]...))
	}

	return ambiguous, nil
}

// rootPrefix is the drive letter or stream wrapper scheme at the start of a
// path, which is case insensitive, in findAmbiguousFolders()' pattern
// '{^(?:[0-9a-z]{2,}+:(?://(?:[a-z]:)?)?|[a-z]:)}i'.
var rootPrefix = php.MustCompile(`{^(?:[0-9a-z]{2,}+:(?://(?:[a-z]:)?)?|[a-z]:)}i`)

// pathRootLength is the length of the drive letter or stream wrapper scheme
// at the start of path, 0 for none.
func pathRootLength(path string) int {
	m, err := rootPrefix.Match(path)
	if err != nil || m == nil {
		return 0
	}

	return len(m.Get(0))
}

// foldCase is foldCase(): it lowercases ASCII characters only, as strtolower
// is locale dependent before PHP 8.2.
func foldCase(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}

	return string(b)
}

// Sort sorts the class map alphabetically by class names (ksort()). Class
// names are never numeric strings, so PHP compares them bytewise.
func (m *ClassMap) Sort() {
	order := make([]int, len(m.classes))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return strings.Compare(m.classes[a], m.classes[b]) })
	classes := make([]string, len(order))
	paths := make([]string, len(order))
	for i, j := range order {
		classes[i], paths[i] = m.classes[j], m.paths[j]
		m.index[classes[i]] = i
	}
	m.classes, m.paths = classes, paths
}

// AddClass maps className to path (addClass()), replacing any previous
// path, and drops the PSR violations recorded for that path.
func (m *ClassMap) AddClass(className, path string) {
	m.unsetPsrViolations(strings.ReplaceAll(path, `\`, "/"))
	if i, ok := m.index[className]; ok {
		m.paths[i] = path

		return
	}
	if m.index == nil {
		m.index = make(map[string]int)
	}
	m.index[className] = len(m.classes)
	m.classes = append(m.classes, className)
	m.paths = append(m.paths, path)
}

// ClassPath returns the path className is mapped to (getClassPath()).
func (m *ClassMap) ClassPath(className string) (string, error) {
	if i, ok := m.index[className]; ok {
		return m.paths[i], nil
	}

	return "", newException(classOutOfBounds, "Class "+className+" is not present in the map")
}

// HasClass reports whether className is mapped.
func (m *ClassMap) HasClass(className string) bool {
	_, ok := m.index[className]

	return ok
}

// path returns the path of a class known to be mapped.
func (m *ClassMap) path(className string) string { return m.paths[m.index[className]] }

// AddPsrViolation records a violation found in path.
func (m *ClassMap) AddPsrViolation(warning, className, path string) {
	path = strings.TrimRight(strings.ReplaceAll(path, `\`, "/"), "/")
	v := PsrViolation{Warning: warning, ClassName: className}
	if i, ok := m.psrIndex[path]; ok {
		m.psrViolations[i].violations = append(m.psrViolations[i].violations, v)

		return
	}
	if m.psrIndex == nil {
		m.psrIndex = make(map[string]int)
	}
	m.psrIndex[path] = len(m.psrViolations)
	m.psrViolations = append(m.psrViolations, psrViolationsOfPath{path: path, violations: []PsrViolation{v}})
	m.psrLive++
}

// ClearPsrViolationsByPath drops the violations of pathPrefix and of
// every path below it.
func (m *ClassMap) ClearPsrViolationsByPath(pathPrefix string) {
	pathPrefix = strings.TrimRight(strings.ReplaceAll(pathPrefix, `\`, "/"), "/")
	for _, p := range m.psrViolations {
		if p.violations != nil && (p.path == pathPrefix || strings.HasPrefix(p.path, pathPrefix+"/")) {
			m.unsetPsrViolations(p.path)
		}
	}
}

// unsetPsrViolations is unset($this->psrViolations[$path]).
func (m *ClassMap) unsetPsrViolations(path string) {
	i, ok := m.psrIndex[path]
	if !ok {
		return
	}
	delete(m.psrIndex, path)
	m.psrViolations[i].violations = nil
	m.psrLive--
	if m.psrLive == 0 {
		m.psrViolations = m.psrViolations[:0]
	}
}

// AddAmbiguousClass records another path className was found in.
func (m *ClassMap) AddAmbiguousClass(className, path string) {
	if i, ok := m.ambiguousIndex[className]; ok {
		m.ambiguous[i].Paths = append(m.ambiguous[i].Paths, path)

		return
	}
	if m.ambiguousIndex == nil {
		m.ambiguousIndex = make(map[string]int)
	}
	m.ambiguousIndex[className] = len(m.ambiguous)
	m.ambiguous = append(m.ambiguous, AmbiguousClass{Class: className, Paths: []string{path}})
}

// Count returns the number of mapped classes.
func (m *ClassMap) Count() int { return len(m.classes) }

// Matcher is a compiled PHP regex as the generator uses it:
// Preg::isMatch(), whether it matches anywhere in s, failing with the
// PcreException Preg throws. *php.Regexp is one. Matchers handed to a
// Generator are called from several goroutines at once.
type Matcher interface {
	IsMatch(s string) (bool, error)
}

// DefaultDuplicatesFilter is getAmbiguousClasses()' default filter,
// '{(?:^|/)(test|fixture|example|stub)s?/}i': a path segment that is test,
// fixture, example or stub, optionally plural, in any ASCII case, at the
// start of the path or after a slash.
var DefaultDuplicatesFilter Matcher = duplicatesFilter{}

type duplicatesFilter struct{}

func (duplicatesFilter) IsMatch(s string) (bool, error) { return matchesDuplicates(s), nil }

func matchesDuplicates(s string) bool {
	for i := -1; i < len(s); i++ {
		if i >= 0 && s[i] != '/' {
			continue
		}
		rest := s[i+1:]
		for _, word := range [...]string{"test", "fixture", "example", "stub"} {
			if len(rest) <= len(word) || !equalFold(rest[:len(word)], word) {
				continue
			}
			end := len(word)
			if rest[end] == 's' || rest[end] == 'S' {
				// s? is greedy but gives the s back if no slash follows.
				if end+1 < len(rest) && rest[end+1] == '/' {
					return true
				}
			}
			if rest[end] == '/' {
				return true
			}
		}
	}

	return false
}

// Ports src/Composer/Json/JsonManipulator.php.

package json

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// manipulatorDefines is JsonManipulator::DEFINES: a recursive grammar of
// JSON values the editing patterns refer to with (?&name).
const manipulatorDefines = `(?(DEFINE)
       (?<number>    -? (?= [1-9]|0(?!\d) ) \d++ (?:\.\d++)? (?:[eE] [+-]?+ \d++)? )
       (?<boolean>   true | false | null )
       (?<string>    " (?:[^"\\]*+ | \\ ["\\bfnrt\/] | \\ u [0-9A-Fa-f]{4} )* " )
       (?<array>     \[  (?:  (?&json) \s*+ (?: , (?&json) \s*+ )*+  )?+  \s*+ \] )
       (?<pair>      \s*+ (?&string) \s*+ : (?&json) \s*+ )
       (?<object>    \{  (?:  (?&pair)  (?: , (?&pair)  )*+  )?+  \s*+ \} )
       (?<json>      \s*+ (?: (?&number) | (?&boolean) | (?&string) | (?&array) | (?&object) ) )
    )`

// Patterns that do not depend on the arguments.
var (
	manipulatorObject     = php.MustCompile(`#^\{(.*)\}$#s`)
	linksNonEmpty         = php.MustCompile(`#^\s*\{\s*\S+.*?(\s*\}\s*)$#s`)
	childrenObject        = php.MustCompile(`#^\{(?P<leadingspace>\s*?)(?P<content>\S+.*?)?(?P<trailingspace>\s*)\}$#s`)
	childrenCleanObject   = php.MustCompile(`#^\{\s*?(?P<content>\S+.*?)?(?P<trailingspace>\s*)\}$#s`)
	childrenList          = php.MustCompile(`#^\[(?P<leadingspace>\s*?)(?P<content>\S+.*?)?(?P<trailingspace>\s*)\]$#s`)
	lastValueBeforeClose  = php.MustCompile(`#[^{\s](\s*)\}$#`)
	closingBrace          = php.MustCompile(`#\}$#`)
	trailingComma         = php.MustCompile(`#,\s*$#`)
	onlyClosingBrace      = php.MustCompile(`#^\}$#`)
	trailingCommaCapture  = php.MustCompile(`#,(\s*)$#`)
	emptyObjectDocument   = php.MustCompile(`#^\{\s*\}\s*$#`)
	repositoryURLRegex    = php.MustCompile(`{` + manipulatorDefines + `^(?P<start>\s*\{\s*(?:(?&string)\s*:\s*(?&json)\s*,\s*)*?"url"\s*:\s*)(?P<url>(?&string))(?P<end>.*)}sx`)
	sortPrefixPatterns    = [...]*php.Regexp{php.MustCompile(`/^php/`), php.MustCompile(`/^hhvm/`), php.MustCompile(`/^ext/`), php.MustCompile(`/^lib/`), php.MustCompile(`/^\D/`)}
	sortPrefixReplacement = [...]string{`0-$0`, `1-$0`, `2-$0`, `3-$0`, `4-$0`}
)

// Manipulator is Composer\Json\JsonManipulator: it edits a composer.json
// document in place with regular expressions, keeping its formatting.
type Manipulator struct {
	contents string
	newline  string
	indent   string
	// regexps caches the patterns built from arguments; the same ones are
	// compiled several times per operation (PHP's PCRE cache does the same).
	regexps map[string]*php.Regexp
}

// typeError is the TypeError PHP raises on a code path of
// JsonManipulator it rejects at run time.
func typeError(msg string) error { return &php.EngineError{Class: php.ClassTypeError, Message: msg} }

// The warnings PHP raises on the code paths below, which Composer's
// ErrorHandler turns into ErrorExceptions.

func warning(msg string) error { return &util.ErrorException{Message: msg} }

// offsetRead is $container[$key] read without isset or ??: a missing key
// or a non-array container is a warning.
func offsetRead(container, key any) (any, error) {
	switch c := container.(type) {
	case *php.Array:
		k := php.ToKey(key)
		if v, ok := c.GetKey(k); ok {
			return v, nil
		}
		if k.IsInt() {
			return nil, warning("Undefined array key " + k.String())
		}

		return nil, warning(`Undefined array key "` + k.String() + `"`)
	case string:
		if s, ok := key.(string); ok && !php.StrKey(s).IsInt() {
			return nil, typeError("Cannot access offset of type string on string")
		}
		if i, ok := stringOffset(c, key); ok {
			return c[i : i+1], nil
		}

		return nil, warning("Uninitialized string offset " + php.ToKey(key).String())
	}

	return nil, warning("Trying to access array offset on " + zvalValueName(container))
}

// foreachable is the check of foreach on a value that may not be an array.
func foreachable(v any) (*php.Array, error) {
	if arr, ok := v.(*php.Array); ok {
		return arr, nil
	}

	return nil, warning("foreach() argument must be of type array|object, " + zvalValueName(v) + " given")
}

// zvalValueName ports zend_zval_value_name, the type a TypeError names.
func zvalValueName(v any) string { return php.ZvalValueName(v) }

// NewManipulator ports JsonManipulator::__construct.
func NewManipulator(contents string) (*Manipulator, error) {
	contents = php.Trim(contents)
	if contents == "" {
		contents = "{}"
	}
	if ok, err := manipulatorObject.IsMatch(contents); err != nil {
		return nil, err
	} else if !ok {
		return nil, &util.InvalidArgumentError{Message: "The json file must be an object ({})"}
	}
	m := &Manipulator{newline: "\n"}
	if strings.Contains(contents, "\r\n") {
		m.newline = "\r\n"
	}
	if contents == "{}" {
		contents = "{" + m.newline + "}"
	}
	m.contents = contents
	var err error
	if m.indent, err = detectIndenting(m.contents); err != nil {
		return nil, err
	}

	return m, nil
}

// DetectIndenting ports JsonManipulator::detectIndenting: the indent of the
// current contents.
func (m *Manipulator) DetectIndenting() error {
	indent, err := detectIndenting(m.contents)
	if err != nil {
		return err
	}
	m.indent = indent

	return nil
}

// Contents ports JsonManipulator::getContents.
func (m *Manipulator) Contents() string { return m.contents + m.newline }

// re compiles a pattern built from arguments, as Preg does (a compilation
// failure is a PcreException).
func (m *Manipulator) re(pattern string) (*php.Regexp, error) {
	if re, ok := m.regexps[pattern]; ok {
		return re, nil
	}
	re, err := php.Compile(pattern)
	if err != nil {
		return nil, &php.PcreError{Function: "preg_match", Pattern: pattern, Code: php.PregInternalError, Warning: "preg_match(): " + err.Error()}
	}
	if m.regexps == nil {
		m.regexps = make(map[string]*php.Regexp)
	}
	m.regexps[pattern] = re

	return re, nil
}

// match runs a dynamic pattern (Preg::match / isMatch with $matches).
func (m *Manipulator) match(pattern, subject string) (*php.Match, error) {
	re, err := m.re(pattern)
	if err != nil {
		return nil, err
	}

	return re.Match(subject)
}

func (m *Manipulator) isMatch(pattern, subject string) (bool, error) {
	re, err := m.re(pattern)
	if err != nil {
		return false, err
	}

	return re.IsMatch(subject)
}

// replace ports Preg::replace with a literal replacement: addcslashes'
// escaping of \ and $ followed by preg_replace's unescaping is the
// identity, so the replacement is inserted as is.
func (m *Manipulator) replaceLiteral(pattern, replacement, subject string) (string, int, error) {
	re, err := m.re(pattern)
	if err != nil {
		return "", 0, err
	}

	return re.ReplaceCallback(subject, func(*php.Match) string { return replacement }, -1)
}

// replaceCallback ports Preg::replaceCallback with a callback that may
// fail; the first failure is returned.
func (m *Manipulator) replaceCallback(re *php.Regexp, subject string, fn func(*php.Match) (string, error)) (string, error) {
	var cbErr error
	out, _, err := re.ReplaceCallback(subject, func(match *php.Match) string {
		if cbErr != nil {
			return ""
		}
		s, err := fn(match)
		if err != nil {
			cbErr = err
		}

		return s
	}, -1)
	if err != nil {
		return "", err
	}

	return out, cbErr
}

// isBacktrackLimit reports a PcreException whose code is
// PREG_BACKTRACK_LIMIT_ERROR, which the node lookups treat as "not
// match-able".
func isBacktrackLimit(err error) bool {
	var pe *php.PcreError

	return errors.As(err, &pe) && pe.Code == php.PregBacktrackLimitError
}

// namedStr is $matches[name] as a string (Preg uses
// PREG_UNMATCHED_AS_NULL; null concatenates as "").
func namedStr(match *php.Match, name string) string {
	s, _ := match.Named(name)

	return s
}

func (m *Manipulator) parse() (any, error) { return ParseJSON(m.contents, "") }

// nodeRegex is the pattern of the main node mainNode holding a value of
// kind ("object" or "array").
func nodeRegex(encodedMainNode, kind string) string {
	return `{` + manipulatorDefines + `^(?P<start> \s* \{ \s* (?: (?&string) \s* : (?&json) \s* , \s* )*?` +
		php.PregQuote(encodedMainNode, "") + `\s*:\s*)(?P<content>(?&` + kind + `))(?P<end>.*)}sx`
}

// AddLink ports JsonManipulator::addLink.
func (m *Manipulator) AddLink(typ, pkg, constraint string, sortPackages bool) (bool, error) {
	decoded, err := m.parse()
	if err != nil {
		return false, err
	}

	// no link of that type yet
	if !issetIndex(decoded, typ) {
		return m.AddMainKey(typ, php.ArrayOf(pkg, constraint))
	}

	encodedType, err := EncodeDefault(typ)
	if err != nil {
		return false, err
	}
	matches, err := m.match(`{`+manipulatorDefines+`^(?P<start>\s*\{\s*(?:(?&string)\s*:\s*(?&json)\s*,\s*)*?)`+
		`(?P<property>`+php.PregQuote(encodedType, "")+`\s*:\s*)(?P<value>(?&json))(?P<end>.*)}sx`, m.contents)
	if err != nil || matches == nil {
		return false, err
	}

	links := namedStr(matches, "value")

	// try to find existing link
	packageRegex := strings.ReplaceAll(php.PregQuote(pkg, ""), "/", `\\?/`)
	packageMatches, err := m.match(`{`+manipulatorDefines+`"(?P<package>`+packageRegex+`)"(\s*:\s*)(?&string)}ix`, links)
	if err != nil {
		return false, err
	}
	// Composer pastes the constraint between bare quotes when it updates
	// an existing link; both paths take it JSON-encoded, the same text for
	// every constraint without a quote, backslash or control character,
	// so those cannot break out of the string.
	encodedConstraint, err := EncodeDefault(constraint)
	if err != nil {
		return false, err
	}
	encodedPkg := ""
	if packageMatches == nil {
		if encodedPkg, err = EncodeDefault(pkg); err != nil {
			return false, err
		}
	}
	switch {
	case packageMatches != nil:
		// update existing link
		existingPackage := namedStr(packageMatches, "package")
		packageRegex = strings.ReplaceAll(php.PregQuote(existingPackage, ""), "/", `\\?/`)
		re, err := m.re(`{` + manipulatorDefines + `"` + packageRegex + `"(?P<separator>\s*:\s*)(?&string)}ix`)
		if err != nil {
			return false, err
		}
		links, err = m.replaceCallback(re, links, func(match *php.Match) (string, error) {
			encoded, err := EncodeDefault(strings.ReplaceAll(existingPackage, `\/`, "/"))

			return encoded + namedStr(match, "separator") + encodedConstraint, err
		})
		if err != nil {
			return false, err
		}
	default:
		match, err := linksNonEmpty.MatchStrictGroups(links)
		if err != nil {
			return false, err
		}
		if match != nil {
			// link missing but non empty links
			links, _, err = m.replaceLiteral(`{`+php.PregQuote(match.Get(1), "")+`$}`,
				","+m.newline+m.indent+m.indent+encodedPkg+": "+encodedConstraint+match.Get(1), links)
			if err != nil {
				return false, err
			}
		} else {
			// links empty
			links = "{" + m.newline + m.indent + m.indent + encodedPkg + ": " + encodedConstraint + m.newline + m.indent + "}"
		}
	}

	if sortPackages {
		requirements, _ := php.JSONDecode(links, true)
		reqs, ok := requirements.(*php.Array)
		if !ok {
			return false, typeError("Composer\\Json\\JsonManipulator::sortPackages(): Argument #1 ($packages) must be of type array, " + zvalValueName(requirements) + " given")
		}
		if err := sortPackageMap(reqs); err != nil {
			return false, err
		}
		if links, err = m.Format(reqs, 0, false); err != nil {
			return false, err
		}
	}

	m.contents = namedStr(matches, "start") + namedStr(matches, "property") + links + namedStr(matches, "end")

	return true, nil
}

// sortPackageMap ports JsonManipulator::sortPackages: platform packages
// first (php, hhvm, ext, lib, others), then the rest, each in natural
// order. The prefix of each name is computed once rather than per
// comparison; as every element takes part in a comparison once there are
// two, PHP's TypeError for an int key (a numeric package name) is raised
// exactly when it would be.
func sortPackageMap(packages *php.Array) error {
	if packages.Len() < 2 {
		return nil
	}
	prefixes := make(map[php.Key]string, packages.Len())
	for k := range packages.All() {
		if k.IsInt() {
			return typeError("Composer\\Repository\\PlatformRepository::isPlatformPackage(): Argument #1 ($name) must be of type string, int given")
		}
		requirement := k.String()
		ok, err := pkg.PlatformPackageRegexp.IsMatch(requirement)
		if err != nil {
			return err
		}
		if !ok {
			prefixes[k] = "5-" + requirement
			continue
		}
		for i, re := range sortPrefixPatterns {
			if requirement, _, err = re.Replace(requirement, sortPrefixReplacement[i], -1); err != nil {
				return err
			}
		}
		prefixes[k] = requirement
	}
	php.Uksort(packages, func(a, b php.Key) int { return php.Strnatcmp(prefixes[a], prefixes[b]) })

	return nil
}

// AddRepository ports JsonManipulator::addRepository; config is a PHP
// array or false.
func (m *Manipulator) AddRepository(name string, config any, appendItem bool) (bool, error) {
	if name != "" {
		if ok, err := m.doRemoveRepository(name); err != nil || !ok {
			return false, err
		}
	}

	if ok, err := m.doConvertRepositoriesFromAssocToList(); err != nil || !ok {
		return false, err
	}

	if arr, ok := config.(*php.Array); ok && !php.IsNumeric(name) && name != "" {
		config = arrayUnion(php.ArrayOf("name", name), arr)
	} else if config == false {
		config = php.ArrayOf(name, config)
	}

	return m.AddListItem("repositories", config, appendItem)
}

// arrayUnion is PHP's $a + $b.
func arrayUnion(a, b *php.Array) *php.Array {
	out := a.Clone()
	for k, v := range b.All() {
		if _, ok := out.GetKey(k); !ok {
			out.SetKey(k, v)
		}
	}

	return out
}

func (m *Manipulator) doConvertRepositoriesFromAssocToList() (bool, error) {
	decoded, _ := php.JSONDecode(m.contents, false)

	repositories, ok := property(decoded, "repositories").(*php.Object)
	if !ok {
		return true, nil
	}
	repos := repositories.ToArray()

	// delete from bottom to top, to ensure keys stay the same
	for _, key := range slices.Backward(repos.Keys()) {
		if ok, err := m.RemoveSubNode("repositories", key.String()); err != nil || !ok {
			return false, err
		}
	}

	if _, err := m.ChangeEmptyMainKeyFromAssocToList("repositories"); err != nil {
		return false, err
	}

	// re-add in order
	for repositoryName, repository := range repos.All() {
		var item any
		obj, isObject := repository.(*php.Object)
		switch {
		case !isObject:
			item = php.ArrayOf(repositoryName, repository)
		case php.IsNumeric(repositoryName.Value()):
			item = repository
		default:
			// prepend name property
			item = arrayUnion(php.ArrayOf("name", repositoryName.Value()), obj.ToArray())
		}
		if ok, err := m.AddListItem("repositories", item, true); err != nil || !ok {
			return false, err
		}
	}

	return true, nil
}

// SetRepositoryURL ports JsonManipulator::setRepositoryUrl.
func (m *Manipulator) SetRepositoryURL(name, url string) (bool, error) {
	decoded, err := m.parse()
	if err != nil {
		return false, err
	}

	var repositoryIndex *php.Key
	if repositories := index(decoded, "repositories"); repositories != nil {
		repos, err := foreachable(repositories)
		if err != nil {
			return false, err
		}
		for idx, repository := range repos.All() {
			if idx.IsString() && idx.String() == name {
				repositoryIndex = &idx

				break
			}
			if n, ok := index(repository, "name").(string); ok && n == name {
				repositoryIndex = &idx

				break
			}
		}
	}

	if repositoryIndex == nil {
		return false, nil
	}

	listRegex := ""
	if repositoryIndex.IsInt() {
		listRegex = `{` + manipulatorDefines + `^(?P<start>\s*\{\s*(?:(?&string)\s*:\s*(?&json)\s*,\s*)*?"repositories"\s*:\s*\[\s*((?&json)\s*+,\s*+){` +
			strconv.FormatInt(max(0, repositoryIndex.Int()), 10) + `})(?P<repository>(?&object))(?P<end>.*)}sx`
	}

	encodedIndex, err := EncodeDefault(repositoryIndex.Value())
	if err != nil {
		return false, err
	}
	objectRegex := `{` + manipulatorDefines + `^(?P<start>\s*\{\s*(?:(?&string)\s*:\s*(?&json)\s*,\s*)*?"repositories"\s*:\s*\{\s*(?:(?&string)\s*:\s*(?&json)\s*,\s*)*?` +
		php.PregQuote(encodedIndex, "") + `\s*:\s*)(?P<repository>(?&object))(?P<end>.*)}sx`

	var matches *php.Match
	if listRegex != "" {
		if matches, err = m.match(listRegex, m.contents); err != nil {
			return false, err
		}
	}
	if matches == nil {
		if matches, err = m.match(objectRegex, m.contents); err != nil {
			return false, err
		}
	}
	if matches == nil {
		return false, nil
	}

	// invalid match due to un-regexable content, abort
	if v, _ := php.JSONDecode(namedStr(matches, "repository"), false); v == false {
		return false, nil
	}

	replaced, err := m.replaceCallback(repositoryURLRegex, namedStr(matches, "repository"), func(repositoryMatches *php.Match) (string, error) {
		encoded, err := EncodeDefault(url)

		return namedStr(repositoryMatches, "start") + encoded + namedStr(repositoryMatches, "end"), err
	})
	if err != nil {
		return false, err
	}
	m.contents = namedStr(matches, "start") + replaced + namedStr(matches, "end")

	return true, nil
}

// InsertRepository ports JsonManipulator::insertRepository; config is a
// PHP array or false.
func (m *Manipulator) InsertRepository(name string, config any, referenceName string, offset int) (bool, error) {
	if name != "" {
		if ok, err := m.doRemoveRepository(name); err != nil || !ok {
			return false, err
		}
	}

	if ok, err := m.doConvertRepositoriesFromAssocToList(); err != nil || !ok {
		return false, err
	}

	decoded, err := m.parse()
	if err != nil {
		return false, err
	}

	var indexToInsert *php.Key
	repositories, err := offsetRead(decoded, "repositories")
	if err != nil {
		return false, err
	}
	repos, err := foreachable(repositories)
	if err != nil {
		return false, err
	}
	for repositoryIndex, repository := range repos.All() {
		if n, ok := index(repository, "name").(string); ok && n == referenceName {
			indexToInsert = &repositoryIndex

			break
		}
		if repositoryIndex.IsString() && repositoryIndex.String() == referenceName {
			indexToInsert = &repositoryIndex

			break
		}
		if isDisabledRepo(repository, referenceName) {
			indexToInsert = &repositoryIndex

			break
		}
	}

	if indexToInsert == nil {
		return false, nil
	}
	if !indexToInsert.IsInt() {
		return false, typeError("Unsupported operand types: string + int")
	}

	if arr, ok := config.(*php.Array); ok && !php.IsNumeric(name) && name != "" {
		config = arrayUnion(php.ArrayOf("name", name), arr)
	} else if config == false {
		config = php.ArrayOf("name", config)
	}

	return m.InsertListItem("repositories", config, int(indexToInsert.Int())+offset)
}

// isDisabledRepo reports [$name => false] === $repository.
func isDisabledRepo(repository any, name string) bool {
	arr, ok := repository.(*php.Array)
	if !ok || arr.Len() != 1 {
		return false
	}
	k, v, _ := arr.First()

	return k == php.StrKey(name) && v == false
}

// RemoveRepository ports JsonManipulator::removeRepository.
func (m *Manipulator) RemoveRepository(name string) (bool, error) {
	if ok, err := m.doRemoveRepository(name); err != nil || !ok {
		return false, err
	}

	return m.RemoveMainKeyIfEmpty("repositories")
}

func (m *Manipulator) doRemoveRepository(name string) (bool, error) {
	decoded, _ := php.JSONDecode(m.contents, false)
	repositories := property(decoded, "repositories")
	_, isAssoc := repositories.(*php.Object)
	if repositories == nil {
		repositories = php.NewArray()
	}

	for repositoryIndex, repository := range arrayCast(repositories).All() {
		if repositoryIndex.IsString() && repositoryIndex.String() == name && isAssoc {
			if ok, err := m.RemoveSubNode("repositories", repositoryIndex.String()); err != nil || !ok {
				return false, err
			}

			break
		}

		if n, ok := property(repository, "name").(string); ok && n == name {
			if isAssoc {
				if ok, err := m.RemoveSubNode("repositories", repositoryIndex.String()); err != nil || !ok {
					return false, err
				}
			} else if ok, err := m.RemoveListItem("repositories", int(repositoryIndex.Int())); err != nil || !ok {
				return false, err
			}

			break
		}

		if isAssoc {
			if repositoryIndex.IsString() && repositoryIndex.String() == name && repository == false {
				if ok, err := m.RemoveSubNode("repositories", name); err != nil || !ok {
					return false, err
				}

				return true, nil
			}
		} else {
			repositoryAsArray := arrayCast(repository)
			if v, _ := repositoryAsArray.Get(name); v == false && repositoryAsArray.Len() == 1 {
				if ok, err := m.RemoveListItem("repositories", int(repositoryIndex.Int())); err != nil || !ok {
					return false, err
				}

				return true, nil
			}
		}
	}

	return true, nil
}

// AddConfigSetting ports JsonManipulator::addConfigSetting.
func (m *Manipulator) AddConfigSetting(name string, value any) (bool, error) {
	// policy config has one more nesting level than the rest of the config e.g. policy.list-name.key
	// Rewrite as a policy.<list> update with the merged list block so addSubNode can do a
	// surgical edit at the list level rather than falling back to a whole-file rewrite.
	if strings.HasPrefix(name, "policy.") && strings.Count(name, ".") >= 2 {
		listName, fieldName, _ := strings.Cut(name[7:], ".")
		if strings.Contains(fieldName, ".") {
			return false, nil
		}
		listBlock, err := m.currentPolicyListBlock(listName)
		if err != nil {
			return false, err
		}
		listBlock.Set(fieldName, value)

		return m.AddSubNode("config", "policy."+listName, listBlock, true)
	}

	return m.AddSubNode("config", name, value, true)
}

// RemoveConfigSetting ports JsonManipulator::removeConfigSetting.
func (m *Manipulator) RemoveConfigSetting(name string) (bool, error) {
	// policy.<list>.<field>: drop the field from the list block, then either rewrite the
	// block (surgical) or defer to the whole-file fallback when the block ends up empty so
	// the cascade-cleanup of empty ancestors can run.
	if strings.HasPrefix(name, "policy.") && strings.Count(name, ".") >= 2 {
		listName, fieldName, _ := strings.Cut(name[7:], ".")
		if strings.Contains(fieldName, ".") {
			return false, nil
		}

		listBlock, err := m.currentPolicyListBlock(listName)
		if err != nil {
			return false, err
		}
		if !listBlock.Has(fieldName) {
			return true, nil
		}
		listBlock.Delete(fieldName)
		if listBlock.Len() == 0 {
			return m.RemoveConfigSetting("policy." + listName)
		}

		return m.AddSubNode("config", "policy."+listName, listBlock, true)
	}

	return m.RemoveSubNode("config", name)
}

func (m *Manipulator) currentPolicyListBlock(listName string) (*php.Array, error) {
	decoded, err := m.parse()
	if err != nil {
		return nil, err
	}
	if _, ok := decoded.(*php.Array); !ok {
		return php.NewArray(), nil
	}
	policySection, ok := index(index(decoded, "config"), "policy").(*php.Array)
	if !ok {
		return php.NewArray(), nil
	}
	listBlock, ok := index(policySection, listName).(*php.Array)
	if !ok {
		return php.NewArray(), nil
	}

	return listBlock, nil
}

// AddProperty ports JsonManipulator::addProperty.
func (m *Manipulator) AddProperty(name string, value any) (bool, error) {
	if rest, ok := strings.CutPrefix(name, "suggest."); ok {
		return m.AddSubNode("suggest", rest, value, true)
	}
	if rest, ok := strings.CutPrefix(name, "extra."); ok {
		return m.AddSubNode("extra", rest, value, true)
	}
	if rest, ok := strings.CutPrefix(name, "scripts."); ok {
		return m.AddSubNode("scripts", rest, value, true)
	}

	return m.AddMainKey(name, value)
}

// RemoveProperty ports JsonManipulator::removeProperty.
func (m *Manipulator) RemoveProperty(name string) (bool, error) {
	for _, node := range [...]string{"suggest", "extra", "scripts", "autoload", "autoload-dev"} {
		if rest, ok := strings.CutPrefix(name, node+"."); ok {
			return m.RemoveSubNode(node, rest)
		}
	}

	return m.RemoveMainKey(name)
}

// splitSubName splits name at its first dot for the main nodes whose
// children are addressed as name.subName.
func splitSubName(mainNode, name string) (string, *string) {
	if mainNode != "config" && mainNode != "extra" && mainNode != "scripts" {
		return name, nil
	}
	if head, sub, ok := strings.Cut(name, "."); ok {
		return head, &sub
	}

	return name, nil
}

// AddSubNode ports JsonManipulator::addSubNode.
func (m *Manipulator) AddSubNode(mainNode, name string, value any, appendItem bool) (bool, error) {
	decoded, err := m.parse()
	if err != nil {
		return false, err
	}

	name, subName := splitSubName(mainNode, name)

	// no main node yet
	if !issetIndex(decoded, mainNode) {
		if subName != nil {
			_, err = m.AddMainKey(mainNode, php.ArrayOf(name, php.ArrayOf(*subName, value)))
		} else {
			_, err = m.AddMainKey(mainNode, php.ArrayOf(name, value))
		}

		return err == nil, err
	}

	// main node content not match-able
	encodedMainNode, err := EncodeDefault(mainNode)
	if err != nil {
		return false, err
	}
	nodeRe, err := m.re(nodeRegex(encodedMainNode, "object"))
	if err != nil {
		return false, err
	}
	match, err := nodeRe.Match(m.contents)
	if err != nil {
		if isBacktrackLimit(err) {
			return false, nil
		}

		return false, err
	}
	if match == nil {
		return false, nil
	}

	children := namedStr(match, "content")
	// invalid match due to un-regexable content, abort
	if v, _ := php.JSONDecode(children, false); !php.ToBool(v) {
		return false, nil
	}

	// child exists
	childRe, err := m.re(`{` + manipulatorDefines + `(?P<start>"` + php.PregQuote(name, "") + `"\s*:\s*)(?P<content>(?&json))(?P<end>,?)}x`)
	if err != nil {
		return false, err
	}
	encodedName := ""
	if ok, err := childRe.IsMatch(children); err != nil {
		return false, err
	} else if ok {
		children, err = m.replaceCallback(childRe, children, func(matches *php.Match) (string, error) {
			value := value
			if subName != nil {
				content, isSet := matches.Named("content")
				if isSet {
					curVal, _ := php.JSONDecode(content, true)
					arr, ok := curVal.(*php.Array)
					if !ok {
						arr = php.NewArray()
					}
					arr.Set(*subName, value)
					value = arr
				}
			}
			formatted, err := m.Format(value, 1, false)

			return namedStr(matches, "start") + formatted + namedStr(matches, "end"), err
		})
		if err != nil {
			return false, err
		}
	} else if match, err := childrenObject.Match(children); err != nil {
		return false, err
	} else if match != nil {
		if encodedName, err = EncodeDefault(name); err != nil {
			return false, err
		}
		if subName != nil {
			value = php.ArrayOf(*subName, value)
		}
		formatted, err := m.Format(value, 1, false)
		if err != nil {
			return false, err
		}
		whitespace := namedStr(match, "trailingspace")
		if _, hasContent := match.Named("content"); hasContent {
			// child missing but non empty children
			if appendItem {
				children, _, err = m.replaceLiteral(`#`+whitespace+`}$#`,
					","+m.newline+m.indent+m.indent+encodedName+": "+formatted+whitespace+"}", children)
			} else {
				whitespace = namedStr(match, "leadingspace")
				children, _, err = m.replaceLiteral(`#^{`+whitespace+`#`,
					"{"+whitespace+encodedName+": "+formatted+","+m.newline+m.indent+m.indent, children)
			}
			if err != nil {
				return false, err
			}
		} else {
			// children present but empty
			children = "{" + m.newline + m.indent + m.indent + encodedName + ": " + formatted + whitespace + "}"
		}
	} else {
		return false, &util.LogicError{Message: "Nothing matched above for: " + children}
	}

	m.contents, err = m.replaceCallback(nodeRe, m.contents, func(match *php.Match) (string, error) {
		return namedStr(match, "start") + children + namedStr(match, "end"), nil
	})

	return err == nil, err
}

// RemoveSubNode ports JsonManipulator::removeSubNode.
func (m *Manipulator) RemoveSubNode(mainNode, name string) (bool, error) {
	decoded, err := m.parse()
	if err != nil {
		return false, err
	}

	// no node or empty node
	if !php.ToBool(index(decoded, mainNode)) {
		return true, nil
	}

	// no node content match-able
	encodedMainNode, err := EncodeDefault(mainNode)
	if err != nil {
		return false, err
	}
	nodeRe, err := m.re(nodeRegex(encodedMainNode, "object"))
	if err != nil {
		return false, err
	}
	match, err := nodeRe.Match(m.contents)
	if err != nil {
		if isBacktrackLimit(err) {
			return false, nil
		}

		return false, err
	}
	if match == nil {
		return false, nil
	}

	children := namedStr(match, "content")

	// invalid match due to un-regexable content, abort
	if v, _ := php.JSONDecode(children, true); !php.ToBool(v) {
		return false, nil
	}

	name, subName := splitSubName(mainNode, name)

	// no node to remove
	node := index(decoded, mainNode)
	if !issetIndex(node, name) || (subName != nil && *subName != "" && *subName != "0" && !issetIndex(index(node, name), *subName)) {
		return true, nil
	}

	// try and find a match for the subkey
	keyRegex := strings.ReplaceAll(php.PregQuote(name, ""), "/", `\\?/`)
	var childrenClean *string
	if ok, err := m.isMatch(`{"`+keyRegex+`"\s*:}i`, children); err != nil {
		return false, err
	} else if ok {
		// find best match for the value of "name"
		re, err := m.re(`{` + manipulatorDefines + `"` + keyRegex + `"\s*:\s*(?:(?&json))}x`)
		if err != nil {
			return false, err
		}
		all, err := re.MatchAll(children)
		if err != nil {
			return false, err
		}
		if len(all) > 0 {
			bestMatch := ""
			for _, match := range all {
				if s := match.Get(0); len(bestMatch) < len(s) {
					bestMatch = s
				}
			}
			clean, count, err := m.replaceLiteral(`{,\s*`+php.PregQuote(bestMatch, "")+`}i`, "", children)
			if err != nil {
				return false, err
			}
			if count != 1 {
				clean, count, err = m.replaceLiteral(`{`+php.PregQuote(bestMatch, "")+`\s*,?\s*}i`, "", clean)
				if err != nil {
					return false, err
				}
				if count != 1 {
					return false, nil
				}
			}
			childrenClean = &clean
		}
	} else {
		childrenClean = &children
	}

	if childrenClean == nil {
		return false, &util.InvalidArgumentError{Message: "JsonManipulator: $childrenClean is not defined. Please report at https://github.com/composer/composer/issues/new."}
	}

	// no child data left, $name was the only key in
	if match, err := childrenCleanObject.Match(*childrenClean); err != nil {
		return false, err
	} else if match != nil {
		if _, hasContent := match.Named("content"); !hasContent {
			m.contents, err = m.replaceCallback(nodeRe, m.contents, func(matches *php.Match) (string, error) {
				return namedStr(matches, "start") + "{" + m.newline + m.indent + "}" + namedStr(matches, "end"), nil
			})
			if err != nil {
				return false, err
			}

			// we have a subname, so we restore the rest of $name
			if subName != nil {
				curVal, _ := php.JSONDecode(children, true)
				rest, err := unsetSubName(curVal, name, *subName)
				if err != nil {
					return false, err
				}
				if _, err := m.AddSubNode(mainNode, name, rest, true); err != nil {
					return false, err
				}
			}

			return true, nil
		}
	}

	m.contents, err = m.replaceCallback(nodeRe, m.contents, func(matches *php.Match) (string, error) {
		clean := *childrenClean
		if subName != nil {
			curVal, _ := php.JSONDecode(namedStr(matches, "content"), true)
			if _, err := unsetSubName(curVal, name, *subName); err != nil {
				return "", err
			}
			var err error
			if clean, err = m.Format(curVal, 0, true); err != nil {
				return "", err
			}
		}

		return namedStr(matches, "start") + clean + namedStr(matches, "end"), nil
	})

	return err == nil, err
}

// unsetSubName performs unset($curVal[$name][$subName]), then replaces
// an emptied $curVal[$name] with an ArrayObject; it returns $curVal[$name].
func unsetSubName(curVal any, name, subName string) (any, error) {
	if err := unsetNested(curVal, name, subName); err != nil {
		return nil, err
	}
	child, err := offsetRead(curVal, name)
	if err != nil {
		return nil, err
	}
	if arr, ok := child.(*php.Array); ok && arr.Len() == 0 {
		if parent, ok := curVal.(*php.Array); ok {
			child = php.NewObject()
			parent.Set(name, child)
		}
	}

	return child, nil
}

// unsetNested is unset($a[$k1][$k2]).
func unsetNested(a any, k1, k2 string) error {
	switch a := a.(type) {
	case nil:
		return nil
	case *php.Array:
		child, ok := a.Get(k1)
		if !ok {
			return nil
		}
		switch c := child.(type) {
		case nil:
			return nil
		case *php.Array:
			c.Delete(k2)

			return nil
		case string:
			return &php.EngineError{Class: php.ClassError, Message: "Cannot unset string offsets"}
		case bool:
			if !c {
				return nil
			}
		}
	case string:
		return &php.EngineError{Class: php.ClassError, Message: "Cannot unset string offsets"}
	case bool:
		if !a {
			return nil
		}
	}

	return &php.EngineError{Class: php.ClassError, Message: "Cannot unset offset in a non-array variable"}
}

// AddListItem ports JsonManipulator::addListItem.
func (m *Manipulator) AddListItem(mainNode string, value any, appendItem bool) (bool, error) {
	decoded, err := m.parse()
	if err != nil {
		return false, err
	}

	// no main node yet
	if !issetIndex(decoded, mainNode) {
		if ok, err := m.AddMainKey(mainNode, php.NewArray()); err != nil || !ok {
			return false, err
		}
	}

	// main node content not match-able
	nodeRe, match, err := m.matchListNode(mainNode)
	if err != nil || match == nil {
		return false, err
	}

	children := namedStr(match, "content")
	// invalid match due to un-regexable content, abort
	if v, _ := php.JSONDecode(children, false); v == false {
		return false, nil
	}

	if match, err = childrenList.Match(children); err != nil {
		return false, err
	}
	if match == nil {
		return false, &util.LogicError{Message: "Nothing matched above for: " + children}
	}

	leadingWhitespace := namedStr(match, "leadingspace")
	whitespace := namedStr(match, "trailingspace")
	leadingItemWhitespace := m.newline + m.indent + m.indent
	trailingItemWhitespace := whitespace
	itemDepth := 1

	// keep oneline lists as one line
	if !strings.Contains(whitespace, m.newline) {
		leadingItemWhitespace = leadingWhitespace
		trailingItemWhitespace = leadingWhitespace
		itemDepth = 0
	}

	formatted, err := m.Format(value, itemDepth, false)
	if err != nil {
		return false, err
	}
	if _, hasContent := match.Named("content"); hasContent {
		// child missing but non empty children
		if appendItem {
			children, _, err = m.replaceLiteral(`#`+whitespace+`]$#`,
				","+leadingItemWhitespace+formatted+trailingItemWhitespace+"]", children)
		} else {
			whitespace = namedStr(match, "leadingspace")
			children, _, err = m.replaceLiteral(`#^\[`+whitespace+`#`,
				"["+whitespace+formatted+","+leadingItemWhitespace, children)
		}
		if err != nil {
			return false, err
		}
	} else {
		// children present but empty
		children = "[" + leadingItemWhitespace + formatted + trailingItemWhitespace + "]"
	}

	m.contents, err = m.replaceCallback(nodeRe, m.contents, func(match *php.Match) (string, error) {
		return namedStr(match, "start") + children + namedStr(match, "end"), nil
	})

	return err == nil, err
}

// matchListNode matches the main node mainNode holding an array; a nil
// match means not match-able (including the backtrack limit).
func (m *Manipulator) matchListNode(mainNode string) (*php.Regexp, *php.Match, error) {
	encodedMainNode, err := EncodeDefault(mainNode)
	if err != nil {
		return nil, nil, err
	}
	nodeRe, err := m.re(nodeRegex(encodedMainNode, "array"))
	if err != nil {
		return nil, nil, err
	}
	match, err := nodeRe.Match(m.contents)
	if err != nil {
		if isBacktrackLimit(err) {
			return nil, nil, nil
		}

		return nil, nil, err
	}

	return nodeRe, match, nil
}

// InsertListItem ports JsonManipulator::insertListItem.
func (m *Manipulator) InsertListItem(mainNode string, value any, idx int) (bool, error) {
	if idx < 0 {
		return false, &util.InvalidArgumentError{Message: "Index can only be positive integer"}
	}

	if idx == 0 {
		return m.AddListItem(mainNode, value, false)
	}

	decoded, err := m.parse()
	if err != nil {
		return false, err
	}

	// no main node yet
	if !issetIndex(decoded, mainNode) {
		if ok, err := m.AddMainKey(mainNode, php.NewArray()); err != nil || !ok {
			return false, err
		}
	}

	node, err := offsetRead(decoded, mainNode)
	if err != nil {
		return false, err
	}
	list, ok := node.(*php.Array)
	if !ok {
		return false, typeError("count(): Argument #1 ($value) must be of type Countable|array, " + zvalValueName(node) + " given")
	}
	if list.Len() == idx {
		return m.AddListItem(mainNode, value, true)
	}

	// main node content not match-able
	nodeRe, match, err := m.matchListNode(mainNode)
	if err != nil || match == nil {
		return false, err
	}

	children := namedStr(match, "content")
	// invalid match due to un-regexable content, abort
	if v, _ := php.JSONDecode(children, false); v == false {
		return false, nil
	}

	skipRe, err := m.re(`{` + manipulatorDefines + `^(?P<start>\[\s*((?&json)\s*+,\s*?){` + strconv.Itoa(max(0, idx)) + `})(?P<space_before_item>(\s*))(?P<end>.*)}sx`)
	if err != nil {
		return false, err
	}
	children, err = m.replaceCallback(skipRe, children, func(match *php.Match) (string, error) {
		formatted, err := m.Format(value, 1, false)
		space := namedStr(match, "space_before_item")

		return namedStr(match, "start") + space + formatted + "," + space + namedStr(match, "end"), err
	})
	if err != nil {
		return false, err
	}

	m.contents, err = m.replaceCallback(nodeRe, m.contents, func(match *php.Match) (string, error) {
		return namedStr(match, "start") + children + namedStr(match, "end"), nil
	})

	return err == nil, err
}

// RemoveListItem ports JsonManipulator::removeListItem.
func (m *Manipulator) RemoveListItem(mainNode string, nodeIndex int) (bool, error) {
	// invalid index, that cannot be removed anyway
	if nodeIndex < 0 {
		return true, nil
	}

	decoded, err := m.parse()
	if err != nil {
		return false, err
	}

	// no node or empty node
	node, err := offsetRead(decoded, mainNode)
	if err != nil {
		return false, err
	}
	if arr, ok := node.(*php.Array); ok && arr.Len() == 0 {
		return true, nil
	}

	// no node content match-able
	_, match, err := m.matchListNode(mainNode)
	if err != nil || match == nil {
		return false, err
	}

	children := namedStr(match, "content")

	// invalid match due to un-regexable content, abort
	if v, _ := php.JSONDecode(children, true); v == false {
		return false, nil
	}

	// no node to remove
	if !issetIndex(index(decoded, mainNode), nodeIndex) {
		return true, nil
	}

	contentRegex := `(?&json)`
	var startRegex, endRegex string
	switch {
	case nodeIndex > 1:
		startRegex = `(?&json)\s*+(?:,(?&json)\s*+){` + strconv.Itoa(nodeIndex-1) + `}`
		// remove leading array separator in case we might remove the last
		contentRegex = `\s*+,?\s*+` + contentRegex
		endRegex = `(?:(\s*+,\s*+(?&json))*(?:\s*+(?&json))?)\s*+`
	case nodeIndex > 0:
		startRegex = `(?&json)\s*+`
		// remove leading array separator in case we might remove the last
		contentRegex = `\s*+,?\s*+` + contentRegex
		endRegex = `(?:(\s*+,\s*+(?&json))*(?:\s*+(?&json))?)\s*+`
	default:
		startRegex = `\s*+`
		// remove trailing array separator when we delete first
		contentRegex += `\s*+,?\s*+`
		endRegex = `(?:((?&json)\s*+,\s*+)*(?:\s*+(?&json))?)\s*+`
	}

	childMatch, err := m.match(`{`+manipulatorDefines+`(?P<start>\[`+startRegex+`)(?P<content>`+contentRegex+`)(?P<end>`+endRegex+`\])}sx`, children)
	if err != nil || childMatch == nil {
		return false, err
	}
	m.contents = namedStr(match, "start") + namedStr(childMatch, "start") + namedStr(childMatch, "end") + namedStr(match, "end")

	return true, nil
}

// mainKeyRegex is the pattern of a main key, with the given group name
// around the key and its value and the given suffix.
func mainKeyRegex(encodedKey, group, suffix string) string {
	return `{` + manipulatorDefines + `^(?P<start>\s*\{\s*(?:(?&string)\s*:\s*(?&json)\s*,\s*)*?)` +
		`(?P<` + group + `>` + php.PregQuote(encodedKey, "") + `\s*:\s*(?&json))` + suffix + `(?P<end>.*)}sx`
}

// AddMainKey ports JsonManipulator::addMainKey.
func (m *Manipulator) AddMainKey(key string, content any) (bool, error) {
	decoded, err := m.parse()
	if err != nil {
		return false, err
	}
	formatted, err := m.Format(content, 0, false)
	if err != nil {
		return false, err
	}
	encodedKey, err := EncodeDefault(key)
	if err != nil {
		return false, err
	}

	// key exists already
	if issetIndex(decoded, key) {
		matches, err := m.match(mainKeyRegex(encodedKey, "key", ""), m.contents)
		if err != nil {
			return false, err
		}
		if matches != nil {
			// invalid match due to un-regexable content, abort
			if v, _ := php.JSONDecode("{"+namedStr(matches, "key")+"}", false); !php.ToBool(v) {
				return false, nil
			}

			m.contents = namedStr(matches, "start") + encodedKey + ": " + formatted + namedStr(matches, "end")

			return true, nil
		}
	}

	// append at the end of the file and keep whitespace
	if match, err := lastValueBeforeClose.Match(m.contents); err != nil {
		return false, err
	} else if match != nil {
		m.contents, _, err = m.replaceLiteral(`#`+match.Get(1)+`\}$#`,
			","+m.newline+m.indent+encodedKey+": "+formatted+m.newline+"}", m.contents)

		return err == nil, err
	}

	// append at the end of the file
	replacement := m.indent + encodedKey + ": " + formatted + m.newline + "}"
	m.contents, _, err = closingBrace.ReplaceCallback(m.contents, func(*php.Match) string { return replacement }, -1)

	return err == nil, err
}

// RemoveMainKey ports JsonManipulator::removeMainKey.
func (m *Manipulator) RemoveMainKey(key string) (bool, error) {
	decoded, err := m.parse()
	if err != nil {
		return false, err
	}

	if !arrayKeyExists(decoded, key) {
		return true, nil
	}

	// key exists already
	encodedKey, err := EncodeDefault(key)
	if err != nil {
		return false, err
	}
	matches, err := m.match(mainKeyRegex(encodedKey, "removal", `\s*,?\s*`), m.contents)
	if err != nil || matches == nil {
		return false, err
	}

	// invalid match due to un-regexable content, abort
	if v, _ := php.JSONDecode("{"+namedStr(matches, "removal")+"}", false); !php.ToBool(v) {
		return false, nil
	}

	start, end := namedStr(matches, "start"), namedStr(matches, "end")
	// check that we are not leaving a dangling comma on the previous line if the last line was removed
	if ok, err := trailingComma.IsMatch(start); err != nil {
		return false, err
	} else if ok {
		if ok, err := onlyClosingBrace.IsMatch(end); err != nil {
			return false, err
		} else if ok {
			replaced, _, err := trailingCommaCapture.Replace(start, "$1", -1)
			if err != nil {
				return false, err
			}
			start = php.RtrimSet(replaced, m.indent)
		}
	}

	m.contents = start + end
	if ok, err := emptyObjectDocument.IsMatch(m.contents); err != nil {
		return false, err
	} else if ok {
		m.contents = "{\n}"
	}

	return true, nil
}

// ChangeEmptyMainKeyFromAssocToList ports
// JsonManipulator::changeEmptyMainKeyFromAssocToList.
func (m *Manipulator) ChangeEmptyMainKeyFromAssocToList(key string) (bool, error) {
	decoded, err := m.parse()
	if err != nil {
		return false, err
	}

	if !arrayKeyExists(decoded, key) {
		return true, nil
	}

	encodedKey, err := EncodeDefault(key)
	if err != nil {
		return false, err
	}
	matches, err := m.match(`{`+manipulatorDefines+`^(?P<start>\s*\{\s*(?:(?&string)\s*:\s*(?&json)\s*,\s*)*?`+php.PregQuote(encodedKey, "")+
		`\s*:\s*)(?P<removal>\{(?P<removal_space>\s*+)\})(?P<end>\s*,?\s*.*)}sx`, m.contents)
	if err != nil || matches == nil {
		return false, err
	}

	// invalid match due to un-regexable content, abort
	if v, _ := php.JSONDecode(namedStr(matches, "removal"), false); v == false {
		return false, nil
	}

	m.contents = namedStr(matches, "start") + "[" + namedStr(matches, "removal_space") + "]" + namedStr(matches, "end")

	return true, nil
}

// RemoveMainKeyIfEmpty ports JsonManipulator::removeMainKeyIfEmpty.
func (m *Manipulator) RemoveMainKeyIfEmpty(key string) (bool, error) {
	decoded, err := m.parse()
	if err != nil {
		return false, err
	}

	if !arrayKeyExists(decoded, key) {
		return true, nil
	}

	if arr, ok := index(decoded, key).(*php.Array); ok && arr.Len() == 0 {
		return m.RemoveMainKey(key)
	}

	return true, nil
}

// Format ports JsonManipulator::format: data as JSON in the document's
// newline and indentation style, nested depth levels deep. A *php.Object
// (stdClass or ArrayObject) always formats as an object.
func (m *Manipulator) Format(data any, depth int, wasObject bool) (string, error) {
	if o, ok := data.(*php.Object); ok {
		data = o.ToArray()
		wasObject = true
	}

	arr, ok := data.(*php.Array)
	if !ok {
		return EncodeDefault(data)
	}

	if arr.Len() == 0 {
		if wasObject {
			return "{" + m.newline + strings.Repeat(m.indent, depth+1) + "}", nil
		}

		return "[]", nil
	}

	var b strings.Builder
	if arr.IsList() {
		b.WriteByte('[')
		first := true
		for _, val := range arr.All() {
			s, err := m.Format(val, depth+1, false)
			if err != nil {
				return "", err
			}
			if !first {
				b.WriteString(", ")
			}
			first = false
			b.WriteString(s)
		}
		b.WriteByte(']')

		return b.String(), nil
	}

	b.WriteString("{")
	b.WriteString(m.newline)
	elemIndent := strings.Repeat(m.indent, depth+2)
	first := true
	for key, val := range arr.All() {
		encodedKey, err := EncodeDefault(key.String())
		if err != nil {
			return "", err
		}
		s, err := m.Format(val, depth+1, false)
		if err != nil {
			return "", err
		}
		if !first {
			b.WriteString(",")
			b.WriteString(m.newline)
		}
		first = false
		b.WriteString(elemIndent)
		b.WriteString(encodedKey)
		b.WriteString(": ")
		b.WriteString(s)
	}
	b.WriteString(m.newline)
	b.WriteString(strings.Repeat(m.indent, depth+1))
	b.WriteString("}")

	return b.String(), nil
}

// index is $container[$key] ?? null.
func index(container, key any) any {
	switch c := container.(type) {
	case *php.Array:
		v, _ := c.Get(key)

		return v
	case string:
		if i, ok := stringOffset(c, key); ok {
			return c[i : i+1]
		}
	}

	return nil
}

// stringOffset resolves an isset()-able offset into a string. An offset
// beyond int cannot index s, so it is out of range like any other.
func stringOffset(s string, key any) (int, bool) {
	n := len(s)

	var i int

	switch k := key.(type) {
	case int:
		i = k
	case int64:
		if k < -int64(n) || k >= int64(n) {
			return 0, false
		}

		i = int(k)
	case string:
		if !php.StrKey(k).IsInt() {
			return 0, false
		}

		v, err := strconv.Atoi(k)
		if err != nil {
			return 0, false
		}

		i = v
	default:
		return 0, false
	}

	if i < 0 {
		i += n
	}

	if i < 0 || i >= n {
		return 0, false
	}

	return i, true
}

// issetIndex is isset($container[$key]) for any container, a string too.
func issetIndex(container, key any) bool { return index(container, key) != nil }

// property is $object->name ?? null.
func property(object any, name string) any {
	if o, ok := object.(*php.Object); ok {
		v, _ := o.Get(name)

		return v
	}

	return nil
}

// arrayKeyExists is array_key_exists($key, $array) on a decoded document.
func arrayKeyExists(array any, key string) bool {
	arr, ok := array.(*php.Array)

	return ok && arr.Has(key)
}

// arrayCast is (array) $value.
func arrayCast(v any) *php.Array {
	switch v := v.(type) {
	case *php.Array:
		return v
	case *php.Object:
		return v.ToArray()
	case nil:
		return php.NewArray()
	}

	return php.ListOf(v)
}

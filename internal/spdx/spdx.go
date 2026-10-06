// Package spdx ports composer/spdx-licenses 1.6.0 (src/SpdxLicenses.php)
// with its res/*.json data. SpdxLicensesUpdater (which regenerates res/
// from spdx.org) is not ported: Composer never uses it.
package spdx

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/php"
)

// The SPDX license and exception lists, byte-identical to res/ of
// composer/spdx-licenses 1.6.0.
var (
	//go:embed res/spdx-licenses.json
	licensesJSON string
	//go:embed res/spdx-exceptions.json
	exceptionsJSON string
)

// Resource file names (SpdxLicenses::LICENSES_FILE, EXCEPTIONS_FILE).
const (
	LicensesFile   = "spdx-licenses.json"
	ExceptionsFile = "spdx-exceptions.json"
)

// License is one entry of getLicenses():
// [identifier, full name, osi certified, deprecated].
type License struct {
	Identifier  string
	Name        string
	OSIApproved bool
	Deprecated  bool
}

// LicenseInfo is what getLicenseByIdentifier returns:
// [full name, osi certified, link to license text, deprecated].
type LicenseInfo struct {
	Name        string
	OSIApproved bool
	URL         string
	Deprecated  bool
}

// Exception is one entry of the exception list: [identifier, full name].
type Exception struct {
	Identifier string
	Name       string
}

// ExceptionInfo is what getExceptionByIdentifier returns:
// [full name, link to license exception text].
type ExceptionInfo struct {
	Name string
	URL  string
}

// InvalidArgumentError is the \InvalidArgumentException validate throws for
// an argument that is neither a string nor an array of strings.
type InvalidArgumentError struct {
	Message string
}

func (e *InvalidArgumentError) Error() string { return e.Message }

// PHPClass is get_class($e) and $e->getCode() (util.PHPClasser).
func (*InvalidArgumentError) PHPClass() (string, int) { return "InvalidArgumentException", 0 }

// SpdxLicenses ports Composer\Spdx\SpdxLicenses. It is immutable and safe
// for concurrent use.
type SpdxLicenses struct {
	licenses    []License      // in file order, as PHP's array
	licenseKeys map[string]int // lowercased identifier => index
	exceptions  []Exception
	exceptKeys  map[string]int
	licenseIDs  trie // lowercased license identifiers
	exceptIDs   trie // lowercased exception identifiers
}

var shared = sync.OnceValue(func() *SpdxLicenses {
	s := &SpdxLicenses{}
	s.licenses, s.licenseKeys = load(licensesJSON, LicensesFile, func(id string, v *php.Array) License {
		name, _ := v.GetString(0)
		osi, _ := v.Get(1)
		dep, _ := v.Get(2)

		return License{id, name, php.ToBool(osi), php.ToBool(dep)}
	})
	s.exceptions, s.exceptKeys = load(exceptionsJSON, ExceptionsFile, func(id string, v *php.Array) Exception {
		name, _ := v.GetString(0)

		return Exception{id, name}
	})

	for key := range s.licenseKeys {
		s.licenseIDs.add(key)
	}

	for key := range s.exceptKeys {
		s.exceptIDs.add(key)
	}

	return s
})

// New returns the license database (new SpdxLicenses()); the embedded data
// is parsed once per process.
func New() *SpdxLicenses {
	return shared()
}

// load ports loadLicenses/loadExceptions: entries keyed by the lowercased
// identifier, a later duplicate replacing the earlier one in place.
func load[T any](data, file string, entry func(id string, v *php.Array) T) ([]T, map[string]int) {
	decoded, err := php.JSONDecode(data, true)
	root, ok := decoded.(*php.Array)

	if err != nil || !ok {
		panic("spdx: invalid embedded " + file)
	}

	list := make([]T, 0, root.Len())
	keys := make(map[string]int, root.Len())

	for k, v := range root.All() {
		fields, _ := v.(*php.Array)
		if fields == nil {
			fields = php.NewArray()
		}

		id := k.String()
		key := php.Strtolower(id)
		e := entry(id, fields)

		if i, dup := keys[key]; dup {
			list[i] = e

			continue
		}

		keys[key] = len(list)
		list = append(list, e)
	}

	return list, keys
}

// GetLicenseByIdentifier ports SpdxLicenses::getLicenseByIdentifier; ok is
// false where PHP returns null.
func (s *SpdxLicenses) GetLicenseByIdentifier(identifier string) (info LicenseInfo, ok bool) {
	i, ok := s.licenseKeys[php.Strtolower(identifier)]
	if !ok {
		return LicenseInfo{}, false
	}

	l := &s.licenses[i]

	return LicenseInfo{
		Name:        l.Name,
		OSIApproved: l.OSIApproved,
		URL:         "https://spdx.org/licenses/" + l.Identifier + ".html#licenseText",
		Deprecated:  l.Deprecated,
	}, true
}

// GetLicenses ports SpdxLicenses::getLicenses: every license in file order.
// PHP keys them by lowercased identifier; GetLicense looks one up by that
// key. The slice is shared and must not be modified.
func (s *SpdxLicenses) GetLicenses() []License {
	return s.licenses
}

// GetLicense returns getLicenses()[key] for a lowercased identifier key.
func (s *SpdxLicenses) GetLicense(key string) (License, bool) {
	i, ok := s.licenseKeys[key]
	if !ok {
		return License{}, false
	}

	return s.licenses[i], true
}

// GetExceptions returns every license exception in file order (PHP keeps
// them in a private array keyed by lowercased identifier). The slice is
// shared and must not be modified.
func (s *SpdxLicenses) GetExceptions() []Exception {
	return s.exceptions
}

// GetExceptionByIdentifier ports SpdxLicenses::getExceptionByIdentifier;
// ok is false where PHP returns null.
func (s *SpdxLicenses) GetExceptionByIdentifier(identifier string) (info ExceptionInfo, ok bool) {
	i, ok := s.exceptKeys[php.Strtolower(identifier)]
	if !ok {
		return ExceptionInfo{}, false
	}

	e := &s.exceptions[i]

	return ExceptionInfo{
		Name: e.Name,
		URL:  "https://spdx.org/licenses/" + e.Identifier + ".html#licenseExceptionText",
	}, true
}

// GetIdentifierByName ports SpdxLicenses::getIdentifierByName: the
// identifier of the first license, then exception, with exactly that full
// name; ok is false where PHP returns null.
func (s *SpdxLicenses) GetIdentifierByName(name string) (identifier string, ok bool) {
	for i := range s.licenses {
		if s.licenses[i].Name == name {
			return s.licenses[i].Identifier, true
		}
	}

	for i := range s.exceptions {
		if s.exceptions[i].Name == name {
			return s.exceptions[i].Identifier, true
		}
	}

	return "", false
}

// IsOsiApprovedByIdentifier ports SpdxLicenses::isOsiApprovedByIdentifier.
// PHP returns null (with a warning) for an unknown identifier; that is
// false here.
func (s *SpdxLicenses) IsOsiApprovedByIdentifier(identifier string) bool {
	i, ok := s.licenseKeys[php.Strtolower(identifier)]

	return ok && s.licenses[i].OSIApproved
}

// IsDeprecatedByIdentifier ports SpdxLicenses::isDeprecatedByIdentifier.
// PHP returns null (with a warning) for an unknown identifier; that is
// false here.
func (s *SpdxLicenses) IsDeprecatedByIdentifier(identifier string) bool {
	i, ok := s.licenseKeys[php.Strtolower(identifier)]

	return ok && s.licenses[i].Deprecated
}

// Validate ports SpdxLicenses::validate for a string: whether license is a
// known identifier, NONE, NOASSERTION or a valid SPDX license expression.
func (s *SpdxLicenses) Validate(license string) bool {
	return s.isValidLicenseString(license)
}

// ValidateList ports SpdxLicenses::validate for an array of strings:
// several licenses are validated as "(a OR b ...)", one as itself and none
// as the empty string.
func (s *SpdxLicenses) ValidateList(licenses []string) bool {
	switch len(licenses) {
	case 0:
		return s.isValidLicenseString("")
	case 1:
		return s.isValidLicenseString(licenses[0])
	default:
		return s.isValidLicenseString("(" + strings.Join(licenses, " OR ") + ")")
	}
}

// ValidateValue ports SpdxLicenses::validate for an arbitrary PHP value
// (string, []string or *php.Array of strings), returning the
// InvalidArgumentError PHP throws for anything else.
func (s *SpdxLicenses) ValidateValue(license any) (bool, error) {
	switch v := license.(type) {
	case string:
		return s.isValidLicenseString(v), nil
	case []string:
		return s.ValidateList(v), nil
	case *php.Array:
		list := make([]string, 0, v.Len())

		for _, item := range v.All() {
			str, ok := item.(string)
			if !ok {
				return false, &InvalidArgumentError{Message: "Array of strings expected."}
			}

			list = append(list, str)
		}

		return s.ValidateList(list), nil
	}

	return false, &InvalidArgumentError{Message: fmt.Sprintf("Array or String expected, %s given.", php.GetType(license))}
}

// isValidLicenseString ports SpdxLicenses::isValidLicenseString: a
// known identifier, or a match of the SPDX expression regex (see grammar).
func (s *SpdxLicenses) isValidLicenseString(license string) bool {
	if _, ok := s.licenseKeys[php.Strtolower(license)]; ok {
		return true
	}

	return s.matchExpression(license)
}

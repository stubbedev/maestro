// Ports src/Composer/Advisory/PartialSecurityAdvisory.php,
// src/Composer/Advisory/SecurityAdvisory.php and
// src/Composer/FilterList/FilterListEntry.php.
//
// These data classes of the Advisory and FilterList namespaces live here
// rather than in internal/advisory and internal/filterlist because the
// repositories create them and those packages import this one (their
// auditors take RepositorySet); they alias these types.

package repository

import (
	"errors"
	"math"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// ConstraintParser is the part of Composer\Semver\VersionParser the
// advisory and filter list factories use; *pkg.VersionParser and
// semver.VersionParser implement it.
type ConstraintParser interface {
	ParseConstraints(constraints string) (semver.ConstraintInterface, error)
}

// Advisory is a PartialSecurityAdvisory or one of its subclasses
// (*SecurityAdvisory, and internal/advisory's IgnoredSecurityAdvisory):
// `instanceof SecurityAdvisory` is a type assertion.
type Advisory interface {
	// Partial returns the PartialSecurityAdvisory part.
	Partial() *PartialSecurityAdvisory
	// JSONSerialize ports jsonSerialize.
	JSONSerialize() *php.Array
}

// PartialSecurityAdvisory ports Composer\Advisory\PartialSecurityAdvisory.
type PartialSecurityAdvisory struct {
	AdvisoryID       string
	PackageName      string
	AffectedVersions semver.ConstraintInterface
}

// Partial returns a.
func (a *PartialSecurityAdvisory) Partial() *PartialSecurityAdvisory { return a }

// JSONSerialize ports PartialSecurityAdvisory::jsonSerialize: the
// properties, affectedVersions as its pretty string.
func (a *PartialSecurityAdvisory) JSONSerialize() *php.Array {
	return php.ArrayOf("advisoryId", a.AdvisoryID, "packageName", a.PackageName, "affectedVersions", a.AffectedVersions.PrettyString())
}

// SecurityAdvisory ports Composer\Advisory\SecurityAdvisory.
type SecurityAdvisory struct {
	PartialSecurityAdvisory
	Title string
	CVE   pkg.NullString
	Link  pkg.NullString
	// ReportedAt keeps the zone the date was given in (UTC by default).
	ReportedAt time.Time
	// Sources is a list of ['name' => ..., 'remoteId' => ...].
	Sources  *php.Array
	Severity pkg.NullString
}

// JSONSerialize ports SecurityAdvisory::jsonSerialize: reportedAt in
// DATE_RFC3339.
func (a *SecurityAdvisory) JSONSerialize() *php.Array {
	data := a.PartialSecurityAdvisory.JSONSerialize()
	data.Set("title", a.Title)
	data.Set("cve", a.CVE.Value())
	data.Set("link", a.Link.Value())
	data.Set("reportedAt", a.ReportedAt.Format(dumper.RFC3339))
	data.Set("sources", a.Sources)
	data.Set("severity", a.Severity.Value())

	return data
}

var advisoryConstraintPrefix = php.MustCompile(`{(^[>=<^~]*[\d.]+).*}`)

// CreatePartialSecurityAdvisory ports PartialSecurityAdvisory::create: a
// *SecurityAdvisory when data has a title, sources and reportedAt, else a
// *PartialSecurityAdvisory. An affectedVersions constraint that does not
// parse is cut down to its leading version (<=3.20-test2 becoming <=3.20),
// or matches nothing.
func CreatePartialSecurityAdvisory(packageName string, data *php.Array, parser ConstraintParser) (Advisory, error) {
	raw, ok := data.Get("affectedVersions")
	if !ok {
		return nil, &util.ErrorException{Message: `Undefined array key "affectedVersions"`}
	}
	constraint, err := parseConstraintsValue(parser, raw)
	if isUnexpectedValue(err) {
		// try to keep only the essential part of the constraint to turn invalid ones like <=3.20-test2 into <=3.20 which is better than nothing
		if raw == nil {
			// Preg::replace() takes scalars only
			return nil, &php.EngineError{Class: php.ClassTypeError, Message: "$subject must be a string, NULL given."}
		}
		var affectedVersion string
		affectedVersion, _, err = advisoryConstraintPrefix.Replace(php.ToString(raw), "$1", -1)
		if err == nil {
			constraint, err = parser.ParseConstraints(affectedVersion)
		}
		if isUnexpectedValue(err) {
			constraint, err = semver.NewConstraintOp(semver.OpEQ, "0.0.0-invalid-version"), nil
		}
	}
	if err != nil {
		return nil, err
	}

	// new self(...), or new SecurityAdvisory(...) when the data is
	// complete: $advisoryId is argument #2 of either
	complete := issetAll(data, "title", "sources", "reportedAt")
	ctor := `Composer\Advisory\PartialSecurityAdvisory`
	if complete {
		ctor = `Composer\Advisory\SecurityAdvisory`
	}
	rawID, ok := data.Get("advisoryId")
	if !ok {
		return nil, &util.ErrorException{Message: `Undefined array key "advisoryId"`}
	}
	advisoryID, idOK := rawID.(string)
	idTypeError := func() error {
		return pkg.ArgumentTypeError(ctor+"::__construct", 2, "advisoryId", "string", rawID)
	}
	if !complete {
		if !idOK {
			return nil, idTypeError()
		}

		return &PartialSecurityAdvisory{AdvisoryID: advisoryID, PackageName: packageName, AffectedVersions: constraint}, nil
	}

	// the arguments are evaluated first, new \DateTimeImmutable($data
	// ['reportedAt']) among them; the constructor then checks its
	// parameters in order
	title, _ := data.Get("title")
	sources, _ := data.Get("sources")
	reportedAt, _ := data.Get("reportedAt")
	reportedAtStr, ok := reportedAt.(string)
	if !ok {
		return nil, pkg.ArgumentTypeError("DateTimeImmutable::__construct", 1, "datetime", "string", reportedAt)
	}
	date, err := loader.ParseDateTime(reportedAtStr)
	if err != nil {
		if de, ok := errors.AsType[*loader.DateTimeError](err); ok {
			return nil, de
		}

		return nil, err
	}
	if !idOK {
		return nil, idTypeError()
	}
	titleStr, ok := title.(string)
	if !ok {
		return nil, securityAdvisoryTypeError(4, "title", "string", title)
	}
	sourceList, ok := sources.(*php.Array)
	if !ok {
		return nil, securityAdvisoryTypeError(5, "sources", "array", sources)
	}
	partial := PartialSecurityAdvisory{AdvisoryID: advisoryID, PackageName: packageName, AffectedVersions: constraint}
	advisory := &SecurityAdvisory{PartialSecurityAdvisory: partial, Title: titleStr, ReportedAt: date, Sources: sourceList}
	for _, f := range []struct {
		key string
		dst *pkg.NullString
		pos int
	}{{"cve", &advisory.CVE, 7}, {"link", &advisory.Link, 8}, {"severity", &advisory.Severity, 9}} {
		v, _ := data.Get(f.key)
		switch v := v.(type) {
		case nil:
		case string:
			*f.dst = pkg.Str(v)
		default:
			return nil, securityAdvisoryTypeError(f.pos, f.key, "?string", v)
		}
	}

	return advisory, nil
}

// FilterListEntry ports Composer\FilterList\FilterListEntry.
type FilterListEntry struct {
	PackageName string
	ListName    string
	Constraint  semver.ConstraintInterface
	URL         pkg.NullString
	Reason      pkg.NullString
	ID          pkg.NullString
	Source      pkg.NullString
}

// CreateFilterListEntry ports FilterListEntry::create.
func CreateFilterListEntry(listName string, data *php.Array, parser ConstraintParser) (*FilterListEntry, error) {
	raw, ok := data.Get("constraint")
	if !ok {
		return nil, &util.ErrorException{Message: `Undefined array key "constraint"`}
	}
	constraint, err := parseConstraintsValue(parser, raw)
	if err != nil {
		return nil, err
	}
	packageName, err := arrayString(data, "package", func(v any) error {
		return pkg.ArgumentTypeError(`Composer\FilterList\FilterListEntry::__construct`, 1, "packageName", "string", v)
	})
	if err != nil {
		return nil, err
	}
	entry := &FilterListEntry{PackageName: packageName, ListName: listName, Constraint: constraint}
	for _, f := range []struct {
		key string
		dst *pkg.NullString
		pos int
	}{{"url", &entry.URL, 4}, {"reason", &entry.Reason, 5}, {"id", &entry.ID, 6}, {"source", &entry.Source, 7}} {
		v, _ := data.Get(f.key)
		switch v := v.(type) {
		case nil:
		case string:
			*f.dst = pkg.Str(v)
		default:
			// new self(...) at line 84
			return nil, pkg.ArgumentTypeError(`Composer\FilterList\FilterListEntry::__construct`, f.pos, f.key, "?string", v)
		}
	}

	return entry, nil
}

// parseConstraintsValue ports Composer\Package\Version\VersionParser::
// parseConstraints($constraints) called at file:line with a value of the
// metadata: the parameter is untyped, so a scalar is no TypeError. It
// indexes its constraint cache with the value (isset() at line 33, the
// assignment at line 37): an array is isset()'s TypeError, a float with a
// fraction PHP's "Implicit conversion" deprecation notice at both (the
// cache maestro keeps per parser is keyed by the string parsed, so a value
// another one already filled the key of is parsed anew), and the scalar is
// parsed as the string it casts to.
func parseConstraintsValue(parser ConstraintParser, v any) (semver.ConstraintInterface, error) {
	switch c := v.(type) {
	case string:
		return parser.ParseConstraints(c)
	case *php.Array:
		err := &php.EngineError{Class: php.ClassTypeError, Message: "Cannot access offset of type array in isset or empty"}

		return nil, err
	case float64:
		if c != math.Trunc(c) && !math.IsInf(c, 0) && !math.IsNaN(c) {
			msg := "Implicit conversion from float " + php.ToString(c) + " to int loses precision"
			util.RaiseDeprecation(msg)
			util.RaiseDeprecation(msg)
		}
	}

	return parser.ParseConstraints(php.ToString(v))
}

// securityAdvisoryTypeError is the TypeError of argument n of new
// SecurityAdvisory(...) (SecurityAdvisory.php:59), called in
// PartialSecurityAdvisory::create at line 60.
func securityAdvisoryTypeError(n int, param, expected string, given any) error {
	return pkg.ArgumentTypeError(`Composer\Advisory\SecurityAdvisory::__construct`, n, param, expected, given)
}

// issetAll is isset($data[k1], $data[k2], ...).
func issetAll(data *php.Array, keys ...string) bool {
	for _, k := range keys {
		if v, _ := data.Get(k); v == nil {
			return false
		}
	}

	return true
}

// arrayString reads the string $data[$key]: a missing key is PHP's
// "Undefined array key" warning (an ErrorException under Composer's error
// handler), another type the error typeError returns.
func arrayString(data *php.Array, key string, typeError func(v any) error) (string, error) {
	v, ok := data.Get(key)
	if !ok {
		return "", &util.ErrorException{Message: `Undefined array key "` + key + `"`}
	}
	s, ok := v.(string)
	if !ok {
		return "", typeError(v)
	}

	return s, nil
}

// isUnexpectedValue reports whether err is (or wraps) PHP's
// UnexpectedValueException, as semver and Composer raise it.
func isUnexpectedValue(err error) bool {
	if err == nil {
		return false
	}
	var semverErr *semver.UnexpectedValueError
	var utilErr *util.UnexpectedValueError

	return errors.As(err, &semverErr) || errors.As(err, &utilErr)
}

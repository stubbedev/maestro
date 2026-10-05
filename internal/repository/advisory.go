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
	affectedVersions, err := arrayString(data, "affectedVersions", "PartialSecurityAdvisory::create")
	if err != nil {
		return nil, err
	}
	constraint, err := parser.ParseConstraints(affectedVersions)
	if isUnexpectedValue(err) {
		// try to keep only the essential part of the constraint to turn invalid ones like <=3.20-test2 into <=3.20 which is better than nothing
		var affectedVersion string
		affectedVersion, _, err = advisoryConstraintPrefix.Replace(affectedVersions, "$1", -1)
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

	advisoryID, err := arrayString(data, "advisoryId", "PartialSecurityAdvisory::__construct")
	if err != nil {
		return nil, err
	}
	partial := PartialSecurityAdvisory{AdvisoryID: advisoryID, PackageName: packageName, AffectedVersions: constraint}

	title, hasTitle := data.Get("title")
	sources, hasSources := data.Get("sources")
	reportedAt, hasReportedAt := data.Get("reportedAt")
	if !hasTitle || title == nil || !hasSources || sources == nil || !hasReportedAt || reportedAt == nil {
		return &partial, nil
	}

	titleStr, ok := title.(string)
	if !ok {
		return nil, pkg.ArgumentTypeError("SecurityAdvisory::__construct", 4, "title", "string", title)
	}
	sourceList, ok := sources.(*php.Array)
	if !ok {
		return nil, pkg.ArgumentTypeError("SecurityAdvisory::__construct", 5, "sources", "array", sources)
	}
	reportedAtStr, ok := reportedAt.(string)
	if !ok {
		return nil, pkg.ArgumentTypeError("DateTimeImmutable::__construct", 1, "datetime", "string", reportedAt)
	}
	date, err := loader.ParseDateTime(reportedAtStr)
	if err != nil {
		return nil, err
	}
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
			return nil, pkg.ArgumentTypeError("SecurityAdvisory::__construct", f.pos, f.key, "?string", v)
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
	constraintStr, err := arrayString(data, "constraint", "Composer\\Semver\\VersionParser::parseConstraints")
	if err != nil {
		return nil, err
	}
	constraint, err := parser.ParseConstraints(constraintStr)
	if err != nil {
		return nil, err
	}
	packageName, err := arrayString(data, "package", "Composer\\FilterList\\FilterListEntry::__construct")
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
			return nil, pkg.ArgumentTypeError("Composer\\FilterList\\FilterListEntry::__construct", f.pos, f.key, "?string", v)
		}
	}

	return entry, nil
}

// arrayString reads the string $data[$key] passed to fn: a missing key is
// PHP's "Undefined array key" warning (an ErrorException under Composer's
// error handler), another type a TypeError.
func arrayString(data *php.Array, key, fn string) (string, error) {
	v, ok := data.Get(key)
	if !ok {
		return "", &util.ErrorException{Message: `Undefined array key "` + key + `"`}
	}
	s, ok := v.(string)
	if !ok {
		return "", pkg.ArgumentTypeError(fn, 1, key, "string", v)
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

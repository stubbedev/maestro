// Ports src/Composer/Advisory/Auditor.php.

package advisory

import (
	"fmt"
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// Auditor::FORMAT_*.
const (
	FormatTable   = "table"
	FormatPlain   = "plain"
	FormatJSON    = "json"
	FormatSummary = "summary"
)

// Formats is Auditor::FORMATS. Treat it as read-only.
var Formats = [...]string{FormatTable, FormatPlain, FormatJSON, FormatSummary}

// Auditor::ABANDONED_* (deprecated aliases of the policy.Audit* values).
const (
	AbandonedIgnore = policy.AuditIgnore
	AbandonedReport = policy.AuditReport
	AbandonedFail   = policy.AuditFail
)

// Auditor::STATUS_*: the audit result.
const (
	StatusOK     = 0
	StatusFailed = 1
)

// RepositorySet is the part of Composer\Repository\RepositorySet the
// Auditor uses; *repository.RepositorySet implements it.
type RepositorySet interface {
	GetMatchingSecurityAdvisories(packages []pkg.PackageInterface, allowPartial, ignoreUnreachable bool) (repository.SecurityAdvisoriesResult, error)
}

var _ RepositorySet = (*repository.RepositorySet)(nil)

// Advisories is array<string, list<PartialSecurityAdvisory>>: package
// name => advisories.
type Advisories = repository.NameMap[[]Advisory]

// Auditor ports Composer\Advisory\Auditor.
type Auditor struct{}

// Audit ports audit: it reports the advisories, abandoned packages and
// filter list matches of the packages in format (one of the Format*
// values), as warnings when warningOnly, else as errors, and returns
// StatusOK or StatusFailed. filterListProviderSet may be nil (null).
func (a Auditor) Audit(out io.IO, repoSet RepositorySet, policyConfig *policy.PolicyConfig, packages []pkg.PackageInterface, format string, warningOnly bool, filterListProviderSet filterlist.ProviderSet) (int, error) {
	ignoreList := policyConfig.Advisories.IgnoreListForOperation("audit")
	ignoredSeverities := policyConfig.Advisories.IgnoreSeverityForOperation("audit")

	result, err := repoSet.GetMatchingSecurityAdvisories(packages, format == FormatSummary, policyConfig.IgnoreUnreachable.Audit)
	if err != nil {
		return 0, err
	}
	allAdvisories := result.Advisories
	unreachableRepos := result.UnreachableRepos

	// we need the CVE & remote IDs set to filter ignores correctly so if we have any matches using the optimized codepath above
	// and ignores are set then we need to query again the full data to make sure it can be filtered
	if format == FormatSummary && a.NeedsCompleteAdvisoryLoad(allAdvisories, ignoreList) {
		result, err = repoSet.GetMatchingSecurityAdvisories(packages, false, policyConfig.IgnoreUnreachable.Audit)
		if err != nil {
			return 0, err
		}
		allAdvisories = result.Advisories
		unreachableRepos = append(unreachableRepos, result.UnreachableRepos...)
	}
	advisories, ignoredAdvisories := a.ProcessAdvisories(allAdvisories, ignoreList, ignoredSeverities)

	abandonedCount := 0
	affectedPackagesCount := advisories.Len()
	var abandonedPackages []pkg.CompletePackageInterface
	if policyConfig.Abandoned.Audit != policy.AuditIgnore {
		abandonedPackages, err = a.FilterAbandonedPackages(packages, policyConfig.Abandoned.FlatIgnoreForOperation("audit"))
		if err != nil {
			return 0, err
		}
		if policyConfig.Abandoned.Audit == policy.AuditFail {
			abandonedCount = len(abandonedPackages)
		}
	}

	var filterAuditor filterlist.FilterListAuditor
	filteredPackages := &repository.NameMap[[]*filterlist.FilterListEntry]{}
	filteredCount := 0
	activeAuditFilterLists := policyConfig.ActiveAuditFilterLists()
	if filterListProviderSet != nil && activeAuditFilterLists.Len() > 0 {
		failingListNames := map[string]bool{}
		for name, list := range activeAuditFilterLists.All() {
			if list.List().Audit == policy.AuditFail {
				failingListNames[name] = true
			}
		}

		filterResult, err := filterAuditor.CollectFilterLists(packages, filterListProviderSet, activeAuditFilterLists.Keys(), policyConfig.IgnoreUnreachable.Audit)
		if err != nil {
			return 0, err
		}
		unreachableRepos = append(unreachableRepos, filterResult.UnreachableRepos...)
		for _, p := range packages {
			entries, err := filterAuditor.GetMatchingAuditEntries(p, filterResult.Filter, policyConfig)
			if err != nil {
				return 0, err
			}
			for _, entry := range entries {
				list, _ := filteredPackages.Get(p.Name())
				filteredPackages.Set(p.Name(), append(list, entry))

				if failingListNames[entry.ListName] {
					filteredCount++
				}
			}
		}
	}

	auditResult := StatusOK
	if affectedPackagesCount > 0 || abandonedCount > 0 || filteredCount > 0 {
		auditResult = StatusFailed
	}

	if format == FormatJSON {
		data := php.ArrayOf("advisories", advisoriesArray(advisories))
		if ignoredAdvisories.Len() > 0 {
			data.Set("ignored-advisories", advisoriesArray(ignoredAdvisories))
		}
		if len(unreachableRepos) > 0 {
			data.Set("unreachable-repositories", php.StringList(unreachableRepos))
		}
		abandoned := php.NewArray()
		for _, p := range abandonedPackages {
			abandoned.Set(p.PrettyName(), p.ReplacementPackage().Value())
		}
		data.Set("abandoned", abandoned)
		filter := php.NewArray()
		for name, entries := range filteredPackages.All() {
			list := php.NewArrayCap(len(entries))
			for _, entry := range entries {
				list.Append(php.ArrayOf(
					"packageName", entry.PackageName,
					"listName", entry.ListName,
					"constraint", entry.Constraint.PrettyString(),
					"url", entry.URL.Value(),
					"reason", entry.Reason.Value(),
					"id", entry.ID.Value(),
					"source", entry.Source.Value(),
				))
			}
			filter.Set(name, list)
		}
		data.Set("filter", filter)

		encoded, err := json.EncodeDefault(data)
		if err != nil {
			return 0, err
		}
		out.Write(encoded, true, io.Normal)

		return auditResult, nil
	}

	errorOrWarn := "error"
	if warningOnly {
		errorOrWarn = "warning"
	}
	if affectedPackagesCount > 0 || ignoredAdvisories.Len() > 0 {
		passes := []struct {
			advisories *Advisories
			message    string
		}{
			{ignoredAdvisories, "<info>Found %d ignored security vulnerability advisor%s affecting %d package%s%s</info>"},
			{advisories, "<" + errorOrWarn + ">Found %d security vulnerability advisor%s affecting %d package%s%s</" + errorOrWarn + ">"},
		}
		for _, pass := range passes {
			pkgCount, totalAdvisoryCount := countAdvisories(pass.advisories)
			if pkgCount > 0 {
				plurality := "ies"
				if totalAdvisoryCount == 1 {
					plurality = "y"
				}
				pkgPlurality := "s"
				if pkgCount == 1 {
					pkgPlurality = ""
				}
				punctuation := ":"
				if format == FormatSummary {
					punctuation = "."
				}
				out.Write(fmt.Sprintf(pass.message, totalAdvisoryCount, plurality, pkgCount, pkgPlurality, punctuation), true, io.Normal)
				if err := outputAdvisories(out, pass.advisories, format); err != nil {
					return 0, err
				}
			}
		}

		if format == FormatSummary {
			out.Write(`Run "composer audit" for a full list of advisories.`, true, io.Normal)
		}
	} else {
		out.Write("<info>No security vulnerability advisories found.</info>", true, io.Normal)
	}

	if len(unreachableRepos) > 0 {
		out.WriteError("<warning>The following repositories were unreachable:</warning>", true, io.Normal)
		for _, repo := range unreachableRepos {
			out.WriteError("  - "+repo, true, io.Normal)
		}
	}

	if len(abandonedPackages) > 0 && format != FormatSummary {
		if err := outputAbandonedPackages(out, abandonedPackages, format); err != nil {
			return 0, err
		}
	}

	if filteredPackages.Len() > 0 {
		plurality := "s"
		if filteredPackages.Len() == 1 {
			plurality = ""
		}
		punctuation := ":"
		if format == FormatSummary {
			punctuation = "."
		}
		style := "warning"
		if filteredCount > 0 {
			style = "error"
		}

		out.Write(fmt.Sprintf("<%s>Found %d package%s matching filters%s</%s>", style, filteredPackages.Len(), plurality, punctuation, style), true, io.Normal)
		if format != FormatSummary {
			if err := outputFilteredPackages(out, filteredPackages, format); err != nil {
				return 0, err
			}
		}
	}

	return auditResult, nil
}

// advisoriesArray is the JSON form of an advisory map.
func advisoriesArray(advisories *Advisories) *php.Array {
	out := php.NewArrayCap(advisories.Len())
	for name, list := range advisories.All() {
		l := php.NewArrayCap(len(list))
		for _, advisory := range list {
			l.Append(advisory.JSONSerialize())
		}
		out.Set(name, l)
	}

	return out
}

// NeedsCompleteAdvisoryLoad ports needsCompleteAdvisoryLoad: whether
// partial advisories were loaded while the ignore list holds IDs other
// than Packagist's, which only full advisories can be matched against.
func (Auditor) NeedsCompleteAdvisoryLoad(advisories *Advisories, ignoreList *policy.Reasons) bool {
	if advisories.Len() == 0 {
		return false
	}

	// no partial advisories present
	allFull := true
	for _, pkgAdvisories := range advisories.All() {
		for _, advisory := range pkgAdvisories {
			if _, ok := AsSecurityAdvisory(advisory); !ok {
				allFull = false
			}
		}
	}
	if allFull {
		return false
	}

	for _, id := range ignoreList.Keys() {
		if !strings.HasPrefix(id, "PKSA-") {
			return true
		}
	}

	return false
}

// FilterAbandonedPackages ports filterAbandonedPackages: the abandoned
// complete packages whose names ignoreAbandoned (name patterns) does not
// match.
func (Auditor) FilterAbandonedPackages(packages []pkg.PackageInterface, ignoreAbandoned *policy.Reasons) ([]pkg.CompletePackageInterface, error) {
	var filter *php.Regexp
	if ignoreAbandoned.Len() != 0 {
		var err error
		if filter, err = php.Compile(pkg.PackageNamesToRegexp(ignoreAbandoned.Keys(), "{^(?:%s)$}iD")); err != nil {
			return nil, err
		}
	}

	var result []pkg.CompletePackageInterface
	for _, p := range packages {
		complete, ok := p.(pkg.CompletePackageInterface)
		if !ok || !complete.IsAbandoned() {
			continue
		}
		if filter != nil {
			matched, err := filter.IsMatch(p.Name())
			if err != nil {
				return nil, err
			}
			if matched {
				continue
			}
		}
		result = append(result, complete)
	}

	return result, nil
}

// ProcessAdvisories ports processAdvisories: the advisories split into
// the active and the ignored ones. ignoreList holds advisory IDs, remote
// IDs, CVE IDs and package names; ignoredSeverities severity levels; both
// with their reasons.
func (Auditor) ProcessAdvisories(allAdvisories *Advisories, ignoreList, ignoredSeverities *policy.Reasons) (advisories, ignored *Advisories) {
	if ignoreList.Len() == 0 && ignoredSeverities.Len() == 0 {
		return allAdvisories, &Advisories{}
	}

	advisories, ignored = &Advisories{}, &Advisories{}
	var ignoreReason *string
	reasonOf := func(m *policy.Reasons, key string) (*string, bool) {
		reason, ok := m.Get(key)

		return reason, ok
	}

	for packageName, pkgAdvisories := range allAdvisories.All() {
		for _, advisory := range pkgAdvisories {
			isActive := true

			if reason, ok := reasonOf(ignoreList, packageName); ok {
				isActive = false
				ignoreReason = reason
			}

			if reason, ok := reasonOf(ignoreList, advisory.Partial().AdvisoryID); ok {
				isActive = false
				ignoreReason = reason
			}

			security, isSecurity := AsSecurityAdvisory(advisory)
			if isSecurity {
				if security.Severity.Valid {
					if reason, ok := reasonOf(ignoredSeverities, security.Severity.S); ok {
						isActive = false
						if reason == nil {
							reason = new(security.Severity.S + " severity is ignored")
						}
						ignoreReason = reason
					}
				}

				if security.CVE.Valid {
					if reason, ok := reasonOf(ignoreList, security.CVE.S); ok {
						isActive = false
						ignoreReason = reason
					}
				}

				for _, source := range sources(security) {
					s, _ := source.(*php.Array)
					var remoteID any
					if s != nil {
						remoteID, _ = s.Get("remoteId")
					}
					if reason, ok := reasonOf(ignoreList, php.ToString(remoteID)); ok {
						isActive = false
						ignoreReason = reason

						break
					}
				}
			}

			if isActive {
				list, _ := advisories.Get(packageName)
				advisories.Set(packageName, append(list, advisory))

				continue
			}

			// Partial security advisories only used in summary mode
			// and in that case we do not need to cast the object.
			if isSecurity {
				var reason pkg.NullString
				if ignoreReason != nil {
					reason = pkg.Str(*ignoreReason)
				}
				advisory = ToIgnoredAdvisory(security, reason)
			}

			list, _ := ignored.Get(packageName)
			ignored.Set(packageName, append(list, advisory))
		}
	}

	return advisories, ignored
}

// sources returns the advisory's sources as a list.
func sources(advisory *SecurityAdvisory) []any {
	if advisory.Sources == nil {
		return nil
	}

	return advisory.Sources.Values()
}

// countAdvisories ports countAdvisories: the number of affected packages
// and of advisories.
func countAdvisories(advisories *Advisories) (packages, total int) {
	for _, packageAdvisories := range advisories.All() {
		total += len(packageAdvisories)
	}

	return advisories.Len(), total
}

// tableIO is `$io instanceof ConsoleIO`, which the table format needs.
type tableIO interface {
	io.IO
	Table() *console.Table
}

// consoleIO returns out as a ConsoleIO, or the InvalidArgumentException
// the table format raises for other IOs.
func consoleIO(out io.IO, line int) (tableIO, error) {
	if t, ok := out.(tableIO); ok {
		return t, nil
	}

	return nil, &util.InvalidArgumentError{Message: "Cannot use table format with " + className(out), Site: phperr.At("Auditor.php", line)}
}

// className is get_class($io).
func className(out io.IO) string {
	if _, ok := out.(*io.NullIO); ok {
		return `Composer\IO\NullIO`
	}

	return fmt.Sprintf("%T", out)
}

func outputAdvisories(out io.IO, advisories *Advisories, format string) error {
	switch format {
	case FormatTable:
		t, err := consoleIO(out, 337)
		if err != nil {
			return err
		}

		return outputAdvisoriesTable(t, advisories)
	case FormatPlain:
		return outputAdvisoriesPlain(out, advisories)
	case FormatSummary:
		return nil
	default:
		return &util.InvalidArgumentError{Message: `Invalid format "` + format + `".`, Site: phperr.At("Auditor.php", 350)}
	}
}

// securityAdvisory is the SecurityAdvisory type check of getSeverity()
// and the other formatting helpers.
func securityAdvisory(advisory Advisory) (*SecurityAdvisory, error) {
	if security, ok := AsSecurityAdvisory(advisory); ok {
		return security, nil
	}

	return nil, &pkg.TypeError{Message: `Composer\Advisory\Auditor::getSeverity(): Argument #1 ($advisory) must be of type Composer\Advisory\SecurityAdvisory, Composer\Advisory\PartialSecurityAdvisory given`, Site: phperr.At("Auditor.php", 472)}
}

func outputAdvisoriesTable(out tableIO, advisories *Advisories) error {
	for _, packageAdvisories := range advisories.All() {
		for _, advisory := range packageAdvisories {
			security, err := securityAdvisory(advisory)
			if err != nil {
				return err
			}
			headers := []any{
				"Package",
				"Severity",
				"Advisory ID",
				"CVE",
				"Title",
				"URL",
				"Affected versions",
				"Reported at",
			}
			row := []string{
				security.PackageName,
				severity(security),
				advisoryID(security),
				cve(security),
				security.Title,
				url(security),
				security.AffectedVersions.PrettyString(),
				security.ReportedAt.Format(dumper.RFC3339),
			}
			if ignored, ok := advisory.(*IgnoredSecurityAdvisory); ok {
				headers = append(headers, "Ignore reason")
				row = append(row, ignoreReason(ignored))
			}
			table := out.Table().SetHorizontal(true).SetHeaders(headers)
			if err := table.AddRow(io.SanitizeMessages(row, true)); err != nil {
				return err
			}
			table.SetColumnWidth(1, 80)
			if err := table.SetColumnMaxWidth(1, 80); err != nil {
				return err
			}
			if err := table.Render(); err != nil {
				return err
			}
		}
	}

	return nil
}

func ignoreReason(advisory *IgnoredSecurityAdvisory) string {
	if advisory.IgnoreReason.Valid {
		return advisory.IgnoreReason.S
	}

	return "None specified"
}

func outputAdvisoriesPlain(out io.IO, advisories *Advisories) error {
	var lines []string
	firstAdvisory := true
	for _, packageAdvisories := range advisories.All() {
		for _, advisory := range packageAdvisories {
			security, err := securityAdvisory(advisory)
			if err != nil {
				return err
			}
			if !firstAdvisory {
				lines = append(lines, "--------")
			}
			lines = append(lines,
				"Package: "+security.PackageName,
				"Severity: "+severity(security),
				"Advisory ID: "+advisoryID(security),
				"CVE: "+cve(security),
				"Title: "+console.Escape(security.Title),
				"URL: "+url(security),
				"Affected versions: "+console.Escape(security.AffectedVersions.PrettyString()),
				"Reported at: "+security.ReportedAt.Format(dumper.RFC3339),
			)
			if ignored, ok := advisory.(*IgnoredSecurityAdvisory); ok {
				lines = append(lines, "Ignore reason: "+ignoreReason(ignored))
			}
			firstAdvisory = false
		}
	}
	out.WriteMessages(lines, true, io.Normal)

	return nil
}

func outputAbandonedPackages(out io.IO, packages []pkg.CompletePackageInterface, format string) error {
	plural := ""
	if len(packages) > 1 {
		plural = "s"
	}
	out.Write(fmt.Sprintf("<error>Found %d abandoned package%s:</error>", len(packages), plural), true, io.Normal)

	if format == FormatPlain {
		for _, p := range packages {
			replacement := "No replacement was suggested"
			if r := p.ReplacementPackage(); r.Valid {
				replacement = "Use " + r.S + " instead"
			}
			out.Write(fmt.Sprintf("%s is abandoned. %s.", packageNameWithLink(p), replacement), true, io.Normal)
		}

		return nil
	}

	t, err := consoleIO(out, 449)
	if err != nil {
		return err
	}

	table := t.Table().SetHeaders([]any{"Abandoned Package", "Suggested Replacement"}).SetColumnWidth(1, 80)
	if err := table.SetColumnMaxWidth(1, 80); err != nil {
		return err
	}

	for _, p := range packages {
		replacement := "none"
		if r := p.ReplacementPackage(); r.Valid {
			replacement = r.S
		}
		if err := table.AddRow(io.SanitizeMessages([]string{packageNameWithLink(p), replacement}, true)); err != nil {
			return err
		}
	}

	return table.Render()
}

func packageNameWithLink(p pkg.PackageInterface) string {
	if packageURL := pkg.GetViewSourceOrHomepageURL(p); packageURL.Valid {
		return "<href=" + console.Escape(packageURL.S) + ">" + p.PrettyName() + "</>"
	}

	return p.PrettyName()
}

func severity(advisory *SecurityAdvisory) string {
	return advisory.Severity.S
}

func advisoryID(advisory *SecurityAdvisory) string {
	if strings.HasPrefix(advisory.AdvisoryID, "PKSA-") {
		return "<href=https://packagist.org/security-advisories/" + advisory.AdvisoryID + ">" + advisory.AdvisoryID + "</>"
	}

	return advisory.AdvisoryID
}

func cve(advisory *SecurityAdvisory) string {
	if !advisory.CVE.Valid {
		return "NO CVE"
	}

	return "<href=https://www.cve.org/CVERecord?id=" + advisory.CVE.S + ">" + advisory.CVE.S + "</>"
}

func url(advisory *SecurityAdvisory) string {
	if !advisory.Link.Valid {
		return ""
	}

	return "<href=" + console.Escape(advisory.Link.S) + ">" + console.Escape(advisory.Link.S) + "</>"
}

func outputFilteredPackages(out io.IO, filteredPackages *repository.NameMap[[]*filterlist.FilterListEntry], format string) error {
	if format == FormatPlain {
		for _, data := range filteredPackages.All() {
			for _, entry := range data {
				parts := []string{entry.PackageName + ` matched dependency policy "` + entry.ListName + `"`}
				if entry.Reason.Valid {
					parts = append(parts, "Reason: "+entry.Reason.S)
				}
				if entry.URL.Valid {
					parts = append(parts, "URL: "+entry.URL.S)
				}
				if entry.Source.Valid {
					parts = append(parts, "Source: "+entry.Source.S)
				}
				out.Write(strings.Join(parts, ". ")+".", true, io.Normal)
			}
		}

		return nil
	}

	t, err := consoleIO(out, 537)
	if err != nil {
		return err
	}

	table := t.Table().SetHeaders([]any{"Package", "Versions", "List", "URL", "Reason", "ID", "Source"})
	if err := table.SetColumnMaxWidth(4, 40); err != nil {
		return err
	}

	for _, data := range filteredPackages.All() {
		for _, entry := range data {
			if err := table.AddRow(io.SanitizeMessages([]string{
				entry.PackageName,
				entry.Constraint.PrettyString(),
				entry.ListName,
				entry.URL.S,
				entry.Reason.S,
				entry.ID.S,
				entry.Source.S,
			}, true)); err != nil {
				return err
			}
		}
	}

	return table.Render()
}

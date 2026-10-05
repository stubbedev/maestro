// Ports tests/Composer/Test/Advisory/AuditorTest.php (MIT,
// testdata/LICENSE-composer).

package advisory_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

const mockAdvisoriesJSON = `{
    "vendor1/package1": [
        {"advisoryId": "ID1", "packageName": "vendor1/package1", "title": "advisory1", "link": "https://advisory.example.com/advisory1", "cve": "CVE1", "affectedVersions": ">=3,<3.4.3|>=1,<2.5.6", "sources": [{"name": "source1", "remoteId": "RemoteID1"}], "reportedAt": "2022-05-25 13:21:00", "composerRepository": "https://packagist.org", "severity": "medium"},
        {"advisoryId": "ID4", "packageName": "vendor1/package1", "title": "advisory4", "link": "https://advisory.example.com/advisory4", "cve": "CVE3", "affectedVersions": ">=8,<8.2.2|>=1,<2.5.6", "sources": [{"name": "source2", "remoteId": "RemoteID4"}], "reportedAt": "2022-05-25 13:21:00", "composerRepository": "https://packagist.org", "severity": "high"},
        {"advisoryId": "ID5", "packageName": "vendor1/package1", "title": "advisory5", "link": "https://advisory.example.com/advisory5", "cve": "", "affectedVersions": ">=8,<8.2.2|>=1,<2.5.6", "sources": [{"name": "source1", "remoteId": "RemoteID3"}], "reportedAt": "2022-05-25 13:21:00", "composerRepository": "https://packagist.org", "severity": "medium"}
    ],
    "vendor1/package2": [
        {"advisoryId": "ID2", "packageName": "vendor1/package2", "title": "advisory2", "link": "https://advisory.example.com/advisory2", "cve": "", "affectedVersions": ">=3,<3.4.3|>=1,<2.5.6", "sources": [{"name": "source1", "remoteId": "RemoteID2"}], "reportedAt": "2022-05-25 13:21:00", "composerRepository": "https://packagist.org", "severity": "medium"}
    ],
    "vendorx/packagex": [
        {"advisoryId": "IDx", "packageName": "vendorx/packagex", "title": "advisory17", "link": "https://advisory.example.com/advisory17", "cve": "CVE5", "affectedVersions": ">=3,<3.4.3|>=1,<2.5.6", "sources": [{"name": "source2", "remoteId": "RemoteIDx"}], "reportedAt": "2015-05-25 13:21:00", "composerRepository": "https://packagist.org", "severity": "medium"}
    ],
    "vendor2/package1": [
        {"advisoryId": "ID3", "packageName": "vendor2/package1", "title": "advisory3", "link": "https://advisory.example.com/advisory3", "cve": "CVE2", "affectedVersions": ">=3,<3.4.3|>=1,<2.5.6", "sources": [{"name": "source2", "remoteId": "RemoteID1"}], "reportedAt": "2022-05-25 13:21:00", "composerRepository": "https://packagist.org", "severity": "medium"},
        {"advisoryId": "ID6", "packageName": "vendor2/package1", "title": "advisory6", "link": "https://advisory.example.com/advisory6", "cve": "CVE4", "affectedVersions": ">=3,<3.4.3|>=1,<2.5.6", "sources": [{"name": "source2", "remoteId": "RemoteID3"}], "reportedAt": "2015-05-25 13:21:00", "composerRepository": "https://packagist.org", "severity": "medium"}
    ],
    "vendory/packagey": [
        {"advisoryId": "IDy", "packageName": "vendory/packagey", "title": "advisory7", "link": "https://advisory.example.com/advisory7", "cve": "CVE5", "affectedVersions": ">=3,<3.4.3|>=1,<2.5.6", "sources": [{"name": "source2", "remoteId": "RemoteID4"}], "reportedAt": "2015-05-25 13:21:00", "composerRepository": "https://packagist.org", "severity": "medium"}
    ],
    "vendor3/package1": [
        {"advisoryId": "ID7", "packageName": "vendor3/package1", "title": "advisory7", "link": "https://advisory.example.com/advisory7", "cve": "CVE5", "affectedVersions": ">=3,<3.4.3|>=1,<2.5.6", "sources": [{"name": "source2", "remoteId": "RemoteID4"}], "reportedAt": "2015-05-25 13:21:00", "composerRepository": "https://packagist.org", "severity": "medium"}
    ]
}`

// getMockAdvisories is AuditorTest::getMockAdvisories().
func getMockAdvisories(t *testing.T) *php.Array {
	t.Helper()

	v, err := php.JSONDecode(mockAdvisoriesJSON, true)
	if err != nil {
		t.Fatal(err)
	}

	return v.(*php.Array)
}

// mockAdvisoryRepo is the ComposerRepository mock of getRepoSet():
// hasSecurityAdvisories() is true and getSecurityAdvisories() serves
// getMockAdvisories().
type mockAdvisoryRepo struct {
	*repository.ArrayRepository
	advisories *php.Array
}

func (r *mockAdvisoryRepo) HasSecurityAdvisories() (bool, error) { return true, nil }

func (r *mockAdvisoryRepo) SecurityAdvisories(packageConstraintMap *repository.ConstraintMap, allowPartial bool) (repository.AdvisoryResult, error) {
	parser := pkg.NewVersionParser()
	advisories := &repository.NameMap[[]repository.Advisory]{}
	for k, list := range r.advisories.All() {
		name := k.String()
		constraint, ok := packageConstraintMap.Get(name)
		if !ok || constraint == nil {
			continue
		}
		var matching []repository.Advisory
		for _, data := range list.(*php.Array).All() {
			a, err := advisory.CreatePartialSecurityAdvisory(name, data.(*php.Array), parser)
			if err != nil {
				return repository.AdvisoryResult{}, err
			}
			if _, ok := a.(*advisory.SecurityAdvisory); !allowPartial && !ok {
				return repository.AdvisoryResult{}, &util.RuntimeError{Message: "Advisory for " + name + " could not be loaded as a full advisory from test repo"}
			}
			if a.Partial().AffectedVersions.Matches(constraint) {
				matching = append(matching, a)
			}
		}
		if len(matching) > 0 {
			advisories.Set(name, matching)
		}
	}

	return repository.AdvisoryResult{NamesFound: packageConstraintMap.Keys(), Advisories: advisories}, nil
}

// getRepoSet is AuditorTest::getRepoSet().
func getRepoSet(t *testing.T) *repository.RepositorySet {
	t.Helper()

	repoSet, err := repository.NewRepositorySet("stable", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	array, err := repository.NewArrayRepository(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repoSet.AddRepository(&mockAdvisoryRepo{ArrayRepository: array, advisories: getMockAdvisories(t)}); err != nil {
		t.Fatal(err)
	}

	return repoSet
}

type policyOptions struct {
	abandoned              string
	ignoreUnreachableAudit bool
	filteredAudit          string // "" is null
	filteredListName       string
	ignoreAdvisories       []reason
	ignoreSeverities       []string
	ignoreAbandoned        []reason
}

// reason is one entry of an array<string, string|null>.
type reason struct {
	key    string
	reason *string
}

func r(key string) reason { return reason{key: key} }

func rr(key, why string) reason { return reason{key: key, reason: &why} }

// createPolicyConfig is AuditorTest::createPolicyConfig().
func createPolicyConfig(o policyOptions) *policy.PolicyConfig {
	if o.abandoned == "" {
		o.abandoned = policy.AuditIgnore
	}
	if o.filteredListName == "" {
		o.filteredListName = "test-list"
	}
	customLists := &policy.OrderedMap[*policy.CustomListPolicyConfig]{}
	if o.filteredAudit != "" {
		customLists.Set(o.filteredListName, policy.NewCustomListPolicyConfig(o.filteredListName, false, o.filteredAudit, nil, nil))
	}

	packageRules := &policy.IgnoreMap{}
	idRules := &policy.OrderedMap[*policy.IgnoreIDRule]{}
	for _, ignore := range o.ignoreAdvisories {
		if strings.Contains(ignore.key, "/") {
			list, _ := packageRules.Get(ignore.key)
			packageRules.Set(ignore.key, append(list, policy.NewIgnorePackageRule(ignore.key, semver.NewMatchAllConstraint(), ignore.reason, true, true)))
		} else {
			idRules.Set(ignore.key, policy.NewIgnoreIDRule(ignore.key, ignore.reason, true, true))
		}
	}
	severityRules := &policy.OrderedMap[*policy.IgnoreSeverityRule]{}
	for _, severity := range o.ignoreSeverities {
		severityRules.Set(severity, policy.NewIgnoreSeverityRule(severity, nil, true, true))
	}
	advisories := policy.NewAdvisoriesPolicyConfig(false, policy.AuditFail, packageRules, idRules, severityRules)

	abandonedIgnore := &policy.IgnoreMap{}
	for _, ignore := range o.ignoreAbandoned {
		list, _ := abandonedIgnore.Get(ignore.key)
		abandonedIgnore.Set(ignore.key, append(list, policy.NewIgnorePackageRule(ignore.key, semver.NewMatchAllConstraint(), ignore.reason, true, true)))
	}

	ignoreUnreachable := policy.IgnoreUnreachableDefault()
	if o.ignoreUnreachableAudit {
		ignoreUnreachable = policy.IgnoreUnreachableAll()
	}

	return &policy.PolicyConfig{
		Enabled:           true,
		Advisories:        advisories,
		Malware:           policy.MalwarePolicyConfigDisabled(),
		Abandoned:         policy.NewAbandonedPolicyConfig(false, o.abandoned, abandonedIgnore),
		CustomLists:       customLists,
		IgnoreUnreachable: ignoreUnreachable,
	}
}

func newBufferIO(t *testing.T) *io.BufferIO {
	t.Helper()

	b, err := io.NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

// normalizedOutput is trim(str_replace("\r", ”, $io->getOutput())).
func normalizedOutput(b *io.BufferIO) string {
	return php.Trim(strings.ReplaceAll(b.Output(), "\r", ""))
}

// assertIOMock is IOMock::expects($expectedOutput, true) checked by
// assertComplete(): every expected line must come next in the output.
func assertIOMock(t *testing.T, b *io.BufferIO, expected []string) {
	t.Helper()

	lines, err := php.PregSplit("{\r?\n}", b.Output(), -1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range expected {
		if i >= len(lines) {
			t.Fatalf("Expected %q to be output still but there is no output left to consume. Complete output:\n%s", want, b.Output())
		}
		if lines[i] != want {
			t.Fatalf("IO output mismatch. Expected:\n%s\nGot:\n%s", want, lines[i])
		}
	}
}

func newCompletePackage(name, version string, abandoned any) *pkg.CompletePackage {
	p := pkg.NewCompletePackage(name, version, version)
	p.SetAbandoned(abandoned)

	return p
}

func TestAuditor_Audit(t *testing.T) {
	abandonedWithReplacement := newCompletePackage("vendor/abandoned", "1.0.0", "foo/bar")
	abandonedNoReplacement := newCompletePackage("vendor/abandoned2", "1.0.0", true)
	abandonedPackages := []pkg.PackageInterface{abandonedWithReplacement, abandonedNoReplacement}

	cases := []struct {
		name            string
		packages        []pkg.PackageInterface
		warningOnly     bool
		abandoned       string
		ignoreAbandoned []reason
		format          string
		expected        int
		output          string
	}{
		{
			name: "Test no advisories returns 0",
			packages: []pkg.PackageInterface{
				pkg.NewPackage("vendor1/package2", "9.0.0", "9.0.0"),
				pkg.NewPackage("vendor1/package1", "9.0.0", "9.0.0"),
				pkg.NewPackage("vendor3/package1", "9.0.0", "9.0.0"),
			},
			warningOnly: true,
			expected:    advisory.StatusOK,
			output:      "No security vulnerability advisories found.",
		},
		{
			name: "Test with advisories returns 1",
			packages: []pkg.PackageInterface{
				pkg.NewPackage("vendor1/package2", "9.0.0", "9.0.0"),
				pkg.NewPackage("vendor1/package1", "8.2.1", "8.2.1"),
				pkg.NewPackage("vendor3/package1", "9.0.0", "9.0.0"),
			},
			warningOnly: true,
			expected:    advisory.StatusFailed,
			output: `<warning>Found 2 security vulnerability advisories affecting 1 package:</warning>
Package: vendor1/package1
Severity: high
Advisory ID: ID4
CVE: CVE3
Title: advisory4
URL: https://advisory.example.com/advisory4
Affected versions: >=8,<8.2.2|>=1,<2.5.6
Reported at: 2022-05-25T13:21:00+00:00
--------
Package: vendor1/package1
Severity: medium
Advisory ID: ID5
CVE: ` + `
Title: advisory5
URL: https://advisory.example.com/advisory5
Affected versions: >=8,<8.2.2|>=1,<2.5.6
Reported at: 2022-05-25T13:21:00+00:00`,
		},
		{
			name:        "abandoned packages ignored",
			packages:    abandonedPackages,
			warningOnly: false,
			abandoned:   policy.AuditIgnore,
			expected:    advisory.StatusOK,
			output:      "No security vulnerability advisories found.",
		},
		{
			name:            "abandoned packages individually ignored via full vendor",
			packages:        abandonedPackages,
			abandoned:       policy.AuditFail,
			ignoreAbandoned: []reason{r("vendor/*")},
			expected:        advisory.StatusOK,
			output:          "No security vulnerability advisories found.",
		},
		{
			name:            "abandoned packages individually ignored via package name",
			packages:        abandonedPackages,
			abandoned:       policy.AuditFail,
			ignoreAbandoned: []reason{r(abandonedWithReplacement.Name()), r(abandonedNoReplacement.Name())},
			expected:        advisory.StatusOK,
			output:          "No security vulnerability advisories found.",
		},
		{
			name:            "abandoned packages individually ignored not matching package name",
			packages:        abandonedPackages,
			abandoned:       policy.AuditFail,
			ignoreAbandoned: []reason{rr("acme/test", "ignoring because yolo")},
			expected:        advisory.StatusFailed,
			output: `No security vulnerability advisories found.
Found 2 abandoned packages:
vendor/abandoned is abandoned. Use foo/bar instead.
vendor/abandoned2 is abandoned. No replacement was suggested.`,
		},
		{
			name:        "abandoned packages reported only",
			packages:    abandonedPackages,
			warningOnly: true,
			abandoned:   policy.AuditReport,
			expected:    advisory.StatusOK,
			output: `No security vulnerability advisories found.
Found 2 abandoned packages:
vendor/abandoned is abandoned. Use foo/bar instead.
vendor/abandoned2 is abandoned. No replacement was suggested.`,
		},
		{
			name:      "abandoned packages fails",
			packages:  abandonedPackages,
			abandoned: policy.AuditFail,
			format:    advisory.FormatTable,
			expected:  advisory.StatusFailed,
			output: `No security vulnerability advisories found.
Found 2 abandoned packages:
+-------------------+----------------------------------------------------------------------------------+
| Abandoned Package | Suggested Replacement                                                            |
+-------------------+----------------------------------------------------------------------------------+
| vendor/abandoned  | foo/bar                                                                          |
| vendor/abandoned2 | none                                                                             |
+-------------------+----------------------------------------------------------------------------------+`,
		},
		{
			name: "vulnerable and abandoned packages fails",
			packages: []pkg.PackageInterface{
				pkg.NewPackage("vendor1/package1", "8.2.1", "8.2.1"),
				abandonedWithReplacement,
				abandonedNoReplacement,
			},
			abandoned: policy.AuditFail,
			format:    advisory.FormatTable,
			expected:  advisory.StatusFailed,
			output: `Found 2 security vulnerability advisories affecting 1 package:
+-------------------+----------------------------------------------------------------------------------+
| Package           | vendor1/package1                                                                 |
| Severity          | high                                                                             |
| Advisory ID       | ID4                                                                              |
| CVE               | CVE3                                                                             |
| Title             | advisory4                                                                        |
| URL               | https://advisory.example.com/advisory4                                           |
| Affected versions | >=8,<8.2.2|>=1,<2.5.6                                                            |
| Reported at       | 2022-05-25T13:21:00+00:00                                                        |
+-------------------+----------------------------------------------------------------------------------+
+-------------------+----------------------------------------------------------------------------------+
| Package           | vendor1/package1                                                                 |
| Severity          | medium                                                                           |
| Advisory ID       | ID5                                                                              |
| CVE               |                                                                                  |
| Title             | advisory5                                                                        |
| URL               | https://advisory.example.com/advisory5                                           |
| Affected versions | >=8,<8.2.2|>=1,<2.5.6                                                            |
| Reported at       | 2022-05-25T13:21:00+00:00                                                        |
+-------------------+----------------------------------------------------------------------------------+
Found 2 abandoned packages:
+-------------------+----------------------------------------------------------------------------------+
| Abandoned Package | Suggested Replacement                                                            |
+-------------------+----------------------------------------------------------------------------------+
| vendor/abandoned  | foo/bar                                                                          |
| vendor/abandoned2 | none                                                                             |
+-------------------+----------------------------------------------------------------------------------+`,
		},
		{
			name:      "abandoned packages fails with json format",
			packages:  abandonedPackages,
			abandoned: policy.AuditFail,
			format:    advisory.FormatJSON,
			expected:  advisory.StatusFailed,
			output: `{
    "advisories": [],
    "abandoned": {
        "vendor/abandoned": "foo/bar",
        "vendor/abandoned2": null
    },
    "filter": []
}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policyConfig := createPolicyConfig(policyOptions{abandoned: tc.abandoned, ignoreAbandoned: tc.ignoreAbandoned})
			format := tc.format
			if format == "" {
				format = advisory.FormatPlain
			}
			b := newBufferIO(t)
			result, err := advisory.Auditor{}.Audit(b, getRepoSet(t), policyConfig, tc.packages, format, tc.warningOnly, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result != tc.expected {
				t.Errorf("result = %d, want %d", result, tc.expected)
			}
			if got := normalizedOutput(b); got != tc.output {
				t.Errorf("output:\n%s\nwant:\n%s", got, tc.output)
			}
		})
	}
}

func advisoryLines(packageName, id, cve, title, url, reportedAt string) []string {
	return []string{
		"Package: " + packageName,
		"Severity: medium",
		"Advisory ID: " + id,
		"CVE: " + cve,
		"Title: " + title,
		"URL: " + url,
		"Affected versions: >=3,<3.4.3|>=1,<2.5.6",
		"Reported at: " + reportedAt,
	}
}

func concat(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}

	return out
}

func TestAuditor_AuditWithIgnore(t *testing.T) {
	advisory1 := advisoryLines("vendor1/package1", "ID1", "CVE1", "advisory1", "https://advisory.example.com/advisory1", "2022-05-25T13:21:00+00:00")
	cases := []struct {
		name           string
		packages       []pkg.PackageInterface
		ignoredIDs     []reason
		exitCode       int
		expectedOutput []string
	}{
		{
			"ignore by CVE",
			[]pkg.PackageInterface{pkg.NewPackage("vendor1/package1", "3.0.0.0", "3.0.0")},
			[]reason{r("CVE1")},
			0,
			concat([]string{"Found 1 ignored security vulnerability advisory affecting 1 package:"}, advisory1),
		},
		{
			"ignore by CVE with reasoning",
			[]pkg.PackageInterface{pkg.NewPackage("vendor1/package1", "3.0.0.0", "3.0.0")},
			[]reason{rr("CVE1", "A good reason")},
			0,
			concat([]string{"Found 1 ignored security vulnerability advisory affecting 1 package:"}, advisory1, []string{"Ignore reason: A good reason"}),
		},
		{
			"ignore by advisory id",
			[]pkg.PackageInterface{pkg.NewPackage("vendor1/package2", "3.0.0.0", "3.0.0")},
			[]reason{r("ID2")},
			0,
			concat([]string{"Found 1 ignored security vulnerability advisory affecting 1 package:"},
				advisoryLines("vendor1/package2", "ID2", "", "advisory2", "https://advisory.example.com/advisory2", "2022-05-25T13:21:00+00:00")),
		},
		{
			"ignore by remote id",
			[]pkg.PackageInterface{pkg.NewPackage("vendorx/packagex", "3.0.0.0", "3.0.0")},
			[]reason{r("RemoteIDx")},
			0,
			concat([]string{"Found 1 ignored security vulnerability advisory affecting 1 package:"},
				advisoryLines("vendorx/packagex", "IDx", "CVE5", "advisory17", "https://advisory.example.com/advisory17", "2015-05-25T13:21:00+00:00")),
		},
		{
			"ignore by package name",
			[]pkg.PackageInterface{pkg.NewPackage("vendor1/package1", "3.0.0.0", "3.0.0")},
			[]reason{r("vendor1/package1")},
			0,
			concat([]string{"Found 1 ignored security vulnerability advisory affecting 1 package:"}, advisory1),
		},
		{
			"ignore by package name with reasoning",
			[]pkg.PackageInterface{pkg.NewPackage("vendor1/package1", "3.0.0.0", "3.0.0")},
			[]reason{rr("vendor1/package1", "Package has known safe usage")},
			0,
			concat([]string{"Found 1 ignored security vulnerability advisory affecting 1 package:"}, advisory1, []string{"Ignore reason: Package has known safe usage"}),
		},
		{
			"1 vulnerability, 0 ignored",
			[]pkg.PackageInterface{pkg.NewPackage("vendor1/package1", "3.0.0.0", "3.0.0")},
			nil,
			1,
			concat([]string{"Found 1 security vulnerability advisory affecting 1 package:"}, advisory1),
		},
		{
			"1 vulnerability, 3 ignored affecting 2 packages",
			[]pkg.PackageInterface{
				pkg.NewPackage("vendor3/package1", "3.0.0.0", "3.0.0"),
				// RemoteIDx
				pkg.NewPackage("vendorx/packagex", "3.0.0.0", "3.0.0"),
				// ID3, ID6
				pkg.NewPackage("vendor2/package1", "3.0.0.0", "3.0.0"),
			},
			[]reason{r("RemoteIDx"), r("ID3"), r("ID6")},
			1,
			concat(
				[]string{"Found 3 ignored security vulnerability advisories affecting 2 packages:"},
				advisoryLines("vendor2/package1", "ID3", "CVE2", "advisory3", "https://advisory.example.com/advisory3", "2022-05-25T13:21:00+00:00"),
				[]string{"Ignore reason: None specified", "--------"},
				advisoryLines("vendor2/package1", "ID6", "CVE4", "advisory6", "https://advisory.example.com/advisory6", "2015-05-25T13:21:00+00:00"),
				[]string{"Ignore reason: None specified", "--------"},
				advisoryLines("vendorx/packagex", "IDx", "CVE5", "advisory17", "https://advisory.example.com/advisory17", "2015-05-25T13:21:00+00:00"),
				[]string{"Ignore reason: None specified", "Found 1 security vulnerability advisory affecting 1 package:"},
				advisoryLines("vendor3/package1", "ID7", "CVE5", "advisory7", "https://advisory.example.com/advisory7", "2015-05-25T13:21:00+00:00"),
			),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policyConfig := createPolicyConfig(policyOptions{ignoreAdvisories: tc.ignoredIDs})
			b := newBufferIO(t)
			result, err := advisory.Auditor{}.Audit(b, getRepoSet(t), policyConfig, tc.packages, advisory.FormatPlain, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertIOMock(t, b, tc.expectedOutput)
			if result != tc.exitCode {
				t.Errorf("result = %d, want %d", result, tc.exitCode)
			}
		})
	}
}

func TestAuditor_AuditWithIgnoreSeverity(t *testing.T) {
	cases := []struct {
		name              string
		ignoredSeverities []string
		exitCode          int
		expectedOutput    []string
	}{
		{"ignore medium", []string{"medium"}, 1, []string{"Found 2 ignored security vulnerability advisories affecting 1 package:"}},
		{"ignore high", []string{"high"}, 1, []string{"Found 1 ignored security vulnerability advisory affecting 1 package:"}},
		{"ignore high and medium", []string{"high", "medium"}, 0, []string{"Found 3 ignored security vulnerability advisories affecting 1 package:"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policyConfig := createPolicyConfig(policyOptions{ignoreSeverities: tc.ignoredSeverities})
			b := newBufferIO(t)
			packages := []pkg.PackageInterface{pkg.NewPackage("vendor1/package1", "2.0.0.0", "2.0.0")}
			result, err := advisory.Auditor{}.Audit(b, getRepoSet(t), policyConfig, packages, advisory.FormatPlain, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertIOMock(t, b, tc.expectedOutput)
			if result != tc.exitCode {
				t.Errorf("result = %d, want %d", result, tc.exitCode)
			}
		})
	}
}

// unreachableRepoSet is the RepositorySet mock of
// testAuditWithIgnoreUnreachable.
type unreachableRepoSet struct{ errorMessage string }

func utc(date string) time.Time {
	d, _ := time.Parse(time.DateOnly, date)

	return d
}

func newSecurityAdvisory(packageName, advisoryID string, affected semver.ConstraintInterface, title string, sources *php.Array, reportedAt time.Time, cve, link, severity pkg.NullString) *advisory.SecurityAdvisory {
	return &advisory.SecurityAdvisory{
		AdvisoryID: advisoryID, PackageName: packageName, AffectedVersions: affected,
		Title:      title,
		Sources:    sources,
		ReportedAt: reportedAt,
		CVE:        cve,
		Link:       link,
		Severity:   severity,
	}
}

func (s unreachableRepoSet) GetMatchingSecurityAdvisories(_ []pkg.PackageInterface, _, ignoreUnreachable bool) (repository.SecurityAdvisoriesResult, error) {
	if !ignoreUnreachable {
		return repository.SecurityAdvisoriesResult{}, util.NewTransportError(s.errorMessage, 404)
	}

	// Simulate multiple repositories with the middle one being unreachable
	// First and third repositories have advisories, middle one is unreachable
	advisories := &repository.NameMap[[]repository.Advisory]{}
	advisories.Set("vendor1/package1", []repository.Advisory{
		newSecurityAdvisory("vendor1/package1", "CVE-2023-12345", semver.NewConstraintOp(semver.OpEQ, "3.0.0.0"), "First repo advisory",
			php.ListOf(php.ArrayOf("name", "test", "remoteId", "1")), utc("2023-01-01"), pkg.Str("CVE-2023-12345"), pkg.Str("https://example.com/advisory/1"), pkg.Str("medium")),
		newSecurityAdvisory("vendor1/package1", "CVE-2023-67890", semver.NewConstraintOp(semver.OpEQ, "3.0.0.0"), "Third repo advisory",
			php.ListOf(php.ArrayOf("name", "test", "remoteId", "3")), utc("2023-01-01"), pkg.Str("CVE-2023-67890"), pkg.Str("https://example.com/advisory/3"), pkg.Str("high")),
	})

	return repository.SecurityAdvisoriesResult{Advisories: advisories, UnreachableRepos: []string{s.errorMessage}}, nil
}

func TestAuditor_AuditWithIgnoreUnreachable(t *testing.T) {
	packages := []pkg.PackageInterface{pkg.NewPackage("vendor1/package1", "3.0.0.0", "3.0.0")}

	errorMessage := `The "https://example.org/packages.json" file could not be downloaded: HTTP/1.1 404 Not Found`
	repoSet := unreachableRepoSet{errorMessage}

	// Test without ignoreUnreachable flag
	_, err := advisory.Auditor{}.Audit(newBufferIO(t), repoSet, createPolicyConfig(policyOptions{}), packages, advisory.FormatPlain, false, nil)
	var transport *util.TransportError
	if !errors.As(err, &transport) || !strings.Contains(transport.Message, "HTTP/1.1 404 Not Found") {
		t.Fatalf("Expected TransportException was not thrown: %v", err)
	}

	// Test with ignoreUnreachable flag
	b := newBufferIO(t)
	result, err := advisory.Auditor{}.Audit(b, repoSet, createPolicyConfig(policyOptions{ignoreUnreachableAudit: true}), packages, advisory.FormatPlain, false, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should find advisories from the reachable repositories
	if result != advisory.StatusFailed {
		t.Errorf("result = %d", result)
	}

	output := b.Output()
	for _, want := range []string{
		"The following repositories were unreachable:",
		"HTTP/1.1 404 Not Found",
		// Verify that advisories from reachable repositories were found
		"First repo advisory",
		"Third repo advisory",
		"CVE-2023-12345",
		"CVE-2023-67890",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}

	// Test with JSON format
	b = newBufferIO(t)
	result, err = advisory.Auditor{}.Audit(b, repoSet, createPolicyConfig(policyOptions{ignoreUnreachableAudit: true}), packages, advisory.FormatJSON, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != advisory.StatusFailed {
		t.Errorf("result = %d", result)
	}

	decoded, err := php.JSONDecode(b.Output(), true)
	if err != nil {
		t.Fatal(err)
	}
	data := decoded.(*php.Array)
	unreachable, ok := data.GetArray("unreachable-repositories")
	if !ok || unreachable.Len() != 1 || !strings.Contains(php.ToString(unreachable.Values()[0]), "HTTP/1.1 404 Not Found") {
		t.Errorf("unreachable-repositories = %v", unreachable)
	}

	// Verify that advisories from reachable repositories were included in JSON output
	advisories, _ := data.GetArray("advisories")
	list, ok := advisories.GetArray("vendor1/package1")
	if !ok || list.Len() != 2 {
		t.Fatalf("advisories = %v", advisories)
	}
	for i, want := range [][2]string{{"CVE-2023-12345", "First repo advisory"}, {"CVE-2023-67890", "Third repo advisory"}} {
		a := list.Values()[i].(*php.Array)
		if cve, _ := a.GetString("cve"); cve != want[0] {
			t.Errorf("#%d cve = %q", i, cve)
		}
		if title, _ := a.GetString("title"); title != want[1] {
			t.Errorf("#%d title = %q", i, title)
		}
	}
}

// providerSetMock is the FilterListProviderSet mock: getMatchingFilterLists
// returns the given entries.
type providerSetMock struct {
	filter *filterlist.Filter
	calls  int
}

func (m *providerSetMock) GetMatchingFilterLists([]pkg.PackageInterface, []string, bool) (filterlist.MatchingFilterLists, error) {
	m.calls++

	return filterlist.MatchingFilterLists{Filter: m.filter}, nil
}

func newEntry(packageName string, constraint semver.ConstraintInterface, listName string, url, reason, id, source pkg.NullString) *filterlist.FilterListEntry {
	return &filterlist.FilterListEntry{PackageName: packageName, Constraint: constraint, ListName: listName, URL: url, Reason: reason, ID: id, Source: source}
}

func ge(version string) semver.ConstraintInterface {
	return semver.NewConstraintOp(semver.OpGE, version)
}

func filterOf(listName string, entries ...*filterlist.FilterListEntry) *filterlist.Filter {
	f := &filterlist.Filter{}
	f.Set(listName, entries)

	return f
}

func TestAuditor_AuditWithFilter(t *testing.T) {
	null := pkg.NullString{}
	matchingEntry := newEntry("vendor/package", ge("8.0.0.0"), "test-list", null, pkg.Str("internal"), pkg.Str("ID-test-1"), null)
	matchingEntryWithDetails := newEntry("vendor/package", ge("8.0.0.0"), "test-list", pkg.Str("https://example.com/filtered"), pkg.Str("internal"), pkg.Str("ID-test-1"), null)
	nonMatchingEntry := newEntry("vendor/package", ge("10.0.0.0"), "test-list", null, pkg.Str("internal"), pkg.Str("ID-test-1"), null)
	matchingEntryWithSource := newEntry("vendor/package", ge("8.0.0.0"), "test-list", pkg.Str("https://example.com/filtered"), pkg.Str("internal"), pkg.Str("ID-test-1"), pkg.Str("aikido"))

	single := func() []pkg.PackageInterface {
		return []pkg.PackageInterface{pkg.NewPackage("vendor/package", "9.0.0", "9.0.0")}
	}

	cases := []struct {
		name     string
		packages []pkg.PackageInterface
		filter   *filterlist.Filter
		filtered string
		format   string
		expected int
		output   string
	}{
		{
			"AUDIT_IGNORE skips filter processing", single(), filterOf("test-list", matchingEntry), policy.AuditIgnore, advisory.FormatPlain, advisory.StatusOK,
			"No security vulnerability advisories found.",
		},
		{
			"AUDIT_FAIL with no matching entry returns STATUS_OK", single(), filterOf("test-list", nonMatchingEntry), policy.AuditFail, advisory.FormatPlain, advisory.StatusOK,
			"No security vulnerability advisories found.",
		},
		{
			"AUDIT_FAIL with matching entry returns STATUS_FAILED (plain)", single(), filterOf("test-list", matchingEntry), policy.AuditFail, advisory.FormatPlain, advisory.StatusFailed,
			`No security vulnerability advisories found.
Found 1 package matching filters:
vendor/package matched dependency policy "test-list". Reason: internal.`,
		},
		{
			"AUDIT_FAIL with matching entry shows url and reason (plain)", single(), filterOf("test-list", matchingEntryWithDetails), policy.AuditFail, advisory.FormatPlain, advisory.StatusFailed,
			`No security vulnerability advisories found.
Found 1 package matching filters:
vendor/package matched dependency policy "test-list". Reason: internal. URL: https://example.com/filtered.`,
		},
		{
			"AUDIT_FAIL with matching entry shows source (plain)", single(), filterOf("test-list", matchingEntryWithSource), policy.AuditFail, advisory.FormatPlain, advisory.StatusFailed,
			`No security vulnerability advisories found.
Found 1 package matching filters:
vendor/package matched dependency policy "test-list". Reason: internal. URL: https://example.com/filtered. Source: aikido.`,
		},
		{
			"AUDIT_REPORT with matching entry returns STATUS_OK", single(), filterOf("test-list", matchingEntry), policy.AuditReport, advisory.FormatPlain, advisory.StatusOK,
			`No security vulnerability advisories found.
<warning>Found 1 package matching filters:</warning>
vendor/package matched dependency policy "test-list". Reason: internal.`,
		},
		{
			"AUDIT_FAIL with matching entry shows summary line only (summary format)", single(), filterOf("test-list", matchingEntry), policy.AuditFail, advisory.FormatSummary, advisory.StatusFailed,
			`No security vulnerability advisories found.
Found 1 package matching filters.`,
		},
		{
			"AUDIT_FAIL with multiple matching packages shows plural form",
			[]pkg.PackageInterface{
				pkg.NewPackage("vendor/package", "9.0.0.0", "9.0.0"),
				pkg.NewPackage("vendor/other", "1.0.0.0", "10.0.0"),
			},
			filterOf("test-list", matchingEntry, newEntry("vendor/other", ge("1.0.0.0"), "test-list", null, pkg.Str("internal"), pkg.Str("ID-TEST"), null)),
			policy.AuditFail, advisory.FormatPlain, advisory.StatusFailed,
			`No security vulnerability advisories found.
Found 2 packages matching filters:
vendor/package matched dependency policy "test-list". Reason: internal.
vendor/other matched dependency policy "test-list". Reason: internal.`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policyConfig := createPolicyConfig(policyOptions{filteredAudit: tc.filtered})
			b := newBufferIO(t)
			result, err := advisory.Auditor{}.Audit(b, getRepoSet(t), policyConfig, tc.packages, tc.format, true, &providerSetMock{filter: tc.filter})
			if err != nil {
				t.Fatal(err)
			}
			if result != tc.expected {
				t.Errorf("result = %d, want %d", result, tc.expected)
			}
			if got := normalizedOutput(b); got != tc.output {
				t.Errorf("output:\n%s\nwant:\n%s", got, tc.output)
			}
		})
	}
}

func TestAuditor_AuditWithFilterJson(t *testing.T) {
	matchingEntry := newEntry("vendor/package", ge("8.0.0.0"), "test-list", pkg.Str("https://example.com/filtered"), pkg.Str("Some reason"), pkg.Str("ID-test"), pkg.NullString{})
	providerSet := &providerSetMock{filter: filterOf("test-list", matchingEntry)}

	policyConfig := createPolicyConfig(policyOptions{filteredAudit: policy.AuditFail})
	b := newBufferIO(t)
	result, err := advisory.Auditor{}.Audit(b, getRepoSet(t), policyConfig, []pkg.PackageInterface{pkg.NewPackage("vendor/package", "9.0.0", "9.0.0")}, advisory.FormatJSON, true, providerSet)
	if err != nil {
		t.Fatal(err)
	}
	if providerSet.calls != 1 {
		t.Errorf("getMatchingFilterLists called %d times", providerSet.calls)
	}
	if result != advisory.StatusFailed {
		t.Errorf("result = %d", result)
	}

	decoded, err := php.JSONDecode(b.Output(), true)
	if err != nil {
		t.Fatal(err)
	}
	filter, ok := decoded.(*php.Array).GetArray("filter")
	if !ok {
		t.Fatal("no filter key")
	}
	entries, ok := filter.GetArray("vendor/package")
	if !ok || entries.Len() != 1 {
		t.Fatalf("filter = %v", filter)
	}
	entry := entries.Values()[0].(*php.Array)
	for key, want := range map[string]string{"packageName": "vendor/package", "listName": "test-list", "url": "https://example.com/filtered", "reason": "Some reason"} {
		if got, _ := entry.GetString(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if _, ok := entry.GetString("constraint"); !ok {
		t.Error("constraint is not a string")
	}
}

func TestAuditor_AuditWithFilterAndVulnerabilities(t *testing.T) {
	matchingEntry := newEntry("vendor1/package2", ge("8.0.0.0"), "test-list", pkg.NullString{}, pkg.Str("internal"), pkg.Str("ID-test"), pkg.NullString{})
	providerSet := &providerSetMock{filter: filterOf("test-list", matchingEntry)}

	policyConfig := createPolicyConfig(policyOptions{filteredAudit: policy.AuditFail})
	b := newBufferIO(t)
	// vendor1/package1 at 8.2.1 is vulnerable; vendor1/package2 at 9.0.0 matches the filter
	result, err := advisory.Auditor{}.Audit(b, getRepoSet(t), policyConfig, []pkg.PackageInterface{
		pkg.NewPackage("vendor1/package1", "8.2.1", "8.2.1"),
		pkg.NewPackage("vendor1/package2", "9.0.0", "9.0.0"),
	}, advisory.FormatPlain, false, providerSet)
	if err != nil {
		t.Fatal(err)
	}
	if providerSet.calls != 1 {
		t.Errorf("getMatchingFilterLists called %d times", providerSet.calls)
	}
	if result != advisory.StatusFailed {
		t.Errorf("result = %d", result)
	}
	output := normalizedOutput(b)
	for _, want := range []string{
		"Found 2 security vulnerability advisories affecting 1 package:",
		"Found 1 package matching filters:",
		`vendor1/package2 matched dependency policy "test-list". Reason: internal`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}
}

func TestAuditor_NeedsCompleteAdvisoryLoad(t *testing.T) {
	full := func(id string) advisory.Advisory {
		return newSecurityAdvisory("foo/bar", id, semver.NewConstraintOp(semver.OpEQ, "1.0.0.0"), "test",
			php.ListOf(php.ArrayOf("name", "foo", "remoteId", "remoteID")), time.Now(), pkg.NullString{}, pkg.NullString{}, pkg.NullString{})
	}
	partial := func(id string) advisory.Advisory {
		return &advisory.PartialSecurityAdvisory{AdvisoryID: id, PackageName: "foo/bar", AffectedVersions: semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")}
	}
	advisoriesOf := func(kv ...any) *advisory.Advisories {
		m := &advisory.Advisories{}
		for i := 0; i < len(kv); i += 2 {
			m.Set(kv[i].(string), kv[i+1].([]advisory.Advisory))
		}

		return m
	}
	ignoreListOf := func(rs ...reason) *policy.Reasons {
		m := &policy.Reasons{}
		for _, r := range rs {
			m.Set(r.key, r.reason)
		}

		return m
	}

	cases := []struct {
		name       string
		advisories *advisory.Advisories
		ignoreList *policy.Reasons
		expected   bool
	}{
		{"no filter or advisories", advisoriesOf(), ignoreListOf(), false},
		{"packagist filters are IDs so work fine with partial advisories", advisoriesOf(), ignoreListOf(r("PKSA-foo-bar")), false},
		{
			"packagist filters are IDs so work fine with partial advisories/2",
			advisoriesOf("vendor1/package1", []advisory.Advisory{full("123"), partial("1234")}),
			ignoreListOf(rr("PKSA-foo-bar", "this is fine \U0001F525")),
			false,
		},
		{"no advisories no need to load any further", advisoriesOf(), ignoreListOf(r("CVE-2025-1234")), false},
		{"no advisories no need to load any further/2", advisoriesOf("vendor1/package1", []advisory.Advisory{}), ignoreListOf(r("CVE-2025-1234")), false},
		{
			"CVE filter or other non-packagist ones might need to fully load for safety if partial advisories are present",
			advisoriesOf("vendor1/package1", []advisory.Advisory{full("123"), partial("1234")}),
			ignoreListOf(r("CVE-2025-1234")),
			true,
		},
		{
			"filter does not trigger load if all advisories are fully loaded",
			advisoriesOf("vendor1/package1", []advisory.Advisory{full("123")}, "vendor1/package2", []advisory.Advisory{full("1234")}),
			ignoreListOf(r("CVE-2025-1234")),
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (advisory.Auditor{}).NeedsCompleteAdvisoryLoad(tc.advisories, tc.ignoreList); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

package advisory_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// escapesRe matches an SGR sequence or an OSC 8 hyperlink's start or end.
var escapesRe = regexp.MustCompile("\x1b\\[[0-9;]*m|\x1b\\]8;[^\x1b\a]*(?:\x1b\\\\|\a)")

// The table format, decorated, is the grouped report, which shows every
// field of every advisory (ignored ones with their reason); undecorated it
// is Composer's tables. The exit code does not depend on it.
func TestAuditor_TableFormatDecorated(t *testing.T) {
	packages := []pkg.PackageInterface{
		pkg.NewPackage("vendor1/package1", "2.0.0.0", "2.0.0"),
		pkg.NewPackage("vendor1/package2", "2.0.0.0", "2.0.0"),
		pkg.NewPackage("vendor2/package1", "3.0.0.0", "3.0.0"),
		pkg.NewPackage("vendorx/packagex", "3.0.0.0", "3.0.0"),
	}
	policyConfig := createPolicyConfig(policyOptions{ignoreAdvisories: []reason{rr("ID6", "not reachable")}})
	audit := func(decorated bool) (int, string) {
		b, err := io.NewBufferIO("", 0, console.NewOutputFormatter(decorated))
		if err != nil {
			t.Fatal(err)
		}
		result, err := advisory.Auditor{}.Audit(b, getRepoSet(t), policyConfig, packages, advisory.FormatTable, false, nil)
		if err != nil {
			t.Fatal(err)
		}

		return result, b.Output()
	}
	plainResult, plain := audit(false)
	decoratedResult, decorated := audit(true)
	if plainResult != decoratedResult || plainResult != advisory.StatusFailed {
		t.Errorf("results %d undecorated, %d decorated, want %d", plainResult, decoratedResult, advisory.StatusFailed)
	}
	if strings.Contains(plain, "\x1b") || !strings.Contains(plain, "| Severity          | medium") {
		t.Errorf("undecorated output is not Composer's tables:\n%s", plain)
	}
	if strings.Contains(decorated, "| Severity") {
		t.Errorf("decorated output has tables:\n%s", decorated)
	}

	shown := php.NormalizeEOL(escapesRe.ReplaceAllString(decorated, ""))
	want := []string{"vendor1/package1 2.0.0", "vendor2/package1 3.0.0", "ignored: not reachable", "Found 1 ignored security vulnerability advisory"}
	for name, list := range getMockAdvisories(t).All() {
		if !auditedPackage(packages, name.String()) {
			continue
		}
		for _, v := range list.(*php.Array).All() {
			a := v.(*php.Array)
			field := func(k string) string { s, _ := a.Get(k); return php.ToString(s) }
			cve := field("cve")
			if v, _ := a.Get("cve"); v == nil {
				cve = "NO CVE"
			}
			reportedAt := strings.Replace(field("reportedAt"), " ", "T", 1) + "+00:00"
			want = append(want, field("packageName"), strings.ToUpper(field("severity")), field("advisoryId"), cve,
				field("title"), field("link"), field("affectedVersions"), reportedAt)
		}
	}
	for _, w := range want {
		if !strings.Contains(shown, w) {
			t.Errorf("decorated output does not show %q:\n%s", w, shown)
		}
	}
	// the most severe advisory of a package comes first
	if high, medium := strings.Index(shown, "advisory4"), strings.Index(shown, "advisory1\n"); high < 0 || medium < 0 || high > medium {
		t.Errorf("high advisory4 after medium advisory1:\n%s", shown)
	}
}

func auditedPackage(packages []pkg.PackageInterface, name string) bool {
	for _, p := range packages {
		if p.Name() == name {
			return true
		}
	}

	return false
}

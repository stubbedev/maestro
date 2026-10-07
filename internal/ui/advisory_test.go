package ui

import (
	"slices"
	"strings"
	"testing"
)

func TestAdvisoryReportMarkup(t *testing.T) {
	r := AdvisoryReport{
		Width:  40,
		Escape: func(s string) string { return strings.ReplaceAll(s, "<", `\<`) },
		Packages: []AdvisoryPackage{{
			Name:    "a/b",
			Version: "1.0.0",
			Advisories: []Advisory{
				{Severity: "low", ID: "ID1", CVE: "NO CVE", Title: "short", AffectedVersions: "<2", ReportedAt: "then"},
				{Severity: "", ID: "ID2", Title: "unknown severity", AffectedVersions: "<2", ReportedAt: "then"},
				{
					Severity: "critical", ID: "PKSA-1", IDLink: "https://p/1", CVE: "CVE-1", CVELink: "https://c/1",
					Title: "a title long enough to wrap onto a second line", URL: "https://u/1",
					AffectedVersions: "<2", ReportedAt: "then", Ignored: true, IgnoreReason: "why",
				},
			},
		}},
	}
	sep := " " + RoleMuted.Wrap(GlyphSeparator.String()) + " "
	want := []string{
		"",
		RolePackage.Wrap("a/b") + " " + RoleVersion.Wrap("1.0.0") + sep + RoleMuted.Wrap("3 advisories"),
		"  " + RoleDanger.Wrap("CRITICAL") + "  " + RoleEmphasis.Wrap("a title long enough to wrap"),
		"            " + RoleEmphasis.Wrap("onto a second line"),
		"            <href=https://c/1>CVE-1</>" + sep + "<href=https://p/1>PKSA-1</>",
		"            " + RoleMuted.Wrap("affected") + ` \<2` + sep + RoleMuted.Wrap("reported") + " then",
		"            " + RoleLink.Wrap("<href=https://u/1>https://u/1</>"),
		"            " + RoleMuted.Wrap("ignored: why"),
		"  " + RoleMuted.Wrap("LOW") + "       " + RoleEmphasis.Wrap("short"),
		"            NO CVE" + sep + "ID1",
		"            " + RoleMuted.Wrap("affected") + ` \<2` + sep + RoleMuted.Wrap("reported") + " then",
		"  " + RoleEmphasis.Wrap("UNKNOWN") + "   " + RoleEmphasis.Wrap("unknown severity"),
		"            ID2",
		"            " + RoleMuted.Wrap("affected") + ` \<2` + sep + RoleMuted.Wrap("reported") + " then",
	}
	if got := r.Markup(); !slices.Equal(got, want) {
		t.Errorf("Markup() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

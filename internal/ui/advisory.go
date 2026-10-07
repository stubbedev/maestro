package ui

import (
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/stubbedev/maestro/internal/php"
)

// Advisory is a security advisory as an audit report shows it, every
// field plain text.
type Advisory struct {
	Severity string
	// ID is the advisory's ID, IDLink its page ("" for none).
	ID, IDLink string
	// CVE is the CVE's ID as Composer shows it ("NO CVE" for none; an
	// empty one is left out), CVELink its page.
	CVE, CVELink     string
	Title            string
	URL              string
	AffectedVersions string
	ReportedAt       string
	// Ignored is whether the advisory is ignored, IgnoreReason why.
	Ignored      bool
	IgnoreReason string
}

// AdvisoryPackage is a package and its advisories.
type AdvisoryPackage struct {
	Name string
	// Version is the audited version ("" when unknown).
	Version    string
	Advisories []Advisory
}

// AdvisoryReport is the decorated look of audit's table format: per
// package, its advisories from the most to the least severe, each with a
// severity badge, its title wrapped to the terminal and its other fields
// beneath it, links as links. Undecorated, audit keeps Composer's tables.
type AdvisoryReport struct {
	Packages []AdvisoryPackage
	// Width is the terminal's width.
	Width int
	// Escape is the formatter's escaping of plain text (console.Escape).
	Escape func(string) string
}

// severities are the known severities, the most severe first, and the
// role of each one's badge.
var severities = [...]struct {
	name string
	role Role
}{
	{"critical", RoleDanger},
	{"high", RoleDanger},
	{"medium", RoleNotice},
	{"low", RoleMuted},
}

// severityRank is the position of a severity in severities; an unknown
// one ranks last.
func severityRank(severity string) int {
	for i, s := range severities {
		if php.Strcasecmp(s.name, severity) == 0 {
			return i
		}
	}

	return len(severities)
}

// badgeRole is the role of a severity's badge.
func badgeRole(severity string) Role {
	if r := severityRank(severity); r < len(severities) {
		return severities[r].role
	}

	return RoleEmphasis
}

// Markup is the report as lines of formatter markup.
func (r AdvisoryReport) Markup() []string {
	badgeWidth := len("UNKNOWN")
	for _, p := range r.Packages {
		for _, a := range p.Advisories {
			badgeWidth = max(badgeWidth, lipgloss.Width(a.Severity))
		}
	}
	indent := strings.Repeat(" ", 2+badgeWidth+2)
	sep := " " + RoleMuted.Wrap(GlyphSeparator.String()) + " "
	link := func(url, text string) string {
		if url == "" {
			return r.Escape(text)
		}

		return "<href=" + r.Escape(url) + ">" + r.Escape(text) + "</>"
	}

	var lines []string
	for _, p := range r.Packages {
		head := RolePackage.Wrap(r.Escape(p.Name))
		if p.Version != "" {
			head += " " + RoleVersion.Wrap(r.Escape(p.Version))
		}
		count := strconv.Itoa(len(p.Advisories)) + " advisories"
		if len(p.Advisories) == 1 {
			count = "1 advisory"
		}
		lines = append(lines, "", head+sep+RoleMuted.Wrap(count))

		advisories := slices.Clone(p.Advisories)
		slices.SortStableFunc(advisories, func(a, b Advisory) int { return severityRank(a.Severity) - severityRank(b.Severity) })
		for _, a := range advisories {
			severity := a.Severity
			if severity == "" {
				severity = "unknown"
			}
			badge := php.Strtoupper(severity)
			badge = badgeRole(a.Severity).Wrap(r.Escape(badge)) + strings.Repeat(" ", badgeWidth-lipgloss.Width(badge))
			for i, l := range wrapWords(a.Title, r.Width-len(indent)) {
				if i == 0 {
					lines = append(lines, "  "+badge+"  "+RoleEmphasis.Wrap(r.Escape(l)))
				} else {
					lines = append(lines, indent+RoleEmphasis.Wrap(r.Escape(l)))
				}
			}
			ids := link(a.IDLink, a.ID)
			if a.CVE != "" {
				ids = link(a.CVELink, a.CVE) + sep + ids
			}
			lines = append(lines,
				indent+ids,
				indent+RoleMuted.Wrap("affected")+" "+r.Escape(a.AffectedVersions)+sep+RoleMuted.Wrap("reported")+" "+r.Escape(a.ReportedAt),
			)
			if a.URL != "" {
				lines = append(lines, indent+RoleLink.Wrap(link(a.URL, a.URL)))
			}
			if a.Ignored {
				lines = append(lines, indent+RoleMuted.Wrap("ignored: "+r.Escape(a.IgnoreReason)))
			}
		}
	}

	return lines
}

// wrapWords breaks text into lines of at most width columns at spaces; a
// word longer than width gets a line of its own, unbroken.
func wrapWords(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if lipgloss.Width(line)+1+lipgloss.Width(w) > width {
			lines = append(lines, line)
			line = w

			continue
		}
		line += " " + w
	}

	return append(lines, line)
}

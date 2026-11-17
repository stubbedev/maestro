package ui

import (
	"slices"
	"strings"
	"testing"
)

func TestProblemReportMarkup(t *testing.T) {
	r := ProblemReport{
		Problems: []Problem{
			{Statements: []Statement{
				{Kind: StatementRequire, Target: "a/app", Constraint: "^1.0", Outcome: "a/app[1.0.0]", Satisfiable: true},
				{Kind: StatementConflict, Package: "x/conflicter", Versions: []string{"2.0.0"}, Target: "c/lib 1.5.0."},
				{
					Kind: StatementRequire, Package: "a/app", Versions: []string{"1.0.0", "1.1.0"}, Target: "c/lib", Constraint: "^2.0",
					Outcome: "found c/lib[1.5.0] but it does not match the constraint.\n\n      Alternatively:\n      - p/provider",
				},
				{Kind: StatementRequire, Package: "c/lib", Versions: []string{"1.5.0"}, Target: "ext-foo", Constraint: "*", Outcome: "it is missing from your system."},
				{Kind: StatementFixed, Package: "l/locked", Outcome: "is locked to version 1.0.0 and an update of this package was not requested."},
			}},
			{Statements: []Statement{{Kind: StatementNote, Text: "Conclusion: don't install c/lib 1.5.0 (conflict analysis result)"}}},
		},
		Hints: []string{"", "Potential causes:\n - A typo"},
	}

	last, blank := treeGlyph(true)
	failed := RoleDanger.Wrap(GlyphCheckFailed.String())
	requires := " " + RoleMuted.Wrap("requires") + " "
	want := []string{
		"",
		"  " + RoleEmphasis.Wrap("Problem 1"),
		"    " + RoleEmphasis.Wrap("root composer.json") + requires + RolePackage.Wrap("a/app") + " " + RoleVersion.Wrap("^1.0"),
		"      " + RoleMuted.Wrap("satisfiable by") + " a/app[1.0.0]",
		"    " + RoleMuted.Wrap(last) + RolePackage.Wrap("a/app") + " " + RoleVersion.Wrap("[1.0.0, 1.1.0]") + requires + RolePackage.Wrap("c/lib") + " " + RoleVersion.Wrap("^2.0"),
		"    " + blank + "  " + failed + " found c/lib[1.5.0] but it does not match the constraint.",
		"",
		"    " + blank + "    Alternatively:",
		"    " + blank + "    - p/provider",
		"    " + blank + RoleMuted.Wrap(last) + RolePackage.Wrap("c/lib") + " " + RoleVersion.Wrap("1.5.0") + requires + RolePackage.Wrap("ext-foo") + " " + RoleVersion.Wrap("*"),
		"    " + blank + blank + "  " + failed + " it is missing from your system.",
		"    " + RolePackage.Wrap("x/conflicter") + " " + RoleVersion.Wrap("2.0.0") + " " + RoleDanger.Wrap("conflicts with") + " c/lib 1.5.0.",
		"    " + RolePackage.Wrap("l/locked") + " " + RoleNotice.Wrap("is locked to version 1.0.0 and an update of this package was not requested."),
		"",
		"  " + RoleEmphasis.Wrap("Problem 2"),
		"    Conclusion: don't install c/lib 1.5.0 (conflict analysis result)",
		"",
		"  " + RoleSuccess.Wrap("Hint:") + " Potential causes:",
		"         - A typo",
	}
	if got := r.Markup(); !slices.Equal(got, want) {
		t.Errorf("Markup() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestProblemTreeNestsBranches(t *testing.T) {
	branch, pipe := treeGlyph(false)
	last, _ := treeGlyph(true)
	p := Problem{Statements: []Statement{
		{Kind: StatementRequire, Target: "a/a", Outcome: "a/a[1.0]", Satisfiable: true},
		{Kind: StatementRequire, Target: "b/b", Outcome: "b/b[1.0]", Satisfiable: true},
		{Kind: StatementRequire, Package: "a/a", Versions: []string{"1.0"}, Target: "c/c", Outcome: "c/c[1.0]", Satisfiable: true},
		{Kind: StatementRequire, Package: "a/a", Versions: []string{"1.0"}, Target: "d/d", Outcome: "no"},
	}}
	var heads []string
	for _, l := range p.markup() {
		if !strings.Contains(l, RoleMuted.Wrap("satisfiable by")) && !strings.Contains(l, GlyphCheckFailed.String()) {
			heads = append(heads, l)
		}
	}
	want := []string{
		"    " + RoleEmphasis.Wrap("root composer.json") + " " + RoleMuted.Wrap("requires") + " " + RolePackage.Wrap("a/a"),
		"    " + RoleMuted.Wrap(branch) + RolePackage.Wrap("a/a") + " " + RoleVersion.Wrap("1.0") + " " + RoleMuted.Wrap("requires") + " " + RolePackage.Wrap("c/c"),
		"    " + RoleMuted.Wrap(last) + RolePackage.Wrap("a/a") + " " + RoleVersion.Wrap("1.0") + " " + RoleMuted.Wrap("requires") + " " + RolePackage.Wrap("d/d"),
		"    " + RoleEmphasis.Wrap("root composer.json") + " " + RoleMuted.Wrap("requires") + " " + RolePackage.Wrap("b/b"),
	}
	if !slices.Equal(heads, want) {
		t.Errorf("heads =\n%s\nwant\n%s", strings.Join(heads, "\n"), strings.Join(want, "\n"))
	}
	if details := p.markup()[3]; !strings.HasPrefix(details, "    "+RoleMuted.Wrap(pipe)+"  ") {
		t.Errorf("a non-last branch's details %q do not continue its line", details)
	}
}

func TestProblemTreeBreaksCycles(t *testing.T) {
	p := Problem{Statements: []Statement{
		{Kind: StatementRequire, Package: "a/a", Versions: []string{"1.0"}, Target: "b/b", Outcome: "x"},
		{Kind: StatementRequire, Package: "b/b", Versions: []string{"1.0"}, Target: "a/a", Outcome: "y"},
	}}
	roots := p.tree()
	count := 0
	var walk func([]*treeNode)
	walk = func(nodes []*treeNode) {
		for _, n := range nodes {
			count++
			walk(n.children)
		}
	}
	walk(roots)
	if len(roots) == 0 || count != len(p.Statements) {
		t.Errorf("%d roots, %d statements shown of %d", len(roots), count, len(p.Statements))
	}
}

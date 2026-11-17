package ui

import (
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

type StatementKind uint8

const (
	StatementRequire StatementKind = iota
	StatementConflict
	StatementFixed
	StatementNote
)

type Statement struct {
	Kind        StatementKind
	Package     string
	Versions    []string
	Target      string
	Constraint  string
	Outcome     string
	Satisfiable bool
	Text        string
}

type Problem struct {
	Statements []Statement
}

type ProblemReport struct {
	Problems []Problem
	Hints    []string
}

const (
	treeBranch   = "├─ "
	treeLast     = "└─ "
	treePipe     = "│  "
	treeBlank    = "   "
	asciiBranch  = "|- "
	asciiLast    = "`- "
	asciiPipe    = "|  "
	problemsPad  = "    "
	hintLabel    = "Hint:"
	rootRequirer = "root composer.json"
)

func (r ProblemReport) Markup() []string {
	var lines []string
	for i, p := range r.Problems {
		lines = append(lines, "", "  "+RoleEmphasis.Wrap("Problem "+strconv.Itoa(i+1)))
		lines = append(lines, p.markup()...)
	}
	for _, h := range r.Hints {
		if h == "" {
			continue
		}
		lines = append(lines, "")
		pad := "  " + strings.Repeat(" ", len(hintLabel)+1)
		for i, l := range SplitLines(h) {
			switch {
			case i == 0:
				lines = append(lines, "  "+RoleSuccess.Wrap(hintLabel)+" "+l)
			case l == "":
				lines = append(lines, "")
			default:
				lines = append(lines, pad+l)
			}
		}
	}

	return lines
}

type treeNode struct {
	statement Statement
	children  []*treeNode
}

func (p Problem) tree() []*treeNode {
	nodes := make([]*treeNode, len(p.Statements))
	for i, s := range p.Statements {
		nodes[i] = &treeNode{statement: s}
	}
	parent := make([]int, len(nodes))
	for i, s := range p.Statements {
		parent[i] = -1
		if s.Package == "" {
			continue
		}
		for j, t := range p.Statements {
			if j != i && t.Kind == StatementRequire && php.Strcasecmp(t.Target, s.Package) == 0 {
				parent[i] = j

				break
			}
		}
	}
	for i := range nodes {
		seen := map[int]bool{i: true}
		for j := parent[i]; j >= 0; j = parent[j] {
			if seen[j] {
				parent[i] = -1

				break
			}
			seen[j] = true
		}
	}

	var roots []*treeNode
	for i, n := range nodes {
		if parent[i] < 0 {
			roots = append(roots, n)

			continue
		}
		nodes[parent[i]].children = append(nodes[parent[i]].children, n)
	}

	return roots
}

func (p Problem) markup() []string {
	var lines []string
	var walk func(nodes []*treeNode, prefix string, nested bool)
	walk = func(nodes []*treeNode, prefix string, nested bool) {
		for i, n := range nodes {
			branch, rest := "", ""
			if nested {
				branch, rest = treeGlyph(i == len(nodes)-1)
			}
			head, details := n.statement.markup()
			lines = append(lines, problemsPad+prefix+muted(branch)+head)
			for _, d := range details {
				lines = append(lines, strings.TrimRight(problemsPad+prefix+muted(rest)+"  "+d, " "))
			}
			walk(n.children, prefix+muted(rest), true)
		}
	}
	walk(p.tree(), "", false)

	return lines
}

func muted(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}

	return RoleMuted.Wrap(text)
}

func treeGlyph(last bool) (branch, rest string) {
	unicode := unicodeConsole()
	switch {
	case last && unicode:
		return treeLast, treeBlank
	case last:
		return asciiLast, treeBlank
	case unicode:
		return treeBranch, treePipe
	default:
		return asciiBranch, asciiPipe
	}
}

func (s Statement) requirer() string {
	if s.Package == "" {
		return RoleEmphasis.Wrap(rootRequirer)
	}
	text := RolePackage.Wrap(s.Package)
	switch len(s.Versions) {
	case 0:
		return text
	case 1:
		return text + " " + RoleVersion.Wrap(s.Versions[0])
	default:
		return text + " " + RoleVersion.Wrap("["+strings.Join(s.Versions, ", ")+"]")
	}
}

func (s Statement) markup() (head string, details []string) {
	switch s.Kind {
	case StatementRequire:
		head = s.requirer() + " " + RoleMuted.Wrap("requires") + " " + RolePackage.Wrap(s.Target)
		if s.Constraint != "" {
			head += " " + RoleVersion.Wrap(s.Constraint)
		}
		outcome := SplitLines(s.Outcome)
		if s.Satisfiable {
			details = append(details, RoleMuted.Wrap("satisfiable by")+" "+outcome[0])
		} else {
			details = append(details, RoleDanger.Wrap(GlyphCheckFailed.String())+" "+outcome[0])
		}
		for _, l := range outcome[1:] {
			details = append(details, "  "+strings.TrimLeft(l, " "))
		}

		return head, details
	case StatementConflict:
		return s.requirer() + " " + RoleDanger.Wrap("conflicts with") + " " + s.Target, nil
	case StatementFixed:
		return s.requirer() + " " + RoleNotice.Wrap(s.Outcome), nil
	}

	lines := SplitLines(s.Text)

	return lines[0], lines[1:]
}

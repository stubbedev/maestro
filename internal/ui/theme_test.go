package ui

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// sgrRe matches an SGR sequence.
var sgrRe = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// Undecorated, every role is the text as it is; decorated, it is the text
// between SGR sequences of the 16 ANSI colours and text attributes only:
// no 256 or true colours, which do not follow the terminal's theme.
func TestRolesRender(t *testing.T) {
	names := map[string]bool{}
	for _, r := range Roles() {
		if r.String() == "" || names[r.String()] {
			t.Errorf("role %d: name %q is empty or not unique", r, r)
		}
		names[r.String()] = true

		if got := r.Render("a b", false); got != "a b" {
			t.Errorf("%s: undecorated Render = %q", r, got)
		}
		if got := r.Render("", true); got != "" {
			t.Errorf("%s: Render of no text = %q", r, got)
		}
		got := r.Render("a b", true)
		if sgrRe.ReplaceAllString(got, "") != "a b" || got == "a b" {
			t.Errorf("%s: decorated Render = %q", r, got)
		}
		for _, m := range sgrRe.FindAllStringSubmatch(got, -1) {
			for p := range strings.SplitSeq(m[1], ";") {
				if !slices.Contains([]string{"0", "1", "2", "4", "22", "24", "39", "49"}, p) &&
					!(len(p) == 2 && (p[0] == '3' || p[0] == '4') && p[1] <= '7') {
					t.Errorf("%s: %q has SGR parameter %q outside the 16 ANSI colours", r, got, p)
				}
			}
		}
	}
}

func TestComposerTags(t *testing.T) {
	var tags []string
	for _, ft := range ComposerTags {
		tags = append(tags, ft.Name)
		if ft.Role >= numRoles {
			t.Errorf("tag %q has no role", ft.Name)
		}
	}
	slices.Sort(tags)
	if want := []string{"comment", "error", "highlight", "info", "question", "warning"}; !slices.Equal(tags, want) {
		t.Errorf("ComposerTags = %q, want %q", tags, want)
	}
}

func TestInline(t *testing.T) {
	for r, want := range map[Role]string{
		RoleSuccess: "fg=green",
		RoleDanger:  "fg=red;options=bold",
		RoleLink:    "options=underscore",
		RoleBanner:  "fg=white;bg=blue",
		RoleMuted:   "",
	} {
		if got := r.Inline(); got != want {
			t.Errorf("%s.Inline() = %q, want %q", r, got, want)
		}
	}
	if got := RoleSuccess.Wrap("x"); got != "<maestro-success>x</maestro-success>" {
		t.Errorf("Wrap = %q", got)
	}
}

// rawStyleRe matches what styles text without a Role: escape sequences,
// Symfony's inline styles and style constructors, lipgloss colours.
var rawStyleRe = regexp.MustCompile(`\\x1b\[|\\033\[|\\u001b\[|\\e\[|[<"';](fg|bg|options)=|MustStyle\(|NewOutputFormatterStyle\(|lipgloss\.Color\(|termenv\.`)

// Every styled surface of maestro takes its look from a Role: no
// package but internal/ui (the palette) and internal/console (the ported
// formatter engine and Symfony's own styles) writes colours or escape
// sequences of its own.
func TestNoRawStylesOutsideUI(t *testing.T) {
	root := filepath.Join("..", "..")
	exempt := []string{filepath.Join(root, "internal", "ui"), filepath.Join(root, "internal", "console")}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if slices.Contains(exempt, path) || strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" {
				return filepath.SkipDir
			}

			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if rawStyleRe.MatchString(line) {
				t.Errorf("%s:%d styles text without a ui.Role: %s", path, i+1, strings.TrimSpace(line))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

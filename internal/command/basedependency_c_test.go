package command_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

func depLink(source, target, constraint string) *pkg.Link {
	return pkg.NewLink(source, target, semver.NewMatchAllConstraint(), pkg.TypeRequire, pkg.Str(constraint))
}

func dependentsFixture() []repository.Dependent {
	b := pkg.NewCompletePackage("b/b", "1.0.0.0", "1.0")
	root := pkg.NewRootPackage("__root__", "1.0.0.0", pkg.DefaultPrettyVersion)
	c := pkg.NewCompletePackage("c/c", "2.0.0.0", "2.0")

	return []repository.Dependent{
		{Package: b, Link: depLink("b/b", "a/a", "^1.0"), Dependents: []repository.Dependent{
			{Package: root, Link: depLink("__root__", "b/b", "^1.0")},
		}},
		{Package: c, Link: depLink("c/c", "a/a", "*"), Cut: true},
	}
}

func TestBaseDependencyCommand_PrintTree(t *testing.T) {
	formatter := console.NewOutputFormatter(false)
	bio, err := io.NewBufferIO("", 0, formatter)
	if err != nil {
		t.Fatal(err)
	}
	c := command.NewBaseDependencyCommand("depends")
	c.SetIO(bio)
	c.InitStylesForTest(console.NewStreamOutput(&bytes.Buffer{}, console.VerbosityNormal, new(false), formatter))
	if err := c.PrintTreeForTest(dependentsFixture()); err != nil {
		t.Fatal(err)
	}
	// str_replace(['└', '├', '──', '│'], ['`-', '|-', '-', '|']): "└──" becomes "`--"
	want := "|--b/b 1.0 (requires a/a ^1.0)\n" +
		"|  `--__root__ (requires b/b ^1.0)\n" +
		"`--c/c 2.0 (requires a/a *) (circular dependency aborted here)\n"
	if got := bio.Output(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestBaseDependencyCommand_PrintTable(t *testing.T) {
	c := command.NewBaseDependencyCommand("depends")
	buf := &bytes.Buffer{}
	out := console.NewStreamOutput(buf, console.VerbosityNormal, new(false), nil)
	if err := c.PrintTableForTest(out, dependentsFixture()); err != nil {
		t.Fatal(err)
	}
	// bottom-up: the root (found at the second level) comes first
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	want := []string{
		"__root__ -   requires b/b (^1.0)",
		"b/b      1.0 requires a/a (^1.0)",
		"c/c      2.0 requires a/a (*)",
	}
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

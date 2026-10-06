package installer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/semver"
)

func newReporter(t *testing.T) (*SuggestedPackagesReporter, *mio.BufferIO) {
	t.Helper()

	io := newBufferIO(t)

	return NewSuggestedPackagesReporter(io), io
}

// expectOutput is IOMock::expects($lines, true): the output is exactly
// these lines.
func expectOutput(t *testing.T, io *mio.BufferIO, lines ...string) {
	t.Helper()

	var want strings.Builder
	for _, l := range lines {
		want.WriteString(l + "\n")
	}

	if got := php.NormalizeEOL(io.Output()); got != want.String() {
		t.Errorf("output:\n%q\nwant\n%q", got, want.String())
	}
}

func TestSuggestedPackagesReporter_Constructor(t *testing.T) {
	r, io := newReporter(t)

	r.AddPackage("a", "b", "c")

	if err := r.Output(ModeList, nil, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, io, "b")
}

func TestSuggestedPackagesReporter_GetPackagesEmptyByDefault(t *testing.T) {
	r, _ := newReporter(t)

	if len(r.Packages()) != 0 {
		t.Error("not empty")
	}
}

func TestSuggestedPackagesReporter_GetPackages(t *testing.T) {
	r, _ := newReporter(t)
	r.AddPackage("a", "b", "c")

	if got := r.Packages(); !reflect.DeepEqual(got, []Suggestion{{"a", "b", "c"}}) {
		t.Errorf("Packages = %v", got)
	}
}

func TestSuggestedPackagesReporter_AddPackageAppends(t *testing.T) {
	r, _ := newReporter(t)
	r.AddPackage("a", "b", "c")
	r.AddPackage("different source", "b", "different reason")

	if got := r.Packages(); !reflect.DeepEqual(got, []Suggestion{{"a", "b", "c"}, {"different source", "b", "different reason"}}) {
		t.Errorf("Packages = %v", got)
	}
}

func TestSuggestedPackagesReporter_AddSuggestionsFromPackage(t *testing.T) {
	r, _ := newReporter(t)

	p := pkg.NewPackage("package-pretty-name", "1.0.0.0", "1.0.0")
	p.SetSuggests(php.ArrayOf("target-a", "reason-a", "target-b", "reason-b"))

	r.AddSuggestionsFromPackage(p)

	want := []Suggestion{
		{"package-pretty-name", "target-a", "reason-a"},
		{"package-pretty-name", "target-b", "reason-b"},
	}

	if got := r.Packages(); !reflect.DeepEqual(got, want) {
		t.Errorf("Packages = %v", got)
	}
}

func TestSuggestedPackagesReporter_Output(t *testing.T) {
	r, io := newReporter(t)
	r.AddPackage("a", "b", "c")

	if err := r.Output(ModeByPackage, nil, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, io, "a suggests:", " - b: c", "")
}

func TestSuggestedPackagesReporter_OutputWithNoSuggestionReason(t *testing.T) {
	r, io := newReporter(t)
	r.AddPackage("a", "b", "")

	if err := r.Output(ModeByPackage, nil, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, io, "a suggests:", " - b", "")
}

func TestSuggestedPackagesReporter_OutputIgnoresFormatting(t *testing.T) {
	r, io := newReporter(t)
	r.AddPackage("source", "target1", "\x1b[1;37;42m Like us\r\non Facebook \x1b[0m")
	r.AddPackage("source", "target2", "<bg=green>Like us on Facebook</>")

	if err := r.Output(ModeByPackage, nil, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, io, "source suggests:", " - target1: [1;37;42m Like us on Facebook [0m", " - target2: <bg=green>Like us on Facebook</>", "")
}

func TestSuggestedPackagesReporter_OutputMultiplePackages(t *testing.T) {
	r, io := newReporter(t)
	r.AddPackage("a", "b", "c")
	r.AddPackage("source package", "target", "because reasons")

	if err := r.Output(ModeByPackage, nil, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, io, "a suggests:", " - b: c", "", "source package suggests:", " - target: because reasons", "")
}

// providing returns a package providing the given names.
func providing(name string, provides ...string) *pkg.Package {
	p := pkg.NewPackage(name, "1.0.0.0", "1.0.0")

	links := make([]*pkg.Link, len(provides))
	for i, target := range provides {
		links[i] = pkg.NewLink(name, target, semver.NewMatchAllConstraint(), pkg.TypeProvide, pkg.NullString{})
	}

	p.SetProvides(pkg.LinksOf(links...))

	return p
}

func installedRepoOf(t *testing.T, packages ...pkg.PackageInterface) *repository.InstalledRepository {
	t.Helper()

	arrayRepo, err := repository.NewInstalledArrayRepository(packages)
	if err != nil {
		t.Fatal(err)
	}

	repo, err := repository.NewInstalledRepository([]repository.RepositoryInterface{arrayRepo})
	if err != nil {
		t.Fatal(err)
	}

	return repo
}

func TestSuggestedPackagesReporter_OutputSkipInstalledPackages(t *testing.T) {
	r, io := newReporter(t)

	repo := installedRepoOf(t, providing("vendor/package1", "x", "y"), providing("vendor/package2", "b"))

	r.AddPackage("a", "b", "c")
	r.AddPackage("source package", "target", "because reasons")

	if err := r.Output(ModeByPackage, repo, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, io, "source package suggests:", " - target: because reasons", "")
}

func TestSuggestedPackagesReporter_OutputShowsSuggestionProvidedBySuggestingPackageItself(t *testing.T) {
	r, io := newReporter(t)

	repo := installedRepoOf(t, providing("acme/polyfill-foo", "ext-foo"))

	r.AddPackage("acme/polyfill-foo", "ext-foo", "install the native extension for better performance")

	if err := r.Output(ModeByPackage, repo, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, io, "acme/polyfill-foo suggests:", " - ext-foo: install the native extension for better performance", "")
}

func TestSuggestedPackagesReporter_OutputNotGettingInstalledPackagesWhenNoSuggestions(t *testing.T) {
	r, io := newReporter(t)

	// a repository whose packages cannot be listed: never asked
	repo := installedRepoOf(t)

	if err := r.Output(ModeByPackage, repo, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, io)
}

// TestSuggestedPackagesReporter_OutputModes covers the by-suggestion and
// full modes, sorting, the dependents filter and the minimalistic output.
func TestSuggestedPackagesReporter_OutputModes(t *testing.T) {
	newFilled := func() (*SuggestedPackagesReporter, *mio.BufferIO) {
		r, io := newReporter(t)
		r.AddPackage("z/z", "ext-b", "why")
		r.AddPackage("a/a", "ext-b", "0")
		r.AddPackage("a/a", "ext-a", "because")
		r.AddPackage("dep/dep", "ext-c", "transitive")

		return r, io
	}

	r, io := newFilled()
	if err := r.Output(ModeBySuggestion, nil, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, io,
		"ext-a is suggested by:", " - a/a: because", "",
		"ext-b is suggested by:", " - z/z: why", " - a/a", "",
		"ext-c is suggested by:", " - dep/dep: transitive", "")

	r, io = newFilled()
	if err := r.Output(ModeByPackage|ModeBySuggestion, nil, nil); err != nil {
		t.Fatal(err)
	}

	if got := php.NormalizeEOL(io.Output()); got[:len("a/a suggests:\n")] != "a/a suggests:\n" {
		t.Errorf("output %q", got)
	}

	root := pkg.NewRootPackage("root/root", "1.0.0.0", "1.0.0")
	root.SetRequires(pkg.LinksOf(pkg.NewLink("root/root", "a/a", semver.NewMatchAllConstraint(), pkg.TypeRequire, pkg.NullString{})))
	root.SetDevRequires(pkg.LinksOf(pkg.NewLink("root/root", "z/z", semver.NewMatchAllConstraint(), pkg.TypeDevRequire, pkg.NullString{})))

	r, io = newFilled()
	if err := r.Output(ModeByPackage, nil, root); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, io,
		"a/a suggests:", " - ext-b", " - ext-a: because", "",
		"z/z suggests:", " - ext-b: why", "",
		"1 additional suggestions by transitive dependencies can be shown with --all")

	r, io = newFilled()
	if err := r.OutputMinimalistic(nil, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, io, "4 package suggestions were added by new dependencies, use `composer suggest` to see details.")
}

func TestInstallerEvent_Getter(t *testing.T) {
	transaction := resolver.NewTransaction(nil, nil)
	event := eventdispatcher.NewInstallerEvent("EVENT_NAME", nil, newBufferIO(t), true, true, transaction)

	if event.Name() != "EVENT_NAME" || !event.IsDevMode() || !event.IsExecutingOperations() {
		t.Error("getters")
	}

	if _, ok := event.Transaction().(*resolver.Transaction); !ok {
		t.Error("transaction")
	}
}

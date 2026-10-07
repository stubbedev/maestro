package resolver

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/semver"
)

// The helpers of Composer\Test\TestCase.

var testParser = pkg.NewVersionParser()

func normalize(t testing.TB, version string) string {
	t.Helper()
	v, err := testParser.Normalize(version)
	if err != nil {
		t.Fatal(err)
	}

	return v
}

// getPackage is TestCase::getPackage with CompletePackage.
func getPackage(t testing.TB, name, version string) *pkg.CompletePackage {
	t.Helper()

	return pkg.NewCompletePackage(name, normalize(t, version), version)
}

// getAliasPackage is TestCase::getAliasPackage.
func getAliasPackage(t testing.TB, p pkg.PackageInterface, version string) pkg.Alias {
	t.Helper()
	switch p := p.(type) {
	case *pkg.RootPackage:
		return pkg.NewRootAliasPackage(p, normalize(t, version), version)
	case *pkg.CompletePackage:
		return pkg.NewCompleteAliasPackage(p, normalize(t, version), version)
	}

	return pkg.NewAliasPackage(p, normalize(t, version), version)
}

// getVersionConstraint is TestCase::getVersionConstraint.
func getVersionConstraint(t testing.TB, operator, version string) *semver.Constraint {
	t.Helper()
	c, err := semver.NewConstraint(operator, normalize(t, version))
	if err != nil {
		t.Fatal(err)
	}
	c.SetPrettyString(operator + " " + version)

	return c
}

// newLink is new Link($source, $target, $constraint, $type).
func newLink(source, target string, constraint semver.ConstraintInterface, typ string) *pkg.Link {
	return pkg.NewLink(source, target, constraint, typ, pkg.NullString{})
}

func multi(t testing.TB, conjunctive bool, constraints ...semver.ConstraintInterface) *semver.MultiConstraint {
	t.Helper()
	m, err := semver.NewMultiConstraint(constraints, conjunctive)
	if err != nil {
		t.Fatal(err)
	}

	return m
}

func matchAll(pretty string) *semver.MatchAllConstraint {
	c := semver.NewMatchAllConstraint()
	c.SetPrettyString(pretty)

	return c
}

func parseConstraints(t testing.TB, constraint string) semver.ConstraintInterface {
	t.Helper()
	c, err := testParser.ParseConstraints(constraint)
	if err != nil {
		t.Fatal(err)
	}

	return c
}

func newArrayRepository(t testing.TB, packages ...pkg.PackageInterface) *repository.ArrayRepository {
	t.Helper()
	repo, err := repository.NewArrayRepository(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range packages {
		addPackage(t, repo, p)
	}

	return repo
}

func newLockArrayRepository(t testing.TB) *repository.LockArrayRepository {
	t.Helper()
	repo, err := repository.NewLockArrayRepository(nil)
	if err != nil {
		t.Fatal(err)
	}

	return repo
}

func addPackage(t testing.TB, repo interface {
	AddPackage(pkg.PackageInterface) error
}, p pkg.PackageInterface,
) {
	t.Helper()
	if err := repo.AddPackage(p); err != nil {
		t.Fatal(err)
	}
}

func newRepositorySet(t testing.TB, minimumStability string) *repository.RepositorySet {
	t.Helper()
	set, err := repository.NewRepositorySet(minimumStability, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	return set
}

func addRepository(t testing.TB, set *repository.RepositorySet, repo repository.RepositoryInterface) {
	t.Helper()
	if err := set.AddRepository(repo); err != nil {
		t.Fatal(err)
	}
}

func requireName(t testing.TB, request *Request, name string, constraint semver.ConstraintInterface) {
	t.Helper()
	if err := request.RequireName(name, constraint); err != nil {
		t.Fatal(err)
	}
}

// loadPackage is ArrayLoader::load of the decoded JSON data.
func loadPackage(t testing.TB, arrayLoader *loader.ArrayLoader, data *php.Array) pkg.PackageInterface {
	t.Helper()
	p, err := arrayLoader.Load(data, pkg.ClassCompletePackage)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func decodeJSON(t testing.TB, data string) any {
	t.Helper()
	v, err := php.JSONDecode(data, true)
	if err != nil {
		t.Fatalf("invalid JSON %q: %v", data, err)
	}

	return v
}

// readTestFile ports the readTestFile of PoolBuilderTest and
// PoolOptimizerTest: the --SECTION-- parts of a .test fixture.
func readTestFile(t testing.TB, file string, sections map[string]bool) map[string]string {
	t.Helper()
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := php.MustCompile(`#(?:^|\n*)--([A-Z-]+)--\n#`).Split(string(content), -1, php.PregSplitDelimCapture)
	if err != nil {
		t.Fatal(err)
	}

	data := map[string]string{}
	section := ""
	for _, token := range tokens {
		if section == "" && token == "" {
			continue // skip leading blank
		}
		if section == "" {
			if _, ok := sections[token]; !ok {
				t.Fatalf(`The test file %q must not contain a section named %q.`, file, token)
			}
			section = token

			continue
		}
		data[section] = token
		section = ""
	}
	for name, required := range sections {
		if _, ok := data[name]; required && !ok {
			t.Fatalf(`The test file %q must have a section named %q.`, file, name)
		}
	}

	return data
}

func fixtureFiles(t testing.TB, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".test") {
			files = append(files, path)
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)

	return files
}

// opResult is an operation as the tests compare them: job and packages.
type opResult struct {
	job      string
	pkg      pkg.PackageInterface
	from, to pkg.PackageInterface
}

func (o opResult) String() string {
	if o.job == "update" {
		return o.job + " " + o.from.String() + " => " + o.to.String()
	}

	return o.job + " " + o.pkg.String()
}

func operationResults(t testing.TB, operations []operation.Operation, uninstallJob string) []opResult {
	t.Helper()
	var result []opResult
	for _, op := range operations {
		switch op := op.(type) {
		case *operation.UpdateOperation:
			result = append(result, opResult{job: "update", from: op.InitialPackage(), to: op.TargetPackage()})
		case *operation.MarkAliasInstalledOperation:
			result = append(result, opResult{job: string(op.OperationType()), pkg: op.Package()})
		case *operation.MarkAliasUninstalledOperation:
			result = append(result, opResult{job: string(op.OperationType()), pkg: op.Package()})
		case *operation.UninstallOperation:
			result = append(result, opResult{job: uninstallJob, pkg: op.Package()})
		case *operation.InstallOperation:
			result = append(result, opResult{job: "install", pkg: op.Package()})
		default:
			t.Fatalf("Unexpected operation: %T", op)
		}
	}

	return result
}

func assertOperations(t testing.TB, expected, result []opResult) {
	t.Helper()
	readable := func(ops []opResult) string {
		var s []string
		for _, op := range ops {
			s = append(s, op.String())
		}

		return strings.Join(s, "\n")
	}
	if readable(expected) != readable(result) {
		t.Fatalf("operations differ\nexpected:\n%s\ngot:\n%s", readable(expected), readable(result))
	}
	for i := range expected {
		if expected[i] != result[i] {
			t.Fatalf("operation %d: same string but different package objects: %s", i, expected[i])
		}
	}
}

var nullIO = io.NewNullIO()

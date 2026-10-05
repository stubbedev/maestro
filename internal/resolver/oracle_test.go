package resolver

import (
	"compress/gzip"
	"errors"
	"io"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/repository"
)

// The real-world oracle: tools/oracle/resolver/record.php recorded what
// Composer's PoolBuilder loaded from Packagist for a few projects, and
// tools/oracle/resolver/solve.php resolved them offline in several
// variants. The Go resolver must build the same pools and reach the same
// lock files, operations and problem messages.

var oracleProjects = []string{"kontainer", "laravel", "symfony-demo"}

var (
	recordingsOnce sync.Once
	recordings     map[string]*php.Array
	recordingsErr  error
)

func readGzipJSON(path string) (*php.Array, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	v, err := php.JSONDecode(string(data), true)
	if err != nil {
		return nil, err
	}
	a, ok := v.(*php.Array)
	if !ok {
		return nil, errors.New(path + ": not a JSON object")
	}

	return a, nil
}

func oracleRecordings(t testing.TB) map[string]*php.Array {
	t.Helper()
	recordingsOnce.Do(func() {
		recordings = map[string]*php.Array{}
		for _, project := range oracleProjects {
			var data *php.Array
			if data, recordingsErr = readGzipJSON("testdata/oracle/" + project + ".json.gz"); recordingsErr != nil {
				return
			}
			recordings[project] = data
		}
	})
	if recordingsErr != nil {
		t.Fatal(recordingsErr)
	}

	return recordings
}

func oracleGolden(t testing.TB) *php.Array {
	t.Helper()
	content, err := os.ReadFile("testdata/oracle/solve.json")
	if err != nil {
		t.Fatal(err)
	}

	return jsonArray(t, string(content))
}

// oracleEnv is the PHP that ran solve.php, as the problem messages see it.
type oracleEnv struct {
	extensions []string
	iniFiles   []string
}

func (e *oracleEnv) ExtensionLoaded(name string) bool {
	return slices.Contains(e.extensions, php.Strtolower(name))
}

func (e *oracleEnv) IniFiles() []string { return e.iniFiles }

func stringList(a *php.Array) []string {
	var list []string
	for _, v := range a.All() {
		list = append(list, php.ToString(v))
	}

	return list
}

// oracleData is a recording with a variant applied: solve.php's
// applyVariant.
type oracleData struct {
	root             *php.Array
	minimumStability string
	preferStable     bool
	preferLowest     bool
	stabilityFlags   *php.Array
	aliases          *php.Array
	references       *php.Array
	platform         [][2]string
	packages         *php.Array
}

func applyVariant(recording *php.Array, platform *php.Array, variant *php.Array) oracleData {
	minimumStability, _ := recording.GetString("minimumStability")
	preferStable, _ := recording.Get("preferStable")
	data := oracleData{
		root:             subArray(recording, "root").Clone(),
		minimumStability: minimumStability,
		preferStable:     php.ToBool(preferStable),
		stabilityFlags:   subArray(recording, "stabilityFlags"),
		aliases:          subArray(recording, "aliases"),
		references:       subArray(recording, "references"),
		packages:         subArray(recording, "packages"),
	}
	for _, p := range platform.All() {
		pair := stringList(p.(*php.Array))
		data.platform = append(data.platform, [2]string{pair[0], pair[1]})
	}

	for name, version := range subArray(variant, "platform").All() {
		for i := range data.platform {
			if data.platform[i][0] == name.String() {
				data.platform[i][1] = php.ToString(version)
			}
		}
	}
	if removed := stringList(subArray(variant, "removePlatform")); len(removed) > 0 {
		data.platform = slices.DeleteFunc(data.platform, func(p [2]string) bool { return slices.Contains(removed, p[0]) })
	}
	if requires := subArray(variant, "require"); requires.Len() > 0 {
		rootRequire := subArray(data.root, "require").Clone()
		for name, constraint := range requires.All() {
			rootRequire.SetKey(name, constraint)
		}
		data.root.Set("require", rootRequire)
	}
	if v, ok := variant.GetString("minimumStability"); ok {
		data.minimumStability = v
	}
	if v, ok := variant.Get("preferStable"); ok {
		data.preferStable = php.ToBool(v)
	}
	if v, ok := variant.Get("preferLowest"); ok {
		data.preferLowest = php.ToBool(v)
	}

	return data
}

// loadOracleRepository is solve.php's loadRepository.
func loadOracleRepository(t testing.TB, packages *php.Array) *repository.ArrayRepository {
	t.Helper()
	arrayLoader := loader.NewArrayLoader(nil, true)
	repo := newArrayRepository(t)
	for _, data := range packages.All() {
		addPackage(t, repo, loadPackage(t, arrayLoader, data.(*php.Array)))
	}

	return repo
}

// buildOracleRequest is common.php's buildRequest: Installer::doUpdate's
// RepositorySet and Request.
func buildOracleRequest(t testing.TB, data oracleData, repositories ...repository.RepositoryInterface) (*repository.RepositorySet, *Request) {
	t.Helper()
	var platformPackages []pkg.PackageInterface
	for _, p := range data.platform {
		platformPackages = append(platformPackages, pkg.NewCompletePackage(p[0], normalize(t, p[1]), p[1]))
	}
	platformRepo := newArrayRepository(t, platformPackages...)

	loaded, err := loader.NewArrayLoader(nil, true).Load(data.root, pkg.ClassRootPackage)
	if err != nil {
		t.Fatal(err)
	}
	root := loaded.(*pkg.RootPackage)

	var links pkg.LinksBuilder
	for _, l := range []pkg.Links{root.Requires(), root.DevRequires()} {
		for i := range l.Len() {
			links.Set(l.Key(i), l.At(i))
		}
	}
	merged := links.Build()

	rootRequires := &repository.ConstraintMap{}
	for req, link := range merged.All() {
		rootRequires.Set(req, link.Constraint())
	}

	fixedRootPackage := pkg.Clone(root).(*pkg.RootPackage)
	fixedRootPackage.SetRequires(pkg.Links{})
	fixedRootPackage.SetDevRequires(pkg.Links{})

	repositorySet, err := repository.NewRepositorySet(data.minimumStability, data.stabilityFlags, repository.RootAliasesFromArray(data.aliases), data.references, rootRequires, nil)
	if err != nil {
		t.Fatal(err)
	}
	rootRepo, err := repository.NewRootPackageRepository(fixedRootPackage)
	if err != nil {
		t.Fatal(err)
	}
	addRepository(t, repositorySet, rootRepo)
	addRepository(t, repositorySet, platformRepo)
	for _, repo := range repositories {
		addRepository(t, repositorySet, repo)
	}

	request := NewRequest(nil)
	request.FixPackage(fixedRootPackage)
	provided := fixedRootPackage.Provides()
	for _, p := range platformPackages {
		if link, ok := provided.Get(p.Name()); !ok || !matchesVersion(link.Constraint(), p.Version()) {
			request.FixPackage(p)
		}
	}
	for _, link := range merged.All() {
		requireName(t, request, link.Target(), link.Constraint())
	}

	return repositorySet, request
}

// oracleResult is solve.php's result of one resolution.
type oracleResult struct {
	pool                      int
	rules                     int
	lock, operations          []string
	problems, problemsVerbose string
}

func solveOracle(t testing.TB, data oracleData, env Environment) oracleResult {
	t.Helper()
	repositorySet, request := buildOracleRequest(t, data, loadOracleRepository(t, data.packages))
	policy := NewDefaultPolicy(data.preferStable, data.preferLowest, nil)
	pool, err := CreatePool(repositorySet, request, nullIO, CreatePoolOptions{PoolOptimizer: NewPoolOptimizer(policy)})
	if err != nil {
		t.Fatal(err)
	}
	result := oracleResult{pool: pool.Count()}

	solver := NewSolver(policy, pool, nullIO)
	transaction, err := solver.Solve(request, nil)
	if problems, ok := errors.AsType[*SolverProblemsError](err); ok {
		if result.problems, err = problems.PrettyString(repositorySet, request, pool, false, false, env); err != nil {
			t.Fatal(err)
		}
		if result.problemsVerbose, err = problems.PrettyString(repositorySet, request, pool, true, false, env); err != nil {
			t.Fatal(err)
		}

		return result
	}
	if err != nil {
		t.Fatal(err)
	}

	result.rules = solver.RuleSetSize()
	lockPackages, err := transaction.NewLockPackages(false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range lockPackages {
		result.lock = append(result.lock, p.PrettyName()+" "+p.PrettyVersion()+" "+p.Version())
	}
	for _, op := range transaction.Operations() {
		result.operations = append(result.operations, op.String())
	}

	return result
}

func diffLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	if slices.Equal(got, want) {
		return
	}
	for i := range max(len(got), len(want)) {
		var g, w string
		if i < len(got) {
			g = got[i]
		}
		if i < len(want) {
			w = want[i]
		}
		if g != w {
			t.Errorf("%s differ at %d (%d vs %d entries):\ngot:  %q\nwant: %q", what, i, len(got), len(want), g, w)

			return
		}
	}
}

func TestOracle_Solve(t *testing.T) {
	if testing.Short() {
		t.Skip("replays large recordings")
	}
	golden := oracleGolden(t)
	recordings := oracleRecordings(t)
	env := &oracleEnv{extensions: stringList(subArray(golden, "loadedExtensions")), iniFiles: stringList(subArray(golden, "iniFiles"))}
	platform := subArray(golden, "platform")
	results := subArray(golden, "results")

	for _, project := range oracleProjects {
		for _, v := range subArray(golden, "variants").All() {
			variant := v.(*php.Array)
			name, _ := variant.GetString("name")
			t.Run(project+"/"+name, func(t *testing.T) {
				want := subArray(subArray(results, project), name)
				got := solveOracle(t, applyVariant(recordings[project], platform, variant), env)

				if wantPool, _ := want.Get("pool"); int64(got.pool) != php.ToInt(wantPool) {
					t.Errorf("pool size %d, want %d", got.pool, php.ToInt(wantPool))
				}
				if wantProblems, ok := want.GetString("problems"); ok {
					if got.problems != wantProblems {
						t.Errorf("problems differ\ngot:\n%s\nwant:\n%s", got.problems, wantProblems)
					}
					if wantVerbose, _ := want.GetString("problemsVerbose"); got.problemsVerbose != wantVerbose {
						t.Errorf("verbose problems differ\ngot:\n%s\nwant:\n%s", got.problemsVerbose, wantVerbose)
					}

					return
				}
				if got.problems != "" {
					t.Fatalf("unexpected problems:\n%s", got.problems)
				}
				if wantRules, _ := want.Get("rules"); int64(got.rules) != php.ToInt(wantRules) {
					t.Errorf("rule set size %d, want %d", got.rules, php.ToInt(wantRules))
				}
				diffLines(t, "lock packages", got.lock, stringList(subArray(want, "lock")))
				diffLines(t, "operations", got.operations, stringList(subArray(want, "operations")))
			})
		}
	}
}

// BenchmarkOracleSolve measures creating the pool and solving the base
// variant of each recorded project; tools/oracle/resolver/solve.php --bench
// measures the same in PHP.
func BenchmarkOracleSolve(b *testing.B) {
	golden := oracleGolden(b)
	recordings := oracleRecordings(b)
	platform := subArray(golden, "platform")

	for _, project := range oracleProjects {
		b.Run(project, func(b *testing.B) {
			data := applyVariant(recordings[project], platform, php.NewArray())
			for b.Loop() {
				b.StopTimer()
				repo := loadOracleRepository(b, data.packages)
				b.StartTimer()

				repositorySet, request := buildOracleRequest(b, data, repo)
				policy := NewDefaultPolicy(data.preferStable, false, nil)
				pool, err := CreatePool(repositorySet, request, nullIO, CreatePoolOptions{PoolOptimizer: NewPoolOptimizer(policy)})
				if err != nil {
					b.Fatal(err)
				}
				if _, err := NewSolver(policy, pool, nullIO).Solve(request, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

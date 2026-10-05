package resolver

import (
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// Ports tests/Composer/Test/DependencyResolver/PoolBuilderTest.php.

func TestPoolBuilder_PoolBuilder(t *testing.T) {
	fixturesDir, err := filepath.Abs("testdata/Fixtures/poolbuilder")
	if err != nil {
		t.Fatal(err)
	}
	files := fixtureFiles(t, fixturesDir)
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	sections := map[string]bool{"TEST": true, "ROOT": false, "REQUEST": true, "FIXED": false, "PACKAGE-REPOS": true, "EXPECT": true, "EXPECT-OPTIMIZED": false}

	t.Chdir(fixturesDir)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			testData := readTestFile(t, file, sections)
			runPoolBuilderTest(t, testData)
		})
	}
}

func jsonArray(t testing.TB, data string) *php.Array {
	t.Helper()
	if data == "" {
		return php.NewArray()
	}
	a, ok := decodeJSON(t, data).(*php.Array)
	if !ok {
		t.Fatalf("not a JSON array or object: %s", data)
	}

	return a
}

func subArray(a *php.Array, key string) *php.Array {
	v, _ := a.GetArray(key)
	if v == nil {
		return php.NewArray()
	}

	return v
}

// expectedSet turns an EXPECT list into the result set's form: ids as
// "id:N", strings as they are.
func expectedSet(list *php.Array) []string {
	var out []string
	for _, v := range list.All() {
		if id, ok := v.(int64); ok {
			out = append(out, "id:"+strconv.FormatInt(id, 10))
		} else {
			out = append(out, php.ToString(v))
		}
	}
	slices.Sort(out)

	return out
}

func runPoolBuilderTest(t *testing.T, testData map[string]string) {
	root := jsonArray(t, testData["ROOT"])
	requestData := jsonArray(t, testData["REQUEST"])
	packageRepos := jsonArray(t, testData["PACKAGE-REPOS"])
	fixed := jsonArray(t, testData["FIXED"])
	expect := jsonArray(t, testData["EXPECT"])
	expectOptimized := expect
	if testData["EXPECT-OPTIMIZED"] != "" {
		expectOptimized = jsonArray(t, testData["EXPECT-OPTIMIZED"])
	}

	var rootAliases []repository.RootAlias
	for _, v := range subArray(root, "aliases").All() {
		alias := v.(*php.Array)
		version, _ := alias.GetString("version")
		aliasName, _ := alias.GetString("alias")
		packageName, _ := alias.GetString("package")
		rootAliases = append(rootAliases, repository.RootAlias{Package: packageName, Version: normalize(t, version), Alias: aliasName, AliasNormalized: normalize(t, aliasName)})
	}
	minimumStability := "stable"
	if s, ok := root.GetString("minimum-stability"); ok && s != "" {
		minimumStability = s
	}
	stabilityFlags := php.NewArray()
	for name, stability := range subArray(root, "stability-flags").All() {
		value, ok := pkg.StabilityValue(php.ToString(stability))
		if !ok {
			t.Fatalf("Invalid stability given: %v", stability)
		}
		stabilityFlags.SetKey(name, value)
	}
	rootReferences := subArray(root, "references")

	arrayLoader := loader.NewArrayLoader(nil, true)
	packageIDs := map[int64]pkg.PackageInterface{}
	load := func(data *php.Array) pkg.PackageInterface {
		data = data.Clone()
		var id int64
		if v, ok := data.Get("id"); ok && php.ToBool(v) {
			id = php.ToInt(v)
			data.Delete("id")
		}
		p := loadPackage(t, arrayLoader, data)
		if id != 0 {
			if _, dup := packageIDs[id]; dup {
				t.Fatalf("Duplicate package id %d defined", id)
			}
			packageIDs[id] = p
		}

		return p
	}

	repositorySet, err := repository.NewRepositorySet(minimumStability, stabilityFlags, rootAliases, rootReferences, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range packageRepos.All() {
		packages := v.(*php.Array)
		if packages.Has("type") {
			repo, err := repository.NewPathRepository(packages, nullIO, util.NewProcessExecutor(nil))
			if err != nil {
				t.Fatal(err)
			}
			addRepository(t, repositorySet, repo)

			continue
		}

		repo := newArrayRepository(t)
		if packages.Has("canonical") || packages.Has("only") || packages.Has("exclude") {
			options := packages.Clone()
			packages = subArray(options, "packages")
			options.Delete("packages")
			filtered, err := repository.NewFilterRepository(repo, options)
			if err != nil {
				t.Fatal(err)
			}
			addRepository(t, repositorySet, filtered)
		} else {
			addRepository(t, repositorySet, repo)
		}
		for _, data := range packages.All() {
			addPackage(t, repo, load(data.(*php.Array)))
		}
	}
	lockedRepo := newLockArrayRepository(t)
	addRepository(t, repositorySet, lockedRepo)

	for _, data := range subArray(requestData, "locked").All() {
		addPackage(t, lockedRepo, load(data.(*php.Array)))
	}
	request := NewRequest(lockedRepo)
	for name, constraint := range subArray(requestData, "require").All() {
		requireName(t, request, name.String(), parseConstraints(t, php.ToString(constraint)))
	}
	if requestData.Has("allowList") {
		transitiveDeps := UpdateOnlyListed
		if v, _ := requestData.Get("allowTransitiveDepsNoRootRequire"); php.ToBool(v) {
			transitiveDeps = UpdateListedWithTransitiveDepsNoRootRequire
		}
		if v, _ := requestData.Get("allowTransitiveDeps"); php.ToBool(v) {
			transitiveDeps = UpdateListedWithTransitiveDeps
		}
		var allowList []string
		for _, v := range subArray(requestData, "allowList").All() {
			allowList = append(allowList, php.ToString(v))
		}
		request.SetUpdateAllowList(allowList, transitiveDeps)
	}

	for _, data := range fixed.All() {
		request.FixPackage(load(data.(*php.Array)))
	}

	pool, err := CreatePool(repositorySet, request, nullIO, CreatePoolOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := packageResultSet(pool, packageIDs), expectedSet(expect); !slices.Equal(got, want) {
		t.Fatalf("Unoptimized pool does not match expected package set\ngot:  %q\nwant: %q", got, want)
	}

	optimizer := NewPoolOptimizer(NewDefaultPolicy(false, false, nil))
	optimized, err := optimizer.Optimize(request, pool)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := packageResultSet(optimized, packageIDs), expectedSet(expectOptimized); !slices.Equal(got, want) {
		t.Fatalf("Optimized pool does not match expected package set\ngot:  %q\nwant: %q", got, want)
	}
}

// packageResultSet ports getPackageResultSet, sorted.
func packageResultSet(pool *Pool, packageIDs map[int64]pkg.PackageInterface) []string {
	idOf := func(p pkg.PackageInterface) (int64, bool) {
		for id, candidate := range packageIDs {
			if candidate == p {
				return id, true
			}
		}

		return 0, false
	}

	var result []string
	for i := 1; i <= pool.Count(); i++ {
		p := pool.PackageByID(i)
		if id, ok := idOf(p); ok {
			result = append(result, "id:"+strconv.FormatInt(id, 10))

			continue
		}
		suffix := ""
		if ref := p.SourceReference(); ref.Valid && php.ToBool(ref.S) {
			suffix = "#" + ref.S
		}
		if _, locked := p.Repository().(*repository.LockArrayRepository); locked {
			suffix += " (locked)"
		}

		if alias, ok := p.(pkg.Alias); ok {
			if id, ok := idOf(alias.AliasOf()); ok {
				result = append(result, p.Name()+"-"+p.Version()+suffix+" (alias of "+strconv.FormatInt(id, 10)+")")
			} else {
				result = append(result, p.Name()+"-"+p.Version()+suffix+" (alias of "+alias.AliasOf().Version()+")")
			}

			continue
		}

		result = append(result, p.Name()+"-"+p.Version()+suffix)
	}
	slices.Sort(result)

	return result
}

package repository

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// readGolden reads a golden of tools/oracle/repository.
func readGolden(t *testing.T, path string) *php.Array {
	t.Helper()
	f := must(os.Open(path))
	defer f.Close()
	var r io.Reader = f
	if filepath.Ext(path) == ".gz" {
		r = must(gzip.NewReader(f))
	}
	decoded, ok := must(php.JSONDecode(string(must(io.ReadAll(r))), true)).(*php.Array)
	if !ok {
		t.Fatal("golden is not an object")
	}

	return decoded
}

// mapInstallPaths answers install paths from the oracle's map, the root
// package's being the working directory.
type mapInstallPaths struct {
	paths *php.Array
	cwd   string
}

func (m mapInstallPaths) InstallPath(p pkg.PackageInterface) (string, bool, error) {
	if _, ok := p.(pkg.RootPackageInterface); ok {
		return m.cwd, true, nil
	}
	if v, ok := m.paths.GetString(p.Name()); ok {
		return v, true, nil
	}

	return "vendor/" + p.Name(), true, nil
}

// TestFilesystemRepository_Oracle compares installed.json and installed.php
// with what Composer's FilesystemRepository::write dumps for the same
// packages (tools/oracle/repository/oracle.php).
func TestFilesystemRepository_Oracle(t *testing.T) {
	golden := readGolden(t, must(filepath.Abs("testdata/oracle/installed.json.gz")))
	tmp := util.Realpath(t.TempDir())
	t.Chdir(tmp)

	for key, c := range golden.All() {
		c, _ := c.(*php.Array)
		get := func(k string) any { v, _ := c.Get(k); return v }

		noErr(t, os.RemoveAll(tmp+"/vendor"))
		dir := tmp + "/vendor/composer"
		arrayLoader := loader.NewArrayLoader(nil, true)
		rootConfig, _ := get("root").(*php.Array)
		root, ok := must(arrayLoader.Load(rootConfig, pkg.ClassRootPackage)).(pkg.RootPackageInterface)
		if !ok {
			t.Fatal("root")
		}
		if alias, ok := get("root_alias").(string); ok {
			root = pkg.NewRootAliasPackage(root, normalize(t, alias), alias)
		}
		dumpVersions, _ := get("dump_versions").(bool)
		repo := must(NewFilesystemRepository(must(json.NewFile(dir+"/installed.json", nil, nil)), dumpVersions, root))
		devNames, _ := get("dev_names").(*php.Array)
		repo.SetDevPackageNames(stringValues(devNames))
		packages, _ := get("packages").(*php.Array)
		for _, data := range packages.All() {
			data, _ := data.(*php.Array)
			noErr(t, repo.AddPackage(must(arrayLoader.Load(data, pkg.ClassCompletePackage))))
		}
		paths, _ := get("paths").(*php.Array)
		devMode, _ := get("dev_mode").(bool)

		err := repo.Write(devMode, mapInstallPaths{paths: paths, cwd: tmp})

		result, _ := get("result").(*php.Array)
		if e, ok := result.GetArray("e"); ok {
			if msg, _ := e.GetString(1); err == nil || err.Error() != msg {
				t.Errorf("case %v: error %v, want %v", key, err, msg)
			}

			continue
		}
		if err != nil {
			t.Errorf("case %v: %v", key, err)

			continue
		}
		wantJSON, _ := result.GetString("json")
		if got := string(must(os.ReadFile(dir + "/installed.json"))); got != wantJSON {
			t.Errorf("case %v: installed.json\ngot  %s\nwant %s", key, got, wantJSON)
		}
		if wantPHP, ok := result.GetString("php"); ok {
			if got := string(must(os.ReadFile(dir + "/installed.php"))); got != wantPHP {
				t.Errorf("case %v: installed.php\ngot  %s\nwant %s", key, got, wantPHP)
			}
			// what write dumps loads back as its data
			if _, ok := SafelyLoadInstalledVersions(dir + "/installed.php"); !ok {
				t.Errorf("case %v: installed.php does not load back", key)
			}
		}
	}
}

// dumpDependents is the oracle's dump_dependents.
func dumpDependents(t *testing.T, results []Dependent) *php.Array {
	t.Helper()
	out := php.NewArray()
	for _, d := range results {
		var children any = false
		if !d.Cut {
			children = dumpDependents(t, d.Dependents)
		}
		out.Append(php.ListOf(
			d.Package.PrettyName()+" "+d.Package.PrettyVersion(),
			php.ListOf(d.Link.Source(), d.Link.Target(), d.Link.Description(), must(d.Link.PrettyConstraint())),
			children,
		))
	}

	return out
}

// TestInstalledRepository_GetDependentsOracle compares getDependents with
// Composer's on random dependency graphs, forward and inverted.
func TestInstalledRepository_GetDependentsOracle(t *testing.T) {
	for key, c := range readGolden(t, "testdata/oracle/dependents.json.gz").All() {
		c, _ := c.(*php.Array)
		get := func(k string) any { v, _ := c.Get(k); return v }

		arrayLoader := loader.NewArrayLoader(nil, true)
		lockRepo := must(NewLockArrayRepository(nil))
		packages, _ := get("packages").(*php.Array)
		for _, data := range packages.All() {
			data, _ := data.(*php.Array)
			noErr(t, lockRepo.AddPackage(must(arrayLoader.Load(data, pkg.ClassCompletePackage))))
		}
		rootConfig, _ := get("root").(*php.Array)
		root, _ := must(arrayLoader.Load(rootConfig, pkg.ClassRootPackage)).(pkg.RootPackageInterface)
		rootRepo := must(NewRootPackageRepository(root))
		repo := must(NewInstalledRepository([]RepositoryInterface{rootRepo, lockRepo}))

		needle, _ := get("needle").(*php.Array)
		var constraint semver.ConstraintInterface
		if s, ok := get("constraint").(string); ok {
			constraint = mustConstraint(t, s)
		}
		invert, _ := get("invert").(bool)
		recurse, _ := get("recurse").(bool)

		got := dumpDependents(t, must(repo.GetDependents(stringValues(needle), constraint, invert, recurse)))
		want, _ := get("result").(*php.Array)
		if !php.StrictEquals(got, want) {
			t.Errorf("case %v:\ngot  %s\nwant %s", key, must(php.JSONEncode(got, 0)), must(php.JSONEncode(want, 0)))
		}
	}
}

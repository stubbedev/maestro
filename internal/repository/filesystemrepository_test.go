package repository

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// Ports tests/Composer/Test/Repository/FilesystemRepositoryTest.php.

// jsonFileMock stands in for the JsonFile mocks of Composer's tests.
type jsonFileMock struct {
	path    string
	exists  bool
	data    any
	reads   int
	written []any
}

func (f *jsonFileMock) Path() string { return f.path }
func (f *jsonFileMock) Exists() bool { return f.exists }
func (f *jsonFileMock) Read() (any, error) {
	f.reads++

	return f.data, nil
}

func (f *jsonFileMock) Write(hash any, _ php.JSONFlag) error {
	f.written = append(f.written, hash)

	return nil
}

// installPaths is an InstallationManager mock.
type installPaths func(p pkg.PackageInterface) string

func (f installPaths) InstallPath(p pkg.PackageInterface) (string, bool, error) {
	return f(p), true, nil
}

// testRepository is a Composer repository of a repository manager's: its
// url, its config, and which dist URLs belong to it.
type testRepository struct {
	*ArrayRepository
	url        string
	repoConfig *php.Array
}

func (r *testRepository) RepoName() string { return "composer repo (" + r.url + ")" }

func (r *testRepository) BaseURL() string { return r.url }

func (r *testRepository) RepoConfig() *php.Array { return r.repoConfig }

func (r *testRepository) IsOwnDistUrl(p pkg.PackageInterface) bool {
	for _, url := range p.DistURLs() {
		if rest, ok := strings.CutPrefix(url, r.url); ok && (rest == "" || strings.IndexByte("/?#", rest[0]) >= 0) {
			return true
		}
	}

	return false
}

func TestFilesystemRepository_RepositoryRead(t *testing.T) {
	file := &jsonFileMock{exists: true, data: php.ListOf(php.ArrayOf("name", "package1", "version", "1.0.0-beta", "type", "vendor"))}
	repo := must(NewFilesystemRepository(file, false, nil))

	packages := must(repo.Packages())

	if len(packages) != 1 || packages[0].Name() != "package1" || packages[0].Version() != "1.0.0.0-beta" || packages[0].Type() != "vendor" {
		t.Fatalf("%v", packages)
	}
	if file.reads != 1 {
		t.Error("read once")
	}
}

func TestFilesystemRepository_CorruptedRepositoryFile(t *testing.T) {
	file := &jsonFileMock{path: "installed.json", exists: true, data: "foo"}
	repo := must(NewFilesystemRepository(file, false, nil))

	_, err := repo.Packages()
	target, ok := errors.AsType[*InvalidRepositoryError](err)
	if !ok {
		t.Fatalf("%v", err)
	}
	if want := "Invalid repository data in installed.json, packages could not be loaded: [UnexpectedValueException] Could not parse package list from the repository"; target.Message != want {
		t.Error(target.Message)
	}
}

func TestFilesystemRepository_UnexistentRepositoryFile(t *testing.T) {
	file := &jsonFileMock{exists: false}
	repo := must(NewFilesystemRepository(file, false, nil))

	if got := must(repo.Packages()); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestFilesystemRepository_RepositoryWrite(t *testing.T) {
	repoDir := util.Realpath(t.TempDir()) + "/repo_write_test/"

	file := &jsonFileMock{exists: true, data: php.NewArray(), path: repoDir + "/vendor/composer/installed.json"}
	repo := must(NewFilesystemRepository(file, false, nil))
	calls := 0
	im := installPaths(func(pkg.PackageInterface) string {
		calls++

		return repoDir + "/vendor/woop/woop"
	})

	repo.SetDevPackageNames([]string{"mypkg2"})
	noErr(t, repo.AddPackage(getPackage(t, "mypkg2", "1.2.3")))
	noErr(t, repo.AddPackage(getPackage(t, "mypkg", "0.1.10")))
	noErr(t, repo.Write(true, im))

	if calls != 2 || len(file.written) != 1 {
		t.Fatalf("calls %d writes %d", calls, len(file.written))
	}
	want := php.ArrayOf(
		"packages", php.ListOf(
			php.ArrayOf("name", "mypkg", "type", "library", "version", "0.1.10", "version_normalized", "0.1.10.0", "install-path", "../woop/woop"),
			php.ArrayOf("name", "mypkg2", "type", "library", "version", "1.2.3", "version_normalized", "1.2.3.0", "install-path", "../woop/woop"),
		),
		"dev", true,
		"dev-package-names", php.ListOf("mypkg2"),
	)
	if !php.LooseEquals(file.written[0], want) {
		got, _ := json.EncodeDefault(file.written[0])
		t.Fatalf("written %s", got)
	}
}

func TestFilesystemRepository_RepositoryWriteRemovesRestorableHttpHeaders(t *testing.T) {
	dir := util.Realpath(t.TempDir())
	file := must(json.NewFile(dir+"/vendor/composer/installed.json", nil, nil))

	options := php.ArrayOf("http", php.ArrayOf("header", php.ListOf("API-TOKEN: secret")))
	manager := NewRepositoryManager(nil, nil, nil, nil, nil)
	manager.AddRepository(&testRepository{
		ArrayRepository: must(NewArrayRepository(nil)), url: "https://example.org",
		repoConfig: php.ArrayOf("url", "https://example.org", "options", options),
	})

	im := installPaths(func(pkg.PackageInterface) string { return dir + "/vendor/a/a" })

	p := pkg.NewPackage("a/a", "1.0.0.0", "1.0.0")
	p.SetDistType(pkg.Str("zip"))
	p.SetDistURL(pkg.Str("https://example.org/dists/a.zip"))
	p.SetTransportOptions(options)

	repo := must(NewFilesystemRepository(file, false, nil))
	repo.SetRepositoryManager(manager)
	noErr(t, repo.AddPackage(p))
	noErr(t, repo.Write(true, im))

	read, err := json.NewFile(file.Path(), nil, nil)
	noErr(t, err)
	data, err := read.Read()
	noErr(t, err)
	if data.(*php.Array).ArrayAt("packages").At(0).(*php.Array).Isset("transport-options") {
		t.Error("transport-options stored")
	}

	// e.g. composer reinstall downloads from the installed packages again so
	// they need their headers
	repo = must(NewFilesystemRepository(file, false, nil))
	repo.SetRepositoryManager(manager)
	if got := must(repo.Packages())[0].TransportOptions(); !php.StrictEquals(got, options) {
		encoded, _ := php.JSONEncode(got, 0)
		t.Errorf("transport options %s, want %s", encoded, must(php.JSONEncode(options, 0)))
	}
}

func TestFilesystemRepository_RepositoryWritesInstalledPhp(t *testing.T) {
	want := must(os.ReadFile("testdata/Fixtures/installed.php"))
	dir := util.Realpath(t.TempDir())
	t.Chdir(dir)

	file := must(json.NewFile(dir+"/installed.json", nil, nil))

	root := getRootPackage(t, "__root__", "dev-master")
	root.SetSourceReference(pkg.Str("sourceref-by-default"))
	root.SetDistReference(pkg.Str("distref"))
	configureLinks(t, root, map[string][][2]string{"provide": {{"foo/impl", "2.0"}}})
	rootAlias, ok := getAliasPackage(t, root, "1.10.x-dev").(*pkg.RootAliasPackage)
	if !ok {
		t.Fatal("root alias")
	}

	repo := must(NewFilesystemRepository(file, true, rootAlias))
	repo.SetDevPackageNames([]string{"c/c"})
	p := getPackage(t, "a/provider", "1.1")
	configureLinks(t, p, map[string][][2]string{"provide": {{"foo/impl", "^1.1"}, {"foo/impl2", "2.0"}}})
	p.SetDistReference(pkg.Str("distref-as-no-source"))
	noErr(t, repo.AddPackage(p))

	p = getPackage(t, "a/provider2", "1.2")
	configureLinks(t, p, map[string][][2]string{"provide": {{"foo/impl", "self.version"}, {"foo/impl2", "2.0"}}})
	p.SetSourceReference(pkg.Str("sourceref"))
	p.SetDistReference(pkg.Str("distref-as-installed-from-dist"))
	p.SetInstallationSource(pkg.Some(pkg.FromDist))
	noErr(t, repo.AddPackage(p))

	noErr(t, repo.AddPackage(getAliasPackage(t, p, "1.4")))

	p = getPackage(t, "b/replacer", "2.2")
	configureLinks(t, p, map[string][][2]string{"replace": {{"foo/impl2", "self.version"}, {"foo/replaced", "^3.0"}}})
	noErr(t, repo.AddPackage(p))

	p = getPackage(t, "c/c", "3.0")
	p.SetDistReference(pkg.Str("{${passthru('bash -i')}} Foo\\Bar" + "\n\ttab\vverticaltab\x00"))
	noErr(t, repo.AddPackage(p))

	p = getPackage(t, "meta/package", "3.0")
	p.SetType("metapackage")
	noErr(t, repo.AddPackage(p))

	im := installPaths(func(p pkg.PackageInterface) string {
		// check for empty paths handling
		if p.Type() == "metapackage" {
			return ""
		}
		if p.Name() == "c/c" {
			// check for absolute paths
			return "/foo/bar/ven\\do{}r/c/c${}"
		}
		if p.Name() == "a/provider" {
			return "vendor/{${passthru('bash -i')}}"
		}
		// check for cwd
		if _, ok := p.(pkg.RootPackageInterface); ok {
			return dir
		}

		// check for relative paths
		return "vendor/" + p.Name()
	})

	var sunk *php.Array
	repo.SetInstalledVersionsSink(func(v *php.Array) { sunk = v })
	noErr(t, repo.Write(true, im))

	got := must(os.ReadFile(dir + "/installed.php"))
	if string(got) != string(want) {
		t.Fatalf("installed.php:\n%s", got)
	}
	if sunk == nil {
		t.Error("sink not called")
	}
	if _, err := os.Stat(dir + "/InstalledVersions.php"); err != nil {
		t.Error(err)
	}

	// the dumped file loads back
	data, ok := SafelyLoadInstalledVersions(dir + "/installed.php")
	if !ok {
		t.Fatal("safelyLoadInstalledVersions")
	}
	root2, _ := data.GetArray("root")
	if path, _ := root2.GetString("install_path"); path != dir+"/./" {
		t.Errorf("install_path %q", path)
	}
}

func TestFilesystemRepository_SafelyLoadInstalledVersions(t *testing.T) {
	fixtures := must(filepath.Abs("testdata/Fixtures"))
	data, ok := SafelyLoadInstalledVersions(fixtures + "/installed_complex.php")
	if !ok {
		t.Fatal("The file should be considered valid")
	}
	want := php.ArrayOf(
		"root", php.ArrayOf(
			"install_path", fixtures+"/./",
			"aliases", php.ListOf("1.10.x-dev", "2.10.x-dev"),
			"name", "__root__",
			"true", true,
			"false", false,
			"null", nil,
		),
		"versions", php.ArrayOf(
			"a/provider", php.ArrayOf(
				"foo", "simple string/no backslash",
				"install_path", fixtures+"/vendor/{${passthru('bash -i')}}",
				"empty array", php.NewArray(),
			),
			"c/c", php.ArrayOf(
				"install_path", "/foo/bar/ven/do{}r/c/c${}",
				"aliases", php.NewArray(),
				"reference", "{${passthru('bash -i')}} Foo\\Bar\n\ttab\vverticaltab\x00",
			),
		),
	)
	if !php.StrictEquals(data, want) {
		got, _ := json.EncodeDefault(data)
		t.Fatalf("%s", got)
	}

	if _, ok := SafelyLoadInstalledVersions(fixtures + "/installed_relative.php"); ok {
		t.Error("installed_relative.php uses $dir and must be rejected")
	}
}

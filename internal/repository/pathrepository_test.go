package repository

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/processmock"
	"github.com/stubbedev/maestro/internal/util/vcs"
)

// Ports tests/Composer/Test/Repository/PathRepositoryTest.php,
// ArtifactRepositoryTest.php, RepositoryFactoryTest.php and
// RepositoryManagerTest.php.

func fixturePath(t *testing.T, parts ...string) string {
	t.Helper()

	return filepath.Join(append([]string{must(filepath.Abs("testdata/Fixtures"))}, parts...)...)
}

func createPathRepo(t *testing.T, options *php.Array) *PathRepository {
	t.Helper()

	// As in a real run, the executor is the Loop's, so VersionGuesser's
	// feature-branch guessing (taken when the checkout is not on a default
	// branch, e.g. a detached HEAD in CI) may run git asynchronously.
	process := util.NewProcessExecutor(nil)
	process.EnableAsync()

	return must(NewPathRepository(options, io.NewNullIO(), process))
}

func TestPathRepository_LoadPackageFromFileSystemWithIncorrectPath(t *testing.T) {
	repo := createPathRepo(t, php.ArrayOf("url", fixturePath(t, "path", "missing")))
	_, err := repo.Packages()
	if _, ok := errors.AsType[*util.RuntimeError](err); !ok {
		t.Fatalf("%v", err)
	}
}

func TestPathRepository_LoadPackageFromFileSystemWithVersion(t *testing.T) {
	repo := createPathRepo(t, php.ArrayOf("url", fixturePath(t, "path", "with-version")))
	must(repo.Packages())

	if count(t, repo) != 1 {
		t.Fatal("count")
	}
	if !must(repo.HasPackage(getPackage(t, "test/path-versioned", "0.0.2"))) {
		t.Fatal("hasPackage")
	}
}

func TestPathRepository_LoadPackageFromFileSystemWithoutVersion(t *testing.T) {
	repo := createPathRepo(t, php.ArrayOf("url", fixturePath(t, "path", "without-version")))
	packages := must(repo.Packages())

	if count(t, repo) < 1 {
		t.Fatal("count")
	}
	if packages[0].Name() != "test/path-unversioned" || packages[0].Version() == "" {
		t.Fatalf("%s %s", packages[0].Name(), packages[0].Version())
	}
}

func TestPathRepository_LoadPackageFromFileSystemWithWildcard(t *testing.T) {
	repo := createPathRepo(t, php.ArrayOf("url", fixturePath(t, "path", "*")))
	packages := must(repo.Packages())

	if count(t, repo) < 2 {
		t.Fatal("count")
	}
	got := []string{packages[0].Name(), packages[1].Name()}
	sort.Strings(got)
	if !slices.Equal(got, []string{"test/path-unversioned", "test/path-versioned"}) {
		t.Fatal(got)
	}
}

func TestPathRepository_LoadPackageWithExplicitVersions(t *testing.T) {
	options := php.ArrayOf("versions", php.ArrayOf(
		"test/path-unversioned", "4.3.2.1",
		"test/path-versioned", "3.2.1.0",
	))
	repo := createPathRepo(t, php.ArrayOf("url", fixturePath(t, "path", "*"), "options", options))
	packages := must(repo.Packages())

	if count(t, repo) != 2 {
		t.Fatal("count")
	}
	versions := map[string]string{packages[0].Name(): packages[0].Version(), packages[1].Name(): packages[1].Version()}
	if versions["test/path-unversioned"] != "4.3.2.1" || versions["test/path-versioned"] != "3.2.1.0" {
		t.Fatal(versions)
	}
}

// TestPathRepository_UrlRemainsRelative verifies relative repository URLs
// remain relative, see composer#4439.
func TestPathRepository_UrlRemainsRelative(t *testing.T) {
	repositoryURL := fixturePath(t, "path", "with-version")
	cwd := util.Realpath(must(util.GetCwd(false)))
	relativeURL := strings.TrimLeft(strings.TrimPrefix(util.Realpath(repositoryURL), cwd), string(filepath.Separator))

	repo := createPathRepo(t, php.ArrayOf("url", relativeURL))
	packages := must(repo.Packages())

	if count(t, repo) != 1 || packages[0].Name() != "test/path-versioned" {
		t.Fatal("package")
	}
	// Platform specific separators become generic URL slashes.
	if got := packages[0].DistURL().S; got != filepath.ToSlash(relativeURL) {
		t.Fatalf("%q != %q", got, relativeURL)
	}
	if transport := packages[0].TransportOptions(); !php.StrictEquals(transport, php.ArrayOf("relative", true)) {
		t.Errorf("transport options %v", transport.Keys())
	}
}

func TestPathRepository_ReferenceNone(t *testing.T) {
	options := php.ArrayOf("reference", "none")
	repo := createPathRepo(t, php.ArrayOf("url", fixturePath(t, "path", "*"), "options", options))
	packages := must(repo.Packages())

	if count(t, repo) < 2 {
		t.Fatal("count")
	}
	for _, p := range packages {
		if p.DistReference().Valid {
			t.Errorf("%s: %q", p.Name(), p.DistReference().S)
		}
	}
}

func TestPathRepository_ReferenceConfig(t *testing.T) {
	options := php.ArrayOf("reference", "config", "relative", true)
	repo := createPathRepo(t, php.ArrayOf("url", fixturePath(t, "path", "*"), "options", options))
	packages := must(repo.Packages())

	if count(t, repo) < 2 {
		t.Fatal("count")
	}
	for _, p := range packages {
		content := must(os.ReadFile(p.DistURL().S + "/composer.json"))
		sum := sha1.Sum(append(content, php.Serialize(options)...))
		if want := hex.EncodeToString(sum[:]); p.DistReference().S != want {
			t.Errorf("%s: %s != %s", p.Name(), p.DistReference().S, want)
		}
	}
}

// TestPathRepository_RootVersionAndGitReference goes beyond Composer's
// tests: COMPOSER_ROOT_VERSION is carried over when the path repository
// shares the root's git HEAD, and the "auto" reference is the checkout's
// commit.
func TestPathRepository_RootVersionAndGitReference(t *testing.T) {
	dir := t.TempDir()
	noErr(t, os.MkdirAll(dir+"/pkg/.git", 0o755))
	noErr(t, os.WriteFile(dir+"/pkg/composer.json", []byte(`{"name": "a/b"}`), 0o644))
	t.Setenv("COMPOSER_ROOT_VERSION", "1.2-dev")
	t.Cleanup(func() { vcs.SetVersion("", false) })
	vcs.SetVersion("2.40.0", true)

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("git", "rev-parse", "HEAD"), Stdout: "abc\n"},
		{Cmd: util.Cmd("git", "rev-parse", "HEAD"), Stdout: "abc\n"},
		{Cmd: util.Cmd("git", "rev-list", "--no-commit-header", "-n1", "--format=%H", "HEAD", "--no-show-signature"), Stdout: "0123456789abcdef\n"},
	}, true, nil)

	repo := must(NewPathRepository(php.ArrayOf("url", dir+"/pkg"), io.NewNullIO(), process))
	packages := must(repo.Packages())
	noErr(t, process.AssertComplete())
	if len(packages) != 1 || packages[0].PrettyVersion() != "1.2.x-dev" || packages[0].DistReference().S != "0123456789abcdef" {
		t.Fatalf("%v %q", packages, packages[0].DistReference().S)
	}
}

func TestArtifactRepository_ExtractsConfigsFromZipArchives(t *testing.T) {
	expected := []string{
		"vendor0/package0-0.0.1",
		"composer/composer-1.0.0-alpha6",
		"vendor1/package2-4.3.2",
		"vendor3/package1-5.4.3",
		"test/jsonInRoot-1.0.0",
		"test/jsonInRootTarFile-1.0.0",
		"test/jsonInFirstLevel-1.0.0",
		// The files not-an-artifact.zip and jsonSecondLevel are not valid
		// artifacts and do not get detected.
	}

	repo := must(NewArtifactRepository(php.ArrayOf("type", "artifact", "url", fixturePath(t, "artifacts")), io.NewNullIO()))

	var found []string
	var tarPackage pkg.PackageInterface
	for _, p := range must(repo.Packages()) {
		found = append(found, p.PrettyName()+"-"+p.PrettyVersion())
		if p.PrettyName() == "test/jsonInRootTarFile" {
			tarPackage = p
		}
	}
	sort.Strings(expected)
	sort.Strings(found)
	if !slices.Equal(found, expected) {
		t.Fatal(found)
	}
	if tarPackage == nil || tarPackage.DistType().S != "tar" {
		t.Fatal("tar package")
	}
}

func TestArtifactRepository_AbsoluteRepoUrlCreatesAbsoluteUrlPackages(t *testing.T) {
	absolutePath := fixturePath(t, "artifacts")
	repo := must(NewArtifactRepository(php.ArrayOf("type", "artifact", "url", absolutePath), io.NewNullIO()))
	for _, p := range must(repo.Packages()) {
		if !strings.HasPrefix(p.DistURL().S, filepath.ToSlash(absolutePath)) {
			t.Error(p.DistURL().S)
		}
	}
}

func TestArtifactRepository_RelativeRepoUrlCreatesRelativeUrlPackages(t *testing.T) {
	relativePath := "testdata/Fixtures/artifacts"
	repo := must(NewArtifactRepository(php.ArrayOf("type", "artifact", "url", relativePath), io.NewNullIO()))
	for _, p := range must(repo.Packages()) {
		if !strings.HasPrefix(p.DistURL().S, relativePath) {
			t.Error(p.DistURL().S)
		}
	}
}

func stubConstructor(config *php.Array, _ Deps) (RepositoryInterface, error) {
	return NewArrayRepository(nil)
}

func TestRepositoryFactory_ManagerWithAllRepositoryTypes(t *testing.T) {
	manager := Manager(io.NewNullIO(), config.New(false, ""), nil, nil, nil, ExternalTypes{Composer: stubConstructor, VCS: stubConstructor})

	want := []string{
		"composer",
		"vcs",
		"package",
		"pear",
		"git",
		"bitbucket",
		"git-bitbucket",
		"github",
		"gitlab",
		"svn",
		"fossil",
		"perforce",
		"hg",
		"artifact",
		"path",
	}
	if got := manager.RepositoryTypes(); !slices.Equal(got, want) {
		t.Fatal(got)
	}
}

func TestRepositoryFactory_GenerateRepositoryName(t *testing.T) {
	for _, c := range []struct {
		index    php.Key
		config   *php.Array
		existing []string
		expected string
	}{
		{php.IntKey(0), php.NewArray(), nil, "0"},
		{php.IntKey(0), php.NewArray(), []string{"0"}, "02"},
		{php.IntKey(0), php.ArrayOf("url", "https://example.org"), nil, "example.org"},
		{php.IntKey(0), php.ArrayOf("url", "https://example.org"), []string{"example.org"}, "example.org2"},
		{php.StrKey("example.org"), php.ArrayOf("url", "https://example.org/repository"), nil, "example.org"},
		{php.StrKey("example.org"), php.ArrayOf("url", "https://example.org/repository"), []string{"example.org"}, "example.org2"},
	} {
		exists := func(name string) bool { return slices.Contains(c.existing, name) }
		if got := GenerateRepositoryName(c.index, c.config, exists); got != c.expected {
			t.Errorf("%v: %q", c.index, got)
		}
	}
}

func newTestManager(t *testing.T) *RepositoryManager {
	t.Helper()
	cfg := config.New(false, "")
	noErr(t, cfg.Merge(php.ArrayOf("config", php.ArrayOf("cache-repo-dir", t.TempDir())), config.SourceUnknown))

	return NewRepositoryManager(io.NewNullIO(), cfg, nil, nil, nil)
}

func TestRepositoryManager_Prepend(t *testing.T) {
	rm := newTestManager(t)
	repository1 := newArrayRepo(t)
	repository2 := newArrayRepo(t)
	rm.AddRepository(repository1)
	rm.PrependRepository(repository2)

	if got := rm.Repositories(); len(got) != 2 || got[0] != repository2 || got[1] != repository1 {
		t.Fatal(got)
	}
}

func TestRepositoryManager_RepoCreation(t *testing.T) {
	for _, c := range []struct {
		typ     string
		options *php.Array
	}{
		{"composer", php.ArrayOf("url", "http://example.org")},
		{"vcs", php.ArrayOf("url", "http://github.com/foo/bar")},
		{"git", php.ArrayOf("url", "http://github.com/foo/bar")},
		{"git", php.ArrayOf("url", "git@example.org:foo/bar.git")},
		{"svn", php.ArrayOf("url", "svn://example.org/foo/bar")},
		{"package", php.ArrayOf("package", php.NewArray())},
		{"artifact", php.ArrayOf("url", "/path/to/zips")},
	} {
		rm := newTestManager(t)
		for _, typ := range []string{"composer", "vcs", "git", "svn", "perforce", "hg"} {
			rm.SetRepositoryClass(typ, stubConstructor)
		}
		rm.SetRepositoryClass("package", newPackageRepository)
		rm.SetRepositoryClass("pear", newPearRepository)
		rm.SetRepositoryClass("artifact", newArtifactRepository)

		must(rm.CreateRepository("composer", php.ArrayOf("url", "http://example.org"), ""))
		must(rm.CreateRepository(c.typ, c.options, ""))
	}
}

func TestRepositoryManager_InvalidRepoCreationThrows(t *testing.T) {
	for _, c := range []struct {
		typ     string
		options *php.Array
	}{
		{"pear", php.ArrayOf("url", "http://pear.example.org/foo")},
		{"invalid", php.NewArray()},
	} {
		rm := Manager(io.NewNullIO(), config.New(false, ""), nil, nil, nil, ExternalTypes{})
		_, err := rm.CreateRepository(c.typ, c.options, "")
		if _, ok := errors.AsType[*util.InvalidArgumentError](err); !ok {
			t.Errorf("%s: %v", c.typ, err)
		}
	}
}

func TestRepositoryManager_FilterRepoWrapping(t *testing.T) {
	rm := newTestManager(t)
	rm.SetRepositoryClass("path", newPathRepository)
	repo := must(rm.CreateRepository("path", php.ArrayOf("type", "path", "url", must(filepath.Abs(".")), "only", php.ListOf("foo/bar")), ""))

	filter, ok := repo.(*FilterRepository)
	if !ok {
		t.Fatalf("%T", repo)
	}
	if _, ok := filter.Repository().(*PathRepository); !ok {
		t.Fatalf("%T", filter.Repository())
	}
}

// TestRepositoryManager_PackagistWarning checks the warning about a
// "packagist" key inside another repository definition.
func TestRepositoryManager_PackagistWarning(t *testing.T) {
	out := must(io.NewBufferIO("", console.VerbosityNormal, nil))
	rm := NewRepositoryManager(out, config.New(false, ""), nil, nil, nil)
	rm.SetRepositoryClass("package", newPackageRepository)
	must(rm.CreateRepository("package", php.ArrayOf("package", php.NewArray(), "packagist", false), "foo"))
	if want := "<warning>Repository \"foo\" ({\"package\":[],\"packagist\":false}) has a packagist key which should be in its own repository definition</warning>\n"; php.NormalizeEOL(out.Output()) != want {
		t.Errorf("%q", php.NormalizeEOL(out.Output()))
	}
}

package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
)

// Ports tests/Composer/Test/Repository/VcsRepositoryTest.php, plus
// integration tests of the scan on a real git repository.

// newGitFixture builds VcsRepositoryTest's repository: tags and branches
// with and without composer.json, a tag whose composer.json version does
// not match, and a branch name with "#".
func newGitFixture(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("This test needs a git binary in the PATH to be able to run")
	}

	repo := t.TempDir()

	run := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Failed to execute git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	write := func(name, content string) {
		t.Helper()
		noErr(t, os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644))
	}

	// init
	run("init", "-q")
	run("checkout", "-q", "-B", "master")
	run("config", "user.email", "composertest@example.org")
	run("config", "user.name", "ComposerTest")
	run("config", "commit.gpgsign", "false")
	write("foo", "")
	run("add", "foo")
	run("commit", "-q", "-m", "init")

	// non-composed tag & branch
	run("tag", "0.5.0")
	run("branch", "oldbranch")

	// add composed tag & master branch
	write("composer.json", `{"name":"a\/b"}`)
	run("add", "composer.json")
	run("commit", "-q", "-m", "addcomposer")
	run("tag", "0.6.0")

	// add feature-a branch
	run("checkout", "-q", "-b", "feature/a-1.0-B")
	write("foo", "bar feature")
	run("add", "foo")
	run("commit", "-q", "-m", "change-a")

	// add foo#bar branch which should result in dev-foo+bar
	run("branch", "foo#bar")

	// add version to composer.json
	run("checkout", "-q", "master")
	write("composer.json", `{"name":"a\/b","version":"1.0.0"}`)
	run("add", "composer.json")
	run("commit", "-q", "-m", "addversion")

	// create tag with wrong version in it
	run("tag", "0.9.0")
	// create tag with correct version in it
	run("tag", "1.0.0")

	// add feature-b branch
	run("checkout", "-q", "-b", "feature-b")
	write("foo", "baz feature")
	run("add", "foo")
	run("commit", "-q", "-m", "change-b")

	// add 1.0 branch
	run("checkout", "-q", "master")
	run("branch", "1.0")

	// add 1.0.x branch
	run("branch", "1.1.x")

	// update master to 2.0
	write("composer.json", `{"name":"a\/b","version":"2.0.0"}`)
	run("add", "composer.json")
	run("commit", "-q", "-m", "bump-version")

	return repo
}

func newFixtureRepository(t *testing.T, ioi io.IO, opts Options) (*VcsRepository, string) {
	t.Helper()

	repo := newGitFixture(t)
	cfg := newConfig(t, t.TempDir())

	r, err := NewVcsRepository(php.ArrayOf("url", repo, "type", "vcs"), ioi, cfg.ForHTTP(), httpmock.New(), nil, opts)
	noErr(t, err)

	return r, repo
}

func prettyVersions(t *testing.T, r repository.RepositoryInterface) []string {
	t.Helper()

	packages, err := r.Packages()
	noErr(t, err)

	versions := make([]string, len(packages))
	for i, p := range packages {
		versions[i] = p.PrettyVersion()
	}

	return versions
}

func TestVcsRepository_LoadVersions(t *testing.T) {
	t.Parallel()

	r, _ := newFixtureRepository(t, io.NewNullIO(), Options{})

	expected := []string{
		"0.6.0",
		"1.0.0",
		"1.0.x-dev",
		"1.1.x-dev",
		"dev-feature-b",
		"dev-feature/a-1.0-B",
		"dev-foo+bar",
		"dev-master",
		"9999999-dev", // alias of dev-master
	}

	got := prettyVersions(t, r)
	for _, v := range got {
		i := slices.Index(expected, v)
		if i < 0 {
			t.Fatalf("Unexpected version %s in %q", v, got)
		}

		expected = slices.Delete(expected, i, i+1)
	}

	if len(expected) > 0 {
		t.Fatalf("Missing versions: %s", strings.Join(expected, ", "))
	}
}

func TestVcsRepository_VeryVerboseScan(t *testing.T) {
	t.Parallel()

	ioi := newTestIO()
	ioi.veryVerbose = true

	r, repo := newFixtureRepository(t, ioi, Options{})

	// the root identifier's branch is loaded first, with the default branch
	// marker (its alias first, as ArrayRepository::addPackage adds it)
	got := prettyVersions(t, r)
	if got[0] != "0.6.0" || got[1] != "1.0.0" || got[2] != "9999999-dev" || got[3] != "dev-master" {
		t.Fatalf("versions %q", got)
	}

	packages, err := r.Packages()
	noErr(t, err)

	for _, p := range packages {
		if p.IsDefaultBranch() != (p.PrettyVersion() == "dev-master" || p.PrettyVersion() == "9999999-dev") {
			t.Errorf("%s default branch = %v", p.PrettyVersion(), p.IsDefaultBranch())
		}

		if p.Name() != "a/b" || p.SourceType().S != "git" || p.SourceURL().S != repo {
			t.Errorf("%s: %s %s %s", p.PrettyVersion(), p.Name(), p.SourceType().S, p.SourceURL().S)
		}
	}

	msgs := ioi.messages()
	for _, want := range []string{
		"<warning>Skipped tag 0.5.0, no composer file</warning>",
		"Reading composer.json of <info>a/b</info> (<comment>0.6.0</comment>)",
		"Importing tag 0.6.0 (0.6.0.0)",
		"<warning>Skipped tag 0.9.0, tag (0.9.0.0) does not match version (1.0.0.0) in composer.json</warning>",
		"Importing tag 1.0.0 (1.0.0.0)",
		"Importing branch master (dev-master)",
		"<warning>Skipped branch oldbranch, no composer file</warning>",
		"Importing branch foo#bar (dev-foo+bar)",
		"Importing branch 1.1.x (1.1.x-dev)",
	} {
		if !slices.Contains(msgs, want) {
			t.Errorf("missing message %q in\n%s", want, strings.Join(msgs, "\n"))
		}
	}

	if len(r.EmptyReferences()) != 2 || r.HadInvalidBranches() {
		t.Errorf("empty references %q, invalid branches %v", r.EmptyReferences(), r.HadInvalidBranches())
	}

	if name := r.RepoName(); name != "vcs repo (git "+repo+")" {
		t.Errorf("repo name %q", name)
	}
}

// fakeVersionCache is a VersionCacheInterface answering from a map:
// version => package data or false.
type fakeVersionCache map[string]any

func (c fakeVersionCache) VersionPackage(version, _ string) any { return c[version] }

func TestVcsRepository_VersionCache(t *testing.T) {
	t.Parallel()

	ioi := newTestIO()
	ioi.veryVerbose = true

	cache := fakeVersionCache{
		"0.6.0": false,
		"1.0.0": php.ArrayOf("name", "a/b", "version", "1.0.0", "version_normalized", "1.0.0.0", "description", "from the cache", "default-branch", true),
	}

	r, _ := newFixtureRepository(t, ioi, Options{VersionCache: cache})

	p, err := r.FindPackage("a/b", nil)
	noErr(t, err)

	if p == nil {
		t.Fatal("no package")
	}

	packages, err := r.Packages()
	noErr(t, err)

	var cached pkg.PackageInterface

	for _, p := range packages {
		if p.PrettyVersion() == "0.6.0" {
			t.Errorf("0.6.0 is cached as absent")
		}

		if p.PrettyVersion() == "1.0.0" {
			cached = p
		}
	}

	complete, ok := cached.(pkg.CompletePackageInterface)
	if !ok || complete.Description().S != "from the cache" || cached.IsDefaultBranch() {
		t.Fatalf("cached package %v", cached)
	}

	msgs := ioi.messages()
	for _, want := range []string{
		"<warning>Skipped 0.6.0, no composer file (cached from ref ",
		"Found cached composer.json of <info>a/b</info> (<comment>1.0.0</comment>)",
	} {
		if !slices.ContainsFunc(msgs, func(m string) bool { return strings.HasPrefix(m, want) }) {
			t.Errorf("missing message %q in\n%s", want, strings.Join(msgs, "\n"))
		}
	}
}

func TestVcsRepository_NoDriver(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	r, err := NewVcsRepository(php.ArrayOf("url", dir, "type", "vcs"), io.NewNullIO(), newConfig(t, t.TempDir()).ForHTTP(), nil, nil, Options{Drivers: []NamedDriver{{"git", gitDriverType}}})
	noErr(t, err)

	_, err = r.Packages()
	expectError[*invalidArgument](t, err, "No driver found to handle VCS repository "+dir)
}

func TestVcsRepository_NoValidComposerJSON(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.email=a@b.c", "-c", "user.name=a", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %q: %v\n%s", args, err, out)
		}
	}

	r, err := NewVcsRepository(php.ArrayOf("url", repo, "type", "git"), io.NewNullIO(), newConfig(t, t.TempDir()).ForHTTP(), nil, nil, Options{})
	noErr(t, err)

	_, err = r.Packages()
	expectError[*repository.InvalidRepositoryError](t, err, "No valid composer.json was found in any branch or tag of "+repo+", could not load a package from it.")
}

func TestVcsRepository_RegisteredTypes(t *testing.T) {
	t.Parallel()

	repo := newGitFixture(t)
	cfg := newConfig(t, t.TempDir())
	rm := repository.Manager(io.NewNullIO(), cfg, nil, nil, nil, repository.ExternalTypes{VCS: NewRepository})

	for _, typ := range []string{"vcs", "git", "github", "gitlab", "bitbucket", "git-bitbucket", "hg", "svn", "fossil", "perforce"} {
		if !slices.Contains(rm.RepositoryTypes(), typ) {
			t.Errorf("type %s is not registered", typ)
		}
	}

	// as in Composer 2.10.3, "forgejo" is a driver of "vcs" repositories,
	// not a repository type
	if slices.Contains(rm.RepositoryTypes(), "forgejo") {
		t.Error("type forgejo is registered")
	}

	r, err := rm.CreateRepository("git", php.ArrayOf("type", "git", "url", repo), "")
	noErr(t, err)

	vcsRepo, ok := r.(*VcsRepository)
	if !ok || vcsRepo.Class() != VcsRepositoryClass {
		t.Fatalf("got %T", r)
	}

	if n, err := r.Count(); err != nil || n != 9 {
		t.Fatalf("count %d, %v", n, err)
	}
}

func TestVcsRepository_GitHubScan(t *testing.T) {
	t.Parallel()

	const (
		api    = "https://api.github.com/repos/composer/packagist"
		sha1   = "1111111111111111111111111111111111111111"
		sha2   = "2222222222222222222222222222222222222222"
		shaDev = "3333333333333333333333333333333333333333"
	)

	contents := func(ref, json string) httpmock.Expectation {
		return httpmock.Expectation{URL: api + "/contents/composer.json?ref=" + ref, Body: `{"encoding":"base64","content":"` + b64(json) + `"}`}
	}
	commit := func(ref string) httpmock.Expectation {
		return httpmock.Expectation{URL: api + "/commits/" + ref, Body: `{"commit": {"committer":{ "date": "2020-01-02T03:04:05Z"}}}`}
	}

	http := httpmock.New()
	http.Expects([]httpmock.Expectation{
		{URL: api, Body: `{"default_branch": "main", "owner": {"login": "composer"}, "name": "packagist", "has_issues": true}`},
		// the root identifier's composer.json
		contents("main", `{"name": "composer/packagist"}`),
		commit("main"),
		{URL: api + "/tags?per_page=100", Body: `[{"name": "v1.0.0", "commit": {"sha": "` + sha1 + `"}}, {"name": "v2.0.0", "commit": {"sha": "` + sha2 + `"}}]`},
		{URL: api + "/git/refs/heads?per_page=100", Body: `[{"ref": "refs/heads/main", "object": {"sha": "` + shaDev + `"}}, {"ref": "refs/heads/gh-pages", "object": {"sha": "` + shaDev + `"}}]`},
		{URL: api + "/contents/.github/FUNDING.yml", Status: 404},
		{URL: "https://api.github.com/repos/composer/.github/contents/FUNDING.yml", Status: 404},
		// tags
		contents(sha1, `{"name": "composer/packagist"}`),
		commit(sha1),
		{URL: api + "/contents/composer.json?ref=" + sha2, Status: 404},
		// branches
		contents(shaDev, `{"name": "composer/packagist", "time": "2021-01-01"}`),
	}, true, nil)

	ioi := newTestIO()
	ioi.veryVerbose = true

	cfg := newConfig(t, t.TempDir())
	r, err := NewVcsRepository(php.ArrayOf("url", "https://github.com/composer/packagist", "type", "github"), ioi, cfg.ForHTTP(), http, nil, Options{})
	noErr(t, err)

	got := prettyVersions(t, r)
	if !slices.Equal(got, []string{"v1.0.0", "9999999-dev", "dev-main"}) {
		t.Fatalf("versions %q", got)
	}

	assertHTTPComplete(t, http)

	tag, err := r.FindPackage("composer/packagist", nil)
	noErr(t, err)

	complete, _ := tag.(pkg.CompletePackageInterface)
	if tag.DistURL().S != api+"/zipball/"+sha1 || tag.SourceURL().S != "https://github.com/composer/packagist.git" ||
		complete.Support().Len() != 2 || pathString(complete.Support(), "source") != "https://github.com/composer/packagist/tree/v1.0.0" ||
		pathString(complete.Support(), "issues") != "https://github.com/composer/packagist/issues" {
		t.Fatalf("package %s %s %v", tag.DistURL().S, tag.SourceURL().S, complete.Support())
	}

	if e, ok := r.VersionTransportExceptions().Tags.Get("v2.0.0"); !ok || e.Code != 404 || !slices.Equal(r.EmptyReferences(), []string{sha2}) {
		t.Fatalf("transport exceptions %v, empty references %q", e, r.EmptyReferences())
	}

	if !slices.Contains(ioi.messages(), "<warning>Skipped tag v2.0.0, no composer file was found (404 HTTP status code)</warning>") {
		t.Fatalf("messages %q", ioi.messages())
	}

	if name := r.RepoName(); name != "vcs repo (github https://github.com/composer/packagist)" {
		t.Fatalf("repo name %q", name)
	}

	// composer.json of commits is cached
	cacheDir, _ := cfg.Get("cache-repo-dir", 0)
	if _, err := os.Stat(filepath.Join(cacheDir.(string), "github.com/composer/packagist", sha1)); err != nil {
		t.Fatal(err)
	}
}

func TestVcsRepository_RethrowsServerErrors(t *testing.T) {
	t.Parallel()

	api := "https://api.github.com/repos/composer/packagist"
	http := httpmock.New()
	http.Expects([]httpmock.Expectation{
		{URL: api, Body: `{"default_branch": "main", "owner": {"login": "composer"}, "name": "packagist"}`},
		{URL: api + "/contents/composer.json?ref=main", Status: 404},
		{URL: api + "/tags?per_page=100", Body: `[{"name": "v1.0.0", "commit": {"sha": "abc"}}]`},
		{URL: api + "/contents/composer.json?ref=abc", Status: 502},
	}, true, nil)

	r, err := NewVcsRepository(php.ArrayOf("url", "https://github.com/composer/packagist", "type", "github"), newTestIO(), newConfig(t, t.TempDir()).ForHTTP(), http, nil, Options{})
	noErr(t, err)

	_, err = r.Packages()

	if e, ok := asTransportError(err); !ok || e.Code != 502 {
		t.Fatalf("got %v", err)
	}
}

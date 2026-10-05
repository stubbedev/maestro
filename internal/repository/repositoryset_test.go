package repository

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// RepositorySet, PackageRepository, RepositoryFactory::configFromString
// and NameMap have no PHPUnit tests of their own in Composer; these cover
// their documented behaviour.

func TestRepositorySet_FindPackages(t *testing.T) {
	high := newArrayRepo(t, getPackage(t, "a/a", "1.0.0"), getPackage(t, "a/a", "2.0.0-beta"))
	low := newArrayRepo(t, getPackage(t, "a/a", "3.0.0"), getPackage(t, "b/b", "1.0.0"))
	composite := must(NewCompositeRepository([]RepositoryInterface{low}))

	set := must(NewRepositorySet("stable", nil, nil, nil, nil, nil))
	noErr(t, set.AddRepository(high))
	noErr(t, set.AddRepository(composite))
	if len(set.Repositories()) != 2 || set.Repositories()[1] != low {
		t.Fatal("composite repositories are added one by one")
	}

	versions := func(packages []pkg.PackageInterface) []string {
		var out []string
		for _, p := range packages {
			out = append(out, p.PrettyVersion())
		}

		return out
	}
	// the first repository holding the name shadows the others
	if got := versions(must(set.FindPackages("a/a", nil, 0))); !slices.Equal(got, []string{"1.0.0"}) {
		t.Error(got)
	}
	if got := versions(must(set.FindPackages("a/a", nil, AllowUnacceptableStabilities))); !slices.Equal(got, []string{"1.0.0", "2.0.0-beta"}) {
		t.Error(got)
	}
	if got := versions(must(set.FindPackages("a/a", nil, AllowShadowedRepositories))); !slices.Equal(got, []string{"1.0.0", "3.0.0"}) {
		t.Error(got)
	}
	if !set.IsPackageAcceptable([]string{"a/a"}, "stable") || set.IsPackageAcceptable([]string{"a/a"}, "beta") {
		t.Error("isPackageAcceptable")
	}

	noErr(t, set.LockForPool())
	if err := set.AddRepository(high); err == nil {
		t.Error("locked set accepted a repository")
	}

	installed := must(NewInstalledArrayRepository(nil))
	set = must(NewRepositorySet("dev", nil, nil, nil, nil, nil))
	noErr(t, set.AddRepository(installed))
	if _, ok := errors.AsType[*util.LogicError](set.LockForPool()); !ok {
		t.Error("installed repository accepted")
	}
	set.AllowInstalledRepositories(true)
	noErr(t, set.LockForPool())

	if _, err := NewRepositorySet("bogus", nil, nil, nil, nil, nil); err == nil {
		t.Error("bogus stability")
	}
}

func TestRepositorySet_RootData(t *testing.T) {
	requires := &ConstraintMap{}
	requires.Set("php", semver.NewMatchAllConstraint())
	requires.Set("a/a", semver.NewMatchAllConstraint())
	aliases := RootAliasesFromArray(php.ListOf(php.ArrayOf("package", "a/a", "version", "dev-main", "alias", "1.0.x-dev", "alias_normalized", "1.0.9999999.9999999-dev")))
	set := must(NewRepositorySet("beta", php.ArrayOf("a/a", 20), aliases, nil, requires, nil))

	if got := set.RootRequires().Keys(); !slices.Equal(got, []string{"a/a"}) {
		t.Error(got)
	}
	if got := set.RootAliases()["a/a"]["dev-main"]; got.Alias != "1.0.x-dev" || got.AliasNormalized != "1.0.9999999.9999999-dev" {
		t.Error(got)
	}
	if got := set.AcceptableStabilities().Keys(); len(got) != 3 {
		t.Error(got)
	}
	if !set.IsPackageAcceptable([]string{"a/a"}, "dev") {
		t.Error("stability flag")
	}
}

func TestPackageRepository_AdvisoriesAndFilter(t *testing.T) {
	config := must(php.JSONDecode(`{
		"package": {"name": "a/a", "version": "1.0.0"},
		"security-advisories": {
			"a/a": [
				{"advisoryId": "PKSA-1", "affectedVersions": ">=1.0,<1.1", "title": "T", "sources": [{"name": "GitHub", "remoteId": "GHSA"}], "reportedAt": "2024-01-02 03:04:05", "cve": "CVE-1"},
				{"advisoryId": "PKSA-2", "affectedVersions": ">=2.0-test2"},
				{"advisoryId": "PKSA-3", "affectedVersions": "<0.5", "title": "T", "sources": [], "reportedAt": "2024-01-02 03:04:05"}
			],
			"b/b": [{"advisoryId": "PKSA-4", "affectedVersions": "*"}]
		},
		"filter": {"malware": [{"package": "a/a", "constraint": "1.0.0", "reason": "bad"}, {"package": "z/z", "constraint": "*"}]}
	}`, true)).(*php.Array)
	repo := must(NewPackageRepository(config))
	if repo.RepoName() != "package repo (defining 1 package)" {
		t.Error(repo.RepoName())
	}

	constraints := NewConstraintMap("a/a", mustConstraint(t, "1.0.0"))
	if _, err := repo.SecurityAdvisories(constraints, false); err == nil {
		t.Error("partial advisory accepted")
	}
	result := must(repo.SecurityAdvisories(constraints, true))
	advisories, _ := result.Advisories.Get("a/a")
	if !slices.Equal(result.NamesFound, []string{"a/a"}) || len(advisories) != 1 {
		t.Fatalf("%+v", result)
	}
	full, ok := advisories[0].(*SecurityAdvisory)
	if !ok || full.AdvisoryID != "PKSA-1" || full.CVE.S != "CVE-1" || full.ReportedAt.Format("2006-01-02T15:04:05-07:00") != "2024-01-02T03:04:05+00:00" {
		t.Fatalf("%+v", advisories[0])
	}
	if encoded := must(php.JSONEncode(full.JSONSerialize(), php.JSONUnescapedSlashes)); encoded != `{"advisoryId":"PKSA-1","packageName":"a/a","affectedVersions":">=1.0,<1.1","title":"T","cve":"CVE-1","link":null,"reportedAt":"2024-01-02T03:04:05+00:00","sources":[{"name":"GitHub","remoteId":"GHSA"}],"severity":null}` {
		t.Error(encoded)
	}

	// an unparsable constraint is cut down to its version
	result = must(repo.SecurityAdvisories(NewConstraintMap("a/a", mustConstraint(t, "2.5.0")), true))
	advisories, _ = result.Advisories.Get("a/a")
	if len(advisories) != 1 || advisories[0].Partial().AdvisoryID != "PKSA-2" {
		t.Fatalf("%+v", advisories)
	}

	filter := must(repo.Filter(constraints, nil))
	entries, _ := filter.Get("malware")
	if len(entries) != 1 || entries[0].Reason.S != "bad" || entries[0].ListName != "malware" {
		t.Fatalf("%+v", entries)
	}
	if lists := must(repo.FilterLists()); !slices.Equal(lists, []string{"malware"}) {
		t.Error(lists)
	}

	set := must(NewRepositorySet("stable", nil, nil, nil, nil, nil))
	noErr(t, set.AddRepository(repo))
	all := must(set.GetSecurityAdvisories([]string{"b/b", "a/a"}, true, false))
	if got := all.Advisories.Keys(); !slices.Equal(got, []string{"a/a", "b/b"}) {
		t.Error(got)
	}
	matching := must(set.GetMatchingSecurityAdvisories(must(repo.Packages()), true, false))
	if got := matching.Advisories.Keys(); !slices.Equal(got, []string{"a/a"}) {
		t.Error(got)
	}
}

func TestPackageRepository_InvalidDefinition(t *testing.T) {
	repo := must(NewPackageRepository(php.ArrayOf("package", php.ArrayOf("name", "a/a"))))
	_, err := repo.Packages()
	if _, ok := errors.AsType[*InvalidRepositoryError](err); !ok {
		t.Fatal(err)
	}
}

func TestRepositoryFactory_ConfigFromString(t *testing.T) {
	if got := must(ConfigFromString("HTTPS://example.org", false, nil)); !php.StrictEquals(got, php.ArrayOf("type", "composer", "url", "HTTPS://example.org")) {
		t.Error(got)
	}
	if got := must(ConfigFromString(`{"type": "vcs", "url": "x"}`, false, nil)); !php.StrictEquals(got, php.ArrayOf("type", "vcs", "url", "x")) {
		t.Error(got)
	}
	if _, err := ConfigFromString("nope", false, nil); err == nil || err.Error() != "Invalid repository url (nope) given. Has to be a .json file, an http url or a JSON object." {
		t.Error(err)
	}

	dir := util.Realpath(t.TempDir())
	repoFile := filepath.Join(dir, "packages.json")
	noErr(t, os.WriteFile(repoFile, []byte(`{"packages": {"a/a": {}}}`), 0o644))
	if got := must(ConfigFromString(repoFile, false, nil)); !php.StrictEquals(got, php.ArrayOf("type", "composer", "url", "file://"+repoFile)) {
		t.Error(got)
	}
	fsFile := filepath.Join(dir, "installed.json")
	noErr(t, os.WriteFile(fsFile, []byte(`[{"name": "a/a", "version": "1.0.0"}]`), 0o644))
	if _, err := ConfigFromString(fsFile, false, nil); err == nil {
		t.Error("filesystem repository without allowFilesystem")
	}
	rm := Manager(nil, nil, nil, nil, util.NewProcessExecutor(nil), ExternalTypes{})
	repo := must(FromString(fsFile, true, rm))
	if fs, ok := repo.(*FilesystemRepository); !ok || count(t, fs) != 1 {
		t.Fatalf("%T", repo)
	}
}

func TestNameMap(t *testing.T) {
	m := &NameMap[int]{}
	for i := range 40 {
		m.Set(string(rune('a'+i%26))+itoa(i), i)
	}
	for i := range 30 {
		m.Delete(string(rune('a'+i%26)) + itoa(i))
	}
	m.Set("e34", 100)
	m.Set("new", 1)
	if got := m.Keys(); !slices.Equal(got, []string{"e30", "f31", "g32", "h33", "i34", "j35", "k36", "l37", "m38", "n39", "e34", "new"}) {
		t.Error(got)
	}
	if v, ok := m.Get("e34"); !ok || v != 100 {
		t.Error(v)
	}
	c := m.Clone()
	c.Delete("new")
	if !m.Has("new") || c.Has("new") || c.Len() != m.Len()-1 {
		t.Error("clone")
	}
	var nilMap *NameMap[int]
	if nilMap.Len() != 0 || nilMap.Has("x") || len(nilMap.Keys()) != 0 {
		t.Error("nil map")
	}
}

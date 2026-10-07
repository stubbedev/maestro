package locker

import (
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

// Ports tests/Composer/Test/Package/LockerTest.php.

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}

// jsonFileMock stands in for the JsonFile mocks of Composer's tests.
type jsonFileMock struct {
	exists  bool
	data    any
	reads   int
	exist   int
	written []any
}

func (f *jsonFileMock) Path() string { return "composer.lock" }
func (f *jsonFileMock) Exists() bool {
	f.exist++

	return f.exists
}

func (f *jsonFileMock) Read() (any, error) {
	f.reads++

	return f.data, nil
}

func (f *jsonFileMock) Write(hash any, _ php.JSONFlag) error {
	f.written = append(f.written, hash)

	return nil
}

type noInstallPaths struct{}

func (noInstallPaths) InstallPath(pkg.PackageInterface) (string, bool, error) { return "", false, nil }

func md5Of(s string) string {
	sum := md5.Sum([]byte(s))

	return hex.EncodeToString(sum[:])
}

// getJSONContent is LockerTest::getJsonContent.
func getJSONContent(custom map[string]string) string {
	data := php.ArrayOf("minimum-stability", "beta", "name", "test")
	for k, v := range custom {
		data.Set(k, v)
	}
	php.Ksort(data, php.SortRegular)

	return must(json.Encode(data, 0, json.IndentDefault))
}

func newLocker(t *testing.T, file repository.JSONFile, content string) *Locker {
	t.Helper()

	return must(New(mio.NewNullIO(), file, noInstallPaths{}, content, nil))
}

func getPackage(name, version string) *pkg.CompletePackage {
	return pkg.NewCompletePackage(name, must(pkg.NewVersionParser().Normalize(version)), version)
}

func TestLocker_IsLocked(t *testing.T) {
	file := &jsonFileMock{exists: true, data: php.ArrayOf("packages", php.NewArray())}
	locker := newLocker(t, file, getJSONContent(nil))

	if !must(locker.IsLocked()) {
		t.Fatal("not locked")
	}
}

func TestLocker_GetNotLockedPackages(t *testing.T) {
	file := &jsonFileMock{exists: false}
	locker := newLocker(t, file, getJSONContent(nil))

	_, err := locker.LockedRepository(false)
	if !phperr.InstanceOf(err, "LogicException") {
		t.Fatal(err)
	}
	if file.exist != 1 {
		t.Error("exists once")
	}
}

func TestLocker_GetLockedPackages(t *testing.T) {
	file := &jsonFileMock{exists: true, data: php.ArrayOf("packages", php.ListOf(
		php.ArrayOf("name", "pkg1", "version", "1.0.0-beta"),
		php.ArrayOf("name", "pkg2", "version", "0.1.10"),
	))}
	locker := newLocker(t, file, getJSONContent(nil))

	repo := must(locker.LockedRepository(false))
	for _, c := range [][2]string{{"pkg1", "1.0.0-beta"}, {"pkg2", "0.1.10"}} {
		if must(repo.FindPackage(c[0], must(repository.ParseConstraint(c[1])))) == nil {
			t.Errorf("%s", c[0])
		}
	}
	if file.exist != 1 || file.reads != 1 {
		t.Errorf("exists %d reads %d", file.exist, file.reads)
	}
}

func TestLocker_SetLockData(t *testing.T) {
	file := &jsonFileMock{}
	jsonContent := getJSONContent(nil) + "  "
	locker := newLocker(t, file, jsonContent)

	contentHash := md5Of(strings.TrimSpace(jsonContent))

	changed := must(locker.SetLockData(LockDataInput{
		Packages:          []pkg.PackageInterface{getPackage("pkg1", "1.0.0-beta"), getPackage("pkg2", "0.1.10")},
		DevPackages:       php.Some([]pkg.PackageInterface{}),
		MinimumStability:  "dev",
		PlatformOverrides: php.ArrayOf("foo/bar", "1.0"),
	}, true))
	if !changed || len(file.written) != 1 {
		t.Fatal("not written")
	}

	want := php.ArrayOf(
		"_readme", php.ListOf("This file locks the dependencies of your project to a known state",
			"Read more about it at https://getcomposer.org/doc/01-basic-usage.md#installing-dependencies",
			"This file is @gener"+"ated automatically"),
		"content-hash", contentHash,
		"packages", php.ListOf(
			php.ArrayOf("name", "pkg1", "version", "1.0.0-beta", "type", "library"),
			php.ArrayOf("name", "pkg2", "version", "0.1.10", "type", "library"),
		),
		"packages-dev", php.NewArray(),
		"aliases", php.NewArray(),
		"minimum-stability", "dev",
		"stability-flags", php.NewObject(),
		"platform", php.NewObject(),
		"platform-dev", php.NewObject(),
		"platform-overrides", php.ArrayOf("foo/bar", "1.0"),
		"prefer-stable", false,
		"prefer-lowest", false,
		"plugin-api-version", repository.PluginAPIVersion,
	)
	// PHPUnit's with() compares with ==: same keys and values, any order.
	got := must(json.Encode(file.written[0], json.DefaultEncodeFlags, json.IndentDefault))
	if !looseEqualJSON(file.written[0], want) {
		t.Fatalf("written %s", got)
	}
}

// TestLocker_SetLockDataPackagesDev checks that setLockData's ?array
// $devPackages keeps null and [] apart in the lock file: null
// (installed without --dev, Locker::setLockData($p, null, ...)) writes
// "packages-dev": null, and an empty list, even a nil slice, writes [].
func TestLocker_SetLockDataPackagesDev(t *testing.T) {
	for _, tc := range []struct {
		name string
		dev  php.Nullable[[]pkg.PackageInterface]
		want string
	}{
		{"null", php.Null[[]pkg.PackageInterface](), `"packages-dev": null,`},
		{"empty", php.Some([]pkg.PackageInterface{}), `"packages-dev": [],`},
		{"nil slice", php.Some[[]pkg.PackageInterface](nil), `"packages-dev": [],`},
		{"packages", php.Some([]pkg.PackageInterface{getPackage("dev1", "2.0.0")}), `"packages-dev": [
        {
            "name": "dev1",
            "version": "2.0.0",
            "type": "library"
        }
    ],`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := &jsonFileMock{}
			locker := newLocker(t, file, getJSONContent(nil))
			if !must(locker.SetLockData(LockDataInput{
				Packages:         []pkg.PackageInterface{getPackage("pkg1", "1.0.0")},
				DevPackages:      tc.dev,
				MinimumStability: "stable",
			}, true)) || len(file.written) != 1 {
				t.Fatal("not written")
			}
			got := must(json.Encode(file.written[0], json.DefaultEncodeFlags, json.IndentDefault))
			if !strings.Contains(got, "\n    "+tc.want+"\n") {
				t.Errorf("lock file:\n%s\nwant it to hold %s", got, tc.want)
			}
		})
	}
}

// looseEqualJSON compares two values as PHPUnit's == does on arrays whose
// objects are empty stdClasses: by their decoded JSON, keys in any order.
func looseEqualJSON(a, b any) bool {
	ja := must(json.Encode(a, 0, json.IndentDefault))
	jb := must(json.Encode(b, 0, json.IndentDefault))
	da := must(php.JSONDecode(ja, true))
	db := must(php.JSONDecode(jb, true))

	return php.LooseEquals(da, db)
}

func TestLocker_LockBadPackages(t *testing.T) {
	file := &jsonFileMock{}
	locker := newLocker(t, file, getJSONContent(nil))

	package1 := pkg.NewPackage("pkg1", "", "")

	_, err := locker.SetLockData(LockDataInput{Packages: []pkg.PackageInterface{package1}, DevPackages: php.Some([]pkg.PackageInterface{}), MinimumStability: "dev"}, true)
	if !phperr.InstanceOf(err, "LogicException") {
		t.Fatal(err)
	}
}

func TestLocker_IsFresh(t *testing.T) {
	jsonContent := getJSONContent(nil)
	for name, c := range map[string]struct {
		lock *php.Array
		want bool
	}{
		"testIsFresh":                         {php.ArrayOf("hash", md5Of(jsonContent)), true},
		"testIsFreshFalse":                    {php.ArrayOf("hash", getJSONContent(map[string]string{"name": "test2"})), false},
		"testIsFreshWithContentHash":          {php.ArrayOf("hash", md5Of(jsonContent+"  "), "content-hash", md5Of(jsonContent)), true},
		"testIsFreshWithContentHashAndNoHash": {php.ArrayOf("content-hash", md5Of(jsonContent)), true},
		"testIsFreshFalseWithContentHash": {php.ArrayOf(
			"hash", md5Of(getJSONContent(map[string]string{"name": "test2"})),
			"content-hash", md5Of(getJSONContent(map[string]string{"name": "test2"})),
		), false},
	} {
		file := &jsonFileMock{data: c.lock}
		locker := newLocker(t, file, jsonContent)
		if got := must(locker.IsFresh()); got != c.want || file.reads != 1 {
			t.Errorf("%s: %v (reads %d)", name, got, file.reads)
		}
	}
}

// Beyond Composer's tests.

func TestLocker_LockedRepositoryWithDevAndAliases(t *testing.T) {
	file := &jsonFileMock{exists: true, data: php.ArrayOf(
		"packages", php.ListOf(php.ArrayOf("name", "a/a", "version", "1.0.0")),
		"packages-dev", php.ListOf(php.ArrayOf("name", "B/b", "version", "dev-main", "extra", php.ArrayOf("branch-alias", php.ArrayOf("dev-main", "2.x-dev")))),
		"aliases", php.ListOf(php.ArrayOf("package", "b/b", "version", "dev-main", "alias", "1.0.x-dev", "alias_normalized", "1.0.9999999.9999999-dev")),
		"platform", php.ArrayOf("php", "^8.1"),
		"platform-dev", php.ArrayOf("ext-json", "*"),
		"prefer-stable", true,
	)}
	locker := newLocker(t, file, getJSONContent(nil))

	if _, err := locker.LockedRepository(false); err != nil {
		t.Fatal(err)
	}
	repo := must(locker.LockedRepository(true))
	var got []string
	for _, p := range must(repo.Packages()) {
		got = append(got, p.Name()+" "+p.PrettyVersion())
	}
	if strings.Join(got, ",") != "a/a 1.0.0,b/b 2.x-dev,b/b dev-main,b/b 1.0.x-dev" {
		t.Fatal(got)
	}
	if names := must(locker.DevPackageNames()); len(names) != 1 || names[0] != "b/b" {
		t.Fatal(names)
	}
	reqs := must(locker.PlatformRequirements(true))
	if reqs.Len() != 2 || !reqs.Has("php") || !reqs.Has("ext-json") {
		t.Fatal(reqs)
	}
	if v, ok := must3(locker.PreferStable()); !ok || !v {
		t.Error("prefer-stable")
	}
	if _, ok := must3(locker.PreferLowest()); ok {
		t.Error("prefer-lowest")
	}
	if api := must(locker.PluginAPI()); api != "1.1.0" {
		t.Error(api)
	}
	if s := must(locker.MinimumStability()); s != "stable" {
		t.Error(s)
	}
}

func must3(v, ok bool, err error) (bool, bool) {
	if err != nil {
		panic(err)
	}

	return v, ok
}

func TestLocker_MissingRequirementInfo(t *testing.T) {
	file := &jsonFileMock{exists: true, data: php.ArrayOf(
		"packages", php.ListOf(
			php.ArrayOf("name", "a/a", "version", "1.0.0"),
			php.ArrayOf("name", "r/r", "version", "2.0.0", "replace", php.ArrayOf("c/c", "1.5")),
		),
		"packages-dev", php.NewArray(),
	)}
	locker := newLocker(t, file, getJSONContent(nil))

	root := pkg.NewRootPackage("__root__", "1.0.0.0", "1.0.0")
	arrayLoader := loader.NewArrayLoader(nil, false)
	requires := must(arrayLoader.ParseLinks("__root__", "1.0.0", pkg.TypeRequire, php.ArrayOf("a/a", "^2.0", "b/b", "*", "c/c", "^2", "php", "^9", "self/v", "self.version")))
	root.SetRequires(requires)
	root.SetDevRequires(pkg.LinksOf(pkg.NewLink("__root__", "a/a", must(semver.VersionParser{}.ParseConstraints("^1.0")), pkg.TypeDevRequire, pkg.Str("^1.0"))))

	info := must(locker.MissingRequirementInfo(root, true))
	want := []string{
		`- Required package "a/a" is in the lock file as "1.0.0" but that does not satisfy your constraint "^2.0".`,
		`- Required package "b/b" is not present in the lock file.`,
		`- Required package "c/c" is in the lock file as "replaced as 1.5 by r/r 2.0.0" but that does not satisfy your constraint "^2".`,
		"This usually happens when composer files are incorrectly merged or the composer.json file is manually edited.",
		"Read more about correctly resolving merge conflicts https://getcomposer.org/doc/articles/resolving-merge-conflicts.md",
		"and prefer using the \"require\" command over editing the composer.json file directly https://getcomposer.org/doc/03-cli.md#require-r",
	}
	if strings.Join(info, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%s", strings.Join(info, "\n"))
	}
}

func TestLocker_UpdateHash(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "composer.lock")
	if err := os.WriteFile(lockPath, []byte(`{"content-hash": "x", "packages": [], "stability-flags": [], "platform": {"php": "*"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	composerJSON := filepath.Join(dir, "composer.json")
	if err := os.WriteFile(composerJSON, []byte(`{"name": "a/b"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	before := must(os.Stat(lockPath)).ModTime()

	locker := must(New(mio.NewNullIO(), must(json.NewFile(lockPath, nil, nil)), noInstallPaths{}, "{}", nil))
	if err := locker.UpdateHash(composerJSON, nil); err != nil {
		t.Fatal(err)
	}
	want := "{\n    \"content-hash\": \"" + must(GetContentHash(`{"name": "a/b"}`)) + "\",\n    \"packages\": [],\n    \"stability-flags\": {},\n    \"platform\": {\n        \"php\": \"*\"\n    }\n}\n"
	if got := string(must(os.ReadFile(lockPath))); got != want {
		t.Fatalf("%s", got)
	}
	if after := must(os.Stat(lockPath)).ModTime(); !after.Equal(before.Truncate(1e9)) {
		t.Errorf("mtime %v != %v", after, before)
	}
}

// IsFresh after LockData compares the lock file's content with what
// LockData decoded, and follows a change of it.
func TestLocker_IsFreshAfterLockData(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "composer.lock")
	composerJSON := `{"name": "a/b"}`
	fresh := `{"content-hash": "` + must(GetContentHash(composerJSON)) + `", "packages": []}`

	if err := os.WriteFile(lockPath, []byte(fresh), 0o644); err != nil {
		t.Fatal(err)
	}

	locker := must(New(mio.NewNullIO(), must(json.NewFile(lockPath, nil, nil)), noInstallPaths{}, composerJSON, nil))
	if _, err := locker.LockData(); err != nil {
		t.Fatal(err)
	}

	if !must(locker.IsFresh()) {
		t.Error("an unchanged fresh lock file is not fresh")
	}

	if err := os.WriteFile(lockPath, []byte(`{"content-hash": "other", "packages": []}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if must(locker.IsFresh()) {
		t.Error("a changed lock file is still fresh")
	}

	if err := os.WriteFile(lockPath, []byte(`{"content-hash": `), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := locker.IsFresh(); err == nil {
		t.Error("a broken lock file read without error")
	}
}

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
		t.Fatal("golden")
	}

	return decoded
}

// TestGetContentHash_Oracle compares content hashes with Composer's.
func TestGetContentHash_Oracle(t *testing.T) {
	for key, c := range readGolden(t, "testdata/oracle/contenthash.json").All() {
		c, _ := c.(*php.Array)
		doc, _ := c.GetString("json")
		want, _ := c.Get("hash")
		got, err := GetContentHash(doc)
		if s, ok := want.(string); ok {
			if err != nil || got != s {
				t.Errorf("case %v: %q %v, want %q", key, got, err, s)
			}
		} else if err == nil {
			t.Errorf("case %v: no error, want %v", key, want)
		}
	}
}

// TestLocker_Oracle compares the lock files written with Composer's
// (tools/oracle/repository/oracle.php).
func TestLocker_Oracle(t *testing.T) {
	golden := readGolden(t, "testdata/oracle/lock.json.gz")
	dir := t.TempDir()

	for key, c := range golden.All() {
		c, _ := c.(*php.Array)
		get := func(k string) any { v, _ := c.Get(k); return v }
		arr := func(k string) *php.Array { a, _ := get(k).(*php.Array); return a }

		lockPath := filepath.Join(dir, "composer.lock")
		_ = os.Remove(lockPath)
		composerJSON, _ := get("composer_json").(string)
		locker := must(New(mio.NewNullIO(), must(json.NewFile(lockPath, nil, nil)), noInstallPaths{}, composerJSON, nil))
		arrayLoader := loader.NewArrayLoader(nil, true)
		load := func(list *php.Array) []pkg.PackageInterface {
			packages := []pkg.PackageInterface{}
			for _, data := range list.All() {
				data, _ := data.(*php.Array)
				packages = append(packages, must(arrayLoader.Load(data, pkg.ClassCompletePackage)))
			}

			return packages
		}
		in := LockDataInput{
			Packages:          load(arr("packages")),
			PlatformReqs:      arr("platform"),
			PlatformDevReqs:   arr("platform_dev"),
			Aliases:           arr("aliases"),
			StabilityFlags:    arr("stability_flags"),
			PlatformOverrides: arr("platform_overrides"),
		}
		in.MinimumStability, _ = get("minimum_stability").(string)
		in.PreferStable, _ = get("prefer_stable").(bool)
		in.PreferLowest, _ = get("prefer_lowest").(bool)
		if dev := arr("dev"); dev != nil {
			in.DevPackages = php.Some(load(dev))
		}

		changed, err := locker.SetLockData(in, true)
		var changedAgain bool
		if err == nil {
			changedAgain, err = locker.SetLockData(in, true)
		}

		result := arr("result")
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
		wantLock, _ := result.GetString("lock")
		if got := string(must(os.ReadFile(lockPath))); got != wantLock {
			t.Errorf("case %v: composer.lock\ngot  %s\nwant %s", key, got, wantLock)
		}
		if want, _ := result.Get("changed"); want != changed {
			t.Errorf("case %v: changed %v", key, changed)
		}
		if want, _ := result.Get("changed_again"); want != changedAgain {
			t.Errorf("case %v: changed again %v", key, changedAgain)
		}
	}
}

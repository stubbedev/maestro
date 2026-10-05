package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/platform/platformmock"
	"github.com/stubbedev/maestro/internal/semver"
)

// Ports tests/Composer/Test/Repository/PlatformRepositoryTest.php. Its
// data providers come from testdata/providers.json
// (tools/oracle/repository/providers.php).

// resourceBundleStub is the test's ResourceBundleStub.
type resourceBundleStub struct{ version string }

func (s resourceBundleStub) Get(field any) (any, error) {
	if field != "Version" {
		return nil, fmt.Errorf("get(%v)", field)
	}

	return s.version, nil
}

// imagickStub is the test's ImagickStub.
type imagickStub struct{ versionString string }

func (s imagickStub) GetVersion() (any, error) {
	return php.ArrayOf("versionString", s.versionString), nil
}

// providerValue decodes a value of providers.json.
func providerValue(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}

	return convertProviderValue(t, v)
}

func convertProviderValue(t *testing.T, v any) any {
	t.Helper()
	switch v := v.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		f, err := v.Float64()
		if err != nil {
			t.Fatal(err)
		}

		return f
	case map[string]any:
		if pairs, ok := v["a"].([]any); ok {
			a := php.NewArray()
			for _, pair := range pairs {
				kv, _ := pair.([]any)
				a.Set(convertProviderValue(t, kv[0]), convertProviderValue(t, kv[1]))
			}

			return a
		}
		// php.Array cannot hold the stubs: they stand in as tokens
		// resolved by stub().
		if version, ok := v["rb"].(string); ok {
			return "\x00rb:" + version
		}
		if version, ok := v["imagick"].(string); ok {
			return "\x00imagick:" + version
		}
		t.Fatalf("unknown provider value %v", v)
	}

	return v
}

// provider returns the cases of a data provider of providers.json.
func provider(t *testing.T, name string) *php.Array {
	t.Helper()
	var all map[string]json.RawMessage
	if err := json.Unmarshal(must(os.ReadFile("testdata/providers.json")), &all); err != nil {
		t.Fatal(err)
	}
	a, ok := providerValue(t, all[name]).(*php.Array)
	if !ok {
		t.Fatalf("no provider %s", name)
	}

	return a
}

// stub resolves the tokens convertProviderValue puts in place of the
// ResourceBundle and Imagick stubs.
func stub(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	if version, ok := strings.CutPrefix(s, "\x00rb:"); ok {
		return resourceBundleStub{version}
	}
	if version, ok := strings.CutPrefix(s, "\x00imagick:"); ok {
		return imagickStub{version}
	}

	return v
}

func arg(c *php.Array, i int) any {
	v, _ := c.Get(i)

	return v
}

func callable(v any) platform.Callable {
	if a, ok := v.(*php.Array); ok {
		return platform.StaticMethod(php.ToString(arg(a, 0)), php.ToString(arg(a, 1)))
	}

	return platform.Func(php.ToString(v))
}

// invokeMap converts willReturnMap rows [callable, arguments, return].
func invokeMap(rows *php.Array) []platformmock.InvokeEntry {
	var entries []platformmock.InvokeEntry
	for _, row := range rows.All() {
		r, _ := row.(*php.Array)
		args, _ := arg(r, 1).(*php.Array)
		entries = append(entries, platformmock.InvokeEntry{Callable: callable(arg(r, 0)), Arguments: args.Values(), Return: stub(arg(r, 2))})
	}

	return entries
}

func newPlatformRepo(t *testing.T, runtime platform.Runtime, hhvm platform.HhvmVersionDetector) *PlatformRepository {
	t.Helper()

	return must(NewPlatformRepository(nil, nil, PlatformOptions{Runtime: runtime, HhvmDetector: hhvm}))
}

func findPackage(t *testing.T, r RepositoryInterface, name string) pkg.PackageInterface {
	t.Helper()

	return must(r.FindPackage(name, semver.NewMatchAllConstraint()))
}

func TestPlatformRepository_HhvmPackage(t *testing.T) {
	has, get := platformmock.ConstantMap(map[string]any{"PHP_VERSION": "7.1.0"})
	runtime := &platformmock.Runtime{HasConstantFunc: has, GetConstantFunc: get}
	repo := newPlatformRepo(t, runtime, platformmock.HhvmDetector{Version: "2.1.0"})

	hhvm := findPackage(t, repo, "hhvm")
	if hhvm == nil || hhvm.PrettyVersion() != "2.1.0" {
		t.Fatal(hhvm)
	}
}

func TestPlatformRepository_PhpVersion(t *testing.T) {
	for i, c := range provider(t, "PlatformRepositoryTest::providePhpFlavorTestCases").All() {
		c, _ := c.(*php.Array)
		constants := map[string]any{}
		for k, v := range arg(c, 0).(*php.Array).All() {
			constants[k.String()] = v
		}
		has, get := platformmock.ConstantMap(constants)
		runtime := &platformmock.Runtime{HasConstantFunc: has, GetConstantFunc: get}
		if functions, ok := arg(c, 2).(*php.Array); ok {
			runtime.InvokeFunc = platformmock.InvokeMap(invokeMap(functions))
		}

		repo := newPlatformRepo(t, runtime, nil)
		for name, version := range arg(c, 1).(*php.Array).All() {
			p := findPackage(t, repo, name.String())
			if p == nil {
				t.Errorf("case %v: Expected to find package %q", i, name)

				continue
			}
			if p.PrettyVersion() != version {
				t.Errorf("case %v: Expected package %q version to be %v, got %s", i, name, version, p.PrettyVersion())
			}
		}
	}
}

func TestPlatformRepository_InetPtonRegression(t *testing.T) {
	has, get := platformmock.ConstantMap(map[string]any{"PHP_VERSION": "7.0.0", "PHP_DEBUG": false})
	_ = has
	runtime := &platformmock.Runtime{
		// suppressing PHP_ZTS & AF_INET6
		HasConstantFunc: func(string, string) bool { return false },
		GetConstantFunc: get,
		InvokeFunc: func(c platform.Callable, args []any) (any, error) {
			if c != platform.Func("inet_pton") || !slices.Equal(args, []any{"::"}) {
				t.Errorf("invoke %v %v", c, args)
			}

			return false, nil
		},
	}
	repo := newPlatformRepo(t, runtime, nil)
	if p := findPackage(t, repo, "php-ipv6"); p != nil {
		t.Fatal(p)
	}
	if n := len(runtime.CallsTo("invoke")); n != 1 {
		t.Fatalf("invoke called %d times", n)
	}
}

func TestPlatformRepository_LibraryInformation(t *testing.T) {
	const extensionVersion = "100.200.300"

	for caseName, c := range provider(t, "PlatformRepositoryTest::provideLibraryTestCases").All() {
		c, _ := c.(*php.Array)
		var extensions []string
		switch e := arg(c, 0).(type) {
		case string:
			extensions = []string{e}
		case *php.Array:
			for _, v := range e.All() {
				extensions = append(extensions, php.ToString(v))
			}
		}
		info, _ := arg(c, 1).(string)
		expectations, _ := arg(c, 2).(*php.Array)
		expectations = expectations.Clone()
		functions, _ := arg(c, 3).(*php.Array)
		constantRows, _ := arg(c, 4).(*php.Array)
		classDefinitions, _ := arg(c, 5).(*php.Array)

		type constant struct{ name, class string }
		constants := map[constant]any{{"PHP_VERSION", ""}: "7.1.0"}
		if constantRows != nil {
			for _, row := range constantRows.All() {
				r, _ := row.(*php.Array)
				constants[constant{php.ToString(arg(r, 0)), php.ToString(arg(r, 1))}] = arg(r, 2)
			}
		}

		runtime := &platformmock.Runtime{
			GetExtensionsFunc: func() []string { return extensions },
			GetExtensionVersionFunc: func(e string) string {
				if slices.Contains(extensions, e) {
					return extensionVersion
				}

				return ""
			},
			GetExtensionInfoFunc: func(e string) (string, error) {
				if slices.Contains(extensions, e) {
					return info, nil
				}

				return "", nil
			},
			HasConstantFunc: func(name, class string) bool {
				_, ok := constants[constant{name, class}]

				return ok
			},
			GetConstantFunc: func(name, class string) (any, error) {
				return constants[constant{name, class}], nil
			},
			HasClassFunc: func(class string) bool {
				if classDefinitions == nil {
					return false
				}
				for _, row := range classDefinitions.All() {
					if arg(row.(*php.Array), 0) == class {
						return true
					}
				}

				return false
			},
			ConstructFunc: func(class string, args []any) (any, error) {
				if classDefinitions == nil {
					return nil, nil
				}
				for _, row := range classDefinitions.All() {
					r, _ := row.(*php.Array)
					wantArgs, _ := arg(r, 1).(*php.Array)
					if arg(r, 0) == class && slices.Equal(wantArgs.Values(), args) {
						return stub(arg(r, 2)), nil
					}
				}

				return nil, nil
			},
		}
		if functions != nil {
			runtime.InvokeFunc = platformmock.InvokeMap(invokeMap(functions))
		}

		repo := newPlatformRepo(t, runtime, nil)

		var libraries []string
		for _, result := range must(repo.Search("lib", SearchName, "")) {
			if strings.HasPrefix(result.Name, "lib-") {
				libraries = append(libraries, result.Name)
			}
		}
		var expectedLibraries []string
		for name, expectation := range expectations.All() {
			if expectation != false {
				expectedLibraries = append(expectedLibraries, name.String())
			}
		}
		if len(libraries) != len(expectedLibraries) {
			t.Errorf("%s: Expected: %v, got %v", caseName, expectedLibraries, libraries)
		}

		for _, extension := range extensions {
			expectations.Set("ext-"+extension, extensionVersion)
		}

		for name, expectation := range expectations.All() {
			packageName := name.String()
			expected, ok := expectation.(*php.Array)
			if !ok {
				expected = php.ListOf(expectation)
			}
			expectedVersion := arg(expected, 0)
			expectedReplaces, ok := arg(expected, 1).(*php.Array)
			if !ok {
				expectedReplaces = php.NewArray()
			}
			expectedProvides, ok := arg(expected, 2).(*php.Array)
			if !ok {
				expectedProvides = php.NewArray()
			}

			p := findPackage(t, repo, packageName)
			if expectedVersion == false {
				if p != nil {
					t.Errorf("%s: Expected to not find package %q", caseName, packageName)
				}

				continue
			}
			if p == nil {
				t.Errorf("%s: Expected to find package %q", caseName, packageName)

				continue
			}
			if p.PrettyVersion() != expectedVersion {
				t.Errorf("%s: Expected version %v for %s, got %s", caseName, expectedVersion, packageName, p.PrettyVersion())
			}
			assertPackageLinks(t, caseName.String()+" replaces", expectedReplaces, p, p.Replaces())
			assertPackageLinks(t, caseName.String()+" provides", expectedProvides, p, p.Provides())
		}
	}
}

func assertPackageLinks(t *testing.T, context string, expectedLinks *php.Array, source pkg.PackageInterface, links pkg.Links) {
	t.Helper()
	if expectedLinks.Len() != links.Len() {
		t.Errorf("%s: expected package count to match: %d != %d", context, expectedLinks.Len(), links.Len())
	}
	for link := range links.Values() {
		if link.Source() != source.Name() {
			t.Errorf("%s: source %s", context, link.Source())
		}
		if !php.InArray(link.Target(), expectedLinks, false) {
			t.Errorf("%s: package %s not in %v", context, link.Target(), expectedLinks.Values())
		}
		if !link.Constraint().Matches(semver.NewConstraintOp(semver.OpEQ, source.Version())) {
			t.Errorf("%s: constraint %s", context, link.Constraint())
		}
	}
}

func TestPlatformRepository_ComposerPlatformVersion(t *testing.T) {
	_, get := platformmock.ConstantMap(map[string]any{"PHP_VERSION": "7.0.0", "PHP_DEBUG": false})
	repo := newPlatformRepo(t, &platformmock.Runtime{GetConstantFunc: get}, nil)

	if p := must(repo.FindPackage("composer", mustConstraint(t, "="+ComposerVersion))); p == nil {
		t.Fatal("Composer package exists")
	}
}

func TestPlatformRepository_ValidPlatformPackages(t *testing.T) {
	for _, c := range provider(t, "PlatformRepositoryTest::providePlatformPackages").All() {
		c, _ := c.(*php.Array)
		name := php.ToString(arg(c, 0))
		if got := IsPlatformPackage(name); got != arg(c, 1) {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// Beyond Composer's tests: config.platform overrides, disabled packages
// and the missing-php path.

func TestPlatformRepository_Overrides(t *testing.T) {
	has, get := platformmock.ConstantMap(map[string]any{"PHP_VERSION": "8.2.1", "PHP_INT_SIZE": int64(8)})
	runtime := &platformmock.Runtime{
		HasConstantFunc: has, GetConstantFunc: get,
		GetExtensionsFunc:       func() []string { return []string{"json", "mbstring"} },
		GetExtensionVersionFunc: func(string) string { return "8.2.1" },
	}
	overrides := php.ArrayOf("php", "8.1.0", "ext-mbstring", false, "ext-foo", "1.2")
	repo := must(NewPlatformRepository(nil, overrides, PlatformOptions{Runtime: runtime}))

	p, ok := findPackage(t, repo, "php").(pkg.CompletePackageInterface)
	if !ok || p.PrettyVersion() != "8.1.0" || p.Description().S != "Package overridden via config.platform, actual: 8.2.1" {
		t.Fatalf("php %v", p)
	}
	p64, ok := findPackage(t, repo, "php-64bit").(pkg.CompletePackageInterface)
	if !ok || p64.PrettyVersion() != "8.1.0" || p64.Description().S != "Package overridden via config.platform, actual: 8.2.1" {
		t.Fatalf("php-64bit %v", p64)
	}
	if findPackage(t, repo, "ext-mbstring") != nil || !repo.IsPlatformPackageDisabled("ext-mbstring") {
		t.Fatal("ext-mbstring")
	}
	disabled, _ := repo.DisabledPackages().Get("ext-mbstring")
	if disabled.Description().S != "The mbstring PHP extension. <warning>Package disabled via config.platform</warning>" {
		t.Error(disabled.Description().S)
	}
	if foo := findPackage(t, repo, "ext-foo"); foo == nil || foo.PrettyVersion() != "1.2" || !foo.IsPlatform() {
		t.Fatal("ext-foo")
	}
	if v, ok := repo.PlatformPhpVersion(); !ok || v != "8.1.0" {
		t.Error(v)
	}

	_, err := NewPlatformRepository(nil, php.ArrayOf("php", false), PlatformOptions{Runtime: runtime})
	if err == nil || err.Error() != "config.platform.php cannot be set to false as you cannot disable php entirely." {
		t.Error(err)
	}
	_, err = NewPlatformRepository(nil, php.ArrayOf("ext-foo", int64(1)), PlatformOptions{Runtime: runtime})
	if err == nil || err.Error() != "config.platform.ext-foo should be a string or false, but got int 1" {
		t.Error(err)
	}
	repo = must(NewPlatformRepository(nil, php.ArrayOf("foo/bar", "1.0"), PlatformOptions{Runtime: runtime}))
	if _, err := repo.Packages(); err == nil || err.Error() != "Invalid platform package name in config.platform: foo/bar" {
		t.Error(err)
	}
}

func TestPlatformRepository_WithoutPHP(t *testing.T) {
	repo := must(NewPlatformRepository(nil, php.ArrayOf("php", "8.1.0"), PlatformOptions{}))
	if got := names(must(repo.Packages())); !slices.Equal(got, []string{"php", "composer", "composer-plugin-api", "composer-runtime-api"}) {
		t.Fatal(got)
	}

	repo = must(NewPlatformRepository(nil, nil, PlatformOptions{}))
	if _, err := repo.Packages(); err == nil {
		t.Fatal("expected an error without php")
	}
}

// TestPlatformRepository_Oracle compares the platform packages with those
// Composer's PlatformRepository lists for six php builds
// (internal/platform/testdata/oracle).
func TestPlatformRepository_Oracle(t *testing.T) {
	files := must(filepath.Glob("../platform/testdata/oracle/*.json"))
	if len(files) == 0 {
		t.Fatal("no goldens")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			decoded := must(php.JSONDecode(string(must(os.ReadFile(file))), true))
			golden, _ := decoded.(*php.Array)
			want, ok := golden.GetArray("platform_packages")
			if !ok {
				t.Skip("no platform_packages in this golden")
			}
			probe, _ := golden.GetString("probe")
			snapshot := must(platform.ParseSnapshot("/usr/bin/php", []byte(probe)))
			view, skipped := snapshot.ComposerView(func(string) (string, bool) { return "", false })

			repo := must(NewPlatformRepository(nil, nil, PlatformOptions{Runtime: platform.NewRuntime(view), SkippedXdebugVersion: skipped}))
			got := php.NewArray()
			for _, p := range must(repo.Packages()) {
				c, _ := p.(pkg.CompletePackageInterface)
				got.Append(php.ArrayOf(
					"name", p.Name(),
					"version", p.Version(),
					"pretty_version", p.PrettyVersion(),
					"description", c.Description().Value(),
					"type", p.Type(),
					"replaces", linksArray(p.Replaces()),
					"provides", linksArray(p.Provides()),
				))
			}
			if !php.LooseEquals(got, want) || !php.StrictEquals(must(php.JSONEncode(got, 0)), must(php.JSONEncode(want, 0))) {
				t.Errorf("got  %s\nwant %s", must(php.JSONEncode(got, php.JSONPrettyPrint)), must(php.JSONEncode(want, php.JSONPrettyPrint)))
			}
		})
	}
}

func linksArray(links pkg.Links) *php.Array {
	a := php.NewArray()
	for k, l := range links.All() {
		a.Set(k, php.ListOf(l.Source(), l.Target(), l.Description(), l.RawPrettyConstraint().Value()))
	}

	return a
}

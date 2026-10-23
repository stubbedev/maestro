package composer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/filter"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/repository/composerrepo"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// Every Installer field is either read by resolutionSettings or named in
// settingsLeftOut, never both: a new setting cannot be left out of the
// resolution cache's key unnoticed.
func TestResolutionSettingsCovered(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "resolutioncache.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	read := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "resolutionSettings" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "i" {
					read[sel.Sel.Name] = true
				}
			}

			return true
		})
	}
	if len(read) == 0 {
		t.Fatal("resolutionSettings reads no field")
	}

	fields := reflect.TypeFor[Installer]()
	for f := range fields.Fields() {
		leftOut := slices.Contains(settingsLeftOut, f.Name)
		switch {
		case read[f.Name] && leftOut:
			t.Errorf("Installer.%s is both read by resolutionSettings and in settingsLeftOut", f.Name)
		case !read[f.Name] && !leftOut:
			t.Errorf("Installer.%s is neither read by resolutionSettings nor in settingsLeftOut", f.Name)
		}
	}
	for _, name := range settingsLeftOut {
		if _, ok := fields.FieldByName(name); !ok {
			t.Errorf("settingsLeftOut names %s, which Installer does not have", name)
		}
	}
}

// keyInstaller is an installer of a project whose lock file holds lock,
// with a console of the given verbosity.
type keyInstaller struct {
	installer *Installer
	platform  *repository.PlatformRepository
	cio       *io.ConsoleIO
}

func newKeyInstaller(t *testing.T, lock string, cfg map[string]any, platformPHP string, verbosity int) keyInstaller {
	t.Helper()
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "composer.lock")
	if err := os.WriteFile(lockPath, []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	cio := io.NewConsoleIO(must(console.NewStringInput("")), console.NewBufferedOutput(verbosity, false, nil), nil)
	lockFile := must(json.NewFile(lockPath, nil, cio))
	l := must(locker.New(cio, lockFile, nil, "{}", nil))

	c := config.New(false, keyHome)
	settings := php.ArrayOf("home", keyHome, "cache-dir", filepath.Join(keyHome, "cache"))
	for k, v := range cfg {
		settings.Set(k, v)
	}
	if err := c.Merge(php.ArrayOf("config", settings), "test"); err != nil {
		t.Fatal(err)
	}

	root := pkg.NewRootPackage("a/project", "1.0.0.0", "1.0.0")
	installer := must(NewInstaller(InstallerDeps{
		IO:                cio,
		Config:            c,
		Package:           root,
		RepositoryManager: repository.NewRepositoryManager(cio, c, nil, nil, nil),
		Locker:            l,
		Resolutions:       t.TempDir(),
	}))
	platform := must(repository.NewPlatformRepository(nil, php.ArrayOf("php", platformPHP), repository.PlatformOptions{}))

	return keyInstaller{installer: installer, platform: platform, cio: cio}
}

func (k keyInstaller) key(t *testing.T) string {
	t.Helper()
	key, ok := k.installer.resolutionKey(k.platform, nil, k.cio)
	if !ok {
		t.Fatal("no key")
	}

	return string(key)
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}

// keyHome is the home of every keyInstaller's configuration: a
// project's configuration names its home, the same for every update.
var keyHome = filepath.Join(os.TempDir(), "maestro-resolution-key-test")

const keyLock = `{"content-hash": "abc", "packages": [], "packages-dev": []}`

// The key changes with each input of a resolution, and only with them.
func TestResolutionKey(t *testing.T) {
	base := func(t *testing.T) keyInstaller {
		return newKeyInstaller(t, keyLock, map[string]any{"sort-packages": true}, "8.4.0", console.VerbosityNormal)
	}
	want := base(t).key(t)
	if again := base(t).key(t); again != want {
		t.Fatal("the same inputs give another key")
	}

	for name, change := range map[string]func(t *testing.T) keyInstaller{
		"a flag": func(t *testing.T) keyInstaller {
			k := base(t)
			k.installer.preferLowest = true

			return k
		},
		"the packages allowed to update": func(t *testing.T) keyInstaller {
			k := base(t)
			k.installer.updateAllowList = []string{"vendor/package"}

			return k
		},
		"the platform requirement filter": func(t *testing.T) keyInstaller {
			k := base(t)
			k.installer.platformRequirementFilter = filter.IgnoreAllFilter()

			return k
		},
		"a lock entry": func(t *testing.T) keyInstaller {
			return newKeyInstaller(t, `{"content-hash": "abd", "packages": [], "packages-dev": []}`, map[string]any{"sort-packages": true}, "8.4.0", console.VerbosityNormal)
		},
		"the configuration": func(t *testing.T) keyInstaller {
			return newKeyInstaller(t, keyLock, map[string]any{"sort-packages": false}, "8.4.0", console.VerbosityNormal)
		},
		"the platform": func(t *testing.T) keyInstaller {
			return newKeyInstaller(t, keyLock, map[string]any{"sort-packages": true}, "8.3.0", console.VerbosityNormal)
		},
		"the verbosity": func(t *testing.T) keyInstaller {
			return newKeyInstaller(t, keyLock, map[string]any{"sort-packages": true}, "8.4.0", console.VerbosityVerbose)
		},
		"a root requirement": func(t *testing.T) keyInstaller {
			k := base(t)
			k.installer.pkg.SetRequires(pkg.LinksOf(pkg.NewLink("a/project", "b/b", nil, pkg.TypeRequire, pkg.Str("^1.0"))))

			return k
		},
		"a Composer variable": func(t *testing.T) keyInstaller {
			t.Setenv("COMPOSER_PREFER_DEV_OVER_PRERELEASE", "1")

			return base(t)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if change(t).key(t) == want {
				t.Error("the key did not change")
			}
		})
	}
}

// An entry reads back as written, for the same key only, and a truncated
// one does not read.
func TestResolutionEntry(t *testing.T) {
	cio := io.NewConsoleIO(must(console.NewStringInput("")), console.NewBufferedOutput(console.VerbosityNormal, false, nil), nil)
	rec := cio.Record()
	cio.WriteError("<info>Updating dependencies</info>", true, io.Normal)
	output, _ := rec.Stop()
	entry := &resolutionEntry{output: output, files: [][]composerrepo.MetadataFile{{
		{URL: "https://example.org/packages.json", CacheKey: "packages.json", Root: true, ID: fsstate.ID{Dev: 1, Ino: 2}},
		{URL: "https://example.org/p2/a/a.json", CacheKey: "provider-a~a.json", LastModified: "Tue", ID: fsstate.ID{Dev: 1, Ino: 3, Size: 9}},
		{URL: "https://example.org/p2/b/b.json", CacheKey: "provider-b~b.json", NotFound: true},
	}}}
	b := appendResolutionEntry(nil, entry)
	got, err := decodeResolutionEntry(b, 1)
	if err != nil || !reflect.DeepEqual(got.files, entry.files) || !got.output.Equal(output) {
		t.Fatalf("read back %+v, %v", got, err)
	}
	if _, err := decodeResolutionEntry(b, 2); err == nil {
		t.Error("read for another number of repositories")
	}
	for n := range b {
		if _, err := decodeResolutionEntry(b[:n], 1); err == nil {
			t.Fatalf("%d of %d bytes read", n, len(b))
		}
	}
}

// An update keeps its resolution at the default verbosity only below -v,
// whose output times the resolution.
func TestKeepResolution_Verbosity(t *testing.T) {
	for verbosity, want := range map[int]bool{
		console.VerbosityQuiet:   true,
		console.VerbosityNormal:  true,
		console.VerbosityVerbose: false,
		console.VerbosityDebug:   false,
	} {
		k := newKeyInstaller(t, keyLock, nil, "8.4.0", verbosity)
		if got := k.installer.keepResolution(k.platform) != nil; got != want {
			t.Errorf("verbosity %d: kept %v, want %v", verbosity, got, want)
		}
	}
}

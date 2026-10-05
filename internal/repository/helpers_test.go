package repository

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/semver"
)

// The helpers of Composer\Test\TestCase.

var testParser = pkg.NewVersionParser()

func normalize(t testing.TB, version string) string {
	t.Helper()
	v, err := testParser.Normalize(version)
	if err != nil {
		t.Fatal(err)
	}

	return v
}

// getPackage is TestCase::getPackage with CompletePackage.
func getPackage(t testing.TB, name, version string) *pkg.CompletePackage {
	t.Helper()

	return pkg.NewCompletePackage(name, normalize(t, version), version)
}

// getRootPackage is TestCase::getRootPackage.
func getRootPackage(t testing.TB, name, version string) *pkg.RootPackage {
	t.Helper()

	return pkg.NewRootPackage(name, normalize(t, version), version)
}

// getAliasPackage is TestCase::getAliasPackage.
func getAliasPackage(t testing.TB, p pkg.PackageInterface, version string) pkg.Alias {
	t.Helper()
	switch p := p.(type) {
	case *pkg.RootPackage:
		return pkg.NewRootAliasPackage(p, normalize(t, version), version)
	case *pkg.CompletePackage:
		return pkg.NewCompleteAliasPackage(p, normalize(t, version), version)
	}

	return pkg.NewAliasPackage(p, normalize(t, version), version)
}

// configureLinks is TestCase::configureLinks: config maps link types
// (require, provide, ...) to target => constraint.
func configureLinks(t testing.TB, p pkg.PackageInterface, config map[string][][2]string) {
	t.Helper()
	arrayLoader := loader.NewArrayLoader(nil, false)
	for _, lt := range pkg.SupportedLinkTypes() {
		pairs, ok := config[lt.Type]
		if !ok {
			continue
		}
		links := php.NewArray()
		for _, pair := range pairs {
			links.Set(pair[0], pair[1])
		}
		parsed, err := arrayLoader.ParseLinks(p.Name(), p.PrettyVersion(), lt.Method, links)
		if err != nil {
			t.Fatal(err)
		}
		setter, ok := p.(interface {
			SetRequires(pkg.Links)
			SetDevRequires(pkg.Links)
			SetProvides(pkg.Links)
			SetReplaces(pkg.Links)
			SetConflicts(pkg.Links)
		})
		if !ok {
			if c, isComplete := pkg.AsPackage(p); isComplete {
				setter = c
			} else {
				t.Fatalf("cannot set links on %T", p)
			}
		}
		switch lt.Method {
		case pkg.TypeRequire:
			setter.SetRequires(parsed)
		case pkg.TypeDevRequire:
			setter.SetDevRequires(parsed)
		case pkg.TypeProvide:
			setter.SetProvides(parsed)
		case pkg.TypeReplace:
			setter.SetReplaces(parsed)
		case pkg.TypeConflict:
			setter.SetConflicts(parsed)
		}
	}
}

func mustConstraint(t testing.TB, s string) semver.ConstraintInterface {
	t.Helper()
	c, err := ParseConstraint(s)
	if err != nil {
		t.Fatal(err)
	}

	return c
}

// must returns v, panicking (failing the test) on err.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}

func names(packages []pkg.PackageInterface) []string {
	out := make([]string, len(packages))
	for i, p := range packages {
		out[i] = p.Name()
	}

	return out
}

var (
	_ pkg.RootPackageInterface     = (*pkg.RootAliasPackage)(nil)
	_ InstalledRepositoryInterface = (*InstalledFilesystemRepository)(nil)
)

func noErr(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

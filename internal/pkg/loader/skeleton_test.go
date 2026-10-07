package loader

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// skeletonVersion is a version of a package with a bit of everything.
func skeletonVersion() *php.Array {
	v, err := php.JSONDecode(`{
		"name": "Acme/Pkg", "version": "2.x-dev", "version_normalized": "2.9999999.9999999.9999999-dev",
		"type": "Library", "description": "d", "keywords": ["k"], "homepage": "https://h",
		"license": "MIT", "authors": [{"name": "a"}], "time": "2024-05-06T07:08:09+00:00",
		"source": {"type": "git", "url": "https://s", "reference": "abc"},
		"dist": {"type": "zip", "url": "https://d", "reference": "abc", "shasum": ""},
		"require": {"php": ">=8.1", "Acme/Other": "self.version"}, "require-dev": {"x/y": "^1"},
		"conflict": {"c/d": "<1"}, "provide": {"p/q": "1.0"}, "replace": {"r/s": "self.version"},
		"suggest": {"x/z": "self.version"}, "autoload": {"psr-4": {"Acme\\": "src/"}},
		"extra": {"branch-alias": {"2.x-dev": "2.1-dev"}, "other": 1}, "bin": ["bin/x"],
		"support": {"issues": "i"}, "funding": [{"url": "f"}], "abandoned": "a/b",
		"scripts": {"post-install-cmd": "x"}, "notification-url": "https://n",
		"transport-options": {"o": 1}, "archive": {"name": "n", "exclude": ["e"]}
	}`, true)
	if err != nil {
		panic(err)
	}

	return v.(*php.Array)
}

// A version loads as a skeleton, unless its rest cannot be loaded later
// alike.
func TestSkeletonFits(t *testing.T) {
	l := NewArrayLoader(nil, false)
	if !l.SkeletonChecker().Fits(skeletonVersion()) {
		t.Fatal("a regular version does not fit")
	}

	for name, change := range map[string]func(v *php.Array){
		"reserved script":   func(v *php.Array) { v.Set("scripts", php.ArrayOf("php", "x")) },
		"relative date":     func(v *php.Array) { v.Set("time", "yesterday noon") },
		"date of this year": func(v *php.Array) { v.Set("time", "May 6") },
		"bad dist":          func(v *php.Array) { v.Set("dist", "x") },
		"bad options":       func(v *php.Array) { v.Set("transport-options", "x") },
		"bad bin":           func(v *php.Array) { v.Set("bin", php.ListOf(1)) },
	} {
		v := skeletonVersion()
		change(v)
		if l.SkeletonChecker().Fits(v) {
			t.Errorf("%s: fits", name)
		}
	}

	// a version that fails to load leaves the checker usable
	checker := l.SkeletonChecker()
	bad := skeletonVersion()
	bad.Set("dist", "x")
	if checker.Fits(bad) || !checker.Fits(skeletonVersion()) {
		t.Error("the checker is broken by a version that does not load")
	}
}

// A skeleton has the solver's properties at once, and the others when
// first used.
func TestLoadSkeleton(t *testing.T) {
	l := NewArrayLoader(nil, false)
	full := skeletonVersion()
	loaded := 0
	p, err := l.Batch().LoadSkeleton(SkeletonConfig(full), func() *php.Array { loaded++; return full })
	if err != nil {
		t.Fatal(err)
	}
	alias, ok := p.(*pkg.CompleteAliasPackage)
	if !ok || alias.Version() != "2.1.9999999.9999999-dev" {
		t.Fatalf("got %T %s, want the branch alias", p, p.Version())
	}
	cp, _ := pkg.AsCompletePackage(alias.AliasOf())
	if cp.Type() != "library" || cp.Requires().Len() != 2 || !cp.IsAbandoned() || !pkg.IsSkeleton(cp) || loaded != 0 {
		t.Fatalf("skeleton: type %q, %d requires, abandoned %v, skeleton %v, loaded %d", cp.Type(), cp.Requires().Len(), cp.IsAbandoned(), pkg.IsSkeleton(cp), loaded)
	}
	if cp.Description().S != "d" || loaded != 1 || pkg.IsSkeleton(cp) {
		t.Fatalf("description %q, loaded %d", cp.Description().S, loaded)
	}

	eager, err := l.Batch().Load(full)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := l.Batch().LoadSkeleton(SkeletonConfig(full), func() *php.Array { return full })
	if !pkg.SkeletonMatches(again, eager) {
		t.Error("the completed skeleton is not the package Load builds")
	}
}

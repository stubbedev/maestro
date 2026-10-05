package resolver

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
)

// Tests for behaviour Composer's DependencyResolver suite covers only
// through the installer fixtures.

// recordingDispatcher handles PRE_POOL_CREATE as a plugin would: it drops
// the packages of one name.
type recordingDispatcher struct {
	events []*eventdispatcher.PrePoolCreateEvent
	drop   string
}

func (d *recordingDispatcher) Dispatch(eventName string, event eventdispatcher.Event) (int, error) {
	e := event.(*eventdispatcher.PrePoolCreateEvent)
	d.events = append(d.events, e)
	var kept []pkg.PackageInterface
	for _, p := range e.Packages() {
		if p.Name() != d.drop {
			kept = append(kept, p)
		}
	}
	e.SetPackages(kept)

	return 0, nil
}

func TestPoolBuilder_PrePoolCreateEvent(t *testing.T) {
	a := getPackage(t, "a/a", "1.0")
	b1 := getPackage(t, "b/b", "1.0")
	b2 := getPackage(t, "b/b", "2.0")
	requires(a, newLink("a/a", "b/b", parseConstraints(t, "*"), pkg.TypeRequire))

	set, err := repository.NewRepositorySet("stable", nil, []repository.RootAlias{{Package: "a/a", Version: "1.0.0.0", Alias: "2.0", AliasNormalized: "2.0.0.0"}}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	addRepository(t, set, newArrayRepository(t, a, b1, b2))

	request := NewRequest(nil)
	requireName(t, request, "a/a", nil)

	dispatcher := &recordingDispatcher{drop: "b/b"}
	pool, err := CreatePool(set, request, nullIO, CreatePoolOptions{EventDispatcher: dispatcher})
	if err != nil {
		t.Fatal(err)
	}

	if len(dispatcher.events) != 1 {
		t.Fatalf("%d events", len(dispatcher.events))
	}
	e := dispatcher.events[0]
	if e.Name() != eventdispatcher.PrePoolCreate || e.Request() != any(request) {
		t.Fatalf("event %s %v", e.Name(), e.Request())
	}
	aliases := e.RootAliases()
	versions, _ := aliases.GetArray("a/a")
	alias, _ := versions.GetArray("1.0.0.0")
	if got, _ := alias.GetString("alias_normalized"); got != "2.0.0.0" {
		t.Fatalf("root aliases %v", alias)
	}

	// the listener's selection is the pool: a/a and its root alias
	if pool.Count() != 2 || len(pool.WhatProvides("b/b", nil)) != 0 {
		t.Fatalf("pool %s", pool)
	}
}

func TestLockTransaction_Helpers(t *testing.T) {
	present := getPackage(t, "a/a", "1.0")
	present.SetSourceType(pkg.Str("git"))
	present.SetSourceReference(pkg.Str("1111111111111111111111111111111111111111"))
	present.SetDistType(pkg.Str("zip"))
	present.SetDistReference(pkg.Str("1111111111111111111111111111111111111111"))
	present.SetDistURL(pkg.Str("https://api.github.com/repos/a/a/zipball/1111111111111111111111111111111111111111"))

	result := getPackage(t, "a/a", "1.0")
	result.SetSourceType(pkg.Str("git"))
	result.SetSourceURL(pkg.Str("https://github.com/a/a-mirror.git"))
	result.SetSourceReference(pkg.Str("2222222222222222222222222222222222222222"))
	result.SetDistType(pkg.Str("zip"))
	result.SetDistReference(pkg.Str("2222222222222222222222222222222222222222"))
	result.SetDistURL(pkg.Str("https://api.github.com/repos/a/a-mirror/zipball/2222222222222222222222222222222222222222"))
	dev := getPackage(t, "d/d", "1.0")
	alias := getAliasPackage(t, result, "1.1")

	pool := NewPool([]pkg.PackageInterface{result, dev, alias}, nil, nil)
	decisions := NewDecisions(pool)
	for _, p := range []pkg.PackageInterface{result, dev, alias} {
		if err := decisions.Decide(int32(p.ID()), 1, NewGenericRule(nil, RuleLearned, 0)); err != nil {
			t.Fatal(err)
		}
	}

	transaction, err := NewLockTransaction(pool, []pkg.PackageInterface{present}, nil, decisions)
	if err != nil {
		t.Fatal(err)
	}

	// the result packages are the decisions, last first
	if got := transaction.all; !slices.Equal(got, []pkg.PackageInterface{alias, dev, result}) {
		t.Fatalf("result packages %v", got)
	}

	// updating mirrors keeps the present package, with the new source URL
	// and the dist URL rewritten to the present reference
	packages, err := transaction.NewLockPackages(false, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(packages, []pkg.PackageInterface{dev, present}) {
		t.Fatalf("lock packages %v", packages)
	}
	if present.SourceURL().S != "https://github.com/a/a-mirror.git" || present.DistURL().S != "https://api.github.com/repos/a/a-mirror/zipball/1111111111111111111111111111111111111111" {
		t.Fatalf("mirrors not updated: %v %v", present.SourceURL(), present.DistURL())
	}

	// the extraction (solved without dev requirements) found only a/a
	extraction, err := NewLockTransaction(NewPool([]pkg.PackageInterface{result}, nil, nil), nil, nil, func() *Decisions {
		d := NewDecisions(NewPool([]pkg.PackageInterface{result}, nil, nil))
		_ = d.Decide(1, 1, NewGenericRule(nil, RuleLearned, 0))

		return d
	}())
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.SetNonDevPackages(extraction); err != nil {
		t.Fatal(err)
	}
	if nonDev, _ := transaction.NewLockPackages(false, false); !slices.Equal(nonDev, []pkg.PackageInterface{result}) {
		t.Fatalf("non-dev %v", nonDev)
	}
	if devPackages, _ := transaction.NewLockPackages(true, false); !slices.Equal(devPackages, []pkg.PackageInterface{dev}) {
		t.Fatalf("dev %v", devPackages)
	}

	aliases := php.ListOf(
		php.ArrayOf("package", "z/z", "version", "1.0.0.0", "alias", "2.0", "alias_normalized", "2.0.0.0"),
		php.ArrayOf("package", "a/a", "version", "1.0.0.0", "alias", "1.1", "alias_normalized", "1.1.0.0"),
	)
	used := transaction.Aliases(aliases)
	if used.Len() != 1 {
		t.Fatalf("aliases %v", used)
	}
	if _, first, _ := used.First(); first.(*php.Array).Len() != 4 {
		t.Fatalf("aliases %v", used)
	} else if name, _ := first.(*php.Array).GetString("package"); name != "a/a" {
		t.Fatalf("alias of %q", name)
	}
}

func TestOperation_Formats(t *testing.T) {
	initial := getPackage(t, "a/a", "dev-master")
	initial.SetSourceType(pkg.Str("git"))
	initial.SetSourceReference(pkg.Str("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	target := getPackage(t, "a/a", "dev-master")
	target.SetSourceType(pkg.Str("git"))
	target.SetSourceReference(pkg.Str("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	older := getPackage(t, "b/b", "2.0")
	newer := getPackage(t, "b/b", "1.0")
	alias := getAliasPackage(t, newer, "1.1")

	for _, c := range []struct {
		op   operation.Operation
		lock bool
		want string
	}{
		{operation.NewInstallOperation(newer), false, "Installing <info>b/b</info> (<comment>1.0</comment>)"},
		{operation.NewInstallOperation(newer), true, "Locking <info>b/b</info> (<comment>1.0</comment>)"},
		{operation.NewUninstallOperation(newer), true, "Removing <info>b/b</info> (<comment>1.0</comment>)"},
		{operation.NewUpdateOperation(older, newer), false, "Downgrading <info>b/b</info> (<comment>2.0</comment> => <comment>1.0</comment>)"},
		{operation.NewUpdateOperation(initial, target), false, "Upgrading <info>a/a</info> (<comment>dev-master aaaaaaa</comment> => <comment>dev-master bbbbbbb</comment>)"},
		{operation.NewMarkAliasInstalledOperation(alias), false, "Marking <info>b/b</info> (<comment>1.1</comment>) as installed, alias of <info>b/b</info> (<comment>1.0</comment>)"},
		{operation.NewMarkAliasUninstalledOperation(alias), false, "Marking <info>b/b</info> (<comment>1.1</comment>) as uninstalled, alias of <info>b/b</info> (<comment>1.0</comment>)"},
	} {
		got, err := c.op.Show(c.lock)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.op.OperationType(), got, c.want)
		}
		if !c.lock && c.op.String() != c.want {
			t.Errorf("%s: String() %q", c.op.OperationType(), c.op.String())
		}
	}
}

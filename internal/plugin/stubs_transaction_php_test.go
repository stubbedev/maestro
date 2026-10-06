package plugin

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
)

// A LockTransaction of maestro's solver as PHP sees it: Composer's
// protected properties (its own and Transaction's $resultPackagesByName,
// with uasort()'s keys), the protected helpers, getNewLockPackages(),
// getAliases() and setNonDevPackages(), whose change reaches PHP's
// properties.
func TestShimStubs_LockTransaction(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	a := loadPackage(t, `{"name": "a/a", "version": "1.0.0", "require": {"b/b": "^1.0"}}`)
	b := loadPackage(t, `{"name": "b/b", "version": "1.0.0", "provide": {"c/c": "1.0"}}`)
	repo, err := repository.NewArrayRepository([]pkg.PackageInterface{a, b})
	if err != nil {
		t.Fatal(err)
	}
	set, err := repository.NewRepositorySet("stable", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.AddRepository(repo); err != nil {
		t.Fatal(err)
	}
	req := resolver.NewRequest(nil)
	if err := req.RequireName("a/a", nil); err != nil {
		t.Fatal(err)
	}
	req.FixPackage(b)
	pool, err := resolver.CreatePool(set, req, io.NewNullIO(), resolver.CreatePoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	lt, err := resolver.NewSolver(resolver.NewDefaultPolicy(false, false, nil), pool, io.NewNullIO()).Solve(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The extraction of a run without dev requirements, here requiring
	// nothing: every package of lt becomes dev.
	empty := resolver.NewRequest(nil)
	emptyPool, err := resolver.CreatePool(set, empty, io.NewNullIO(), resolver.CreatePoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	extraction, err := resolver.NewSolver(resolver.NewDefaultPolicy(false, false, nil), emptyPool, io.NewNullIO()).Solve(empty, nil)
	if err != nil {
		t.Fatal(err)
	}

	got := evalPHP(t, rt, `
		$t = $vars['t'];
		$names = function (array $packages) {
			return implode(',', array_map(function ($p) { return $p->getName(); }, $packages));
		};
		$read = function () use ($names) {
			$byName = [];
			foreach ($this->resultPackagesByName as $name => $packages) {
				$byName[] = $name.':'.implode('', array_keys($packages));
			}

			return [
				'all '.$names($this->resultPackages['all']),
				'non-dev '.$names($this->resultPackages['non-dev']),
				'dev '.$names($this->resultPackages['dev']),
				'present '.$names($this->presentMap).' '.var_export(array_keys($this->presentMap) === array_map('spl_object_id', array_values($this->presentMap)), true),
				'unlockable '.$names($this->unlockableMap).' '.implode(',', array_map(function ($p) { return $p->getId(); }, $this->unlockableMap)).'='.implode(',', array_keys($this->unlockableMap)),
				'by name '.implode(' ', $byName),
				'providers '.$names($this->getProvidersInResult(new \Composer\Package\Link('x/x', 'c/c', new \Composer\Semver\Constraint\MatchAllConstraint()))).'|'.$names($this->getProvidersInResult(new \Composer\Package\Link('x/x', 'd/d', new \Composer\Semver\Constraint\MatchAllConstraint()))),
				'roots '.$names($this->getRootPackages()),
				'calculated '.count($this->calculateOperations()).' of '.count($this->getOperations()),
			];
		};
		$out = \Closure::bind($read, $t, \Composer\DependencyResolver\LockTransaction::class)();
		$out[] = 'class '.get_class($t);
		$out[] = 'lock packages '.$names($t->getNewLockPackages(false)).'|'.$names($t->getNewLockPackages(true));
		$out[] = 'aliases '.count($t->getAliases([['package' => 'a/a', 'version' => '1.0.0.0', 'alias' => '2.0', 'alias_normalized' => '2.0.0.0']]));
		$t->setNonDevPackages($vars['extraction']);
		$after = \Closure::bind($read, $t, \Composer\DependencyResolver\LockTransaction::class)();
		$out[] = 'after '.$after[1].' '.$after[2];

		return $out;
	`, php.ArrayOf("t", rt.value(lt), "extraction", rt.value(extraction)))

	all, _, _ := lt.ResultPackages()
	var allNames []string
	for _, p := range all {
		allNames = append(allNames, p.Name())
	}
	want := strings.Join([]string{
		"all " + strings.Join(allNames, ","),
		"non-dev a/a",
		"dev ",
		"present b/b true",
		"unlockable b/b " + php.ToString(b.ID()) + "=" + php.ToString(b.ID()),
		"by name " + byNameWant(allNames),
		"providers b/b|",
		"roots a/a",
		"calculated 1 of 1",
		`class Composer\DependencyResolver\LockTransaction`,
		"lock packages a/a|",
		"aliases 0",
		"after non-dev  dev a/a",
	}, "\n")
	if g := lines(got); g != want {
		t.Errorf("got\n%s\nwant\n%s", g, want)
	}
}

// byNameWant is $resultPackagesByName's names (b/b provides c/c), each
// with its one package at key 0, for result packages in this order.
func byNameWant(allNames []string) string {
	var out []string
	for _, n := range allNames {
		out = append(out, n+":0")
		if n == "b/b" {
			out = append(out, "c/c:0")
		}
	}

	return strings.Join(out, " ")
}

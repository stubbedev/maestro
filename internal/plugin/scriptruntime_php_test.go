package plugin

import (
	"testing"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
)

// Each makeAutoloader builds a new ClassLoader in PHP, also from parts of
// the contents PHP had already (which maestro does not send again).
func TestScriptRuntime_InstallAutoloader(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	contents := func(class, prefix string) *eventdispatcher.LoaderContents {
		c := &eventdispatcher.LoaderContents{
			VendorDir: "/p/vendor",
			Psr4:      php.ArrayOf(prefix, php.ListOf("/p/src")),
		}
		if class != "" {
			c.ClassMap = php.ArrayOf(class, "/p/lib/"+class+".php")
		}

		return c
	}
	loader := `
		$p = new \ReflectionProperty(\Maestro\Shim\Dispatch::class, 'loader');
		if (PHP_VERSION_ID < 80100) {
			$p->setAccessible(true); // reads no private property otherwise
		}
		$l = $p->getValue();
		$registered = in_array([$l, 'loadClass'], spl_autoload_functions(), true);
		$new = !in_array($l, isset($GLOBALS['loaders']) ? $GLOBALS['loaders'] : [], true);
		$GLOBALS['loaders'][] = $l;
		return [$new, implode(',', array_keys($l->getClassMap())), implode(',', array_keys($l->getPrefixesPsr4())), $registered];
	`

	for i, tc := range []struct{ class, prefix string }{
		{"A", `App\`},
		{"A", `App\`}, // the same
		{"A", `Dev\`}, // the same class map
		{"B", `Dev\`}, // the same psr-4
		{"", `Dev\`},  // no class map
		{"B", `Dev\`}, // a class map again
	} {
		if err := rt.InstallAutoloader(contents(tc.class, tc.prefix)); err != nil {
			t.Fatal(err)
		}
		got := evalPHP(t, rt, loader, nil).(*php.Array).Values()
		if got[0] != true || got[1] != tc.class || got[2] != tc.prefix || got[3] != true {
			t.Errorf("install %d: PHP's loader (new, class map, psr-4, registered) %v", i, got)
		}
	}
}

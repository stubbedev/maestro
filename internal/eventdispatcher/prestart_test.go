package eventdispatcher

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// prestartRuntime is the fake runtime, counting prestarts.
type prestartRuntime struct {
	*fakeRuntime
	prestarts int
}

func (r *prestartRuntime) Prestart() { r.prestarts++ }

// Expect starts PHP ahead only for events whose root scripts run in PHP.
func TestExpect(t *testing.T) {
	scripts := php.ArrayOf(
		"post-autoload-dump", php.ListOf(`App\Scripts::dumped`, "@php artisan package:discover"),
		"post-install-cmd", php.ListOf("@auto-scripts"),
		"auto-scripts", php.ListOf(`App\Console\CacheCommand`),
		"post-update-cmd", php.ListOf("@php -v", "echo done", "@composer validate"),
		"pre-install-cmd", php.ListOf(`Composer\Config::disableProcessTimeout`),
		"loop-a", php.ListOf("@loop-b"),
		"loop-b", php.ListOf("@loop-a"),
	)
	for _, tc := range []struct {
		name       string
		events     []string
		runScripts bool
		want       int
	}{
		{"Class::method", []string{"pre-update-cmd", "post-autoload-dump"}, true, 1},
		{"command class through @script", []string{"post-install-cmd"}, true, 1},
		{"processes only", []string{"post-update-cmd"}, true, 0},
		{"native script", []string{"pre-install-cmd"}, true, 0},
		{"no scripts", []string{"pre-update-cmd"}, true, 0},
		{"reference cycle", []string{"loop-a"}, true, 0},
		{"scripts off", []string{"post-autoload-dump"}, false, 0},
	} {
		composer := createComposerInstance()
		composer.root.SetScripts(scripts)
		d := newDispatcher(t, composer, newRecordingIO(), processmock.New())
		rt := &prestartRuntime{fakeRuntime: newFakeRuntime()}
		d.SetScriptRuntime(rt)
		d.SetRunScripts(tc.runScripts)

		Expect(d, tc.events...)
		if rt.prestarts != tc.want {
			t.Errorf("%s: %d prestarts, want %d", tc.name, rt.prestarts, tc.want)
		}
	}

	// A runtime that cannot start ahead, or another dispatcher: nothing.
	composer := createComposerInstance()
	composer.root.SetScripts(scripts)
	d := newDispatcher(t, composer, newRecordingIO(), processmock.New())
	d.SetScriptRuntime(newFakeRuntime())
	Expect(d, "post-autoload-dump")
	Expect(struct{}{}, "post-autoload-dump")
}

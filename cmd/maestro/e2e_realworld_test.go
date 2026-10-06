//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
)

// realWorldScenarios are real projects. Their composer.json files declare
// plugins (symfony/flex, pestphp/pest-plugin, php-http/discovery), which
// the plugin phases have not landed yet, so every step runs with
// --no-plugins (both tools); scripts call the projects' own consoles
// (artisan, bin/console) or generate random keys, so they run with
// --no-scripts.
func realWorldScenarios() []scenario {
	noPlugins := []string{"--no-plugins", "--no-scripts"}
	with := func(args ...string) []string { return append(args, noPlugins...) }

	return []scenario{
		{
			name: "laravel",
			steps: []step{
				{args: with("create-project", "laravel/laravel", "app", "v13.10.1"), dir: "."},
				{args: with("install"), dir: "app", setup: removeDir("app/vendor")},
				{args: with("dump-autoload", "-o"), dir: "app"},
				{args: with("show"), dir: "app"},
				{args: with("update", "--dry-run"), dir: "app"},
			},
		},
		{
			name: "symfony",
			steps: []step{
				{args: with("create-project", "symfony/skeleton:v7.4.99", "app"), dir: "."},
				{args: with("require", "symfony/webapp-pack"), dir: "app"},
				{args: with("install"), dir: "app", setup: removeDir("app/vendor")},
				{args: with("dump-autoload", "--classmap-authoritative"), dir: "app"},
				{args: with("why", "symfony/http-kernel"), dir: "app"},
			},
		},
		{
			name:  "private-app",
			setup: privateAppProject,
			skip: func() string {
				if dir := privateAppDir(); dir == "" {
					return "no private application checkout (MAESTRO_E2E_PRIVATE_APP)"
				} else if _, err := os.Stat(filepath.Join(dir, "composer.lock")); err != nil {
					return "no private application checkout at " + dir + " (MAESTRO_E2E_PRIVATE_APP)"
				}

				return ""
			},
			steps: []step{
				// the project requires php ^8.5 and ext-mongodb
				{args: with("install", "--ignore-platform-reqs")},
				{args: with("install", "--ignore-platform-reqs", "--no-dev"), setup: removeVendor},
				{args: with("show", "--tree")},
				{args: with("licenses")},
				{args: with("validate")},
			},
		},
	}
}

// removeDir returns a setup deleting a directory under the scenario root.
func removeDir(rel string) func(*testing.T, string) {
	return func(t *testing.T, root string) {
		t.Helper()

		if err := os.RemoveAll(filepath.Join(root, rel)); err != nil {
			t.Fatal(err)
		}
	}
}

// privateAppDir is the checkout of a private Laravel application, a
// project with private repositories and many plugins.
func privateAppDir() string {
	return os.Getenv("MAESTRO_E2E_PRIVATE_APP")
}

// privateAppPrivate are the private application's packages that come from
// private repositories (a private fork, the developer's own tools); they
// are dropped, as the resolver oracle dropped them.
func privateAppPrivate(name string) bool {
	return name == "spiritix/lada-cache" || strings.HasPrefix(name, "stubbedev/")
}

// privateAppProject copies the private application's composer.json and
// composer.lock (read-only) into the project, without the private
// packages and repositories, and with the lock's content-hash recomputed,
// plus the committed sources its autoload configuration scans.
func privateAppProject(t *testing.T, root string) {
	t.Helper()

	project := filepath.Join(root, "project")

	archive := exec.Command("sh", "-c", `git -C "$1" archive HEAD app database template/providers tests tools/lint | tar -x -C "$2"`, "sh", privateAppDir(), project)
	if out, err := archive.CombinedOutput(); err != nil {
		t.Fatalf("copying the private application's sources: %v\n%s", err, out)
	}

	decode := func(name string) *php.Array {
		data, err := os.ReadFile(filepath.Join(privateAppDir(), name))
		if err != nil {
			t.Fatal(err)
		}

		v, err := php.JSONDecode(string(data), true)
		if err != nil {
			t.Fatal(err)
		}

		return v.(*php.Array)
	}

	cj := decode("composer.json")

	if repos, ok := cj.GetArray("repositories"); ok {
		kept := php.NewArray()

		for _, r := range repos.All() {
			if repo, ok := r.(*php.Array); ok {
				if typ, _ := repo.GetString("type"); typ == "package" {
					kept.Append(repo)
				}
			}
		}

		cj.Set("repositories", kept)
	}

	for _, section := range []string{"require", "require-dev"} {
		if reqs, ok := cj.GetArray(section); ok {
			for _, k := range reqs.Keys() {
				if privateAppPrivate(k.String()) {
					reqs.DeleteKey(k)
				}
			}
		}
	}

	composerJSON, err := json.EncodeDefault(cj)
	if err != nil {
		t.Fatal(err)
	}

	composerJSON += "\n"

	lock := decode("composer.lock")

	for _, section := range []string{"packages", "packages-dev"} {
		if packages, ok := lock.GetArray(section); ok {
			kept := php.NewArray()

			for _, p := range packages.All() {
				name, _ := p.(*php.Array).GetString("name")
				if !privateAppPrivate(name) {
					kept.Append(p)
				}
			}

			lock.Set(section, kept)
		}
	}

	if aliases, ok := lock.GetArray("aliases"); ok {
		kept := php.NewArray()

		for _, a := range aliases.All() {
			name, _ := a.(*php.Array).GetString("package")
			if !privateAppPrivate(name) {
				kept.Append(a)
			}
		}

		lock.Set("aliases", kept)
	}

	if flags, ok := lock.GetArray("stability-flags"); ok {
		for _, k := range flags.Keys() {
			if privateAppPrivate(k.String()) {
				flags.DeleteKey(k)
			}
		}

		if flags.Len() == 0 {
			lock.Set("stability-flags", php.NewObject())
		}
	}

	hash, err := locker.GetContentHash(composerJSON)
	if err != nil {
		t.Fatal(err)
	}

	lock.Set("content-hash", hash)

	lockJSON, err := json.EncodeDefault(lock)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(project, "composer.json"), []byte(composerJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(project, "composer.lock"), []byte(lockJSON+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

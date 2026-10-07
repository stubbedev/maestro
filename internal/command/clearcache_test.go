// Ports tests/Composer/Test/Command/ClearCacheCommandTest.php.

package command_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util"
)

// clearCacheTester isolates the caches: Composer's test clears the real
// ones of the user running it.
func clearCacheTester(t *testing.T) *commandtest.ApplicationTester {
	t.Helper()
	home := commandtest.UniqueTmpDirectory(t)
	util.PutEnv("COMPOSER_HOME", home)
	util.PutEnv("COMPOSER_CACHE_DIR", home+"/cache")
	util.PutEnv("MAESTRO_CACHE_DIR", home+"/maestro")
	t.Cleanup(func() {
		// --no-cache triggers the env to change so make sure the env is cleaned up after these tests run
		util.ClearEnv("COMPOSER_CACHE_DIR")
		util.ClearEnv("COMPOSER_HOME")
		util.ClearEnv("MAESTRO_CACHE_DIR")
	})

	return commandtest.GetApplicationTester(t)
}

func runCommandSuccessfully(t *testing.T, appTester *commandtest.ApplicationTester, kv ...any) string {
	t.Helper()
	code, err := appTester.RunArgs(commandtest.Options{}, kv...)
	if err != nil || code != 0 {
		t.Fatalf("command failed: %d %v\n%s", code, err, appTester.Display(true))
	}

	return appTester.Display(true)
}

func TestClearCacheCommand_Success(t *testing.T) {
	output := runCommandSuccessfully(t, clearCacheTester(t), "command", "clear-cache")
	if !strings.Contains(output, "All caches cleared.") {
		t.Errorf("output %q", output)
	}
}

func TestClearCacheCommand_WithOptionGarbageCollection(t *testing.T) {
	output := runCommandSuccessfully(t, clearCacheTester(t), "command", "clear-cache", "--gc", true)
	if !strings.Contains(output, "All caches garbage-collected.") {
		t.Errorf("output %q", output)
	}
}

func TestClearCacheCommand_WithOptionNoCache(t *testing.T) {
	output := runCommandSuccessfully(t, clearCacheTester(t), "command", "clear-cache", "--no-cache", true)
	if !strings.Contains(output, "Cache is not enabled") {
		t.Errorf("output %q", output)
	}
}

// clear-cache removes maestro's decoded repository metadata with the
// repository cache, and --gc ages it with cache-ttl, without output of its
// own.
func TestClearCacheCommand_DecodedMetadata(t *testing.T) {
	for _, gc := range []bool{false, true} {
		appTester := clearCacheTester(t)
		home, _ := util.GetEnv("COMPOSER_HOME")
		if err := os.MkdirAll(home+"/cache/repo", 0o777); err != nil {
			t.Fatal(err)
		}
		slots := home + "/maestro/p2/v1"
		if err := os.MkdirAll(slots, 0o777); err != nil {
			t.Fatal(err)
		}
		past := time.Now().AddDate(-1, 0, 0)
		for _, name := range []string{"old.bin", "new.bin"} {
			if err := os.WriteFile(slots+"/"+name, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chtimes(slots+"/old.bin", past, past); err != nil {
			t.Fatal(err)
		}

		var output string
		if gc {
			output = runCommandSuccessfully(t, appTester, "command", "clear-cache", "--gc", true)
		} else {
			output = runCommandSuccessfully(t, appTester, "command", "clear-cache")
		}
		if strings.Contains(output, "p2") || strings.Contains(output, home+"/maestro") {
			t.Errorf("gc=%v: output %q", gc, output)
		}
		_, errOld := os.Stat(slots + "/old.bin")
		_, errNew := os.Stat(slots + "/new.bin")
		if !os.IsNotExist(errOld) || gc == os.IsNotExist(errNew) {
			t.Errorf("gc=%v: old %v, new %v", gc, errOld, errNew)
		}
	}
}

// A full clear-cache removes every one of maestro's own caches but the
// store (pruned, not removed) with the Composer cache directory it
// follows, without output of its own; --gc leaves the fresh ones.
func TestClearCacheCommand_OwnCaches(t *testing.T) {
	for _, gc := range []bool{false, true} {
		appTester := clearCacheTester(t)
		home, _ := util.GetEnv("COMPOSER_HOME")
		for _, dir := range []string{"/cache/repo", "/cache/files", "/cache/vcs"} {
			if err := os.MkdirAll(home+dir, 0o777); err != nil {
				t.Fatal(err)
			}
		}
		for _, o := range cache.Owned() {
			if o.Path == "store/v1" {
				continue
			}
			p := filepath.Join(home, "maestro", filepath.FromSlash(o.Path), "entry")
			if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		args := []any{"command", "clear-cache"}
		if gc {
			args = append(args, "--gc", true)
		}
		output := runCommandSuccessfully(t, appTester, args...)
		if strings.Contains(output, home+"/maestro") {
			t.Errorf("gc=%v: output %q", gc, output)
		}
		for _, o := range cache.Owned() {
			if o.Path == "store/v1" {
				continue
			}
			_, err := os.Stat(filepath.Join(home, "maestro", filepath.FromSlash(o.Path)))
			if gone := os.IsNotExist(err); gone == gc {
				t.Errorf("gc=%v: %s removed %v", gc, o.Path, gone)
			}
		}
	}
}

// cacheDirKeys are the cache directories clear-cache handles, in its
// order, with their directory under cache-dir ("" is cache-dir itself).
var cacheDirKeys = []struct{ key, dir string }{
	{"cache-vcs-dir", "vcs"}, {"cache-repo-dir", "repo"}, {"cache-files-dir", "files"}, {"cache-dir", ""},
}

// cacheFixture is clearCacheTester's caches holding, in each cache
// directory, an entry used a year ago ("old") and one used now ("new"),
// and a project with composer.json (nil: none; a string: its content).
// It returns the tester and cache-dir.
func cacheFixture(t *testing.T, composerJSON *string) (*commandtest.ApplicationTester, string) {
	t.Helper()
	dir := commandtest.InitTempDir(t)
	if composerJSON != nil {
		if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(*composerJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tester := clearCacheTester(t)
	home, _ := util.GetEnv("COMPOSER_HOME")
	cacheDir := filepath.Join(home, "cache")
	past := time.Now().AddDate(-1, 0, 0)
	for _, d := range cacheDirKeys {
		for _, age := range []string{"old", "new"} {
			// vcs mirrors are directories, the other entries files
			p := filepath.Join(cacheDir, d.dir, age)
			if d.dir == "vcs" {
				p = filepath.Join(p, "HEAD")
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if age == "old" {
				for _, q := range []string{p, filepath.Join(cacheDir, d.dir, age)} {
					if err := os.Chtimes(q, past, past); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}

	return tester, cacheDir
}

// cacheLines are clear-cache's lines on stderr: verb (Clearing or
// Garbage-collecting) for each of keys, then the final line.
func cacheLines(cacheDir, verb, last string, keys ...string) string {
	var b strings.Builder
	for _, d := range cacheDirKeys {
		if slices.Contains(keys, d.key) {
			b.WriteString(verb + " cache (" + d.key + "): " + filepath.Join(cacheDir, d.dir) + "\n")
		}
	}

	return b.String() + last + "\n"
}

// cacheEntries reports which of the fixture's entries remain, as
// "<dir>/<age>" ("/old" for cache-dir's own).
func cacheEntries(t *testing.T, cacheDir string) []string {
	t.Helper()
	var left []string
	for _, d := range cacheDirKeys {
		for _, age := range []string{"old", "new"} {
			if _, err := os.Stat(filepath.Join(cacheDir, d.dir, age)); err == nil {
				left = append(left, d.dir+"/"+age)
			}
		}
	}

	return left
}

// TestClearCacheCommand_Caches: what clear-cache writes (stderr only)
// and leaves of each cache, by option and configuration. --gc removes
// the entries unused for cache-files-ttl or cache-ttl (six months by
// default) and leaves cache-dir alone; cache-read-only reports the
// clearing and deletes nothing; it needs no project. (An invalid
// composer.json fails it: the errors oracle's clear-cache-invalid-json.)
func TestClearCacheCommand_Caches(t *testing.T) {
	all := []string{"cache-vcs-dir", "cache-repo-dir", "cache-files-dir", "cache-dir"}
	every := []string{"vcs/old", "vcs/new", "repo/old", "repo/new", "files/old", "files/new", "/old", "/new"}
	cleared := "All caches cleared."
	readOnly := `{"config": {"cache-read-only": true}}`
	for _, c := range []struct {
		name         string
		composerJSON *string
		args         []any
		stderr       func(cacheDir string) string
		left         []string
	}{
		{"clear", nil, nil, func(d string) string { return cacheLines(d, "Clearing", cleared, all...) }, nil},
		{
			"gc", nil,
			[]any{"--gc", true},
			func(d string) string {
				return cacheLines(d, "Garbage-collecting", "All caches garbage-collected.", all[:3]...)
			},
			[]string{"vcs/new", "repo/new", "files/new", "/old", "/new"},
		},
		{"read-only", &readOnly, nil, func(d string) string { return cacheLines(d, "Clearing", cleared, all...) }, every},
	} {
		t.Run(c.name, func(t *testing.T) {
			tester, cacheDir := cacheFixture(t, c.composerJSON)
			got := tester.RunStreams(append([]any{"command", "clear-cache"}, c.args...)...)
			if want := (commandtest.Streams{Stderr: c.stderr(cacheDir)}); got != want {
				t.Errorf("got %+v\nwant %+v", got, want)
			}
			if left := cacheEntries(t, cacheDir); !slices.Equal(left, c.left) {
				t.Errorf("left %q, want %q", left, c.left)
			}
		})
	}
}

// TestClearCacheCommand_MissingDirectories: a cache directory that does
// not exist is reported with an empty path (realpath()'s false); startup
// creates cache-dir itself.
func TestClearCacheCommand_MissingDirectories(t *testing.T) {
	commandtest.InitTempDir(t)
	got := clearCacheTester(t).RunStreams("command", "clear-cache")
	home, _ := util.GetEnv("COMPOSER_HOME")
	var want strings.Builder
	for _, d := range cacheDirKeys[:3] {
		want.WriteString("Cache directory does not exist (" + d.key + "): \n")
	}
	want.WriteString(cacheLines(filepath.Join(home, "cache"), "Clearing", "All caches cleared.", "cache-dir"))
	if got != (commandtest.Streams{Stderr: want.String()}) {
		t.Errorf("got %+v\nwant stderr %q", got, want.String())
	}
}

// TestClearCacheCommand_Aliases: every alias runs the command.
func TestClearCacheCommand_Aliases(t *testing.T) {
	for _, alias := range command.NewClearCacheCommand().Aliases() {
		t.Run(alias, func(t *testing.T) {
			commandtest.InitTempDir(t)
			got := clearCacheTester(t).RunStreams("command", alias)
			if got.Code != 0 || got.Err != nil || !strings.HasSuffix(got.Stderr, "All caches cleared.\n") {
				t.Errorf("%s: %+v", alias, got)
			}
		})
	}
}

// TestClearCacheCommand_NonNumericTTL: --gc reads the TTLs as Composer's
// Config does, (int) cast, so a value that is no number is 0, not an
// error (unlike cache-files-maxsize: the errors oracle's
// clear-cache-gc-maxsize-invalid).
func TestClearCacheCommand_NonNumericTTL(t *testing.T) {
	for _, key := range []string{"cache-files-ttl", "cache-ttl"} {
		t.Run(key, func(t *testing.T) {
			composerJSON := `{"config": {"` + key + `": "lots"}}`
			tester, cacheDir := cacheFixture(t, &composerJSON)
			got := tester.RunStreams("command", "clear-cache", "--gc", true)
			want := commandtest.Streams{Stderr: cacheLines(cacheDir, "Garbage-collecting", "All caches garbage-collected.", "cache-vcs-dir", "cache-repo-dir", "cache-files-dir")}
			if got != want {
				t.Errorf("got %+v\nwant %+v", got, want)
			}
		})
	}
}

// TestClearCacheCommand_Store: a full clear empties maestro's package
// store (it holds the files cache extracted); --gc prunes the releases
// unused for cache-files-ttl and keeps the others.
func TestClearCacheCommand_Store(t *testing.T) {
	for _, gc := range []bool{false, true} {
		t.Run(fmt.Sprint("gc=", gc), func(t *testing.T) {
			tester, _ := cacheFixture(t, nil)
			s, err := store.Open(cache.Store(), nil)
			if err != nil {
				t.Fatal(err)
			}
			tree := t.TempDir()
			if err := os.WriteFile(filepath.Join(tree, "a.php"), []byte("<?php\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			old, fresh := [32]byte{1}, [32]byte{2}
			if _, err := s.InsertDir(old, tree); err != nil {
				t.Fatal(err)
			}
			past := time.Now().AddDate(-1, 0, 0)
			if err := filepath.WalkDir(cache.Store(), func(p string, _ os.DirEntry, err error) error {
				if err != nil {
					return err
				}

				return os.Chtimes(p, past, past)
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.InsertDir(fresh, tree); err != nil {
				t.Fatal(err)
			}

			args := []any{"command", "clear-cache"}
			if gc {
				args = append(args, "--gc", true)
			}
			if got := tester.RunStreams(args...); got.Code != 0 || got.Err != nil {
				t.Fatalf("run: %+v", got)
			}
			s, err = store.Open(cache.Store(), nil)
			if err != nil {
				t.Fatal(err)
			}
			for id, want := range map[[32]byte]bool{old: false, fresh: gc} {
				if _, err := s.LookupNamed(id); (err == nil) != want {
					t.Errorf("release %x kept %v, want %v", id[0], err == nil, want)
				}
			}
		})
	}
}

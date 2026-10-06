package vcs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// fakeGit puts a binary named git (a copy of the test binary: what it
// prints does not matter, the tests never run it) first in PATH, and
// trusts its fresh times.
func fakeGit(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	git := filepath.Join(dir, "git")
	if err := os.WriteFile(git, data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	margin := versionTrustMargin
	t.Cleanup(func() { versionTrustMargin = margin })
	versionTrustMargin = -time.Hour

	return git
}

func useVersionCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	UseVersionCache(dir)
	SetVersion("", false)
	t.Cleanup(func() {
		UseVersionCache("")
		SetVersion("", false)
	})

	return dir
}

// The version of the git a ProcessExecutor runs is taken from an earlier
// run's entry, without running git.
func TestVersionCache_KeptVersionUsed(t *testing.T) {
	fakeGit(t)
	useVersionCache(t)
	process := util.NewProcessExecutor(nil)
	path := versionCachePath(process)
	if path == "" {
		t.Fatal("no entry for a git binary")
	}
	storeVersion(path, "9.8.7")
	version, ok, err := GetVersion(process)
	if err != nil || !ok || version != "9.8.7" {
		t.Errorf("GetVersion = %q, %v, %v; want the kept 9.8.7", version, ok, err)
	}
}

// A test's mock never reads or writes entries: it is asked for the
// version.
func TestVersionCache_NotForMocks(t *testing.T) {
	fakeGit(t)
	dir := useVersionCache(t)
	storeVersion(versionCachePath(util.NewProcessExecutor(nil)), "9.8.7")
	process := processmock.New()
	process.Expects([]processmock.Expectation{{Cmd: util.Cmd("git", "--version"), Stdout: "git version 2.40.1\n"}}, true, nil)
	if path := versionCachePath(process); path != "" {
		t.Errorf("entry %s for a mock", path)
	}
	version, ok, err := GetVersion(process)
	if err != nil || !ok || version != "2.40.1" {
		t.Errorf("GetVersion = %q, %v, %v; want 2.40.1", version, ok, err)
	}
	if err := process.AssertComplete(); err != nil {
		t.Error(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("%d entries, want the one written by hand", len(entries))
	}
}

// An entry is keyed on the binary's identity, and only binaries named git
// that are not scripts get one.
func TestVersionCache_Key(t *testing.T) {
	git := fakeGit(t)
	limit := time.Now().Add(time.Hour)
	key := gitBinaryKey(git, limit)
	if key == "" {
		t.Fatal("no key for a git binary")
	}
	if gitBinaryKey(git, time.Now().Add(-time.Hour)) != "" {
		t.Error("a key for a binary changed after the limit")
	}

	time.Sleep(10 * time.Millisecond)
	if err := os.Chmod(git, 0o700); err != nil {
		t.Fatal(err)
	}
	if gitBinaryKey(git, limit) == key {
		t.Error("the key did not change with the binary")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "git")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho git version 1.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if gitBinaryKey(script, limit) != "" {
		t.Error("a key for a script")
	}
	multi := filepath.Join(dir, "snap")
	if err := os.Link(git, multi); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "git")
	if err := os.Symlink(multi, link); err != nil {
		t.Fatal(err)
	}
	if gitBinaryKey(link, limit) != "" {
		t.Error("a key for a binary not named git")
	}
}

func TestVersionCache_Entries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "entry")
	if _, ok := loadVersion(path); ok {
		t.Fatal("a version without an entry")
	}
	storeVersion(path, "2.55.0")
	if v, ok := loadVersion(path); !ok || v != "2.55.0" {
		t.Errorf("loadVersion = %q, %v", v, ok)
	}
	old := time.Now().Add(-versionCacheMaxAge - time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadVersion(path); ok {
		t.Error("an entry older than a day was used")
	}
}

// Ports tests/Composer/Test/Command/DumpAutoloadCommandTest.php.
//
// The PHP tests that do not call initTempComposer run in Composer's own
// checkout (which has a composer.json); here they run in an empty temp
// project instead, so nothing is written into the source tree.

package command_test

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// dumpAutoload runs dump-autoload with the "key", value pairs and returns
// the status code, the display and the exception.
func dumpAutoload(t *testing.T, kv ...any) (int, string, error) {
	t.Helper()
	appTester := commandtest.GetApplicationTester(t)
	code, err := appTester.RunArgs(commandtest.Options{}, append([]any{"command", "dump-autoload"}, kv...)...)

	return code, appTester.Display(true), err
}

func assertDumpSucceeds(t *testing.T, kv ...any) string {
	t.Helper()
	code, output, err := dumpAutoload(t, kv...)
	if err != nil {
		t.Fatalf("unexpected exception: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code %d, want 0\n%s", code, output)
	}

	return output
}

func assertOutputHas(t *testing.T, output, needle string) {
	t.Helper()
	if !strings.Contains(output, needle) {
		t.Errorf("output does not contain %q:\n%s", needle, output)
	}
}

func assertOutputMatches(t *testing.T, pattern, output string) {
	t.Helper()
	ok, err := php.PregIsMatch(pattern, output)
	if err != nil || !ok {
		t.Errorf("output does not match %s:\n%s", pattern, output)
	}
}

func assertInvalidArgument(t *testing.T, err error, message string) {
	t.Helper()
	if !phperr.InstanceOf(err, "InvalidArgumentException") {
		t.Fatalf("expected InvalidArgumentException, got %v", err)
	}
	if err.Error() != message {
		t.Errorf("exception message %q, want %q", err.Error(), message)
	}
}

func TestDumpAutoloadCommand_DumpAutoload(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	output := assertDumpSucceeds(t)
	assertDumpMode(t, output, plainDump)
}

func TestDumpAutoloadCommand_DumpDevAutoload(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	output := assertDumpSucceeds(t, "--dev", true)
	assertDumpMode(t, output, plainDump)
}

func TestDumpAutoloadCommand_DumpNoDevAutoload(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	// Composer's test passes --dev here too.
	output := assertDumpSucceeds(t, "--dev", true)
	assertDumpMode(t, output, plainDump)
}

func TestDumpAutoloadCommand_UsingOptimizeAndStrictPsr(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	output := assertDumpSucceeds(t, "--optimize", true, "--strict-psr", true)
	assertDumpMode(t, output, optimizedDump)
}

func TestDumpAutoloadCommand_FailsUsingStrictPsrIfClassMapViolationsAreFound(t *testing.T) {
	dir := commandtest.InitTempComposer(t, `{"autoload": {"psr-4": {"Application\\": "src"}}}`, nil, nil, true)
	if err := os.Mkdir(dir+"/src/", 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/src/Foo.php", []byte(`<?php namespace Application\Src; class Foo {}`), 0o666); err != nil {
		t.Fatal(err)
	}
	code, output, err := dumpAutoload(t, "--optimize", true, "--strict-psr", true)
	if err != nil {
		t.Fatalf("unexpected exception: %v", err)
	}
	if code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
	assertOutputMatches(t, `#Class Application\\Src\\Foo located in .*? does not comply with psr-4 autoloading standard \(rule: Application\\ => \./src\)\. Skipping\.#`, output)
}

func TestDumpAutoloadCommand_UsingClassmapAuthoritative(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	output := assertDumpSucceeds(t, "--classmap-authoritative", true)
	assertDumpMode(t, output, authoritativeDump)
}

func TestDumpAutoloadCommand_UsingClassmapAuthoritativeAndStrictPsr(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	output := assertDumpSucceeds(t, "--classmap-authoritative", true, "--strict-psr", true)
	assertDumpMode(t, output, authoritativeDump)
}

func TestDumpAutoloadCommand_StrictPsrDoesNotWorkWithoutOptimizedAutoloader(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	_, _, err := dumpAutoload(t, "--strict-psr", true)
	assertInvalidArgument(t, err, "--strict-psr mode only works with optimized autoloader, use --optimize or --classmap-authoritative if you want a strict return value.")
}

func TestDumpAutoloadCommand_DevAndNoDevCannotBeCombined(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	_, _, err := dumpAutoload(t, "--dev", true, "--no-dev", true)
	assertInvalidArgument(t, err, "You can not use both --no-dev and --dev as they conflict with each other.")
}

func readAutoloadPHP(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(dir + "/vendor/autoload.php")
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestDumpAutoloadCommand_WithCustomAutoloaderSuffix(t *testing.T) {
	dir := commandtest.InitTempComposer(t, `{"config": {"autoloader-suffix": "Foobar"}}`, nil, nil, true)
	assertDumpSucceeds(t)
	assertOutputHas(t, readAutoloadPHP(t, dir), "ComposerAutoloaderInitFoobar")
}

// dumpAutoloadLock is the composer.lock of the lock tests with the given
// content-hash.
func dumpAutoloadLock(contentHash string) string {
	encoded, _ := php.JSONEncode(contentHash, 0)

	return `{
		"_readme": [
			"This file locks the dependencies of your project to a known state",
			"Read more about it at https://getcomposer.org/doc/01-basic-usage.md#installing-dependencies",
			"This file is @generated automatically"
		],
		"content-hash": ` + encoded + `,
		"packages": [],
		"packages-dev": [],
		"aliases": [],
		"minimum-stability": "stable",
		"stability-flags": [],
		"prefer-stable": false,
		"prefer-lowest": false,
		"platform": [],
		"platform-dev": [],
		"plugin-api-version": "2.6.0"
	}`
}

func TestDumpAutoloadCommand_WithExistingComposerLockAndAutoloaderSuffix(t *testing.T) {
	dir := commandtest.InitTempComposer(t, `{"config": {"autoloader-suffix": "Foobar"}}`, nil, dumpAutoloadLock("d751713988987e9331980363e24189ce"), true)
	assertDumpSucceeds(t)
	assertOutputHas(t, readAutoloadPHP(t, dir), "ComposerAutoloaderInitFoobar")
}

func TestDumpAutoloadCommand_WithExistingComposerLockWithoutAutoloaderSuffix(t *testing.T) {
	dir := commandtest.InitTempComposer(t, `{"name": "foo/bar"}`, nil, dumpAutoloadLock("2d4a6be9a93712c5d6a119b26734a047"), true)
	assertDumpSucceeds(t)
	assertOutputHas(t, readAutoloadPHP(t, dir), "ComposerAutoloaderInit2d4a6be9a93712c5d6a119b26734a047")
}

func TestDumpAutoloadCommand_WithConflictedComposerLockWithoutAutoloaderSuffix(t *testing.T) {
	// what JsonFile::parseJson() puts in place of a conflicted content-hash
	dir := commandtest.InitTempComposer(t, `{"name": "foo/bar"}`, nil, dumpAutoloadLock("VCS merge conflict detected. Please run `composer update --lock`."), true)
	assertDumpSucceeds(t)

	autoload := readAutoloadPHP(t, dir)
	if strings.Contains(autoload, "merge conflict detected") {
		t.Errorf("autoload.php contains the conflict marker:\n%s", autoload)
	}
	assertOutputMatches(t, `{ComposerAutoloaderInit[a-f0-9]{32}::getLoader\(\);}`, autoload)
}

// dumpMode is an autoloader mode's pair of output lines: the one before
// the dump and a pattern for the one after it.
type dumpMode struct{ generating, generated string }

var (
	plainDump         = dumpMode{"Generating autoload files", `/Generated autoload files/`}
	optimizedDump     = dumpMode{"Generating optimized autoload files", `/Generated optimized autoload files containing \d+ classes/`}
	authoritativeDump = dumpMode{"Generating optimized autoload files (authoritative)", `/Generated optimized autoload files \(authoritative\) containing \d+ classes/`}
)

func assertDumpMode(t *testing.T, output string, mode dumpMode) {
	t.Helper()
	assertOutputHas(t, output, mode.generating)
	assertOutputMatches(t, mode.generated, output)
}

// psr4Project is a project whose one class complies with its PSR-4 rule.
func psr4Project(t *testing.T, extra string) string {
	t.Helper()
	dir := commandtest.InitTempComposer(t, `{"autoload": {"psr-4": {"App\\": "src/"}}`+extra+`}`, nil, nil, true)
	if err := os.Mkdir(dir+"/src", 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/src/Foo.php", []byte(`<?php namespace App; class Foo {}`), 0o666); err != nil {
		t.Fatal(err)
	}

	return dir
}

// readTree is every file under dir by its path relative to dir.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, path)
		files[rel] = string(data)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return files
}

func TestDumpAutoloadCommand_StrictAmbiguousDoesNotWorkWithoutOptimizedAutoloader(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	_, _, err := dumpAutoload(t, "--strict-ambiguous", true)
	assertInvalidArgument(t, err, "--strict-ambiguous mode only works with optimized autoloader, use --optimize or --classmap-authoritative if you want a strict return value.")
}

func TestDumpAutoloadCommand_StrictAmbiguousWithoutAmbiguousClasses(t *testing.T) {
	psr4Project(t, "")
	output := assertDumpSucceeds(t, "--optimize", true, "--strict-ambiguous", true)
	assertDumpMode(t, output, optimizedDump)
}

// The config options turn on the modes their command line options do.
func TestDumpAutoloadCommand_ConfigFallbacks(t *testing.T) {
	for _, tc := range []struct {
		config string
		mode   dumpMode
		real   string // in autoload_real.php
	}{
		{"optimize-autoloader", optimizedDump, ""},
		{"classmap-authoritative", authoritativeDump, "$loader->setClassMapAuthoritative(true);"},
		{"apcu-autoloader", plainDump, "$loader->setApcuPrefix("},
	} {
		t.Run(tc.config, func(t *testing.T) {
			dir := psr4Project(t, `, "config": {"`+tc.config+`": true}`)
			assertDumpMode(t, assertDumpSucceeds(t), tc.mode)
			if tc.real != "" {
				real, err := os.ReadFile(dir + "/vendor/composer/autoload_real.php")
				if err != nil {
					t.Fatal(err)
				}
				assertOutputHas(t, string(real), tc.real)
			}
		})
	}
}

// --dry-run reports the dump without changing a file of vendor/.
func TestDumpAutoloadCommand_DryRun(t *testing.T) {
	dir := psr4Project(t, "")
	assertDumpSucceeds(t)
	before := readTree(t, dir+"/vendor")
	if err := os.WriteFile(dir+"/composer.json", []byte(`{"autoload": {"psr-4": {"Other\\": "lib/"}, "classmap": ["src/"]}, "repositories": [{"packagist.org": false}]}`), 0o666); err != nil {
		t.Fatal(err)
	}
	assertDumpMode(t, assertDumpSucceeds(t, "--optimize", true, "--dry-run", true), optimizedDump)
	if after := readTree(t, dir+"/vendor"); !maps.Equal(before, after) {
		t.Errorf("--dry-run changed vendor/:\nbefore %v\nafter %v", slices.Sorted(maps.Keys(before)), slices.Sorted(maps.Keys(after)))
	}
}

// The alias runs the command: same output, same files.
func TestDumpAutoloadCommand_Alias(t *testing.T) {
	dir := psr4Project(t, "")
	want := assertDumpSucceeds(t, "--optimize", true)
	wantFiles := readTree(t, dir+"/vendor")
	appTester := commandtest.GetApplicationTester(t)
	code, err := appTester.RunArgs(commandtest.Options{}, "command", "dumpautoload", "--optimize", true)
	if err != nil || code != 0 {
		t.Fatalf("dumpautoload: exit %d, %v", code, err)
	}
	if got := appTester.Display(true); got != want {
		t.Errorf("dumpautoload output %q, want %q", got, want)
	}
	if !maps.Equal(readTree(t, dir+"/vendor"), wantFiles) {
		t.Error("dumpautoload wrote other files than dump-autoload")
	}
}

// Ports tests/Composer/Test/Command/DumpAutoloadCommandTest.php.
//
// The PHP tests that do not call initTempComposer run in Composer's own
// checkout (which has a composer.json); here they run in an empty temp
// project instead, so nothing is written into the source tree.

package command_test

import (
	"os"
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
	assertOutputHas(t, output, "Generating autoload files")
	assertOutputHas(t, output, "Generated autoload files")
}

func TestDumpAutoloadCommand_DumpDevAutoload(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	output := assertDumpSucceeds(t, "--dev", true)
	assertOutputHas(t, output, "Generating autoload files")
	assertOutputHas(t, output, "Generated autoload files")
}

func TestDumpAutoloadCommand_DumpNoDevAutoload(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	// Composer's test passes --dev here too.
	output := assertDumpSucceeds(t, "--dev", true)
	assertOutputHas(t, output, "Generating autoload files")
	assertOutputHas(t, output, "Generated autoload files")
}

func TestDumpAutoloadCommand_UsingOptimizeAndStrictPsr(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	output := assertDumpSucceeds(t, "--optimize", true, "--strict-psr", true)
	assertOutputHas(t, output, "Generating optimized autoload files")
	assertOutputMatches(t, `/Generated optimized autoload files containing \d+ classes/`, output)
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
	assertOutputHas(t, output, "Generating optimized autoload files (authoritative)")
	assertOutputMatches(t, `/Generated optimized autoload files \(authoritative\) containing \d+ classes/`, output)
}

func TestDumpAutoloadCommand_UsingClassmapAuthoritativeAndStrictPsr(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	output := assertDumpSucceeds(t, "--classmap-authoritative", true, "--strict-psr", true)
	assertOutputHas(t, output, "Generating optimized autoload files")
	assertOutputMatches(t, `/Generated optimized autoload files \(authoritative\) containing \d+ classes/`, output)
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

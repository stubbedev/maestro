package loader_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/internal/pkgtest"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/util"
)

// RootPackageLoader's self-require error and ValidatingArrayLoader's SPDX
// warning join their lines with PHP_EOL, "\r\n" on Windows.
func TestLoader_MessagesUseWindowsEOL(t *testing.T) {
	php.SetEOLForTest(t, "\r\n")
	keepGitEnv(t)

	process := pkgtest.NewProcessExecutorMock(t)
	process.Expects(nil, false, pkgtest.Expectation{Return: 1})
	_, err := loader.NewRootPackageLoader(repositoryManagerStub{}, rootConfigStub{}, nil, version.NewVersionGuesser(process, nil), nil).Load(php.ArrayOf(
		"name", "foo/bar",
		"version", "1.0.0",
		"require", php.ArrayOf("foo/bar", "^1.0"),
	), pkg.ClassRootPackage)
	want := "Root package 'foo/bar' cannot require itself in its composer.json\r\nDid you accidentally name your root package after an external package?"
	if re, ok := errors.AsType[*util.RuntimeError](err); !ok || re.Message != want {
		t.Errorf("self-require: %v\nwant %q", err, want)
	}

	l := loader.NewValidatingArrayLoader(&capturingLoader{}, nil, loader.CheckAll)
	if _, err := l.Load(php.ArrayOf("name", "a/b", "license", "XXXXX"), pkg.ClassCompletePackage); err != nil {
		t.Fatal(err)
	}
	wantWarnings := []string{`License "XXXXX" is not a valid SPDX license identifier, see https://spdx.org/licenses/ if you use an open license.` + "\r\n" +
		`If the software is closed-source, you may use "proprietary" as license.`}
	if got := l.Warnings(); !slices.Equal(got, wantWarnings) {
		t.Errorf("warnings %q, want %q", got, wantWarnings)
	}
}

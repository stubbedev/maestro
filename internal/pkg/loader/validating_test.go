// Ports tests/Composer/Test/Package/Loader/ValidatingArrayLoaderTest.php.

package loader_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/internal/pkgtest"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

func providerCases(t *testing.T, name string) []*pkgtest.Arr {
	t.Helper()

	var out []*pkgtest.Arr
	for _, c := range pkgtest.Providers(t)["ValidatingArrayLoaderTest::"+name] {
		out = append(out, c.Args)
	}

	return out
}

func TestValidatingArrayLoader_LoadSuccess(t *testing.T) {
	for _, args := range providerCases(t, "successProvider") {
		config := args.At(0).(*pkgtest.Arr).PHP()
		inner := &capturingLoader{}

		if _, err := loader.NewValidatingArrayLoader(inner, nil, loader.CheckAll).Load(config, pkg.ClassCompletePackage); err != nil {
			t.Errorf("%s: %v", enc(t, config), err)

			continue
		}

		if !php.LooseEquals(config, inner.config) {
			t.Errorf("loaded %s, want %s", enc(t, inner.config), enc(t, config))
		}
	}
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	php.SortSlice(s, func(a, b string) int { return php.Compare(a, b) })

	return s
}

func TestValidatingArrayLoader_LoadFailureThrowsException(t *testing.T) {
	for _, args := range providerCases(t, "errorProvider") {
		config := args.At(0).(*pkgtest.Arr).PHP()
		expected := args.At(1).(*pkgtest.Arr).Strings()

		_, err := loader.NewValidatingArrayLoader(&capturingLoader{}, nil, loader.CheckAll).Load(config, pkg.ClassCompletePackage)

		var invalid *loader.InvalidPackageError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: got %v, want an InvalidPackageError", enc(t, config), err)

			continue
		}

		if got := sorted(invalid.Errors()); !slices.Equal(got, sorted(expected)) {
			t.Errorf("%s: errors\n got %q\nwant %q", enc(t, config), got, sorted(expected))
		}
	}
}

func TestValidatingArrayLoader_LoadWarnings(t *testing.T) {
	for _, args := range providerCases(t, "warningProvider") {
		config := args.At(0).(*pkgtest.Arr).PHP()
		expected := args.At(1).(*pkgtest.Arr).Strings()

		l := loader.NewValidatingArrayLoader(&capturingLoader{}, nil, loader.CheckAll)
		if _, err := l.Load(config, pkg.ClassCompletePackage); err != nil {
			t.Errorf("%s: %v", enc(t, config), err)

			continue
		}

		// The provider's messages were exported where PHP_EOL is "\n".
		if got := sorted(normalizeEOLs(l.Warnings())); !slices.Equal(got, sorted(expected)) {
			t.Errorf("%s: warnings\n got %q\nwant %q", enc(t, config), got, sorted(expected))
		}
	}
}

func TestValidatingArrayLoader_LoadSkipsWarningDataWhenIgnoringErrors(t *testing.T) {
	for _, args := range providerCases(t, "warningProvider") {
		if args.Len() > 2 && args.At(2) == false {
			continue
		}

		expectedArray := php.ArrayOf("name", "a/b")
		if args.Len() > 3 {
			expectedArray = args.At(3).(*pkgtest.Arr).PHP()
		}

		config := args.At(0).(*pkgtest.Arr).PHP()
		config.Set("name", "a/b")

		inner := &capturingLoader{}
		if _, err := loader.NewValidatingArrayLoader(inner, nil, loader.CheckAll).Load(config, pkg.ClassCompletePackage); err != nil {
			t.Errorf("%s: %v", enc(t, config), err)

			continue
		}

		if !php.LooseEquals(expectedArray, inner.config) {
			t.Errorf("%s: loaded %s, want %s", enc(t, config), enc(t, inner.config), enc(t, expectedArray))
		}
	}
}

func testPackage(name, version string) *pkg.CompletePackage {
	normalized, err := pkg.NewVersionParser().Normalize(version)
	if err != nil {
		panic(err)
	}

	return pkg.NewCompletePackage(name, normalized, version)
}

func TestValidatingArrayLoader_ValidatePackageAllowsValidPackages(t *testing.T) {
	p := testPackage("vendor/package", "1.0.0")
	p.SetSourceType(pkg.Str("git"))
	p.SetSourceURL(pkg.Str("https://example.org/vendor/package.git"))
	p.SetSourceReference(pkg.Str("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	p.SetDistType(pkg.Str("zip"))
	p.SetDistURL(pkg.Str("https://example.org/vendor/package.zip"))
	p.SetDistReference(pkg.Str("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	p.SetBinaries(php.ListOf("bin/foo", "console", "some.bin"))

	perforce := testPackage("vendor/perforce", "1.0.0")
	perforce.SetSourceType(pkg.Str("perforce"))
	perforce.SetSourceURL(pkg.Str("ssl:p4.example.org:1666"))
	perforce.SetSourceReference(pkg.Str("//depot/main"))

	root := pkg.NewRootPackage("__root__", "1.0.0.0", "1.0.0")

	// platform packages are accepted as-is, and the root package is skipped entirely
	for _, p := range []pkg.PackageInterface{p, perforce, testPackage("php", "8.2.0"), root} {
		if err := loader.ValidatePackage(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func TestValidatingArrayLoader_ValidatePackageRejectsMaliciousMetadata(t *testing.T) {
	badSourceURL := testPackage("vendor/pkg", "1.0.0")
	badSourceURL.SetSourceType(pkg.Str("git"))
	badSourceURL.SetSourceURL(pkg.Str("--upload-pack=touch /tmp/pwned"))
	badSourceURL.SetSourceReference(pkg.Str("main"))

	badSourceReference := testPackage("vendor/pkg", "1.0.0")
	badSourceReference.SetSourceType(pkg.Str("git"))
	badSourceReference.SetSourceURL(pkg.Str("https://example.org/vendor/pkg.git"))
	badSourceReference.SetSourceReference(pkg.Str("--upload-pack=touch /tmp/pwned"))

	badDistURL := testPackage("vendor/pkg", "1.0.0")
	badDistURL.SetDistType(pkg.Str("zip"))
	badDistURL.SetDistURL(pkg.Str("-oProxyCommand=touch /tmp/pwned"))

	badDistReference := testPackage("vendor/pkg", "1.0.0")
	badDistReference.SetDistType(pkg.Str("zip"))
	badDistReference.SetDistURL(pkg.Str("https://example.org/vendor/pkg.zip"))
	badDistReference.SetDistReference(pkg.Str("--evil"))

	badBin := testPackage("vendor/pkg", "1.0.0")
	badBin.SetBinaries(php.ListOf("bin/ok", "../../../../escape-target.txt"))

	badPerforceURL := testPackage("vendor/pkg", "1.0.0")
	badPerforceURL.SetSourceType(pkg.Str("perforce"))
	badPerforceURL.SetSourceURL(pkg.Str("rsh:touch /tmp/pwned"))
	badPerforceURL.SetSourceReference(pkg.Str("//depot/main"))

	for _, c := range []struct {
		p       pkg.PackageInterface
		message string
	}{
		{testPackage("--evil/pkg", "1.0.0"), "Invalid package found during dependency resolution, aborting: --evil/pkg is invalid"},
		{badSourceURL, `vendor/pkg has an invalid source.url, it must not start with a "-": --upload-pack=touch /tmp/pwned`},
		{badSourceReference, `vendor/pkg has an invalid source.reference, it must not start with a "-": --upload-pack=touch /tmp/pwned`},
		{badDistURL, `vendor/pkg has an invalid dist.url, it must not start with a "-": -oProxyCommand=touch /tmp/pwned`},
		{badDistReference, `vendor/pkg has an invalid dist.reference, it must not start with a "-": --evil`},
		{badBin, `vendor/pkg has an invalid bin ../../../../escape-target.txt, it must not contain ".." path segments`},
		{badPerforceURL, "vendor/pkg has an invalid source.url, it must be a Perforce port of the form [tcp|ssl:][host:]port: rsh:touch /tmp/pwned"},
	} {
		err := loader.ValidatePackage(c.p)

		var security *pkg.SecurityError
		if !errors.As(err, &security) || !strings.Contains(err.Error(), c.message) {
			t.Errorf("%s: got %v, want %q", c.p, err, c.message)
		}
	}
}

// normalizeEOLs is php.NormalizeEOL on each message.
func normalizeEOLs(messages []string) []string {
	out := make([]string, len(messages))
	for i, m := range messages {
		out[i] = php.NormalizeEOL(m)
	}

	return out
}

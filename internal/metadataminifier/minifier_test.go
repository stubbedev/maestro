// Ports composer/metadata-minifier's tests/MetadataMinifierTest.php
// (MIT, testdata/LICENSE-metadata-minifier).

package metadataminifier_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/metadataminifier"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
)

func assertSame(t *testing.T, label string, want, got []*php.Array) {
	t.Helper()

	w, _ := php.JSONEncode(toList(want), 0)
	g, _ := php.JSONEncode(toList(got), 0)
	if !php.StrictEquals(toList(want), toList(got)) {
		t.Errorf("%s:\n got %s\nwant %s", label, g, w)
	}
}

func toList(versions []*php.Array) *php.Array {
	l := php.NewArray()
	for _, v := range versions {
		l.Append(v)
	}

	return l
}

func TestMetadataMinifier_MinifyExpand(t *testing.T) {
	package1 := pkg.NewCompletePackage("foo/bar", "2.0.0.0", "2.0.0")
	package1.SetScripts(php.ArrayOf("foo", php.ListOf("bar")))
	package1.SetLicense(php.ListOf("MIT"))
	package2 := pkg.NewCompletePackage("foo/bar", "1.2.0.0", "1.2.0")
	package2.SetLicense(php.ListOf("GPL"))
	package2.SetHomepage(pkg.Str("https://example.org"))
	package3 := pkg.NewCompletePackage("foo/bar", "1.0.0.0", "1.0.0")
	package3.SetLicense(php.ListOf("GPL"))
	package4 := pkg.NewCompletePackage("foo/bar", "0.1.0.0", "0.1.0")
	package4.SetLicense(php.ListOf("GPL"))

	minified := []*php.Array{
		php.ArrayOf("name", "foo/bar", "version", "2.0.0", "version_normalized", "2.0.0.0", "type", "library", "scripts", php.ArrayOf("foo", php.ListOf("bar")), "license", php.ListOf("MIT")),
		php.ArrayOf("version", "1.2.0", "version_normalized", "1.2.0.0", "license", php.ListOf("GPL"), "homepage", "https://example.org", "scripts", "__unset"),
		php.ArrayOf("version", "1.0.0", "version_normalized", "1.0.0.0", "homepage", "__unset"),
		php.ArrayOf("version", "0.1.0", "version_normalized", "0.1.0.0"),
	}

	var source []*php.Array
	for _, p := range []pkg.PackageInterface{package1, package2, package3, package4} {
		dumped, err := dumper.ArrayDumper{}.Dump(p)
		if err != nil {
			t.Fatal(err)
		}
		source = append(source, dumped)
	}

	assertSame(t, "minify", minified, metadataminifier.Minify(source))
	assertSame(t, "expand", source, metadataminifier.Expand(minified))
}

func TestMetadataMinifier_MinifyExpandWithNullValues(t *testing.T) {
	source := []*php.Array{
		php.ArrayOf("name", "foo/bar", "version", "2.0.0", "dist", nil),
		php.ArrayOf("name", "foo/bar", "version", "1.0.0", "dist", nil),
	}

	minified := []*php.Array{
		php.ArrayOf("name", "foo/bar", "version", "2.0.0", "dist", nil), // first kept in full
		php.ArrayOf("version", "1.0.0"),                                 // name + null dist unchanged => omitted
	}

	assertSame(t, "minify", minified, metadataminifier.Minify(source))
	assertSame(t, "expand", source, metadataminifier.Expand(minified))
}

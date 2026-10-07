// Differential tests against the goldens tools/oracle/pkg/loader.php
// records by running the real Composer 2.10.3.

package loader_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/metadataminifier"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/util"
)

// p2Inputs returns the expanded versions of every p2 file.
func p2Inputs(t *testing.T) map[string][]*php.Array {
	t.Helper()

	out := map[string][]*php.Array{}

	for _, name := range []string{
		"doctrine_orm", "doctrine_orm~dev", "guzzlehttp_guzzle", "guzzlehttp_guzzle~dev", "laravel_framework", "laravel_framework~dev",
		"monolog_monolog", "monolog_monolog~dev", "phpunit_phpunit", "phpunit_phpunit~dev", "symfony_symfony", "symfony_symfony~dev",
	} {
		data, _ := decodePHP(t, string(readFile(t, "oracle/p2/"+name+".json.gz"))).(*php.Array)
		packages, _ := data.GetArray("packages")

		for _, versions := range packages.All() {
			list, _ := versions.(*php.Array)
			versionList := make([]*php.Array, 0, list.Len())
			for _, v := range list.Values() {
				a, _ := v.(*php.Array)
				versionList = append(versionList, a)
			}
			out[name] = metadataminifier.Expand(versionList)
		}
	}

	return out
}

func rawList(t *testing.T, raw json.RawMessage) []json.RawMessage {
	t.Helper()

	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}

	return list
}

func TestOracle_LoadPackages(t *testing.T) {
	if testing.Short() {
		t.Skip("replays a large golden; skipped in -short mode")
	}
	golden := readGolden(t, "oracle/packages.json.gz")

	for name, versions := range p2Inputs(t) {
		want := rawList(t, golden[name+"#loadPackages"])

		packages, err := loader.NewArrayLoader(nil, false).LoadPackages(versions)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if len(packages) != len(want) {
			t.Fatalf("%s: %d packages, want %d", name, len(packages), len(want))
		}

		for i, p := range packages {
			if got := enc(t, describe(t, p)); got != string(want[i]) {
				t.Errorf("%s #%d:\n got %s\nwant %s", name, i, got, want[i])
			}
		}

		var hashes []string
		if err := json.Unmarshal(golden[name+"#load"], &hashes); err != nil {
			t.Fatal(err)
		}

		l := loader.NewArrayLoader(nil, false)
		for i, version := range versions {
			p, err := l.Load(version, pkg.ClassCompletePackage)
			if err != nil {
				t.Fatalf("%s #%d: %v", name, i, err)
			}

			if got := md5hex(enc(t, describe(t, p))); got != hashes[i] {
				t.Errorf("%s #%d: load() differs: %s", name, i, enc(t, describe(t, p)))
			}
		}
	}
}

func TestOracle_LockFile(t *testing.T) {
	golden := readGolden(t, "oracle/packages.json.gz")
	want := rawList(t, golden["composer.lock"])

	lock, _ := decodePHP(t, string(readFile(t, "oracle/composer.lock"))).(*php.Array)
	packages, _ := lock.GetArray("packages")
	dev, _ := lock.GetArray("packages-dev")
	all := append(packages.Values(), dev.Values()...)

	for i, config := range all {
		p, err := loader.NewArrayLoader(nil, true).Load(config.(*php.Array), pkg.ClassCompletePackage)
		if err != nil {
			t.Fatal(err)
		}

		if got := enc(t, describe(t, p)); got != string(want[i]) {
			t.Errorf("#%d:\n got %s\nwant %s", i, got, want[i])
		}
	}
}

type validatingCase struct {
	In          string          `json:"in"`
	Passed      *string         `json:"passed"`
	Invalid     *string         `json:"invalid"`
	Data        *string         `json:"data"`
	E           []string        `json:"e"`
	Errors      []string        `json:"errors"`
	Warnings    []string        `json:"warnings"`
	ArrayLoader json.RawMessage `json:"arrayLoader"`
	RootLoader  json.RawMessage `json:"rootLoader"`
}

// capturingLoader records the array a ValidatingArrayLoader hands on.
type capturingLoader struct{ config *php.Array }

func (c *capturingLoader) Load(config *php.Array, _ string) (pkg.PackageInterface, error) {
	c.config = config

	return pkg.NewCompletePackage("captured/package", "1.0.0.0", "1.0.0"), nil
}

func TestOracle_ValidatingArrayLoader(t *testing.T) {
	if testing.Short() {
		t.Skip("replays a large golden; skipped in -short mode")
	}
	golden := readGolden(t, "oracle/validating.json.gz")

	var now int64
	if err := json.Unmarshal(golden["now"], &now); err != nil {
		t.Fatal(err)
	}

	keys := make([]string, 0, len(golden))
	for k := range golden {
		if k != "now" {
			keys = append(keys, k)
		}
	}

	slices.SortFunc(keys, func(a, b string) int {
		x, _ := strconv.Atoi(a)
		y, _ := strconv.Atoi(b)

		return x - y
	})

	for _, k := range keys {
		var c validatingCase
		if err := json.Unmarshal(golden[k], &c); err != nil {
			t.Fatal(err)
		}

		config, _ := decodePHP(t, c.In).(*php.Array)
		what := "#" + k + " " + c.In
		timeValue, _ := config.Get("time")

		inner := &capturingLoader{}
		l := loader.NewValidatingArrayLoader(inner, nil, loader.CheckAll)
		l.Now = func() time.Time { return time.Unix(now, 0) }

		_, err := l.Load(config, pkg.ClassCompletePackage)

		switch {
		case c.Passed != nil:
			if err != nil {
				t.Errorf("%s: %v", what, err)
			} else if got := enc(t, inner.config); got != *c.Passed {
				t.Errorf("%s: passed\n got %s\nwant %s", what, got, *c.Passed)
			}
		case c.Invalid != nil:
			checkException(t, what, err, `Composer\Package\Loader\InvalidPackageException`, *c.Invalid)

			if ip := (*loader.InvalidPackageError)(nil); errors.As(err, &ip) && enc(t, ip.Data()) != *c.Data {
				t.Errorf("%s: data %s, want %s", what, enc(t, ip.Data()), *c.Data)
			}
		default:
			checkException(t, what, err, c.E[0], c.E[1])
		}

		if c.E == nil {
			if !slices.Equal(l.Errors(), c.Errors) {
				t.Errorf("%s: errors\n got %q\nwant %q", what, l.Errors(), c.Errors)
			}

			// The goldens' PHP_EOL is "\n".
			if got := normalizeEOLs(l.Warnings()); !slices.Equal(got, c.Warnings) {
				t.Errorf("%s: warnings\n got %q\nwant %q", what, got, c.Warnings)
			}
		}

		for _, run := range []struct {
			name  string
			class string
			want  json.RawMessage
		}{
			{"ArrayLoader", pkg.ClassCompletePackage, c.ArrayLoader},
			{"ArrayLoader root", pkg.ClassRootPackage, c.RootLoader},
		} {
			p, err := loader.NewArrayLoader(nil, true).Load(config, run.class)
			if class, message, ok := asException(run.want); ok {
				checkException(t, what+" "+run.name, err, class, message)

				continue
			}

			if err != nil {
				t.Errorf("%s %s: %v", what, run.name, err)

				continue
			}

			want, got := string(run.want), enc(t, describe(t, p))
			if nowDependent(timeValue, true) {
				// the release date is when each side ran
				want, got = withoutTime(t, want), withoutTime(t, got)
			}

			if got != want {
				t.Errorf("%s %s:\n got %s\nwant %s", what, run.name, got, want)
			}
		}
	}
}

func TestOracle_Formats(t *testing.T) {
	golden := readGolden(t, "oracle/formats.json")

	var times [][2]json.RawMessage
	if err := json.Unmarshal(golden["time"], &times); err != nil {
		t.Fatal(err)
	}

	for _, c := range times {
		var s string
		_ = json.Unmarshal(c[0], &s)

		got, err := loader.ParseDateTime(s)
		if _, message, ok := asException(c[1]); ok {
			if err == nil {
				t.Errorf("DateTime(%q): got %s, want %q", s, got.Format(time.RFC3339), message)
			} else if err.Error() != message {
				t.Errorf("DateTime(%q): got %q, want %q", s, err, message)
			}

			continue
		}

		var want []any
		_ = json.Unmarshal(c[1], &want)

		if err != nil {
			t.Errorf("DateTime(%q): %v, want %v", s, err, want)

			continue
		}

		if nowDependent(s, false) {
			continue // the golden has no clock; testdata/oracle/datetime.json covers these
		}

		if formatted := got.Format("2006-01-02T15:04:05-07:00"); formatted != want[0] || float64(got.Unix()) != want[1] || float64(got.Nanosecond()/1000) != want[2] {
			t.Errorf("DateTime(%q) = %s %d %d, want %v", s, formatted, got.Unix(), got.Nanosecond()/1000, want)
		}
	}

	var emails [][2]json.RawMessage
	if err := json.Unmarshal(golden["email"], &emails); err != nil {
		t.Fatal(err)
	}

	for _, c := range emails {
		var (
			s    string
			want bool
		)

		_ = json.Unmarshal(c[0], &s)
		_ = json.Unmarshal(c[1], &want)

		if got := util.FilterValidateEmail(s); got != want {
			t.Errorf("filter_var(%q, FILTER_VALIDATE_EMAIL) = %v, want %v", s, got, want)
		}
	}

	var urls [][3]json.RawMessage
	if err := json.Unmarshal(golden["url"], &urls); err != nil {
		t.Fatal(err)
	}

	for _, c := range urls {
		var (
			s         string
			http, irc bool
		)

		_ = json.Unmarshal(c[0], &s)
		_ = json.Unmarshal(c[1], &http)
		_ = json.Unmarshal(c[2], &irc)

		if got, _ := loader.FilterURL(s, "http", "https"); got != http {
			t.Errorf("filterUrl(%q) = %v, want %v", s, got, http)
		}

		if got, _ := loader.FilterURL(s, "irc", "ircs"); got != irc {
			t.Errorf("filterUrl(%q, irc) = %v, want %v", s, got, irc)
		}
	}
}

type rootLoaderCase struct {
	Requires   string          `json:"requires"`
	Minimum    string          `json:"minimum"`
	Flags      json.RawMessage `json:"flags"`
	References json.RawMessage `json:"references"`
	Aliases    json.RawMessage `json:"aliases"`
}

func TestOracle_RootPackageLoaderExtracts(t *testing.T) {
	for k, raw := range readGolden(t, "oracle/rootloader.json") {
		var c rootLoaderCase
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}

		requires, _ := decodePHP(t, c.Requires).(*php.Array)
		what := "#" + k + " " + c.Requires

		check := func(name string, want json.RawMessage, got *php.Array, err error) {
			if class, message, ok := asException(want); ok {
				checkException(t, what+" "+name, err, class, message)

				return
			}

			var s string
			_ = json.Unmarshal(want, &s)

			if err != nil {
				t.Errorf("%s %s: %v", what, name, err)
			} else if enc(t, got) != s {
				t.Errorf("%s %s: got %s, want %s", what, name, enc(t, got), s)
			}
		}

		flags, err := loader.ExtractStabilityFlags(requires, c.Minimum, php.ArrayOf("vendor/pkg", 20, "x/y", 5))
		check("flags", c.Flags, flags, err)

		refs, err := loader.ExtractReferences(requires, php.ArrayOf("x/y", "abc"))
		check("references", c.References, refs, err)

		aliases, err := loader.ExtractAliases(loader.NewRootPackageLoader(nil, nil, nil, nil, nil), requires)
		check("aliases", c.Aliases, aliases, err)
	}
}

func TestOracle_PackageNaming(t *testing.T) {
	for k, raw := range readGolden(t, "oracle/naming.json") {
		var c [4]json.RawMessage
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}

		var (
			name       string
			isPlatform bool
		)

		_ = json.Unmarshal(c[0], &name)
		_ = json.Unmarshal(c[1], &isPlatform)

		if got := pkg.IsPlatformPackage(name); got != isPlatform {
			t.Errorf("#%s isPlatformPackage(%q) = %v", k, name, got)
		}

		for i, isLink := range []bool{false, true} {
			var want *string
			_ = json.Unmarshal(c[2+i], &want)

			msg, bad, err := loader.HasPackageNamingError(name, isLink)
			if err != nil {
				t.Fatal(err)
			}

			if bad != (want != nil) || (bad && msg != *want) {
				t.Errorf("#%s hasPackageNamingError(%q, %v) = %q %v, want %v", k, name, isLink, msg, bad, c[2+i])
			}
		}
	}
}

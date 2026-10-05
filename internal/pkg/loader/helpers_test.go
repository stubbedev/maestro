package loader_test

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/testutil"
	"github.com/stubbedev/maestro/internal/util"
)

const jsonFlags = php.JSONUnescapedSlashes | php.JSONUnescapedUnicode | php.JSONPreserveZeroFraction

// enc is the oracle's enc(): json_encode with its flags.
func enc(t *testing.T, v any) string {
	t.Helper()

	s, err := php.JSONEncode(v, jsonFlags)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	return s
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))

	return hex.EncodeToString(sum[:])
}

// readFile reads a testdata file, gunzipping .gz files.
func readFile(t *testing.T, name string) []byte {
	t.Helper()

	return testutil.ReadGolden(t, filepath.Join("testdata", name))
}

// readGolden reads a golden written by the oracle's write_golden.
func readGolden(t *testing.T, name string) map[string]json.RawMessage {
	t.Helper()

	var out map[string]json.RawMessage
	if err := json.Unmarshal(readFile(t, name), &out); err != nil {
		t.Fatalf("%s: %v", name, err)
	}

	return out
}

// decodePHP decodes JSON as json_decode($s, true).
func decodePHP(t *testing.T, s string) any {
	t.Helper()

	v, err := php.JSONDecode(s, true)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}

	return v
}

// phpClass maps a Go error to the PHP exception class it stands for.
func phpClass(err error) string {
	var (
		unexpected  *util.UnexpectedValueError
		semverUV    *semver.UnexpectedValueError
		logic       *util.LogicError
		typeErr     *pkg.TypeError
		invalid     *loader.InvalidPackageError
		runtime     *util.RuntimeError
		invalidArg  *semver.InvalidArgumentError
		pcre        *php.PatternError
		invalidArg2 *util.InvalidArgumentError
	)

	switch {
	case errors.As(err, &unexpected), errors.As(err, &semverUV):
		return "UnexpectedValueException"
	case errors.As(err, &logic):
		return "LogicException"
	case errors.As(err, &typeErr):
		return "TypeError"
	case errors.As(err, &invalid):
		return `Composer\Package\Loader\InvalidPackageException`
	case errors.As(err, &runtime):
		return "RuntimeException"
	case errors.As(err, &invalidArg), errors.As(err, &invalidArg2):
		return "InvalidArgumentException"
	case errors.As(err, &pcre):
		return `Composer\Pcre\PcreException`
	}

	return "?"
}

// phpException is the {"e": [class, message]} the oracle records.
type phpException struct {
	E []string `json:"e"`
}

func asException(raw json.RawMessage) (class, message string, ok bool) {
	var e phpException
	if len(raw) > 0 && raw[0] == '{' && json.Unmarshal(raw, &e) == nil && len(e.E) == 2 {
		return e.E[0], e.E[1], true
	}

	return "", "", false
}

// checkException compares err with a recorded exception. PHP's TypeError
// messages end with where the call came from, which the Go ones leave
// out.
func checkException(t *testing.T, what string, err error, class, message string) {
	t.Helper()

	if err == nil {
		t.Errorf("%s: no error, want %s(%q)", what, class, message)

		return
	}

	if got := phpClass(err); got != class {
		t.Errorf("%s: got %s(%q), want %s(%q)", what, got, err, class, message)

		return
	}

	if class == "TypeError" {
		if !strings.HasPrefix(message, err.Error()) {
			t.Errorf("%s: got %q, want a prefix of %q", what, err, message)
		}

		return
	}

	if err.Error() != message {
		t.Errorf("%s: got %q, want %q", what, err, message)
	}
}

func shortClass(p pkg.PackageInterface) string {
	c := p.Class()

	return c[strings.LastIndexByte(c, '\\')+1:]
}

func strList(s []string) *php.Array { return php.StringList(s) }

// keyList is array_keys of an array with these keys: numeric names
// become ints.
func keyList(s []string) *php.Array {
	a := php.NewArrayCap(len(s))
	for _, v := range s {
		a.Append(php.StrKey(v).Value())
	}

	return a
}

// info is the oracle's info().
func info(t *testing.T, p pkg.PackageInterface) *php.Array {
	t.Helper()

	fpv := php.NewArray()
	for _, mode := range []pkg.DisplayMode{pkg.DisplaySourceRefIfDev, pkg.DisplaySourceRef, pkg.DisplayDistRef} {
		for _, truncate := range []bool{true, false} {
			fpv.Append(p.FullPrettyVersion(truncate, mode))
		}
	}

	a := php.ArrayOf(
		"unique", p.UniqueName(),
		"pretty", p.PrettyString(),
		"string", p.String(),
		"names", keyList(p.Names(true)),
		"namesNoProvides", keyList(p.Names(false)),
		"dev", p.IsDev(),
		"stability", p.Stability(),
		"priority", p.StabilityPriority(),
		"type", p.Type(),
		"targetDir", p.TargetDir().Value(),
		"sourceUrls", strList(p.SourceURLs()),
		"distUrls", strList(p.DistURLs()),
		"fullPrettyVersions", fpv,
		"defaultBranch", p.IsDefaultBranch(),
	)

	if c, ok := p.(pkg.CompletePackageInterface); ok {
		a.Set("abandoned", c.IsAbandoned())
		a.Set("replacement", c.ReplacementPackage().Value())
	}

	if alias, ok := p.(pkg.Alias); ok {
		a.Set("rootAlias", alias.IsRootPackageAlias())
		a.Set("selfVersionRequires", alias.HasSelfVersionRequires())

		links := php.NewArray()
		for _, m := range []struct {
			name  string
			links pkg.Links
		}{
			{"getRequires", p.Requires()},
			{"getDevRequires", p.DevRequires()},
			{"getConflicts", p.Conflicts()},
			{"getProvides", p.Provides()},
			{"getReplaces", p.Replaces()},
		} {
			for k, link := range m.links.All() {
				pc, err := link.PrettyConstraint()
				if err != nil {
					t.Fatal(err)
				}

				links.Append(php.ListOf(m.name, k, link.String(), pc, link.Constraint().PrettyString()))
			}
		}

		a.Set("links", links)
	}

	return a
}

// describe is the oracle's describe().
func describe(t *testing.T, p pkg.PackageInterface) *php.Array {
	t.Helper()

	dumped, err := dumper.ArrayDumper{}.Dump(p)
	if err != nil {
		t.Fatal(err)
	}

	d := php.ArrayOf("class", shortClass(p), "dump", enc(t, dumped), "info", info(t, p))
	if alias, ok := p.(pkg.Alias); ok {
		d.Set("aliasOf", describe(t, alias.AliasOf()))
	}

	return d
}

// relativeTime reports whether new \DateTime() reads the time value as
// relative to now (blank, a military zone letter or, without the "@"
// ArrayLoader adds, digits), which parseDateTime does not support.
func relativeTime(v any, validating bool) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}

	s = strings.TrimSpace(s)

	switch {
	case s == "":
		return true
	case len(s) == 1 && (s[0]|0x20 >= 'a' && s[0]|0x20 <= 'z'):
		return true
	case validating && strings.Trim(s, "0123456789") == "":
		return true
	}

	return false
}

// withoutTime removes the dumped "time" from a describe() golden (and its
// aliasOf).
func withoutTime(t *testing.T, raw string) string {
	t.Helper()

	d, _ := decodePHP(t, raw).(*php.Array)

	var strip func(d *php.Array)
	strip = func(d *php.Array) {
		dump, _ := d.GetString("dump")
		a, _ := decodePHP(t, dump).(*php.Array)
		a.Delete("time")
		d.Set("dump", enc(t, a))

		if alias, ok := d.GetArray("aliasOf"); ok {
			strip(alias)
		}
	}

	strip(d)

	return enc(t, d)
}

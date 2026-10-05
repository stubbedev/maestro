// Ports tests/SpdxLicensesTest.php of composer/spdx-licenses 1.6.0.

package spdx

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/testutil"
)

// licenseCase is a provider value: a string or, when list is set, an array.
type licenseCase struct {
	str  string
	list []string
	arr  bool
}

func (c licenseCase) validate(s *SpdxLicenses) bool {
	if c.arr {
		return s.ValidateList(c.list)
	}

	return s.Validate(c.str)
}

func str(s string) licenseCase     { return licenseCase{str: s} }
func list(l ...string) licenseCase { return licenseCase{list: l, arr: true} }

func (c licenseCase) String() string {
	if c.arr {
		return "[" + strings.Join(c.list, ", ") + "]"
	}

	return c.str
}

func provideValidLicenses(t *testing.T) []licenseCase {
	t.Helper()

	data, err := os.ReadFile("res/" + LicensesFile)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := php.JSONDecode(string(data), true)
	if err != nil {
		t.Fatal(err)
	}

	valid := []licenseCase{
		str("MIT"),
		str("MIT+"),
		list("(MIT)"),
		str("NONE"),
		str("NOASSERTION"),
		str("LicenseRef-3"),
		list("LGPL-2.0-only", "GPL-3.0-or-later"),
		str("(LGPL-2.0-only or GPL-3.0-or-later)"),
		str("(LGPL-2.0-only OR GPL-3.0-or-later)"),
		list("EUDatagrid and GPL-3.0-or-later"),
		str("(EUDatagrid and GPL-3.0-or-later)"),
		str("(EUDatagrid AND GPL-3.0-or-later)"),
		str("GPL-2.0-only with Autoconf-exception-2.0"),
		str("GPL-2.0-only WITH Autoconf-exception-2.0"),
		str("GPL-2.0-or-later WITH Autoconf-exception-2.0"),
		list("(GPL-3.0-only and GPL-2.0-only or GPL-3.0-or-later)"),
	}

	for _, k := range decoded.(*php.Array).Keys() {
		valid = append(valid, str(k.String()))
	}

	return valid
}

func TestSpdxLicenses_Validate(t *testing.T) {
	s := New()
	for _, c := range provideValidLicenses(t) {
		if !c.validate(s) {
			t.Errorf("validate(%s) = false, want true", c)
		}
	}
}

func TestSpdxLicenses_InvalidLicenses(t *testing.T) {
	s := New()
	for _, c := range []licenseCase{
		str(""),
		list(),
		str("The system pwns you"),
		str("()"),
		str("(MIT"),
		str("MIT)"),
		str("MIT NONE"),
		str("MIT AND NONE"),
		str("MIT (MIT and MIT)"),
		str("(MIT and MIT) MIT"),
		list("LGPL-2.0-only", "The system pwns you"),
		str("and GPL-3.0-or-later"),
		str("(EUDatagrid and GPL-3.0-or-later and  )"),
		str("(EUDatagrid xor GPL-3.0-or-later)"),
		str("(NONE or MIT)"),
		str("(NOASSERTION or MIT)"),
		str("Autoconf-exception-2.0 WITH MIT"),
		str("MIT WITH"),
		str("MIT OR"),
		str("MIT AND"),
	} {
		if c.validate(s) {
			t.Errorf("validate(%s) = true, want false", c)
		}
	}
}

func TestSpdxLicenses_InvalidArgument(t *testing.T) {
	s := New()
	obj := php.NewObject()

	for _, c := range []struct {
		arg any
		msg string
	}{
		{nil, "Array or String expected, NULL given."},
		{obj, "Array or String expected, object given."},
		{php.ListOf(obj), "Array of strings expected."},
		{php.ListOf("mixed", obj), "Array of strings expected."},
		{php.ListOf(obj, obj), "Array of strings expected."},
	} {
		_, err := s.ValidateValue(c.arg)

		var iae *InvalidArgumentError
		if !errors.As(err, &iae) || iae.Message != c.msg {
			t.Errorf("validate(%#v): err = %v, want InvalidArgumentError %q", c.arg, err, c.msg)
		}
	}

	for _, c := range []struct {
		arg  any
		want bool
	}{
		{"MIT", true},
		{[]string{"MIT", "X11"}, true},
		{php.ListOf("MIT", "nope"), false},
		{php.ListOf("MIT"), true},
		{php.NewArray(), false},
	} {
		got, err := s.ValidateValue(c.arg)
		if err != nil || got != c.want {
			t.Errorf("validate(%#v) = %v, %v; want %v", c.arg, got, err, c.want)
		}
	}
}

// TestSpdxLicenses_ResourceFilesContainJson covers testGetResourcesDir,
// testResourceFilesExist and testResourceFilesContainJson: the embedded
// res/ files are byte-identical copies that decode to non-empty objects.
func TestSpdxLicenses_ResourceFilesContainJson(t *testing.T) {
	for file, embedded := range map[string]string{LicensesFile: licensesJSON, ExceptionsFile: exceptionsJSON} {
		data, err := os.ReadFile("res/" + file)
		if err != nil {
			t.Fatalf("Expected file to exist in resources dir: %s", file)
		}

		if string(data) != embedded {
			t.Errorf("embedded %s differs from res/", file)
		}

		decoded, err := php.JSONDecode(embedded, true)
		if err != nil {
			t.Fatalf("Could not decode JSON within %s - %v", file, err)
		}

		if a, ok := decoded.(*php.Array); !ok || a.Len() == 0 {
			t.Errorf("%s: want a non-empty object", file)
		}
	}
}

func TestSpdxLicenses_GetLicenseByIdentifier(t *testing.T) {
	s := New()

	license, ok := s.GetLicenseByIdentifier("AGPL-1.0-only")
	if !ok {
		t.Fatal("AGPL-1.0-only not found")
	}

	if license.Name != "Affero General Public License v1.0 only" || license.OSIApproved ||
		!strings.HasPrefix(license.URL, "https://spdx.org/licenses/") || license.Deprecated {
		t.Errorf("AGPL-1.0-only = %+v", license)
	}

	if _, ok := s.GetLicenseByIdentifier("AGPL-1.0-Illegal"); ok {
		t.Error("AGPL-1.0-Illegal found")
	}
}

func TestSpdxLicenses_GetLicenses(t *testing.T) {
	got, ok := New().GetLicense("cc-by-sa-4.0")
	want := License{"CC-BY-SA-4.0", "Creative Commons Attribution Share Alike 4.0 International", false, false}

	if !ok || got != want {
		t.Errorf("getLicenses()['cc-by-sa-4.0'] = %+v, %v; want %+v", got, ok, want)
	}
}

func TestSpdxLicenses_GetExceptionByIdentifier(t *testing.T) {
	s := New()

	if _, ok := s.GetExceptionByIdentifier("Font-exception-2.0-Errorl"); ok {
		t.Error("Font-exception-2.0-Errorl found")
	}

	e, ok := s.GetExceptionByIdentifier("Font-exception-2.0")
	if !ok || e.Name != "Font exception 2.0" {
		t.Errorf("Font-exception-2.0 = %+v, %v", e, ok)
	}
}

func TestSpdxLicenses_GetIdentifierByName(t *testing.T) {
	s := New()
	for name, want := range map[string]string{
		"Affero General Public License v1.0": "AGPL-1.0",
		`BSD 2-Clause "Simplified" License`:  "BSD-2-Clause",
		"Font exception 2.0":                 "Font-exception-2.0",
		"null-identifier-name":               "",
	} {
		got, ok := s.GetIdentifierByName(name)
		if got != want || ok != (want != "") {
			t.Errorf("getIdentifierByName(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
}

func TestSpdxLicenses_IsOsiApprovedByIdentifier(t *testing.T) {
	s := New()
	if !s.IsOsiApprovedByIdentifier("MIT") {
		t.Error("MIT not OSI approved")
	}

	if s.IsOsiApprovedByIdentifier("AGPL-1.0") {
		t.Error("AGPL-1.0 OSI approved")
	}
}

func TestSpdxLicenses_IsDeprecatedByIdentifier(t *testing.T) {
	s := New()
	if !s.IsDeprecatedByIdentifier("GPL-3.0") {
		t.Error("GPL-3.0 not deprecated")
	}

	if s.IsDeprecatedByIdentifier("GPL-3.0-only") {
		t.Error("GPL-3.0-only deprecated")
	}
}

func loadOracle(t *testing.T, v any) {
	t.Helper()

	data, err := testutil.ReadGoldenFile("testdata/oracle/spdx.json.gz")
	if err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

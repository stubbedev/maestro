package spdx

import (
	"encoding/hex"
	"testing"
)

// Goldens from tools/oracle/spdx/spdx.php (the real composer/spdx-licenses).

type oracleString struct {
	Input string `json:"input"`
	Hex   string `json:"hex"`
}

func (o oracleString) value(t *testing.T) string {
	t.Helper()

	if o.Hex == "" {
		return o.Input
	}

	b, err := hex.DecodeString(o.Hex)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

type spdxOracle struct {
	Validate []struct {
		oracleString
		List  []oracleString `json:"list"`
		Valid bool           `json:"valid"`
		Error string         `json:"error"`
	} `json:"validate"`
	Lookup []struct {
		ID         string `json:"id"`
		License    []any  `json:"license"`
		Exception  []any  `json:"exception"`
		OSI        bool   `json:"osi"`
		Deprecated bool   `json:"deprecated"`
	} `json:"lookup"`
	ByName []struct {
		Name string  `json:"name"`
		ID   *string `json:"id"`
	} `json:"byName"`
	Licenses [][]any `json:"licenses"`
}

func TestOracle_Validate(t *testing.T) {
	var o spdxOracle
	loadOracle(t, &o)

	s := New()
	if len(o.Validate) < 10000 {
		t.Fatalf("only %d validate goldens", len(o.Validate))
	}

	for _, c := range o.Validate {
		if c.Error != "" {
			t.Fatalf("golden has a PHP error %q", c.Error)
		}

		if c.List != nil {
			l := make([]string, len(c.List))
			for i, e := range c.List {
				l[i] = e.value(t)
			}

			if got := s.ValidateList(l); got != c.Valid {
				t.Errorf("validate(%q) = %v, want %v", l, got, c.Valid)
			}

			continue
		}

		in := c.value(t)
		if got := s.Validate(in); got != c.Valid {
			t.Errorf("validate(%q) = %v, want %v", in, got, c.Valid)
		}
	}
}

func TestOracle_Lookups(t *testing.T) {
	var o spdxOracle
	loadOracle(t, &o)

	s := New()

	for _, c := range o.Lookup {
		l, ok := s.GetLicenseByIdentifier(c.ID)
		if ok != (c.License != nil) {
			t.Errorf("getLicenseByIdentifier(%q) found = %v", c.ID, ok)
		} else if ok {
			want := LicenseInfo{c.License[0].(string), c.License[1].(bool), c.License[2].(string), c.License[3].(bool)}
			if l != want {
				t.Errorf("getLicenseByIdentifier(%q) = %+v, want %+v", c.ID, l, want)
			}

			if s.IsOsiApprovedByIdentifier(c.ID) != c.OSI || s.IsDeprecatedByIdentifier(c.ID) != c.Deprecated {
				t.Errorf("%q: osi/deprecated mismatch", c.ID)
			}
		}

		e, ok := s.GetExceptionByIdentifier(c.ID)
		if ok != (c.Exception != nil) {
			t.Errorf("getExceptionByIdentifier(%q) found = %v", c.ID, ok)
		} else if ok && e != (ExceptionInfo{c.Exception[0].(string), c.Exception[1].(string)}) {
			t.Errorf("getExceptionByIdentifier(%q) = %+v, want %v", c.ID, e, c.Exception)
		}
	}

	for _, c := range o.ByName {
		id, ok := s.GetIdentifierByName(c.Name)
		if ok != (c.ID != nil) || (ok && id != *c.ID) {
			t.Errorf("getIdentifierByName(%q) = %q, %v; want %v", c.Name, id, ok, c.ID)
		}
	}

	licenses := s.GetLicenses()
	if len(licenses) != len(o.Licenses) {
		t.Fatalf("getLicenses(): %d entries, want %d", len(licenses), len(o.Licenses))
	}

	for i, w := range o.Licenses {
		want := License{w[1].(string), w[2].(string), w[3].(bool), w[4].(bool)}
		if licenses[i] != want {
			t.Errorf("getLicenses()[%d] = %+v, want %+v", i, licenses[i], want)
		}

		if got, ok := s.GetLicense(w[0].(string)); !ok || got != want {
			t.Errorf("getLicenses()[%q] = %+v, %v", w[0], got, ok)
		}
	}
}

func BenchmarkValidate(b *testing.B) {
	s := New()
	b.ReportAllocs()

	for b.Loop() {
		s.Validate("(MIT OR Apache-2.0) AND GPL-2.0-or-later WITH Classpath-exception-2.0")
	}
}

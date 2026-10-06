// Ports tests/Composer/Test/Platform/VersionTest.php.

package platform

import (
	"testing"

	"github.com/stubbedev/maestro/internal/semver"
)

func TestVersion_ParseOpensslVersions(t *testing.T) {
	// input, parsed version, fips expected, normalized version ("" when
	// it equals the parsed version)
	cases := []struct {
		input, parsed string
		fips          bool
		normalized    string
	}{
		// Generated
		{"1.2.3", "1.2.3.0", false, ""},
		{"1.2.3-beta3", "1.2.3.0-beta3", false, ""},
		{"1.2.3-beta3-dev", "1.2.3.0-beta3-dev", false, ""},
		{"1.2.3-beta3-fips", "1.2.3.0-beta3", true, ""},
		{"1.2.3-beta3-fips-dev", "1.2.3.0-beta3-dev", true, ""},
		{"1.2.3-dev", "1.2.3.0-dev", false, ""},
		{"1.2.3-fips", "1.2.3.0", true, ""},
		{"1.2.3-fips-beta3", "1.2.3.0-beta3", true, ""},
		{"1.2.3-fips-beta3-dev", "1.2.3.0-beta3-dev", true, ""},
		{"1.2.3-fips-dev", "1.2.3.0-dev", true, ""},
		{"1.2.3-pre2", "1.2.3.0-alpha2", false, ""},
		{"1.2.3-pre2-dev", "1.2.3.0-alpha2-dev", false, ""},
		{"1.2.3-pre2-fips", "1.2.3.0-alpha2", true, ""},
		{"1.2.3-pre2-fips-dev", "1.2.3.0-alpha2-dev", true, ""},
		{"1.2.3a", "1.2.3.1", false, ""},
		{"1.2.3a-beta3", "1.2.3.1-beta3", false, ""},
		{"1.2.3a-beta3-dev", "1.2.3.1-beta3-dev", false, ""},
		{"1.2.3a-dev", "1.2.3.1-dev", false, ""},
		{"1.2.3a-dev-fips", "1.2.3.1-dev", true, ""},
		{"1.2.3a-fips", "1.2.3.1", true, ""},
		{"1.2.3a-fips-beta3", "1.2.3.1-beta3", true, ""},
		{"1.2.3a-fips-dev", "1.2.3.1-dev", true, ""},
		{"1.2.3beta3", "1.2.3.0-beta3", false, ""},
		{"1.2.3beta3-dev", "1.2.3.0-beta3-dev", false, ""},
		{"1.2.3zh", "1.2.3.34", false, ""},
		{"1.2.3zh-dev", "1.2.3.34-dev", false, ""},
		{"1.2.3zh-fips", "1.2.3.34", true, ""},
		{"1.2.3zh-fips-dev", "1.2.3.34-dev", true, ""},
		// Additional cases
		{"1.2.3zh-fips-rc3", "1.2.3.34-rc3", true, "1.2.3.34-RC3"},
		{"1.2.3zh-alpha10-fips", "1.2.3.34-alpha10", true, ""},
		{"1.1.1l (Schannel)", "1.1.1.12", false, ""},
		// Check that alphabetical patch levels overflow correctly
		{"1.2.3", "1.2.3.0", false, ""},
		{"1.2.3a", "1.2.3.1", false, ""},
		{"1.2.3z", "1.2.3.26", false, ""},
		{"1.2.3za", "1.2.3.27", false, ""},
		{"1.2.3zy", "1.2.3.51", false, ""},
		{"1.2.3zz", "1.2.3.52", false, ""},
		// 3.x
		{"3.0.0", "3.0.0", false, "3.0.0.0"},
		{"3.2.4-dev", "3.2.4-dev", false, "3.2.4.0-dev"},
	}

	for _, c := range cases {
		parsed, isFips, ok, err := ParseOpenssl(c.input)
		if err != nil || !ok || parsed != c.parsed || isFips != c.fips {
			t.Errorf("ParseOpenssl(%q) = %q, %v, %v; want %q, %v", c.input, parsed, isFips, ok, c.parsed, c.fips)

			continue
		}

		want := c.normalized
		if want == "" {
			want = c.parsed
		}

		if got, err := (semver.VersionParser{}).Normalize(parsed); err != nil || got != want {
			t.Errorf("normalize(%q) = %q, %v; want %q", parsed, got, err, want)
		}
	}

	if v, fips, ok, err := ParseOpenssl("not a version"); err != nil || ok || fips || v != "" {
		t.Errorf("ParseOpenssl(invalid) = %q, %v, %v, %v", v, fips, ok, err)
	}
}

func TestVersion_ParseLibjpegVersion(t *testing.T) {
	for _, c := range [][2]string{
		{"9", "9.0"},
		{"9a", "9.1"},
		{"9b", "9.2"},
		// Never seen in the wild, just for overflow correctness
		{"9za", "9.27"},
	} {
		if got, ok, err := ParseLibjpeg(c[0]); err != nil || !ok || got != c[1] {
			t.Errorf("ParseLibjpeg(%q) = %q, %v, %v; want %q", c[0], got, ok, err, c[1])
		}
	}

	if _, ok, err := ParseLibjpeg("9.0"); err != nil || ok {
		t.Error("ParseLibjpeg(9.0) should be null")
	}
}

func TestVersion_ParseZoneinfoVersion(t *testing.T) {
	for _, c := range [][2]string{
		{"2019c", "2019.3"},
		{"2020a", "2020.1"},
		// Never happened so far but fixate overflow behavior
		{"2020za", "2020.27"},
	} {
		if got, ok, err := ParseZoneinfoVersion(c[0]); err != nil || !ok || got != c[1] {
			t.Errorf("ParseZoneinfoVersion(%q) = %q, %v, %v; want %q", c[0], got, ok, err, c[1])
		}
	}

	if _, ok, err := ParseZoneinfoVersion("20a"); err != nil || ok {
		t.Error("ParseZoneinfoVersion(20a) should be null")
	}
}

func TestVersion_ConvertVersionIds(t *testing.T) {
	for _, c := range []struct {
		id   int64
		want string
	}{
		{30411, "3.4.11"},
		{20601, "2.6.1"},
		{0, "0.0.0"},
		{99, "0.0.99"},
		{1000000, "100.0.0"},
	} {
		if got := ConvertLibxpmVersionId(c.id); got != c.want {
			t.Errorf("ConvertLibxpmVersionId(%d) = %q, want %q", c.id, got, c.want)
		}

		if got := ConvertOpenldapVersionId(c.id); got != c.want {
			t.Errorf("ConvertOpenldapVersionId(%d) = %q, want %q", c.id, got, c.want)
		}
	}
}

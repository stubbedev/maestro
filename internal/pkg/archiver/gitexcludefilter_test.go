package archiver

import "testing"

// Ports tests/Composer/Test/Package/Archiver/GitExcludeFilterTest.php.

func TestGitExcludeFilter_PatternEscape(t *testing.T) {
	for _, c := range []struct {
		ignore string
		want   ExcludePattern
	}{
		{"app/config/parameters.yml export-ignore", ExcludePattern{`{(?=[^\.])app/(?=[^\.])config/(?=[^\.])parameters\.yml(?=$|/)}`, false, false}},
		{"app/config/parameters.yml -export-ignore", ExcludePattern{`{(?=[^\.])app/(?=[^\.])config/(?=[^\.])parameters\.yml(?=$|/)}`, true, false}},
	} {
		filter, err := NewGitExcludeFilter("/")
		if err != nil {
			t.Fatal(err)
		}

		if got, ok := filter.ParseGitAttributesLine(c.ignore); !ok || got != c.want {
			t.Errorf("ParseGitAttributesLine(%q) = %+v, %v, want %+v", c.ignore, got, ok, c.want)
		}
	}
}

package advisory

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
)

func TestOSVRangeIntervals(t *testing.T) {
	parser := pkg.NewVersionParser()
	for _, tc := range []struct {
		name   string
		events []osvEvent
		want   []string
	}{
		{"from the start", []osvEvent{{Introduced: "0"}, {Fixed: "6.5.8"}}, []string{"<6.5.8"}},
		{"bounded", []osvEvent{{Introduced: "7.0.0"}, {Fixed: "7.4.5"}}, []string{">=7.0.0 <7.4.5"}},
		{"last affected", []osvEvent{{Introduced: "1.0"}, {LastAffected: "1.2.3"}}, []string{">=1.0 <=1.2.3"}},
		{"limit", []osvEvent{{Introduced: "2.0"}, {Limit: "3.0"}}, []string{">=2.0 <3.0"}},
		{"open ended", []osvEvent{{Introduced: "8.0.0"}}, []string{">=8.0.0"}},
		{"every version", []osvEvent{{Introduced: "0"}}, []string{"*"}},
		{
			"several intervals, unsorted",
			[]osvEvent{{Fixed: "8.8.1"}, {Introduced: "8.8.0"}, {Introduced: "8.0.0"}, {Fixed: "8.7.11"}},
			[]string{">=8.0.0 <8.7.11", ">=8.8.0 <8.8.1"},
		},
		{"v prefix", []osvEvent{{Introduced: "v1.0.3"}, {Fixed: "v2.0.0"}}, []string{">=v1.0.3 <v2.0.0"}},
		{"a fix without an introduction", []osvEvent{{Fixed: "1.0.0"}}, nil},
		{"a version Composer cannot read", []osvEvent{{Introduced: "0"}, {Fixed: "not a version"}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := osvRangeIntervals(tc.events, parser); !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOSVAffectedVersions(t *testing.T) {
	parser := pkg.NewVersionParser()
	affected := func(name, ecosystem string, ranges []osvRange, versions ...string) osvAffected {
		return osvAffected{Package: osvPackage{Name: name, Ecosystem: ecosystem}, Ranges: ranges, Versions: versions}
	}
	ecosystem := func(typ string, events ...osvEvent) []osvRange { return []osvRange{{Type: typ, Events: events}} }

	for _, tc := range []struct {
		name     string
		affected []osvAffected
		want     string
	}{
		{
			"entries of the package are alternatives",
			[]osvAffected{
				affected("vendor/pkg", "Packagist", ecosystem("ECOSYSTEM", osvEvent{Introduced: "0"}, osvEvent{Fixed: "1.5"})),
				affected("Vendor/Pkg", "Packagist", ecosystem("SEMVER", osvEvent{Introduced: "2.0"}, osvEvent{Fixed: "2.3"})),
			},
			"<1.5 || >=2.0 <2.3",
		},
		{
			"other packages and ecosystems are left out",
			[]osvAffected{
				affected("vendor/other", "Packagist", ecosystem("ECOSYSTEM", osvEvent{Introduced: "0"})),
				affected("vendor/pkg", "npm", ecosystem("ECOSYSTEM", osvEvent{Introduced: "0"})),
			},
			"",
		},
		{
			"listed versions without a readable range",
			[]osvAffected{affected("vendor/pkg", "Packagist", ecosystem("GIT", osvEvent{Introduced: "abc123"}), "1.0.0", "not a version", "1.0.1")},
			"1.0.0 || 1.0.1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := osvAffectedVersions("vendor/pkg", tc.affected, parser); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Ports tests/Composer/Test/Package/PackageTest.php.

package pkg_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
)

func TestPackage_SetSourceDistReferences(t *testing.T) {
	const oldRef = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const newRef = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	ref := pkg.Str
	cases := []struct {
		name                  string
		distURL               string
		distReference         pkg.NullString
		expectedURL           string
		expectedDistReference pkg.NullString
	}{
		{"github api zipball", "https://api.github.com/repos/foo/bar/zipball/" + oldRef, ref(oldRef), "https://api.github.com/repos/foo/bar/zipball/" + newRef, ref(newRef)},
		{"gitlab sha query", "https://gitlab.com/api/v4/projects/foo%2Fbar/repository/archive.zip?sha=" + oldRef, ref(oldRef), "https://gitlab.com/api/v4/projects/foo%2Fbar/repository/archive.zip?sha=" + newRef, ref(newRef)},
		{"bitbucket zip archive", "https://bitbucket.org/foo/bar/get/" + oldRef + ".zip", ref(oldRef), "https://bitbucket.org/foo/bar/get/" + newRef + ".zip", ref(newRef)},
		{"codeberg zip archive", "https://codeberg.org/foo/bar/archive/" + oldRef + ".zip", ref(oldRef), "https://codeberg.org/foo/bar/archive/" + newRef + ".zip", ref(newRef)},
		{"codeberg uppercase host", "https://CODEBERG.ORG/foo/bar/archive/" + oldRef + ".zip", ref(oldRef), "https://CODEBERG.ORG/foo/bar/archive/" + newRef + ".zip", ref(newRef)},
		{"codeberg api archive", "https://codeberg.org/api/v1/repos/foo/bar/archive/" + oldRef + ".zip", ref(oldRef), "https://codeberg.org/api/v1/repos/foo/bar/archive/" + newRef + ".zip", ref(newRef)},
		{"codeberg zip archive with query", "https://codeberg.org/api/v1/repos/foo/bar/archive/" + oldRef + ".zip?token=x", ref(oldRef), "https://codeberg.org/api/v1/repos/foo/bar/archive/" + newRef + ".zip?token=x", ref(newRef)},
		{"codeberg without prior dist reference", "https://codeberg.org/foo/bar/archive/" + oldRef + ".zip", pkg.NullString{}, "https://codeberg.org/foo/bar/archive/" + newRef + ".zip", ref(newRef)},
		{"codeberg tar.gz is not rewritten", "https://codeberg.org/foo/bar/archive/" + oldRef + ".tar.gz", pkg.NullString{}, "https://codeberg.org/foo/bar/archive/" + oldRef + ".tar.gz", ref(newRef)},
		{"unknown host keeps url", "https://example.org/foo/bar/archive/" + oldRef + ".zip", ref(oldRef), "https://example.org/foo/bar/archive/" + oldRef + ".zip", ref(newRef)},
		{"codeberg lookalike host is not matched", "https://notcodeberg.org/foo/bar/archive/" + oldRef + ".zip", pkg.NullString{}, "https://notcodeberg.org/foo/bar/archive/" + oldRef + ".zip", pkg.NullString{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := pkg.NewPackage("foo/bar", "1.0.0.0", "1.0")
			p.SetSourceReference(ref(oldRef))
			p.SetDistType(ref("zip"))
			p.SetDistURL(ref(c.distURL))
			p.SetDistReference(c.distReference)

			p.SetSourceDistReferences(newRef)

			if got := p.SourceReference(); got != ref(newRef) {
				t.Errorf("source reference %v, want %s", got, newRef)
			}
			if got := p.DistReference(); got != c.expectedDistReference {
				t.Errorf("dist reference %v, want %v", got, c.expectedDistReference)
			}
			if got := p.DistURL(); got != ref(c.expectedURL) {
				t.Errorf("dist url %v, want %s", got, c.expectedURL)
			}
		})
	}
}

package composerrepo

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// Options that are not an array are kept (an untyped property) and fail
// where they reach a typed parameter (strict_types): without an event
// dispatcher, the HttpDownloader::get() of fetchFile; with one,
// PreFileDownloadEvent::setTransportOptions() (create-project with such a
// --repository, checked against Composer 2.10.3).
func TestComposerRepository_OptionsNotArray(t *testing.T) {
	r := newRepo(t, php.ArrayOf("url", "https://example.org", "options", "x"), createConfig(t), mock(t))
	_, err := r.fetchFileRelative("packages.json")
	want := `Composer\Util\HttpDownloader::get(): Argument #2 ($options) must be of type array, string given, called in ` + phperr.AbsPath("ComposerRepository.php") + " on line 1761"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v\nwant %s", err, want)
	}
	if site, _ := phperr.SiteOf(err); site.File != "HttpDownloader.php" || site.Line != 105 {
		t.Errorf("site %s:%d", site.File, site.Line)
	}
}

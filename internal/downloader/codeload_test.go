package downloader

import (
	"os"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

const (
	zipballURL  = "https://api.github.com/repos/acme/lib/zipball/0123456789abcdef0123456789abcdef01234567"
	codeloadURL = "https://codeload.github.com/acme/lib/legacy.zip/0123456789abcdef0123456789abcdef01234567"
)

// distPackageAt is a package with a dist of typ at url.
func distPackageAt(url, typ string) *pkg.CompletePackage {
	p := dummyPackage()
	p.SetDistType(pkg.Str(typ))
	p.SetDistURL(pkg.Str(url))
	p.SetDistReference(pkg.Str("0123456789abcdef0123456789abcdef01234567"))

	return p
}

// Only an api.github.com zipball URL of a zip dist is fetched from
// codeload, and only with no PRE_FILE_DOWNLOAD listener.
func TestFileDownloader_CodeloadURL(t *testing.T) {
	d := newTestFileDownloader(t, nil, nil, nil, nil, nil)
	for url, want := range map[string]string{
		zipballURL: codeloadURL,
		"https://api.github.com/repos/acme/lib/tarball/0123":       "",
		"https://api.github.com/repos/acme/lib/zipball/0123?x=1":   "",
		"https://api.github.com/repos/acme/lib/zipball/feat%2Fx":   "",
		"https://api.github.com/repos/acme/lib/zipball/0123/extra": "",
		"https://api.github.com/repos/acme//zipball/0123":          "",
		"http://api.github.com/repos/acme/lib/zipball/0123":        "",
		"https://github.example.org/repos/acme/lib/zipball/0123":   "",
	} {
		if got := d.codeloadURL(distPackageAt(url, "zip"), url); got != want {
			t.Errorf("%s: %q, want %q", url, got, want)
		}
	}
	if got := d.codeloadURL(distPackageAt(zipballURL, "tar"), zipballURL); got != "" {
		t.Errorf("a tar dist: %q", got)
	}

	// a dispatcher that cannot tell may have a listener changing the URL
	withListener := newTestFileDownloader(t, nil, nil, &fakeDispatcher{}, nil, nil)
	if got := withListener.codeloadURL(distPackageAt(zipballURL, "zip"), zipballURL); got != "" {
		t.Errorf("with a listener: %q", got)
	}
}

// A GitHub dist is fetched from codeload, and from its URL when codeload
// fails; either way the files cache keeps it under its URL's key.
func TestFileDownloader_DownloadsFromCodeload(t *testing.T) {
	for _, tc := range []struct {
		name          string
		codeloadFails bool
		want          []string
	}{
		{"codeload", false, []string{codeloadURL}},
		{"not found there", true, []string{codeloadURL, zipballURL}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir()
			h := &fakeHTTP{copy: func(url, to string) (*http.Response, error) {
				if tc.codeloadFails && url == codeloadURL {
					return nil, &util.TransportError{Message: `The "` + url + `" file could not be downloaded (HTTP/2 404 )`, Code: 404, StatusCode: 404}
				}
				if err := os.WriteFile(to, []byte("zip"), 0o644); err != nil {
					return nil, err
				}

				return http.NewResponse(url, 200, nil, ""), nil
			}}
			var cached []string
			cache := &fakeCache{t: t, copyFrom: func(key, _ string) bool { cached = append(cached, key); return true }}
			d := newTestFileDownloader(t, nil, getConfig(t, "vendor-dir", path+"/vendor"), nil, cache, h)

			p := distPackageAt(zipballURL, "zip")
			if err := await(d.Download(p, path+"/pkg", nil)); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(h.calls, tc.want) {
				t.Errorf("requested %q, want %q", h.calls, tc.want)
			}
			if want := []string{cacheKey(p, zipballURL)}; !slices.Equal(cached, want) {
				t.Errorf("cached under %q, want %q", cached, want)
			}
		})
	}
}

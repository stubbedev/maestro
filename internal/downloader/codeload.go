// Ports nothing: GitHub dists fetched from codeload.github.com directly
// (deliberate deviation 3, speed).

package downloader

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// codeloadURL is where GitHub serves p's dist when url, the processed URL
// of its request, is the api.github.com zipball URL Packagist gives GitHub
// packages: api.github.com only redirects it to
// codeload.github.com/{owner}/{repo}/legacy.zip/{ref}, the same bytes,
// which costs a round trip and GitHub's work behind it. It is "" for any
// other URL or dist type, and when a PRE_FILE_DOWNLOAD listener could
// change the request.
func (d *FileDownloader) codeloadURL(p pkg.PackageInterface, url string) string {
	if p.DistType().S != "zip" {
		return ""
	}
	rest, ok := strings.CutPrefix(url, "https://api.github.com/repos/")
	if !ok || strings.ContainsAny(rest, "?#%") {
		return ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 4 || parts[2] != "zipball" || slices.Contains(parts, "") {
		return ""
	}
	getter, _ := d.http.(http.Getter)
	if eventdispatcher.MayListen(d.events, eventdispatcher.NewPreFileDownloadEvent(eventdispatcher.PreFileDownload, getter, url, "package", p)) {
		return ""
	}

	return "https://codeload.github.com/" + parts[0] + "/" + parts[1] + "/legacy.zip/" + parts[3]
}

// transfer requests p's dist at url into file: from codeload.github.com
// when GitHub serves it there (codeloadURL), and from url when that fails
// in any way, so that what fails is url's request (a private repository
// is not found there, and takes url's authentication).
func (d *FileDownloader) transfer(p pkg.PackageInterface, url, file string) (*util.Promise[*http.Response], error) {
	options := p.TransportOptions()
	direct := d.codeloadURL(p, url)
	if direct == "" {
		return d.http.AddCopy(url, file, options)
	}
	first, err := d.http.AddCopy(direct, file, options)
	if err != nil {
		return d.http.AddCopy(url, file, options)
	}

	return util.Chain(first, func(r *http.Response) (*util.Promise[*http.Response], *http.Response, error) {
		return nil, r, nil
	}, func(error) (*util.Promise[*http.Response], *http.Response, error) {
		next, err := d.http.AddCopy(url, file, options)

		return next, nil, err
	}), nil
}

// Ports nothing: downloads into a file started ahead of time (deliberate
// deviation 3, speed).

package http

import (
	"context"
	"io"
	"os"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// PrefetchCopy is Prefetch for a later AddCopy(url, to, options): the body
// goes to a private spool file, and the AddCopy taking the transfer copies
// it into its own file, then prints, retries and settles exactly as
// without it. Nothing reaches to or any cache before that AddCopy. Not on
// Windows, which cannot unlink a file held open.
func (h *HttpDownloader) PrefetchCopy(url string, options *php.Array) {
	if util.IsWindows() {
		return
	}

	h.prefetch(url, options, false, true)
}

// newSpool returns an unlinked temporary file, nil when none can be made.
func newSpool() *os.File {
	f, err := os.CreateTemp("", "maestro-prefetch-*")
	if err != nil {
		return nil
	}

	if os.Remove(f.Name()) != nil {
		_ = f.Close()

		return nil
	}

	return f
}

// handTo returns the result of t, a finished prefetched transfer, as the
// result of r, an identical request: a body t spooled is copied into r's
// file first. A copy that fails makes r's request after all.
func (t *prefetchedTransfer) handTo(ctx context.Context, p *transportPool, r *transferRequest) *transferResult {
	spool := t.r.body
	if spool == nil || spool == r.body {
		return t.res
	}

	defer func() { _ = spool.Close() }()

	if res := t.res; res == nil || res.errno != 0 || res.err != nil {
		return t.res
	}

	if _, err := spool.Seek(0, io.SeekStart); err == nil {
		if _, err = io.Copy(fileWriter{r.body}, spool); err == nil {
			return t.res
		}
	}

	// what the copy wrote is overwritten (a file that cannot be rewound
	// cannot be written either: the request then fails writing)
	_, _ = r.body.Seek(0, io.SeekStart)
	_ = r.body.Truncate(0)

	return p.do(ctx, r)
}

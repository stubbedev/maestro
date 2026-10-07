package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// A prefetched download is made once, silently, into a spool: the Copy of
// the same URL takes it, writes it into its own file and prints what it
// prints without a prefetch; a Get of the same URL does not take it.
func TestHttpDownloader_PrefetchCopyIsTakenByTheCopy(t *testing.T) {
	if util.IsWindows() {
		t.Skip("no prefetched copies on Windows")
	}

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte("PK archive bytes"))
	}))
	defer srv.Close()

	h, b := newPrefetchDownloader(t)
	ssl, _ := h.options.At("ssl").(*php.Array)
	cafile, _ := optionString(ssl, "cafile")
	ValidateCaFile(cafile, nil)
	mark := len(b.Output())

	h.PrefetchCopy(srv.URL+"/a.zip", nil)
	h.PrefetchCopy(srv.URL+"/a.zip", nil) // the same transfer
	waitFor(t, func() bool { return calls.Load() == 1 })
	if out := b.Output()[mark:]; out != "" {
		t.Fatalf("the prefetch printed %q", out)
	}

	// a request kept in memory is another exchange
	if _, err := h.Get(srv.URL+"/a.zip", nil); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("%d requests reached the server, want 2", n)
	}

	target := filepath.Join(t.TempDir(), "a.zip")
	mark = len(b.Output())
	r, err := h.Copy(srv.URL+"/a.zip", target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode() != 200 {
		t.Errorf("status %d", r.StatusCode())
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("%d requests reached the server, want 2", n)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "PK archive bytes" {
		t.Errorf("file %q %v", data, err)
	}
	if out := b.Output()[mark:]; !strings.Contains(out, "Downloading "+srv.URL+"/a.zip") || !strings.Contains(out, "[200] "+srv.URL+"/a.zip") {
		t.Errorf("output %q lacks the request's lines", out)
	}

	// taken once
	if _, err := h.Copy(srv.URL+"/a.zip", target, nil); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("%d requests reached the server, want 3", n)
	}
}

package http

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestDecodingReader_CurlParity checks content decoding against what
// php-curl (libcurl 8.22 with zlib, brotli and zstd) returned for the same
// bodies (testdata/encoding, "hello encoded world" encoded by the named
// encodings; trunc- holds the first half of a longer text's encoding,
// corrupt- has bytes flipped in its middle third).
func TestDecodingReader_CurlParity(t *testing.T) {
	const hello = "hello encoded world"

	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join("testdata", "encoding", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}

		return string(data)
	}

	gz := func(s string) string {
		var b bytes.Buffer
		w := gzip.NewWriter(&b)
		_, _ = w.Write([]byte(s))
		_ = w.Close()

		return b.String()
	}

	zl := func(s string) string {
		var b bytes.Buffer
		w := zlib.NewWriter(&b)
		_, _ = w.Write([]byte(s))
		_ = w.Close()

		return b.String()
	}

	raw := func(s string) string {
		var b bytes.Buffer
		w, _ := flate.NewWriter(&b, flate.DefaultCompression)
		_, _ = w.Write([]byte(s))
		_ = w.Close()

		return b.String()
	}

	for _, tc := range []struct {
		header, body string
		errno        int
		want         string // body, or the error message with errno
	}{
		{"gzip", read("gzip"), 0, hello},
		{"x-gzip", read("gzip"), 0, hello},
		{"GZIP", read("gzip"), 0, hello},
		{"deflate", read("deflate"), 0, hello},
		{"br", read("br"), 0, hello},
		{"zstd", read("zstd"), 0, hello},
		{"br, gzip", read("br-gzip"), 0, hello},
		{"zstd,deflate", read("zstd-deflate"), 0, hello},
		{"identity", hello, 0, hello},
		{"none", hello, 0, hello},
		{"", hello, 0, hello},
		{"foo", hello, 61, "Unrecognized content encoding type"},
		{"foo", "", 0, ""},
		{"gzip", "", 0, ""},
		{"gzip", "hello", 61, "Error while processing content unencoding: incorrect header check"},
		{"zstd", "hello", 61, "Unrecognized or bad HTTP Content or Transfer-Encoding"},
		{"br", "hello", 0, ""},
		{"gzip", read("trunc-gzip"), 0, "hello encoded world, hello e"},
		{"deflate", read("trunc-deflate"), 0, "hello encoded world, hello enc"},
		{"br", read("trunc-br"), 0, ""},
		{"zstd", read("trunc-zstd"), 0, ""},
		{"gzip", read("corrupt-gzip"), 61, "Error while processing content unencoding: incorrect data check"},
		{"deflate", read("corrupt-deflate"), 61, "Error while processing content unencoding: incorrect data check"},
		{"br", read("corrupt-br"), 61, "Unrecognized or bad HTTP Content or Transfer-Encoding"},
		{"zstd", read("corrupt-zstd"), 61, "Unrecognized or bad HTTP Content or Transfer-Encoding"},
		// zlib data after the end of the stream (gzcompress/gzencode/gzdeflate)
		{"gzip", zl("zlib body"), 0, "zlib body"},
		{"deflate", raw("raw one"), 0, "raw one"},
		{"gzip", gz("one") + gz("two"), 23, "Multi-member gzip response not supported"},
		{"gzip", gz("one") + "garbage", 23, "Failed writing received data to disk/application"},
		{"gzip", gz("one") + "\x1f", 23, "Failed writing received data to disk/application"},
		{"gzip", zl("one") + "garbage", 23, "Failed writing received data to disk/application"},
		{"deflate", zl("one") + "garbage", 23, "Failed writing received data to disk/application"},
		{"deflate", raw("one") + "garbage", 23, "Failed writing received data to disk/application"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if tc.header != "" {
				w.Header().Set("Content-Encoding", tc.header)
			}

			w.Header().Set("Content-Length", strconv.Itoa(len(tc.body)))
			_, _ = w.Write([]byte(tc.body))
		}))

		var pool transportPool

		res := pool.do(context.Background(), &transferRequest{url: srv.URL, decode: true, curlStatusLines: true, key: transportKey{fresh: true}})

		srv.Close()

		switch {
		case res.err != nil:
			t.Errorf("%s %q: %v", tc.header, tc.body, res.err)
		case tc.errno != 0 && (res.errno != tc.errno || res.errMsg != tc.want):
			t.Errorf("%s %q: got %d %q, want %d %q", tc.header, tc.body, res.errno, res.errMsg, tc.errno, tc.want)
		case tc.errno == 0 && (res.errno != 0 || string(res.body) != tc.want):
			t.Errorf("%s %q: got %d %q %q, want %q", tc.header, tc.body, res.errno, res.errMsg, res.body, tc.want)
		}
	}
}

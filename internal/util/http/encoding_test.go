package http

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestCurlAcceptEncoding checks the Accept-Encoding for libcurl builds:
// curl_version()'s features decide, curl 8.7.0/8.7.1 gets "gzip".
func TestCurlAcceptEncoding(t *testing.T) {
	all := int64(curlVersionLibz | curlVersionBrotli | curlVersionZstd)

	for _, tc := range []struct {
		info CurlInfo
		want string
	}{
		{CurlInfo{Version: "8.22.0", Features: all}, "deflate, gzip, br, zstd"},
		{CurlInfo{Version: "8.5.0", Features: curlVersionLibz}, "deflate, gzip"},
		{CurlInfo{Version: "8.5.0", Features: curlVersionLibz | curlVersionZstd}, "deflate, gzip, zstd"},
		{CurlInfo{Version: "8.7.1", Features: all}, "gzip"},
		{CurlInfo{Version: "8.5.0"}, ""},
	} {
		if got := curlAcceptEncoding(tc.info); got != tc.want {
			t.Errorf("%+v: got %q, want %q", tc.info, got, tc.want)
		}
	}

	// a libcurl without brotli does not know "br"
	r := decodingReaderFor(strings.NewReader("x"), "br", curlVersionLibz)
	if _, err := io.ReadAll(r); err == nil || err.Error() != "Unrecognized content encoding type" {
		t.Fatalf("got %v", err)
	}
}

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

	hx := func(s string) string {
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}

		return string(b)
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
		// corrupt streams fail with zlib's message (inflate.go); a
		// "deflate" body failing before its first block ends is decoded
		// again as raw deflate, which may then be followed by 4 bytes
		{"deflate", hx("4b043e"), 61, "Error while processing content unencoding: invalid distance code"},                                                                       // deflate-raw-baddist
		{"deflate", hx("1b03"), 61, "Error while processing content unencoding: invalid literal/length code"},                                                                   // deflate-raw-badlit
		{"deflate", hx("0700"), 61, "Error while processing content unencoding: invalid block type"},                                                                            // deflate-raw-btype
		{"deflate", hx("4b044200"), 61, "Error while processing content unencoding: invalid distance too far back"},                                                             // deflate-raw-far
		{"deflate", hx("010500000068656c6c6f"), 61, "Error while processing content unencoding: invalid stored block lengths"},                                                  // deflate-raw-stored
		{"deflate", hx("f500000000000000000000"), 61, "Error while processing content unencoding: too many length or distance symbols"},                                         // deflate-raw-toomany
		{"deflate", hx("789c4b043e"), 61, "Error while processing content unencoding: invalid stored block lengths"},                                                            // deflate-zlib-baddist
		{"deflate", hx("789c4b044200"), 61, "Error while processing content unencoding: invalid stored block lengths"},                                                          // deflate-zlib-far
		{"gzip", hx("1f8b08000000000000034b043e"), 61, "Error while processing content unencoding: invalid distance code"},                                                      // gzip-baddist
		{"gzip", hx("1f8b08000000000000031b03"), 61, "Error while processing content unencoding: invalid literal/length code"},                                                  // gzip-badlit
		{"gzip", hx("1f8b08000000000000030700"), 61, "Error while processing content unencoding: invalid block type"},                                                           // gzip-btype
		{"gzip", hx("1f8b0800000000000003cb48cdc9c95728cf2fca4951c818658fb2a9c406000000000058020000"), 61, "Error while processing content unencoding: incorrect data check"},   // gzip-crc
		{"gzip", hx("1f8b08000000000000034b044200"), 61, "Error while processing content unencoding: invalid distance too far back"},                                            // gzip-far
		{"gzip", hx("1f8b08e0000000000003cb48cdc9c95728cf2fca4951c818658fb2a9c40600"), 61, "Error while processing content unencoding: unknown header flags set"},               // gzip-flags
		{"gzip", hx("1f8b08020000000000030000cb48cdc9c95728cf2fca4951c818658fb2a9c40600"), 61, "Error while processing content unencoding: header crc mismatch"},                // gzip-hcrc
		{"gzip", hx("1f8b0800000000000003cb48cdc9c95728cf2fca4951c818658fb2a9c406000d2ead2559020000"), 61, "Error while processing content unencoding: incorrect length check"}, // gzip-len
		{"gzip", hx("1f8b0700000000000003cb48cdc9c95728cf2fca4951c818658fb2a9c40600"), 61, "Error while processing content unencoding: unknown compression method"},             // gzip-method
		{"gzip", hx("1f8b0800000000000003010500000068656c6c6f"), 61, "Error while processing content unencoding: invalid stored block lengths"},                                 // gzip-stored
		{"gzip", hx("1f8b0800000000000003f500000000000000000000"), 61, "Error while processing content unencoding: too many length or distance symbols"},                        // gzip-toomany
		{"deflate", hx("789ccb48cdc9c95728cf2fca4951c818658fb2a9c406008649e000"), 61, "Error while processing content unencoding: incorrect data check"},                        // zlib-adler
		{"gzip", hx("8705cb48cdc9c95728cf2fca4951c818658fb2a9c40600"), 61, "Error while processing content unencoding: unknown compression method"},                             // zlib-cm7
		{"gzip", hx("78bbcb48cdc9c95728cf2fca4951c818658fb2a9c40600"), 61, "Error while processing content unencoding: Unknown failure within decompression software."},         // zlib-dict
		{"gzip", hx("799ccb48cdc9c95728cf2fca4951c818658fb2a9c40600"), 61, "Error while processing content unencoding: incorrect header check"},                                 // zlib-method
		{"gzip", hx("881ccb48cdc9c95728cf2fca4951c818658fb2a9c40600"), 61, "Error while processing content unencoding: invalid window size"},                                    // zlib-window
		{"deflate", hx("05c1010900000080a0ffaf2d0000000000000000"), 61, "Error while processing content unencoding: invalid distances set"},                                     // d-distset
		{"deflate", hx("0580810800000080fcad0f0000000000000000"), 61, "Error while processing content unencoding: invalid literal/lengths set"},                                 // d-litset
		{"deflate", hx("050080e47f1b0000000000000000"), 61, "Error while processing content unencoding: invalid code -- missing end-of-block"},                                  // d-noeob
		{"deflate", hx("050092000000000000000000"), 61, "Error while processing content unencoding: invalid code lengths set"},                                                  // d-oversub
		{"deflate", hx("050002240000000000000000"), 61, "Error while processing content unencoding: invalid bit length repeat"},                                                 // d-repeat
		{"gzip", hx("1f8b080000000000000305c1010900000080a0ffaf2d0000000000000000"), 61, "Error while processing content unencoding: invalid distances set"},                    // g-distset
		{"gzip", hx("1f8b08000000000000030580810800000080fcad0f0000000000000000"), 61, "Error while processing content unencoding: invalid literal/lengths set"},                // g-litset
		{"gzip", hx("1f8b0800000000000003050080e47f1b0000000000000000"), 61, "Error while processing content unencoding: invalid code -- missing end-of-block"},                 // g-noeob
		{"gzip", hx("1f8b0800000000000003050092000000000000000000"), 61, "Error while processing content unencoding: invalid code lengths set"},                                 // g-oversub
		{"gzip", hx("1f8b0800000000000003050002240000000000000000"), 61, "Error while processing content unencoding: invalid bit length repeat"},                                // g-repeat
		{"deflate", hx("cbcf4b050061626364"), 0, "one"},                                                                                        // raw-trail4
		{"deflate", hx("cbcf4b05006162636465"), 23, "Failed writing received data to disk/application"},                                        // raw-trail5
		{"deflate", hx("789ccbcf4b0500029101436162"), 23, "Failed writing received data to disk/application"},                                  // zlib-trail
		{"deflate", hx("789c000200fdff686907"), 61, "Error while processing content unencoding: invalid block type"},                           // zlib-second
		{"deflate", hx("1f8b0800000000000003cbcf4b0500f1866c7a03000000"), 61, "Error while processing content unencoding: invalid block type"}, // gzip-as-deflate
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
		case tc.errno != 0 && (res.fail.Errno != tc.errno || res.fail.Message != tc.want):
			t.Errorf("%s %q: got %d %q, want %d %q", tc.header, tc.body, res.fail.Errno, res.fail.Message, tc.errno, tc.want)
		case tc.errno == 0 && (res.fail.Errno != 0 || string(res.body) != tc.want):
			t.Errorf("%s %q: got %d %q %q, want %q", tc.header, tc.body, res.fail.Errno, res.fail.Message, res.body, tc.want)
		}
	}
}

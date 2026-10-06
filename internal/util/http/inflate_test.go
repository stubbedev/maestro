package http

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"math/rand/v2"
	"strings"
	"testing"
)

// inflateSamples are inputs exercising stored, fixed and dynamic blocks,
// long codes, matches across the window and outputs beyond it.
func inflateSamples() [][]byte {
	rng := rand.New(rand.NewPCG(1, 2))

	random := make([]byte, 40_000)
	for i := range random {
		random[i] = byte(rng.Uint32())
	}

	skewed := make([]byte, 80_000)
	for i := range skewed {
		// a geometric distribution: long codes for the rare bytes
		b := 0
		for b < 255 && rng.IntN(3) == 0 {
			b++
		}

		skewed[i] = byte(b)
	}

	text := []byte(strings.Repeat(`{"name":"vendor/package","version":"1.2.3","require":{"php":">=8.1"}}`, 1000))

	return [][]byte{nil, []byte("a"), []byte("hello encoded world"), random, skewed, text, append(append([]byte(nil), text...), random...)}
}

// TestInflate_RoundTrip decodes what compress/flate, gzip and zlib encode
// at every level.
func TestInflate_RoundTrip(t *testing.T) {
	samples := inflateSamples()
	if testing.Short() {
		samples = samples[:4]
	}

	for i, data := range samples {
		for _, level := range []int{flate.HuffmanOnly, flate.NoCompression, flate.BestSpeed, flate.DefaultCompression, flate.BestCompression} {
			var raw, gz, zl bytes.Buffer

			fw, _ := flate.NewWriter(&raw, level)
			_, _ = fw.Write(data)
			_ = fw.Close()

			gw, _ := gzip.NewWriterLevel(&gz, level)
			_, _ = gw.Write(data)
			_ = gw.Close()

			zw, _ := zlib.NewWriterLevel(&zl, level)
			_, _ = zw.Write(data)
			_ = zw.Close()

			for _, tc := range []struct {
				name string
				wrap zlibWrap
				in   []byte
			}{
				{"raw", wrapRaw, raw.Bytes()},
				{"gzip", wrapAuto, gz.Bytes()},
				{"zlib", wrapAuto, zl.Bytes()},
				{"deflate", wrapZlib, zl.Bytes()},
				{"deflate-raw", wrapZlib, raw.Bytes()},
			} {
				got, err := io.ReadAll(newInflater(bufio.NewReader(bytes.NewReader(tc.in)), tc.wrap))

				switch {
				case len(data) == 0 && tc.name == "deflate-raw" && err != nil:
					// an empty raw stream is not a zlib header either
				case err != nil:
					t.Errorf("sample %d level %d %s: %v", i, level, tc.name, err)
				case !bytes.Equal(got, data):
					t.Errorf("sample %d level %d %s: got %d bytes, want %d", i, level, tc.name, len(got), len(data))
				}
			}
		}
	}
}

// TestInflate_SmallReads reads through small buffers and from a body
// arriving a byte at a time.
func TestInflate_SmallReads(t *testing.T) {
	data := inflateSamples()[6]

	var gz bytes.Buffer

	gw := gzip.NewWriter(&gz)
	_, _ = gw.Write(data)
	_ = gw.Close()

	f := newInflater(bufio.NewReader(&oneByteReader{gz.Bytes()}), wrapAuto)

	var got []byte

	buf := make([]byte, 7)

	for {
		n, err := f.Read(buf)
		got = append(got, buf[:n]...)

		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatal(err)
		}
	}

	if !bytes.Equal(got, data) {
		t.Fatalf("got %d bytes, want %d", len(got), len(data))
	}
}

type oneByteReader struct{ b []byte }

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}

	if len(p) == 0 {
		return 0, nil
	}

	p[0] = r.b[0]
	r.b = r.b[1:]

	return 1, nil
}

func BenchmarkInflate(b *testing.B) {
	data := inflateSamples()[6]

	var gz bytes.Buffer

	gw := gzip.NewWriter(&gz)
	_, _ = gw.Write(data)
	_ = gw.Close()

	b.SetBytes(int64(len(data)))

	b.Run("zlib-port", func(b *testing.B) {
		for b.Loop() {
			_, _ = io.Copy(io.Discard, newInflater(bufio.NewReader(bytes.NewReader(gz.Bytes())), wrapAuto))
		}
	})

	b.Run("compress-gzip", func(b *testing.B) {
		for b.Loop() {
			r, _ := gzip.NewReader(bytes.NewReader(gz.Bytes()))
			_, _ = io.Copy(io.Discard, r)
		}
	})
}

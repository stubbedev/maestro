package deflate64_test

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"hash/crc32"
	"io"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/archive/archivetest"
	"github.com/stubbedev/maestro/internal/archive/deflate64"
)

func inflate(t *testing.T, data []byte) ([]byte, error) {
	t.Helper()

	r := deflate64.NewReader(bytes.NewReader(data))
	defer func() { _ = r.Close() }()

	return io.ReadAll(r)
}

func randomText(n int, seed uint64) []byte {
	r := rand.New(rand.NewPCG(seed, seed))
	b := make([]byte, n)

	for i := range b {
		b[i] = "abcdefghij klmnop\n"[r.IntN(18)]
	}

	return b
}

// Streams using only what Deflate and Deflate64 share decode as Deflate
// does: stored blocks, and dynamic Huffman blocks of literals.
func TestSharedWithDeflate(t *testing.T) {
	for _, level := range []int{flate.NoCompression, flate.HuffmanOnly} {
		for _, n := range []int{0, 1, 1000, 70000, 300000} {
			data := randomText(n, uint64(n))

			var z bytes.Buffer

			w, err := flate.NewWriter(&z, level)
			if err != nil {
				t.Fatal(err)
			}

			_, _ = w.Write(data)
			_ = w.Close()

			got, err := inflate(t, z.Bytes())
			if err != nil {
				t.Fatalf("level %d, %d bytes: %v", level, n, err)
			}

			if !bytes.Equal(got, data) {
				t.Fatalf("level %d, %d bytes: output differs", level, n)
			}
		}
	}
}

// What only Deflate64 has: length code 285 with 16 extra bits, distance
// codes 30 and 31, the 64 KiB window.
func TestDeflate64Features(t *testing.T) {
	head := randomText(50000, 1)
	cases := map[string][]byte{
		"long run":       append([]byte("x"), bytes.Repeat([]byte{'x'}, 65538*2+17)...),
		"far repeat":     append(append([]byte{}, head...), head...),
		"window edge":    append(append(append([]byte{}, head...), randomText(15536, 2)...), head[:3000]...),
		"mixed":          []byte(strings.Repeat("abc", 30000) + string(head) + strings.Repeat("abc", 30000)),
		"just a literal": []byte("a"),
	}

	for name, data := range cases {
		got, err := inflate(t, archivetest.Deflate64(data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if !bytes.Equal(got, data) {
			t.Fatalf("%s: output differs (%d bytes, want %d)", name, len(got), len(data))
		}

		// Deflate reads the same stream differently or not at all.
		if out, err := io.ReadAll(flate.NewReader(bytes.NewReader(archivetest.Deflate64(data)))); err == nil && bytes.Equal(out, data) && name != "just a literal" {
			t.Errorf("%s: plain Deflate decodes the Deflate64 stream too: the case tests nothing", name)
		}
	}
}

// 7-Zip's Deflate64 (dynamic Huffman blocks using what only Deflate64
// has) decodes to what unzip extracts (testdata made with 7z a -tzip
// -mm=Deflate64; TestDifferentialZip64 compares it with unzip).
func TestSevenZip(t *testing.T) {
	zr, err := zip.OpenReader("../testdata/deflate64-7z.zip")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = zr.Close() }()

	seen := false

	for _, f := range zr.File {
		if f.Method != 9 {
			continue
		}

		seen = true

		raw, err := f.OpenRaw()
		if err != nil {
			t.Fatal(err)
		}

		data, err := io.ReadAll(raw)
		if err != nil {
			t.Fatal(err)
		}

		got, err := inflate(t, data)
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}

		if uint64(len(got)) != f.UncompressedSize64 {
			t.Fatalf("%s: %d bytes, want %d", f.Name, len(got), f.UncompressedSize64)
		}

		if sum := crc(got); sum != f.CRC32 {
			t.Fatalf("%s: CRC %08x, want %08x", f.Name, sum, f.CRC32)
		}
	}

	if !seen {
		t.Fatal("no Deflate64 entry in the fixture")
	}
}

func TestCorrupt(t *testing.T) {
	good := archivetest.Deflate64(randomText(100000, 3))

	for name, data := range map[string][]byte{
		"reserved block type": {0x07},
		"truncated":           good[:len(good)/2],
		"empty":               nil,
		"cut fixed block":     {0x03, 0x02},
	} {
		_, err := inflate(t, data)

		var corrupt deflate64.CorruptInputError
		if err == nil || (!errors.As(err, &corrupt) && !errors.Is(err, io.ErrUnexpectedEOF)) {
			t.Errorf("%s: %v, want corrupt input or unexpected EOF", name, err)
		}
	}
}

func TestReset(t *testing.T) {
	a, b := randomText(70000, 4), randomText(1000, 5)
	r := deflate64.NewReader(bytes.NewReader(archivetest.Deflate64(a)))

	if _, err := io.ReadAll(r); err != nil {
		t.Fatal(err)
	}

	if err := r.(deflate64.Resetter).Reset(bytes.NewReader(archivetest.Deflate64(b)), nil); err != nil {
		t.Fatal(err)
	}

	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, b) {
		t.Fatalf("after Reset: %d bytes, %v", len(got), err)
	}
}

func crc(b []byte) uint32 { return crc32.ChecksumIEEE(b) }

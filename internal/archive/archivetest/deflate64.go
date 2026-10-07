package archivetest

import "math/bits"

// Deflate64 compresses data as Deflate64 (zip method 9) in one fixed
// Huffman block, with a greedy LZ77 over Deflate64's 64 KiB window: it
// uses what Deflate lacks, matches longer than 258 bytes (length code 285
// with 16 extra bits) and distances past 32 KiB (distance codes 30 and
// 31), so what decodes it right is a Deflate64 decoder. It is meant for
// test archives, not for speed.
func Deflate64(data []byte) []byte {
	w := &bitWriter{}

	w.bits(1, 1) // final block
	w.bits(1, 2) // fixed Huffman codes

	const (
		window = 1 << 16
		maxLen = 65538
		minLen = 3
	)

	// heads maps three bytes to the positions holding them, latest last.
	heads := map[[3]byte][]int{}

	add := func(i int) {
		if i+minLen <= len(data) {
			k := [3]byte{data[i], data[i+1], data[i+2]}
			heads[k] = append(heads[k], i)
		}
	}

	for i := 0; i < len(data); {
		bestLen, bestDist := 0, 0

		if i+minLen <= len(data) {
			cands := heads[[3]byte{data[i], data[i+1], data[i+2]}]
			for j := len(cands) - 1; j >= 0 && j >= len(cands)-16; j-- {
				c := cands[j]
				if i-c > window {
					break
				}

				n := 0
				for n < maxLen && i+n < len(data) && data[c+n] == data[i+n] {
					n++
				}

				if n > bestLen {
					bestLen, bestDist = n, i-c
				}
			}
		}

		if bestLen < minLen {
			w.literal(int(data[i]))
			add(i)
			i++

			continue
		}

		w.length(bestLen)
		w.distance(bestDist)

		for k := range bestLen {
			add(i + k)
		}

		i += bestLen
	}

	w.literal(256)

	return w.flush()
}

// bitWriter writes a deflate bit stream, least significant bit first.
type bitWriter struct {
	out []byte
	acc uint64
	n   uint
}

func (w *bitWriter) bits(v uint64, n uint) {
	w.acc |= v << w.n
	w.n += n

	for w.n >= 8 {
		w.out = append(w.out, byte(w.acc)) //nolint:gosec // the low byte
		w.acc >>= 8
		w.n -= 8
	}
}

// code writes a Huffman code, most significant bit first.
func (w *bitWriter) code(c uint16, n uint) {
	w.bits(uint64(bits.Reverse16(c)>>(16-n)), n)
}

// literal writes a literal/length symbol with the fixed code.
func (w *bitWriter) literal(v int) {
	switch {
	case v < 144:
		w.code(uint16(0x30+v), 8) //nolint:gosec // v < 144
	case v < 256:
		w.code(uint16(0x190+v-144), 9)
	case v < 280:
		w.code(uint16(v-256), 7)
	default:
		w.code(uint16(0xc0+v-280), 8) //nolint:gosec // v < 288
	}
}

var (
	lengthBase  = [...]int{3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227}
	lengthExtra = [...]uint{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5}
	distBase    = [...]int{1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577, 32769, 49153}
)

// length writes a match length: codes 257 to 284 as in Deflate (284
// reaching 258), code 285 with 16 extra bits for anything longer.
func (w *bitWriter) length(n int) {
	if n > 258 {
		w.literal(285)
		w.bits(uint64(n-3), 16)

		return
	}

	i := len(lengthBase) - 1
	for lengthBase[i] > n {
		i--
	}

	w.literal(257 + i)
	w.bits(uint64(n-lengthBase[i]), lengthExtra[i]) //nolint:gosec // n >= lengthBase[i]
}

// distance writes a match distance with the fixed 5-bit codes.
func (w *bitWriter) distance(d int) {
	i := len(distBase) - 1
	for distBase[i] > d {
		i--
	}

	w.code(uint16(i), 5)

	extra := uint(0)
	if i >= 4 {
		extra = uint(i-2) / 2
	}

	w.bits(uint64(d-distBase[i]), extra) //nolint:gosec // d >= distBase[i]
}

func (w *bitWriter) flush() []byte {
	if w.n > 0 {
		w.out = append(w.out, byte(w.acc)) //nolint:gosec // the low byte
	}

	return w.out
}

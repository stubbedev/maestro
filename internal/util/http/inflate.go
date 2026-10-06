// Inflate as zlib does it for libcurl's gzip and deflate decoders
// (lib/content_encoding.c over zlib 1.3's inflate.c and inftrees.c), so
// that corrupt data fails where and as zlib fails: with z_stream.msg
// ("invalid distance too far back", "invalid stored block lengths", ...),
// which curl reports as "Error while processing content unencoding:
// <msg>". compress/flate only reports the offset of a defect.

package http

import (
	"bufio"
	"errors"
	"hash/crc32"
	"io"
)

// zlibError is a failure of zlib's inflate(): msg is z_stream.msg, ""
// when zlib sets none (Z_NEED_DICT), which curl words as an unknown
// failure (process_zlib_error).
type zlibError struct{ msg string }

func (e *zlibError) Error() string {
	if e.msg == "" {
		return "Unknown failure within decompression software."
	}

	return e.msg
}

// zlibWrap is the format inflateInit2 was asked for.
type zlibWrap int

const (
	// wrapRaw is raw deflate (windowBits -15).
	wrapRaw zlibWrap = iota
	// wrapZlib is a zlib stream (inflateInit: curl's "deflate").
	wrapZlib
	// wrapAuto is a gzip or zlib stream, told by its header (windowBits
	// 15 + 32: curl's "gzip").
	wrapAuto
)

const (
	inflateWindow = 1 << 15
	// curlDecompressBuffer is DECOMPRESS_BUFFER_SIZE, the output buffer
	// curl hands inflate().
	curlDecompressBuffer = 16384
	// inflateFastBits is the width of the first-level decoding table.
	inflateFastBits = 9
)

// inflater states.
const (
	infHeader = iota
	infBlock
	infStored
	infCodes
	infTrailer
	infTrailing
	infDone
)

// inflater decodes a zlib, gzip or raw deflate stream. As curl's deflate
// writer does, a zlib stream failing with a data error before any output
// was handed on (before the end of its first block, or 16 KiB of output)
// is decoded again from the start as raw deflate, some servers sending
// "deflate" without the zlib wrapper; up to 4 bytes may then follow the
// stream. Other bytes after the stream fail as curl fails them.
type inflater struct {
	in   inflateInput
	wrap zlibWrap
	gzip bool
	// retryRaw allows the restart as raw deflate; started is set once
	// curl would have handed output on.
	retryRaw, started bool
	trailerLen        int

	bits  uint64
	nbits uint

	hist   []byte
	wr, rd int
	// summed is where the check value was computed up to in hist.
	summed int
	total  int64

	state     int
	final     bool
	stored    int
	lit, dist *huffman
	copyLen   int
	copyDist  int
	check     uint32
	err       error
}

func newInflater(r *bufio.Reader, wrap zlibWrap) *inflater {
	f := &inflater{in: inflateInput{r: r}, wrap: wrap, hist: make([]byte, inflateWindow)}
	if wrap == wrapRaw {
		f.state = infBlock
		f.trailerLen = 4
	}

	if wrap == wrapZlib {
		f.retryRaw = true
		f.in.recording = true
	}

	return f
}

func (f *inflater) Read(b []byte) (int, error) {
	for {
		if f.rd < f.wr && (f.started || !f.retryRaw) {
			n := copy(b, f.hist[f.rd:f.wr])
			f.rd += n

			return n, nil
		}

		if f.rd == len(f.hist) {
			f.rd, f.wr, f.summed = 0, 0, 0
		}

		if f.err != nil {
			return 0, f.err
		}

		if f.state == infDone {
			return 0, io.EOF
		}

		err := f.step()

		var zerr *zlibError
		if errors.As(err, &zerr) && zerr.msg != "" && f.retryRaw && !f.started {
			f.restartRaw()

			continue
		}

		if err != nil {
			f.err = err
			f.in.recording, f.in.rec = false, nil

			if errors.As(err, &zerr) {
				// curl hands on no output of the failing inflate() call
				f.rd = f.wr
			}

			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				// a stream the body ends inside: what was decoded is the
				// data (inflate() returned it with Z_OK)
				f.err = io.ErrUnexpectedEOF
				f.started = true
			}
		}

		if f.started && f.in.recording {
			f.in.recording, f.in.rec = false, nil
		}
	}
}

// restartRaw starts over as raw deflate on the bytes read so far (curl's
// inflateInit2(z, -MAX_WBITS) on the same input, trailerlen 4).
func (f *inflater) restartRaw() {
	f.in.replay()
	f.wrap, f.retryRaw = wrapRaw, false
	f.bits, f.nbits = 0, 0
	f.wr, f.rd, f.summed, f.total = 0, 0, 0, 0
	f.state, f.final, f.copyLen = infBlock, false, 0
	f.trailerLen = 4
}

// step decodes until the window is full, the stream ends or fails.
func (f *inflater) step() error {
	err := f.run()

	f.sum()

	if f.retryRaw && f.total >= curlDecompressBuffer {
		f.started = true
	}

	if err == nil && f.state == infDone {
		return nil
	}

	return err
}

func (f *inflater) run() error {
	for f.wr < len(f.hist) {
		switch f.state {
		case infHeader:
			if err := f.header(); err != nil {
				return err
			}

			f.state = infBlock
		case infBlock:
			if err := f.blockHeader(); err != nil {
				return err
			}
		case infStored:
			n, err := f.readSome(f.hist[f.wr : f.wr+min(f.stored, len(f.hist)-f.wr)])
			f.wr += n
			f.total += int64(n)
			f.stored -= n

			if err != nil {
				return err
			}

			if f.stored == 0 {
				f.blockDone()
			}
		case infCodes:
			if err := f.codes(); err != nil {
				return err
			}
		case infTrailer:
			if err := f.trailer(); err != nil {
				return err
			}

			f.state = infTrailing
		case infTrailing:
			if err := f.trailing(); err != nil {
				return err
			}

			f.state = infDone
		case infDone:
			return nil
		}

		if f.retryRaw && f.total >= curlDecompressBuffer {
			f.started = true
		}
	}

	return nil
}

// sum brings the check value up to the output decoded.
func (f *inflater) sum() {
	if f.wrap != wrapRaw && f.wr > f.summed {
		if f.gzip {
			f.check = crc32.Update(f.check, crc32.IEEETable, f.hist[f.summed:f.wr])
		} else {
			f.check = adler32Update(f.check, f.hist[f.summed:f.wr])
		}
	}

	f.summed = f.wr
}

// blockDone ends a block: inflate(Z_BLOCK) returns to curl there, which
// hands the output on.
func (f *inflater) blockDone() {
	f.started = true

	if f.final {
		f.state = infTrailer
	} else {
		f.state = infBlock
	}
}

// header reads the zlib or gzip header (inflate.c, HEAD to DICT).
func (f *inflater) header() error {
	if !f.need(16) {
		return io.ErrUnexpectedEOF
	}

	if f.wrap == wrapAuto && f.bits&0xffff == 0x8b1f {
		f.gzip = true

		return f.gzipHeader()
	}

	cmf, flg := uint32(f.bits&0xff), uint32(f.bits>>8&0xff)

	switch {
	case (cmf<<8+flg)%31 != 0:
		return &zlibError{"incorrect header check"}
	case cmf&0x0f != 8:
		return &zlibError{"unknown compression method"}
	case cmf>>4+8 > 15:
		return &zlibError{"invalid window size"}
	}

	f.drop(16)
	f.check = 1

	if flg&0x20 != 0 {
		// DICTID, then Z_NEED_DICT: curl has no dictionary to give
		if !f.need(32) {
			return io.ErrUnexpectedEOF
		}

		return &zlibError{}
	}

	return nil
}

// gzipHeader reads a gzip header, the magic being in the bit buffer.
func (f *inflater) gzipHeader() error {
	hcrc := uint32(0)
	take := func(n int) ([]byte, error) {
		b := make([]byte, n)
		if err := f.readBytes(b); err != nil {
			return nil, err
		}

		hcrc = crc32.Update(hcrc, crc32.IEEETable, b)

		return b, nil
	}

	f.drop(16)
	hcrc = crc32.Update(hcrc, crc32.IEEETable, []byte{0x1f, 0x8b})

	b, err := take(2)
	if err != nil {
		return err
	}

	method, flags := b[0], b[1]

	switch {
	case method != 8:
		return &zlibError{"unknown compression method"}
	case flags&0xe0 != 0:
		return &zlibError{"unknown header flags set"}
	}

	// MTIME, XFL, OS
	if _, err := take(6); err != nil {
		return err
	}

	if flags&0x04 != 0 {
		b, err := take(2)
		if err != nil {
			return err
		}

		if _, err := take(int(b[0]) | int(b[1])<<8); err != nil {
			return err
		}
	}

	for _, flag := range []byte{0x08, 0x10} {
		if flags&flag == 0 {
			continue
		}

		for {
			b, err := take(1)
			if err != nil {
				return err
			}

			if b[0] == 0 {
				break
			}
		}
	}

	if flags&0x02 != 0 {
		b, err := take(2)
		if err != nil {
			return err
		}

		if uint32(b[0])|uint32(b[1])<<8 != hcrc&0xffff {
			return &zlibError{"header crc mismatch"}
		}
	}

	f.check = 0

	return nil
}

// blockHeader reads a block header (TYPEDO to LENLENS/CODELENS).
func (f *inflater) blockHeader() error {
	if !f.need(3) {
		return io.ErrUnexpectedEOF
	}

	f.final = f.bits&1 != 0
	kind := f.bits >> 1 & 3
	f.drop(3)

	switch kind {
	case 0:
		f.drop(f.nbits % 8)

		var b [4]byte
		if err := f.readBytes(b[:]); err != nil {
			return err
		}

		n, nn := uint16(b[0])|uint16(b[1])<<8, uint16(b[2])|uint16(b[3])<<8
		if n != ^nn {
			return &zlibError{"invalid stored block lengths"}
		}

		f.stored = int(n)
		f.state = infStored

		if n == 0 {
			f.blockDone()
		}
	case 1:
		f.lit, f.dist = fixedLit, fixedDist
		f.state = infCodes
	case 2:
		if err := f.dynamicTables(); err != nil {
			return err
		}

		f.state = infCodes
	default:
		return &zlibError{"invalid block type"}
	}

	return nil
}

// codeLengthOrder is the order of the code length code lengths.
var codeLengthOrder = [19]int{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}

// dynamicTables reads the code tables of a dynamic block (TABLE to
// CODELENS).
func (f *inflater) dynamicTables() error {
	if !f.need(14) {
		return io.ErrUnexpectedEOF
	}

	nlen := int(f.bits&0x1f) + 257
	ndist := int(f.bits>>5&0x1f) + 1
	ncode := int(f.bits>>10&0x0f) + 4
	f.drop(14)

	if nlen > 286 || ndist > 30 {
		return &zlibError{"too many length or distance symbols"}
	}

	var lens [320]uint8

	for i := range ncode {
		if !f.need(3) {
			return io.ErrUnexpectedEOF
		}

		lens[codeLengthOrder[i]] = uint8(f.bits & 7)
		f.drop(3)
	}

	codes, ok := newHuffman(lens[:19], huffCodes)
	if !ok {
		return &zlibError{"invalid code lengths set"}
	}

	lens = [320]uint8{}

	for have := 0; have < nlen+ndist; {
		sym, err := f.decode(codes)
		if err != nil {
			return err
		}

		if sym < 16 {
			lens[have] = uint8(sym) //nolint:gosec // a code length, below 16
			have++

			continue
		}

		var (
			val   uint8
			n     int
			extra uint
			base  int
		)

		switch sym {
		case 16:
			extra, base = 2, 3
		case 17:
			extra, base = 3, 3
		default:
			extra, base = 7, 11
		}

		if !f.need(extra) {
			return io.ErrUnexpectedEOF
		}

		if sym == 16 {
			if have == 0 {
				return &zlibError{"invalid bit length repeat"}
			}

			val = lens[have-1] //nolint:gosec // have is above 0 here
		}

		n = base + int(f.bits&(1<<extra-1)) //nolint:gosec // at most 7 bits
		f.drop(extra)

		if have+n > nlen+ndist {
			return &zlibError{"invalid bit length repeat"}
		}

		for range n {
			lens[have] = val
			have++
		}
	}

	if lens[256] == 0 {
		return &zlibError{"invalid code -- missing end-of-block"}
	}

	if f.lit, ok = newHuffman(lens[:nlen], huffLens); !ok {
		return &zlibError{"invalid literal/lengths set"}
	}

	if f.dist, ok = newHuffman(lens[nlen:nlen+ndist], huffDists); !ok {
		return &zlibError{"invalid distances set"}
	}

	return nil
}

var (
	lengthBase  = [29]int{3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258}
	lengthExtra = [29]uint{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0}
	distBase    = [30]int{1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577}
	distExtra   = [30]uint{0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13}
)

// codes decodes literals and matches of a Huffman block (LEN to MATCH).
func (f *inflater) codes() error {
	for f.wr < len(f.hist) {
		if f.copyLen > 0 {
			src := f.wr - f.copyDist
			if src < 0 {
				src += len(f.hist)
			}

			n := min(f.copyLen, len(f.hist)-f.wr, len(f.hist)-src, f.copyDist)
			copy(f.hist[f.wr:f.wr+n], f.hist[src:src+n])
			f.wr += n
			f.total += int64(n)
			f.copyLen -= n

			continue
		}

		sym, err := f.decode(f.lit)
		if err != nil {
			return err
		}

		switch {
		case sym < 0 || sym > 285:
			return &zlibError{"invalid literal/length code"}
		case sym < 256:
			f.hist[f.wr] = byte(sym)
			f.wr++
			f.total++

			continue
		case sym == 256:
			f.blockDone()

			return nil
		}

		sym -= 257

		if !f.need(lengthExtra[sym]) {
			return io.ErrUnexpectedEOF
		}

		length := lengthBase[sym] + int(f.bits&(1<<lengthExtra[sym]-1)) //nolint:gosec // at most 5 bits
		f.drop(lengthExtra[sym])

		dsym, err := f.decode(f.dist)
		if err != nil {
			return err
		}

		if dsym < 0 || dsym > 29 {
			return &zlibError{"invalid distance code"}
		}

		if !f.need(distExtra[dsym]) {
			return io.ErrUnexpectedEOF
		}

		dist := distBase[dsym] + int(f.bits&(1<<distExtra[dsym]-1)) //nolint:gosec // at most 13 bits
		f.drop(distExtra[dsym])

		if int64(dist) > f.total {
			return &zlibError{"invalid distance too far back"}
		}

		f.copyLen, f.copyDist = length, dist
	}

	return nil
}

// trailer checks the zlib or gzip trailer (CHECK, LENGTH).
func (f *inflater) trailer() error {
	if f.wrap == wrapRaw {
		return nil
	}

	f.sum()
	f.drop(f.nbits % 8)

	var b [4]byte
	if err := f.readBytes(b[:]); err != nil {
		return err
	}

	if f.gzip {
		if uint32(b[0])|uint32(b[1])<<8|uint32(b[2])<<16|uint32(b[3])<<24 != f.check {
			return &zlibError{"incorrect data check"}
		}

		if err := f.readBytes(b[:]); err != nil {
			return err
		}

		if uint32(b[0])|uint32(b[1])<<8|uint32(b[2])<<16|uint32(b[3])<<24 != uint32(f.total) { //nolint:gosec // ISIZE is the length modulo 2^32
			return &zlibError{"incorrect length check"}
		}

		return nil
	}

	if uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]) != f.check {
		return &zlibError{"incorrect data check"}
	}

	return nil
}

// trailing is curl's process_trailer once zlib reports the end of the
// stream: up to trailerLen bytes may follow (raw deflate), anything more
// is CURLE_WRITE_ERROR, a second gzip member with its own message.
func (f *inflater) trailing() error {
	f.drop(f.nbits % 8)
	f.in.recording = false

	limit := f.trailerLen + 2

	var next []byte

	for len(next) < limit {
		b, err := f.readByte()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return err
		}

		next = append(next, b)
	}

	if len(next) <= f.trailerLen {
		return nil
	}

	if f.gzip && len(next) >= 2 && next[0] == 0x1f && next[1] == 0x8b {
		return &encodingError{curleWriteError, "Multi-member gzip response not supported"}
	}

	return &encodingError{curleWriteError, curlWriteError}
}

// need makes n bits available; false at the end of the input.
func (f *inflater) need(n uint) bool {
	for f.nbits < n {
		b, err := f.in.ReadByte()
		if err != nil {
			return false
		}

		f.bits |= uint64(b) << f.nbits
		f.nbits += 8
	}

	return true
}

func (f *inflater) drop(n uint) {
	f.bits >>= n
	f.nbits -= n
}

// readByte reads a byte at a byte boundary: from the bit buffer first.
func (f *inflater) readByte() (byte, error) {
	if f.nbits >= 8 {
		b := byte(f.bits) //nolint:gosec // the next byte of the bit buffer
		f.drop(8)

		return b, nil
	}

	return f.in.ReadByte()
}

// readBytes fills b at a byte boundary.
func (f *inflater) readBytes(b []byte) error {
	_, err := f.readSome(b)

	return err
}

// readSome fills b at a byte boundary, returning how much it filled.
func (f *inflater) readSome(b []byte) (int, error) {
	n := 0

	for n < len(b) && f.nbits >= 8 {
		b[n] = byte(f.bits) //nolint:gosec // the next byte of the bit buffer
		f.drop(8)
		n++
	}

	if n == len(b) {
		return n, nil
	}

	m, err := io.ReadFull(&f.in, b[n:])
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}

	return n + m, err
}

// decode reads a symbol: -1 for a code the table marks invalid.
func (f *inflater) decode(h *huffman) (int, error) {
	if h.none {
		// inftrees.c's table for no symbols: one invalid entry of one
		// bit, whose value (0) the code length decoding uses as is
		if !f.need(1) {
			return 0, f.truncated()
		}

		f.drop(1)

		if h.kind == huffCodes {
			return 0, nil
		}

		return -1, nil
	}

	f.need(inflateFastBits)

	if e := h.fast[f.bits&(1<<inflateFastBits-1)]; e != 0 && uint(e&15) <= f.nbits {
		f.drop(uint(e & 15))

		return int(e >> 4), nil
	}

	code, first, index := 0, 0, 0

	for l := 1; l <= h.maxLen; l++ {
		if !f.need(1) {
			return 0, f.truncated()
		}

		code |= int(f.bits & 1)
		f.drop(1)

		count := int(h.count[l])
		if code-count < first {
			return int(h.symbol[index+code-first]), nil
		}

		index += count
		first += count
		first <<= 1
		code <<= 1
	}

	return -1, nil
}

// truncated is the error for input that ended (the body's own errors
// are the decoder's caller's, sourceReader).
func (f *inflater) truncated() error {
	return io.ErrUnexpectedEOF
}

// huffman tables (inftrees.c's CODES, LENS, DISTS).
const (
	huffCodes = iota
	huffLens
	huffDists
)

// huffman is a canonical Huffman code: a first-level table of
// inflateFastBits bits (symbol<<4 | length, 0 for longer or invalid
// codes) and the counts and symbols for the rest.
type huffman struct {
	kind   int
	none   bool
	maxLen int
	count  [16]uint16
	symbol []uint16
	fast   [1 << inflateFastBits]uint16
}

// newHuffman builds a code from code lengths, rejecting what
// inflate_table rejects: an over-subscribed set, and an incomplete one
// unless it is a literal/length or distance code of a single bit.
func newHuffman(lens []uint8, kind int) (*huffman, bool) {
	h := &huffman{kind: kind}

	for _, l := range lens {
		if l > 0 {
			h.count[l]++
			h.maxLen = max(h.maxLen, int(l))
		}
	}

	if h.maxLen == 0 {
		h.none = true

		return h, true
	}

	left := 1
	for l := 1; l < 16; l++ {
		left = left<<1 - int(h.count[l])
		if left < 0 {
			return nil, false
		}
	}

	if left > 0 && (kind == huffCodes || h.maxLen != 1) {
		return nil, false
	}

	var offs [16]int
	for l := 1; l < 15; l++ {
		offs[l+1] = offs[l] + int(h.count[l])
	}

	h.symbol = make([]uint16, offs[15]+int(h.count[15]))

	// the first code of each length (RFC 1951, 3.2.2)
	var next [16]int

	for l, code := 1, 0; l < 16; l++ {
		code = (code + int(h.count[l-1])) << 1
		next[l] = code
	}

	for sym, l := range lens {
		if l == 0 {
			continue
		}

		h.symbol[offs[l]] = uint16(sym)
		offs[l]++

		c := next[l]
		next[l]++

		if int(l) > inflateFastBits {
			continue
		}

		rev := 0
		for i := range int(l) {
			rev |= (c >> i & 1) << (int(l) - 1 - i)
		}

		for i := rev; i < len(h.fast); i += 1 << l {
			h.fast[i] = uint16(sym)<<4 | uint16(l)
		}
	}

	return h, true
}

// The fixed codes (inflate.c, fixedtables): symbols 286 and 287 and
// distances 30 and 31 have codes that decode to invalid symbols.
var fixedLit, fixedDist = func() (*huffman, *huffman) {
	var lens [288]uint8

	for i := range lens {
		switch {
		case i < 144:
			lens[i] = 8
		case i < 256:
			lens[i] = 9
		case i < 280:
			lens[i] = 7
		default:
			lens[i] = 8
		}
	}

	lit, _ := newHuffman(lens[:], huffLens)

	var dlens [32]uint8
	for i := range dlens {
		dlens[i] = 5
	}

	dist, _ := newHuffman(dlens[:], huffDists)

	return lit, dist
}()

// adler32Update is Adler-32 over p, continuing from adler.
func adler32Update(adler uint32, p []byte) uint32 {
	const mod = 65521

	s1, s2 := adler&0xffff, adler>>16

	for len(p) > 0 {
		n := min(len(p), 5552)

		for _, b := range p[:n] {
			s1 += uint32(b)
			s2 += s1
		}

		s1 %= mod
		s2 %= mod
		p = p[n:]
	}

	return s2<<16 | s1
}

// inflateInput is the compressed data: the body, after any bytes being
// read again (the raw deflate restart), keeping what it reads while
// recording.
type inflateInput struct {
	r         *bufio.Reader
	pre       []byte
	rec       []byte
	recording bool
}

func (in *inflateInput) ReadByte() (byte, error) {
	if len(in.pre) > 0 {
		b := in.pre[0]
		in.pre = in.pre[1:]

		return b, nil
	}

	b, err := in.r.ReadByte()
	if err == nil && in.recording {
		in.rec = append(in.rec, b)
	}

	return b, err
}

func (in *inflateInput) Read(p []byte) (int, error) {
	if len(in.pre) > 0 {
		n := copy(p, in.pre)
		in.pre = in.pre[n:]

		return n, nil
	}

	n, err := in.r.Read(p)
	if in.recording {
		in.rec = append(in.rec, p[:n]...)
	}

	return n, err
}

// replay reads the recorded bytes again, and stops recording.
func (in *inflateInput) replay() {
	in.pre = append(in.rec, in.pre...)
	in.rec, in.recording = nil, false
}

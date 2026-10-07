package archivetest

import (
	"fmt"
	"io/fs"
	"strings"
)

// Case is one archive of a corpus.
type Case struct {
	Name string
	// URL is the dist URL, for gzip dists.
	URL  string
	Data []byte
	// MayRefuse marks archives maestro may refuse although the real
	// extractor succeeds, because the result depends on more than the
	// archive (a distribution's unzip patches, say).
	MayRefuse bool
	// Fails marks archives the real extractor is expected to refuse.
	Fails bool
}

// ZipCorpus is the generated zip corpus: every unzip rule the archive
// package reproduces, alone and combined.
func ZipCorpus() []Case {
	var cs []Case

	add := func(name string, entries ...ZipEntry) {
		cs = append(cs, Case{Name: name, Data: Zip("", entries...)})
	}

	gh := func(name string, entries ...ZipEntry) {
		add(name, append([]ZipEntry{UnixDir("pkg-1a2b3c/", 0o755)}, entries...)...)
	}

	// The shape of a GitHub zipball.
	gh("github",
		UnixFile("pkg-1a2b3c/composer.json", 0o644, `{"name":"a/b"}`),
		UnixDir("pkg-1a2b3c/src/", 0o755),
		UnixFile("pkg-1a2b3c/src/A.php", 0o644, "<?php class A {}\n"),
		UnixDir("pkg-1a2b3c/bin/", 0o755),
		UnixFile("pkg-1a2b3c/bin/tool", 0o755, "#!/usr/bin/env php\n"),
		UnixFile("pkg-1a2b3c/empty", 0o644, ""),
	)

	// File modes, set-id and sticky bits included.
	for _, m := range []fs.FileMode{0o600, 0o644, 0o664, 0o666, 0o775, 0o777, 0o444, 0o400, 0o000, 0o755 | fs.ModeSetuid, 0o755 | fs.ModeSetgid, 0o777 | fs.ModeSticky} {
		gh(fmt.Sprintf("file-mode-%v", m), UnixFile("pkg-1a2b3c/f", m, "data"))
	}

	// Directory modes, listed and holding files.
	for _, m := range []fs.FileMode{0o700, 0o755, 0o775, 0o777, 0o555, 0o500, 0o711, 0o755 | fs.ModeSetgid, 0o777 | fs.ModeSticky} {
		gh(fmt.Sprintf("dir-mode-%v", m),
			UnixDir("pkg-1a2b3c/d/", m),
			UnixFile("pkg-1a2b3c/d/f", 0o644, "x"),
			UnixDir("pkg-1a2b3c/d/e/", m),
		)
	}

	// The package directory's own mode.
	for _, m := range []fs.FileMode{0o700, 0o750, 0o777} {
		add(fmt.Sprintf("root-mode-%v", m), UnixDir("pkg/", m), UnixFile("pkg/f", 0o644, "x"))
	}

	add("implied-dirs", UnixFile("pkg/a/b/c.txt", 0o644, "c"), UnixFile("pkg/a/d.txt", 0o600, "d"))
	add("dir-listed-after-content", UnixFile("pkg/a/f", 0o644, "f"), UnixDir("pkg/a/", 0o700), UnixDir("pkg/", 0o700))
	add("empty-dirs", UnixDir("pkg/", 0o755), UnixDir("pkg/e1/", 0o755), UnixDir("pkg/e2/e3/", 0o700))
	add("ds-store-beside", UnixDir("pkg/", 0o755), UnixFile("pkg/f", 0o644, "f"), UnixFile(".DS_Store", 0o644, "junk"))
	add("ds-store-inside", UnixDir("pkg/", 0o755), UnixFile("pkg/.DS_Store", 0o644, "junk"))
	add("two-top-dirs", UnixFile("a/f", 0o644, "a"), UnixFile("b/f", 0o644, "b"))
	add("top-dir-and-file", UnixFile("a/f", 0o644, "a"), UnixFile("README", 0o644, "r"))
	add("single-top-file", UnixFile("only.php", 0o640, "<?php"))
	add("nested-single-dirs", UnixFile("a/b/c/d.txt", 0o644, "d"))
	add("no-dirs-at-all", UnixFile("a.php", 0o644, "a"), UnixFile("b.php", 0o755, "b"))
	add("only-ds-store", UnixFile(".DS_Store", 0o644, "junk"))

	// Symlinks: deferred to the end, never followed.
	gh("symlinks",
		UnixFile("pkg-1a2b3c/target.txt", 0o644, "t"),
		UnixLink("pkg-1a2b3c/rel", "target.txt"),
		UnixLink("pkg-1a2b3c/up", "../../outside"),
		UnixLink("pkg-1a2b3c/abs", "/etc/passwd"),
		UnixLink("pkg-1a2b3c/dangling", "nowhere/at/all"),
		UnixDir("pkg-1a2b3c/d/", 0o755),
		UnixLink("pkg-1a2b3c/d/dirlink", ".."),
	)
	gh("symlink-empty-target", ZipEntry{Name: "pkg-1a2b3c/l", Host: HostUnix, HostVer: 30, Attr: 0o120777 << 16})
	gh("symlink-then-below", UnixLink("pkg-1a2b3c/l", "d"), UnixFile("pkg-1a2b3c/l/f", 0o644, "f"))
	gh("symlink-to-dir-then-below", UnixDir("pkg-1a2b3c/d/", 0o755), UnixLink("pkg-1a2b3c/l", "d"), UnixFile("pkg-1a2b3c/l/f", 0o644, "f"))
	gh("symlink-deflated", ZipEntry{Name: "pkg-1a2b3c/l", Data: "some/target", Host: HostUnix, HostVer: 30, Attr: 0o120755 << 16, Deflate: true})
	gh("symlink-from-fat", ZipEntry{Name: "pkg-1a2b3c/l", Data: "t", Host: HostFAT, HostVer: 30, Attr: 0o120755<<16 | 0x20})
	add("symlink-only-top", UnixLink("lnk", "."))
	add("symlink-top-and-ds", UnixLink("lnk", "/tmp"), UnixFile(".DS_Store", 0o644, ""))

	// MS-DOS attributes: read-only and directory bits expand under the
	// umask unless the Unix bits a FAT entry carries agree with them.
	add("fat-plain", FatFile("pkg/a.txt", 0x20, "a"), FatFile("pkg/ro.txt", 0x21, "ro"), FatFile("pkg/sub/", 0x10, ""))
	add("fat-dir-without-bit", FatFile("pkg/", 0x00, ""), FatFile("pkg/f", 0x00, "f"))
	add("fat-unix-consistent", FatFile("pkg/a", 0o100644<<16|0x20, "a"), FatFile("pkg/b", 0o100444<<16|0x21, "b"), FatFile("pkg/d/", 0o040755<<16|0x10, ""))
	add("fat-unix-inconsistent", FatFile("pkg/a", 0o100600<<16|0x21, "a"), FatFile("pkg/b", 0o100755<<16|0x20, "b"))
	add("ntfs", ZipEntry{Name: "pkg/a", Data: "a", Host: HostNTFS, HostVer: 20, Attr: 0o100755<<16 | 0x20}, ZipEntry{Name: "pkg/b", Data: "b", Host: HostNTFS, HostVer: 20, Attr: 0x01})
	add("unix-zero-attrs", ZipEntry{Name: "pkg/a", Data: "a", Host: HostUnix, HostVer: 30, Attr: 0x20}, ZipEntry{Name: "pkg/ro", Data: "r", Host: HostUnix, HostVer: 30, Attr: 0x01})
	add("unix-asi-extra", ZipEntry{Name: "pkg/a", Data: "a", Host: HostUnix, HostVer: 30, Attr: 0x20, Extra: asiExtra(0o100750)})
	add("amiga", ZipEntry{Name: "pkg/a", Data: "a", Host: 1, HostVer: 20, Attr: 0x5 << 17})

	// Names.
	gh("utf8-flagged", ZipEntry{Name: "pkg-1a2b3c/ünïcødé €.txt", Data: "u", Host: HostUnix, HostVer: 30, Attr: 0o100644 << 16, Flags: 1 << 11})
	gh("utf8-unflagged-unix", UnixFile("pkg-1a2b3c/naïve.txt", 0o644, "n"))
	gh("utf8-unicode-path", ZipEntry{Name: "pkg-1a2b3c/plain.txt", Data: "p", Host: HostUnix, HostVer: 30, Attr: 0o100644 << 16, Extra: UnicodePath("pkg-1a2b3c/plain.txt", "pkg-1a2b3c/plaín.txt")})
	gh("utf8-supplementary", ZipEntry{Name: "pkg-1a2b3c/emoji-\U0001F600.txt", Data: "e", Host: HostUnix, HostVer: 30, Attr: 0o100644 << 16, Flags: 1 << 11})
	gh("latin1-unix", UnixFile("pkg-1a2b3c/caf\xe9.txt", 0o644, "c"))
	cs = append(cs, Case{Name: "oem-fat", Data: Zip("", FatFile("pkg/caf\x82.txt", 0x20, "c")), MayRefuse: true})
	gh("control-chars", UnixFile("pkg-1a2b3c/a\x01b\x7fc.txt", 0o644, "x"))
	gh("backslash-unix", UnixFile(`pkg-1a2b3c/back\slash.txt`, 0o644, "x"))
	add("backslash-fat", FatFile(`pkg\a.txt`, 0x20, "x"))
	gh("dot-components", UnixFile("pkg-1a2b3c/./a/./b.txt", 0o644, "b"), UnixFile("pkg-1a2b3c//c.txt", 0o644, "c"))
	add("leading-dot-slash", UnixFile("./pkg/a.txt", 0o644, "a"))
	gh("final-dot", UnixFile("pkg-1a2b3c/d/.", 0o644, "dot"), UnixFile("pkg-1a2b3c/e/..", 0o644, "dotdot"))
	gh("vms-version", UnixFile("pkg-1a2b3c/a.txt;1", 0o644, "a"), UnixFile("pkg-1a2b3c/b;x", 0o644, "b"), UnixFile("pkg-1a2b3c/c;", 0o644, "c"))
	gh("parent-components", UnixFile("pkg-1a2b3c/../evil.txt", 0o644, "e"))
	add("absolute", UnixFile("/tmp/evil.txt", 0o644, "e"))
	gh("long-name", UnixFile("pkg-1a2b3c/"+strings.Repeat("n", 200)+".txt", 0o644, "n"))
	gh("deep", UnixFile("pkg-1a2b3c/"+strings.Repeat("d/", 60)+"f", 0o644, "f"))

	// Duplicates and conflicts: unzip would prompt.
	gh("duplicate-file", UnixFile("pkg-1a2b3c/a", 0o644, "1"), UnixFile("pkg-1a2b3c/a", 0o644, "2"))
	gh("duplicate-dir", UnixDir("pkg-1a2b3c/d/", 0o700), UnixDir("pkg-1a2b3c/d/", 0o755))
	gh("file-then-dir", UnixFile("pkg-1a2b3c/a", 0o644, "1"), UnixFile("pkg-1a2b3c/a/b", 0o644, "2"))

	// Storage.
	gh("stored", ZipEntry{Name: "pkg-1a2b3c/s", Data: strings.Repeat("stored ", 100), Host: HostUnix, HostVer: 30, Attr: 0o100644 << 16})
	gh("descriptors", ZipEntry{Name: "pkg-1a2b3c/a", Data: strings.Repeat("abc", 1000), Host: HostUnix, HostVer: 30, Attr: 0o100644 << 16, Deflate: true, Descriptor: true},
		ZipEntry{Name: "pkg-1a2b3c/b", Data: "", Host: HostUnix, HostVer: 30, Attr: 0o100600 << 16, Deflate: true, Descriptor: true})
	gh("big", UnixFile("pkg-1a2b3c/big", 0o644, strings.Repeat("0123456789abcdef", 1<<16)))

	cs = append(cs, Zip64Cases()...)

	cs = append(cs, Case{Name: "comment", Data: Zip("a comment", UnixFile("pkg/a", 0o644, "a"))})
	cs = append(cs, Case{Name: "empty-archive", Data: Zip("")})
	cs = append(cs, Case{Name: "overlap", Data: Zip("", UnixFile("pkg/a", 0o644, "aaaa"), ZipEntry{Name: "pkg/a", Data: "aaaa", Host: HostUnix, HostVer: 30, Attr: 0o100644 << 16, Deflate: true, Link: 1})})

	bad := Zip("", UnixFile("pkg/a", 0o644, "hello world"))
	bad[30+len("pkg/a")+2] ^= 0xff // corrupt the deflate data
	cs = append(cs, Case{Name: "corrupt-data", Data: bad})

	return cs
}

// asiExtra is an ASi Unix extra field carrying mode.
func asiExtra(mode uint16) []byte {
	return []byte{0x6e, 0x75, 10, 0, 0, 0, 0, 0, byte(mode & 0xff), byte(mode >> 8), 0, 0, 0, 0}
}

// deflate64Data is content Deflate64 compresses with what Deflate lacks:
// a repeat further back than 32 KiB and runs longer than 258 bytes.
func deflate64Data() string {
	var b strings.Builder

	x := uint32(2463534242)
	for b.Len() < 40000 {
		// xorshift: text that does not repeat on its own
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b.WriteByte("abcdefghijklmnopqrstuvwxyz \n"[x%28])
	}

	head := b.String()

	return head + strings.Repeat("\x00", 70000) + head + head[:5000]
}

// Zip64Cases are zip64 layouts (as Info-ZIP zip -fz, zipstream-php and
// other streaming writers make them) and Deflate64 entries, part of
// ZipCorpus.
func Zip64Cases() []Case {
	var cs []Case

	add := func(name string, opts ZipOptions, entries ...ZipEntry) {
		cs = append(cs, Case{Name: name, Data: ZipWith(opts, "", entries...)})
	}

	z64 := func(e ZipEntry) ZipEntry {
		e.Zip64 = true

		return e
	}

	github := []ZipEntry{
		UnixDir("pkg-1a2b3c/", 0o755),
		UnixFile("pkg-1a2b3c/composer.json", 0o644, `{"name":"a/b"}`),
		UnixDir("pkg-1a2b3c/bin/", 0o755),
		UnixFile("pkg-1a2b3c/bin/tool", 0o755, "#!/usr/bin/env php\n"),
	}

	zip64 := make([]ZipEntry, len(github))
	streamed := make([]ZipEntry, len(github))

	for i, e := range github {
		zip64[i] = z64(e)
		streamed[i] = z64(e)
		streamed[i].Descriptor = true
	}

	add("zip64-end", ZipOptions{End64: true}, github...)
	add("zip64-end-saturated", ZipOptions{End64: true, Saturate: true}, github...)
	add("zip64-entries", ZipOptions{End64: true, Saturate: true}, zip64...)
	add("zip64-entries-plain-end", ZipOptions{}, zip64...)
	add("zip64-streamed", ZipOptions{End64: true, Saturate: true}, streamed...)
	add("zip64-streamed-plain-end", ZipOptions{}, streamed...)
	add("zip64-mixed", ZipOptions{End64: true}, github[0], zip64[1], github[2], streamed[3])

	// a stored entry, its data descriptor last before the central directory
	add("zip64-streamed-stored", ZipOptions{End64: true}, ZipEntry{Name: "pkg/s", Data: "stored", Host: HostUnix, HostVer: 30, Attr: 0o100644 << 16, Zip64: true, Descriptor: true})

	// a zip64 local field makes unzip read 64-bit descriptor sizes where
	// the CVE-2019-13232 patch applied as written: a 32-bit descriptor
	// then overlaps the next entry, which those builds refuse
	short32 := streamed[1]
	short32.Descriptor32 = true
	cs = append(cs, Case{Name: "zip64-streamed-32-bit-descriptor", Data: ZipWith(ZipOptions{End64: true}, "", streamed[0], short32, streamed[2], streamed[3]), MayRefuse: true})

	// placeholders with nothing to replace them: unzip fails
	cs = append(cs, Case{Name: "end-saturated-without-zip64", Data: ZipWith(ZipOptions{Saturate: true}, "", github...), Fails: true})

	short := UnixFile("pkg/a", 0o644, "a")
	short.Extra = []byte{0x01, 0x00, 0x08, 0x00, 1, 0, 0, 0, 0, 0, 0, 0}
	cs = append(cs, Case{Name: "zip64-extra-without-placeholders", Data: Zip("", short)})

	data := deflate64Data()
	d64 := func(name string, mode fs.FileMode) ZipEntry {
		return ZipEntry{Name: name, Data: data, Host: HostUnix, HostVer: 30, Attr: (0o100000 | unixBits(mode)) << 16, Deflate64: true}
	}

	add("deflate64", ZipOptions{},
		UnixDir("pkg-1a2b3c/", 0o755),
		d64("pkg-1a2b3c/a.txt", 0o644),
		ZipEntry{Name: "pkg-1a2b3c/small", Data: "<?php\n", Host: HostUnix, HostVer: 30, Attr: 0o100755 << 16, Deflate64: true},
		ZipEntry{Name: "pkg-1a2b3c/empty", Host: HostUnix, HostVer: 30, Attr: 0o100644 << 16, Deflate64: true},
		ZipEntry{Name: "pkg-1a2b3c/link", Data: "a.txt", Host: HostUnix, HostVer: 30, Attr: 0o120777 << 16, Deflate64: true},
	)

	streamedD64 := z64(d64("pkg/b.txt", 0o600))
	streamedD64.Descriptor = true
	add("deflate64-zip64", ZipOptions{End64: true, Saturate: true}, z64(d64("pkg/a.txt", 0o644)), streamedD64)

	bad := d64("pkg/a.txt", 0o644)
	bad.Data = "abc"
	badZip := Zip("", bad)
	badZip[30+len("pkg/a.txt")] ^= 0x06 // the block type: reserved
	cs = append(cs, Case{Name: "deflate64-corrupt", Data: badZip, Fails: true})

	return cs
}

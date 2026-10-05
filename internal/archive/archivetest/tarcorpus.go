package archivetest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"strings"

	"github.com/ulikunitz/xz"
)

// TarCase is one tar of the corpus, uncompressed.
type TarCase struct {
	Name string
	Tar  []byte
	// MayRefusePhar and MayRefuseGNU mark tars maestro may refuse to
	// reproduce for PharData and GNU tar respectively.
	MayRefusePhar bool
	MayRefuseGNU  bool
}

// T is one member of a generated tar.
type T struct {
	Name, Link, Data string
	Mode             int64
	Type             byte
}

// TFile, TDir, TSym and THard describe members.
func TFile(name string, mode int64, data string) T {
	return T{Name: name, Mode: mode, Data: data, Type: tar.TypeReg}
}

func TDir(name string, mode int64) T { return T{Name: name, Mode: mode, Type: tar.TypeDir} }

func TSym(name, target string) T {
	return T{Name: name, Link: target, Mode: 0o777, Type: tar.TypeSymlink}
}

func THard(name, target string) T {
	return T{Name: name, Link: target, Mode: 0o644, Type: tar.TypeLink}
}

// Tar writes members in the given format, with a pax global header first
// when global is set (as git archive does).
func Tar(format tar.Format, global bool, members ...T) []byte {
	var b bytes.Buffer

	w := tar.NewWriter(&b)

	if global {
		_ = w.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": "1a2b3c"}, Format: tar.FormatPAX})
	}

	for _, m := range members {
		h := &tar.Header{Name: m.Name, Linkname: m.Link, Mode: m.Mode, Typeflag: m.Type, Size: int64(len(m.Data)), Format: format, Uname: "u", Gname: "g"}
		if m.Type != tar.TypeReg {
			h.Size = 0
		}

		if err := w.WriteHeader(h); err != nil {
			panic(fmt.Sprintf("%s: %v", m.Name, err))
		}

		_, _ = w.Write([]byte(m.Data)[:h.Size])
	}

	_ = w.Close()

	return b.Bytes()
}

// TarCorpus is the generated tar corpus.
func TarCorpus() []TarCase {
	var cs []TarCase

	add := func(name string, format tar.Format, members ...T) {
		cs = append(cs, TarCase{Name: name, Tar: Tar(format, false, members...)})
	}

	cs = append(cs, TarCase{Name: "github", Tar: Tar(tar.FormatPAX, true,
		TDir("pkg-1a2b3c/", 0o775),
		TFile("pkg-1a2b3c/composer.json", 0o664, `{"name":"a/b"}`),
		TDir("pkg-1a2b3c/src/", 0o775),
		TFile("pkg-1a2b3c/src/A.php", 0o664, "<?php class A {}\n"),
		TFile("pkg-1a2b3c/bin/tool", 0o775, "#!/usr/bin/env php\n"),
		TFile("pkg-1a2b3c/empty", 0o664, ""),
	)})

	for _, f := range []tar.Format{tar.FormatUSTAR, tar.FormatGNU, tar.FormatPAX} {
		for _, m := range []int64{0o600, 0o644, 0o664, 0o755, 0o777, 0o444, 0o000, 0o4755, 0o2755, 0o1777} {
			add(fmt.Sprintf("%v-file-mode-%o", f, m), f, TDir("pkg/", 0o755), TFile("pkg/f", m, "data"))
		}
	}

	for _, m := range []int64{0o700, 0o755, 0o555, 0o2775, 0o1777} {
		add(fmt.Sprintf("dir-mode-%o", m), tar.FormatPAX, TDir("pkg/", 0o755), TDir("pkg/d/", m), TFile("pkg/d/f", 0o644, "f"), TDir("pkg/d/e/", m))
		add(fmt.Sprintf("dir-before-parent-%o", m), tar.FormatPAX, TDir("pkg/d/", m), TFile("pkg/d/f", 0o644, "f"))
	}

	add("implied-dirs", tar.FormatPAX, TFile("pkg/a/b/c", 0o644, "c"), TFile("pkg/a/d", 0o640, "d"))
	add("empty-dirs", tar.FormatPAX, TDir("pkg/", 0o755), TDir("pkg/e/", 0o755), TFile("pkg/f", 0o644, "f"))
	add("only-empty-dir", tar.FormatPAX, TDir("pkg/", 0o755))
	add("dir-after-content", tar.FormatPAX, TFile("pkg/a/f", 0o644, "f"), TDir("pkg/a/", 0o700), TDir("pkg/", 0o750))
	add("ds-store-beside", tar.FormatPAX, TFile("pkg/f", 0o644, "f"), TFile(".DS_Store", 0o644, "junk"))
	add("two-top-dirs", tar.FormatPAX, TFile("a/f", 0o644, "a"), TFile("b/f", 0o644, "b"))
	add("single-top-file", tar.FormatPAX, TFile("only.php", 0o640, "<?php"))
	add("dot-slash", tar.FormatPAX, TDir("./", 0o755), TDir("./pkg/", 0o755), TFile("./pkg/f", 0o644, "f"))
	add("dot-components", tar.FormatPAX, TFile("pkg/./a//b", 0o644, "b"))
	// PharData takes an empty tar for a tar only when the file name says
	// so.
	cs = append(cs, TarCase{Name: "empty", Tar: Tar(tar.FormatPAX, false), MayRefusePhar: true})

	cs = append(cs,
		TarCase{Name: "symlinks", MayRefuseGNU: false, Tar: Tar(tar.FormatPAX, false,
			TDir("pkg/", 0o755), TFile("pkg/t", 0o644, "t"), TSym("pkg/rel", "t"), TSym("pkg/abs", "/etc/passwd"), TSym("pkg/up", "../x"))},
		TarCase{Name: "hardlink", Tar: Tar(tar.FormatPAX, false,
			TDir("pkg/", 0o755), TFile("pkg/t", 0o640, "t"), THard("pkg/h", "pkg/t"))},
		TarCase{Name: "hardlink-absolute-target", Tar: Tar(tar.FormatPAX, false,
			TDir("pkg/", 0o755), TFile("pkg/sub/t", 0o755, "t"), THard("pkg/h", "/pkg/sub/t"), THard("pkg/sub/h2", "pkg/h"))},
		TarCase{Name: "hardlink-to-symlink", MayRefuseGNU: true, Tar: Tar(tar.FormatPAX, false,
			TDir("pkg/", 0o755), TSym("pkg/s", "t"), THard("pkg/h", "pkg/s"))},
		TarCase{Name: "hardlink-to-dir", MayRefuseGNU: true, Tar: Tar(tar.FormatPAX, false,
			TDir("pkg/", 0o755), TDir("pkg/d/", 0o755), THard("pkg/h", "pkg/d"))},
		TarCase{Name: "hardlink-missing", Tar: Tar(tar.FormatPAX, false, TDir("pkg/", 0o755), THard("pkg/h", "pkg/nope"))},
		TarCase{Name: "symlink-then-below", Tar: Tar(tar.FormatPAX, false, TDir("pkg/", 0o755), TSym("pkg/l", "d"), TFile("pkg/l/f", 0o644, "f"))},
		TarCase{Name: "parent-components", MayRefusePhar: true, Tar: Tar(tar.FormatPAX, false, TFile("pkg/../../evil", 0o644, "e"))},
		TarCase{Name: "absolute", Tar: Tar(tar.FormatPAX, false, TFile("/pkg/abs", 0o644, "a"))},
		TarCase{Name: "duplicate", MayRefusePhar: true, MayRefuseGNU: true, Tar: Tar(tar.FormatPAX, false, TFile("pkg/a", 0o644, "1"), TFile("pkg/a", 0o600, "2"))},
		TarCase{Name: "duplicate-dir", MayRefusePhar: true, MayRefuseGNU: true, Tar: Tar(tar.FormatPAX, false, TDir("pkg/d/", 0o700), TDir("pkg/d/", 0o755), TFile("pkg/d/f", 0o644, "f"))},
		TarCase{Name: "file-then-dir", Tar: Tar(tar.FormatPAX, false, TFile("pkg/a", 0o644, "1"), TFile("pkg/a/b", 0o644, "2"))},
	)

	long := "pkg/" + strings.Repeat("long-directory-name/", 8) + "file.php"
	add("long-gnu", tar.FormatGNU, TDir("pkg/", 0o755), TFile(long, 0o644, "l"))
	add("long-ustar-prefix", tar.FormatUSTAR, TDir("pkg/", 0o755), TFile("pkg/"+strings.Repeat("p", 90)+"/"+strings.Repeat("n", 90), 0o644, "l"))
	cs = append(cs, TarCase{Name: "long-pax", MayRefusePhar: true, Tar: Tar(tar.FormatPAX, false, TDir("pkg/", 0o755), TFile(long, 0o644, "l"))})
	add("unicode", tar.FormatPAX, TFile("pkg/ünïcødé.txt", 0o644, "u"))
	add("big", tar.FormatPAX, TFile("pkg/big", 0o644, strings.Repeat("0123456789abcdef", 1<<16)))

	cs = append(cs, TarCase{Name: "v7", Tar: v7Tar(
		v7{name: "pkg/", mode: 0o040755, typ: 0},
		v7{name: "pkg/f", mode: 0o100644, typ: 0, data: "v7 file"},
		v7{name: "pkg/g", mode: 0o100600, typ: '0', data: "zero"},
	), MayRefuseGNU: true})

	return cs
}

type v7 struct {
	name, data string
	mode       int64
	typ        byte
}

// v7Tar writes pre-POSIX headers, without the ustar magic.
func v7Tar(members ...v7) []byte {
	var b bytes.Buffer

	for _, m := range members {
		var h [512]byte

		copy(h[:100], m.name)
		copy(h[100:], fmt.Sprintf("%07o\x00", m.mode))
		copy(h[108:], "0000000\x000000000\x00")
		copy(h[124:], fmt.Sprintf("%011o\x00", len(m.data)))
		copy(h[136:], "00000000000\x00")
		copy(h[148:], "        ")
		h[156] = m.typ

		sum := 0
		for _, c := range h {
			sum += int(c)
		}

		copy(h[148:], fmt.Sprintf("%06o\x00 ", sum))
		b.Write(h[:])
		b.WriteString(m.data)
		b.Write(make([]byte, (512-len(m.data)%512)%512))
	}

	b.Write(make([]byte, 1024))

	return b.Bytes()
}

// Gzip compresses data into one gzip member.
func Gzip(data []byte) []byte {
	var b bytes.Buffer

	w := gzip.NewWriter(&b)
	_, _ = w.Write(data)
	_ = w.Close()

	return b.Bytes()
}

// Xz compresses data into one xz stream.
func Xz(data []byte) []byte {
	var b bytes.Buffer

	w, err := xz.NewWriter(&b)
	if err != nil {
		panic(err)
	}

	_, _ = w.Write(data)
	_ = w.Close()

	return b.Bytes()
}

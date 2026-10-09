//go:build darwin

package platform

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// machoLoad is a load command writing a dylib's install name: cmd, the
// command's type (the plain LC_LOAD_DYLIB, or a weak or lazy one).
type machoLoad struct {
	cmd  uint32
	name string
}

const (
	testCmdDylib = 0xc
	testCmdRpath = 0x8000001c
)

// machoBytes is a 64-bit Mach-O file that loads loads and carries
// rpaths: a header and load commands, as much of a file as the walk
// reads.
func machoBytes(loads []machoLoad, rpaths []string) []byte {
	// every command that names a dylib has the dylib_command shape: the
	// lc_str at 24, after a timestamp and two versions
	command := func(cmd uint32, str string, strAt uint32, dylibShaped bool) []byte {
		payload := append([]byte(str), 0)
		size := (strAt + uint32(len(payload)) + 7) &^ 7

		c := bytes.NewBuffer(make([]byte, 0, size))
		_ = binary.Write(c, binary.LittleEndian, cmd)
		_ = binary.Write(c, binary.LittleEndian, size)
		_ = binary.Write(c, binary.LittleEndian, strAt)

		if dylibShaped {
			_ = binary.Write(c, binary.LittleEndian, [3]uint32{})
		}

		c.Write(payload)
		c.Write(bytes.Repeat([]byte{0}, int(size)-c.Len()))

		return c.Bytes()
	}

	var cmds bytes.Buffer
	for _, l := range loads {
		cmds.Write(command(l.cmd, l.name, 24, true))
	}
	for _, r := range rpaths {
		cmds.Write(command(testCmdRpath, r, 12, false))
	}

	out := bytes.NewBuffer(make([]byte, 0, 32+cmds.Len()))
	_ = binary.Write(out, binary.LittleEndian, [2]uint32{0xfeedfacf, 0x0100000c}) // magic, cputype
	_ = binary.Write(out, binary.LittleEndian, [2]uint32{0, 6})                   // cpusubtype, MH_DYLIB
	_ = binary.Write(out, binary.LittleEndian, [2]uint32{uint32(len(loads) + len(rpaths)), uint32(cmds.Len())})
	_ = binary.Write(out, binary.LittleEndian, [2]uint32{0, 0}) // flags, reserved
	out.Write(cmds.Bytes())

	return out.Bytes()
}

func writeMachO(t *testing.T, path string, loads []machoLoad, rpaths []string) {
	t.Helper()

	if err := os.WriteFile(path, machoBytes(loads, rpaths), 0o755); err != nil {
		t.Fatal(err)
	}
}

// writeFatMachO wraps a Mach-O file as a universal one holding the one
// architecture cpu.
func writeFatMachO(t *testing.T, path string, cpu uint32, loads []machoLoad, rpaths []string) {
	t.Helper()

	const offset = 0x1000

	thin := machoBytes(loads, rpaths)

	out := bytes.NewBuffer(make([]byte, 0, offset+len(thin)))
	_ = binary.Write(out, binary.BigEndian, uint32(0xcafebabe)) // FAT_MAGIC
	_ = binary.Write(out, binary.BigEndian, uint32(1))          // nfat_arch
	_ = binary.Write(out, binary.BigEndian, cpu)
	_ = binary.Write(out, binary.BigEndian, [4]uint32{0, offset, uint32(len(thin)), 12})
	out.Write(bytes.Repeat([]byte{0}, offset-out.Len()))
	out.Write(thin)

	if err := os.WriteFile(path, out.Bytes(), 0o755); err != nil {
		t.Fatal(err)
	}
}

// otherCpu is a CPU type this process does not run as.
func otherCpu() uint32 {
	if runtime.GOARCH == "amd64" {
		return 0x0100000c
	}

	return 0x01000007
}

func TestReadMachO(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	thin := filepath.Join(dir, "thin.dylib")
	writeMachO(t, thin, []machoLoad{{testCmdDylib, "/usr/lib/libSystem.B.dylib"}, {lcLoadWeakDylib, "@rpath/optional.dylib"}}, []string{"@loader_path/../lib"})

	info, ok := readMachO(thin)
	if !ok {
		t.Fatal("readMachO of a thin file failed")
	}

	want := machoInfo{
		dylibs: []string{"/usr/lib/libSystem.B.dylib", "@rpath/optional.dylib"},
		rpaths: []string{"@loader_path/../lib"},
	}
	if !slices.Equal(info.dylibs, want.dylibs) || !slices.Equal(info.rpaths, want.rpaths) {
		t.Errorf("readMachO = %+v, want %+v", info, want)
	}

	fat := filepath.Join(dir, "fat.dylib")
	writeFatMachO(t, fat, uint32(hostCpu()), []machoLoad{{testCmdDylib, "a.dylib"}}, nil)

	if info, ok := readMachO(fat); !ok || !slices.Equal(info.dylibs, []string{"a.dylib"}) {
		t.Errorf("readMachO of a universal file = %+v, %v", info, ok)
	}

	// a universal file of another architecture only is none of this one's
	writeFatMachO(t, fat, otherCpu(), []machoLoad{{testCmdDylib, "a.dylib"}}, nil)

	if _, ok := readMachO(fat); ok {
		t.Error("readMachO of another architecture's file succeeded")
	}

	text := filepath.Join(dir, "text")
	writeFile(t, text, "not a mach-o file")

	if _, ok := readMachO(text); ok {
		t.Error("readMachO of a text file succeeded")
	}

	if _, ok := readMachO(filepath.Join(dir, "gone")); ok {
		t.Error("readMachO of a missing file succeeded")
	}
}

func TestResolveDylib(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "other", "c.dylib")
	writeFile(t, existing, "")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		label             string
		install           string
		loaderDir, exeDir string
		rpaths            []string
		want              string
	}{
		{"loader path", "@loader_path/liba.dylib", "/x", "/bin", nil, "/x/liba.dylib"},
		{"executable path", "@executable_path/../lib/b.dylib", "/load", "/bin", nil, "/lib/b.dylib"},
		{"absolute", "/usr/lib/libSystem.B.dylib", "/x", "/bin", nil, "/usr/lib/libSystem.B.dylib"},
		{"rpath first that exists", "@rpath/c.dylib", dir, "/bin", []string{"@loader_path/libs", "@loader_path/other"}, existing},
		{"rpath bare loader path", "@rpath/c.dylib", dir, "/bin", []string{"@loader_path", "@loader_path/other"}, existing},
		{"rpath none exists", "@rpath/c.dylib", dir, "/bin", []string{"@loader_path/libs"}, filepath.Join(dir, "libs", "c.dylib")},
		{"rpath none named", "@rpath/c.dylib", dir, "/bin", nil, ""},
		{"other at-name", "@weak/c.dylib", dir, "/bin", nil, ""},
		{"relative", "libz.dylib", "/x", "/bin", nil, filepath.Join(cwd, "libz.dylib")},
	} {
		t.Run(c.label, func(t *testing.T) {
			if got := resolveDylib(c.install, c.loaderDir, c.exeDir, c.rpaths); got != c.want {
				t.Errorf("resolveDylib(%q) = %q, want %q", c.install, got, c.want)
			}
		})
	}
}

// TestMachoWalk checks that the walk takes a file and every file its
// load commands name, theirs too, once around a cycle, and an unreadable
// file that is not the walk's root stays among the paths.
func TestMachoWalk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root := filepath.Join(dir, "php")

	a := filepath.Join(dir, "a.dylib")
	b := filepath.Join(dir, "b.dylib")
	writeMachO(t, a, []machoLoad{{testCmdDylib, "@loader_path/b.dylib"}, {lcLoadWeakDylib, "@loader_path/gone.dylib"}}, nil)
	writeMachO(t, b, []machoLoad{{testCmdDylib, "@loader_path/a.dylib"}}, nil)
	writeMachO(t, root, []machoLoad{{testCmdDylib, "@loader_path/a.dylib"}}, []string{"@loader_path"})

	walk := newMachoWalk(root)
	if !walk.add(root, root) {
		t.Fatal("the walk failed on its root")
	}

	want := []string{root, a, b, filepath.Join(dir, "gone.dylib")}
	slices.Sort(walk.paths)
	slices.Sort(want)
	if !slices.Equal(walk.paths, want) {
		t.Errorf("the walk took %v, want %v", walk.paths, want)
	}

	// a file added after keeps the walk's run paths for its own @rpath
	// names
	late := filepath.Join(dir, "late.dylib")
	writeMachO(t, late, []machoLoad{{testCmdDylib, "@rpath/late-dep.dylib"}}, nil)

	walk.add(late, root)

	if !slices.Contains(walk.paths, filepath.Join(dir, "late-dep.dylib")) {
		t.Errorf("the late file's rpath did not resolve: %v", walk.paths)
	}

	// a root that is no Mach-O file fails the walk
	text := filepath.Join(dir, "text")
	writeFile(t, text, "not a mach-o file")

	if walk := newMachoWalk(text); walk.add(text, text) {
		t.Error("the walk succeeded on a text root")
	}
}

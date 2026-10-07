// Reading the bare mirror of a git source in Go (deliberate deviation 3
// in docs/PORTING.md): fetchRefOrSyncMirror asks the mirror whether it
// is one (rev-parse --git-dir), whether it holds the reference
// (rev-parse --verify <sha>^{commit}) and which branches and tags it has
// (git branch, git tag). The answers come from HEAD, the refs, packed-refs
// and the pack indexes instead, but only where they are certain to be
// git's: anything this reader does not know for sure (an abbreviated or
// symbolic reference, a tag object to peel, a missing object, a symbolic
// or broken ref, alternates, replace refs, extensions, another owner,
// configuration or environment that changes what git would see or print)
// runs git as before. Only -vvv output (the commands logged) differs.
//
// Each kind of answer is moreover checked against git once per Git: the
// first time a mirror is eligible both run, and a disagreement turns the
// reader off for that kind of answer.

package vcs

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// mirrorQuery is a kind of answer the reader gives in place of git.
type mirrorQuery int

const (
	queryGitDir mirrorQuery = iota // git rev-parse --git-dir
	queryVerify                    // git rev-parse --quiet --verify <sha>^{commit}
	queryBranch                    // git branch
	queryTag                       // git tag
	queryCount
)

var mirrorQueryNames = [queryCount]string{"rev-parse --git-dir", "rev-parse --verify", "branch", "tag"}

// mirrorReading is what a Git has learnt about reading mirrors in Go:
// whether git's configuration allows it (probed once) and, per kind of
// answer, whether it agreed with git (0 not yet compared, 1 agreed, -1
// disagreed).
type mirrorReading struct {
	mu     sync.Mutex
	probed bool
	usable bool
	trust  [queryCount]int
}

// mirrorEnv are the environment variables under which git may see
// another repository, other objects or refs, or other configuration than
// the reader does.
var mirrorEnv = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_REPLACE_REF_BASE",
	"GIT_CONFIG", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM",
	"GIT_REF_PARANOIA", "GIT_DEFAULT_HASH", "GIT_DEFAULT_REF_FORMAT", "GIT_EXEC_PATH",
}

func mirrorEnvSafe() bool {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(mirrorEnv, name) || strings.HasPrefix(name, "GIT_TEST_") {
			return false
		}
	}

	return true
}

// trusted reports whether the reader answers q without git.
func (g *Git) trusted(q mirrorQuery) bool {
	g.mirror.mu.Lock()
	defer g.mirror.mu.Unlock()

	return g.mirror.trust[q] == 1
}

// calibrate records whether the reader's answer to q agreed with git's.
func (g *Git) calibrate(q mirrorQuery, agree bool) {
	g.mirror.mu.Lock()
	defer g.mirror.mu.Unlock()

	switch {
	case !agree:
		if g.mirror.trust[q] != -1 {
			g.io.WriteError("Reading git mirrors in Go disagrees with git "+mirrorQueryNames[q]+", running git instead", true, mio.Debug)
		}

		g.mirror.trust[q] = -1
	case g.mirror.trust[q] == 0:
		g.mirror.trust[q] = 1
	}
}

// configUsable probes, once, git's configuration outside the mirror at
// dir (the mirror's own is read by openMirror): conditional includes may
// differ between mirrors, and colour or columns change what git branch
// and git tag print in ways one comparison need not reveal.
func (g *Git) configUsable(dir string) bool {
	g.mirror.mu.Lock()
	defer g.mirror.mu.Unlock()

	if g.mirror.probed {
		return g.mirror.usable
	}

	g.mirror.probed = true

	var output string

	code, err := g.process.Execute(util.Cmd("git", "config", "--list", "--show-origin", "-z"), &output, dir)
	if err != nil || code != 0 {
		return false
	}

	// -z: origin NUL key LF value NUL (key NUL without a value)
	records := strings.Split(output, "\x00")
	for i := 0; i+1 < len(records); i += 2 {
		if records[i] == "file:config" {
			continue
		}

		key, value, _ := strings.Cut(records[i+1], "\n")
		if !mirrorConfigSafe(php.Strtolower(key), php.Strtolower(value)) {
			return false
		}
	}

	g.mirror.usable = true

	return true
}

func mirrorConfigSafe(key, value string) bool {
	switch {
	case strings.HasPrefix(key, "includeif."), strings.HasPrefix(key, "column."):
		return false
	case key == "color.ui" || key == "color.branch" || key == "color.tag":
		return slices.Contains([]string{"", "auto", "true", "yes", "on", "1", "false", "no", "off", "0", "never"}, value)
	case key == "safe.barerepository":
		return value == "all"
	case key == "core.replacerefsbase":
		return false
	}

	return true
}

// mirror is a bare repository the reader may answer for.
type mirror struct {
	dir  string
	head string // the branch HEAD names, without refs/heads/

	refsOnce sync.Once
	refs     map[string]string // refname -> object name, nil when unknown

	packsOnce sync.Once
	packs     []*packIndex // nil when unknown
}

// mirrorAt returns the reader for the mirror at dir, or nil when git must
// be asked: dir is not certainly a bare repository git would use as is.
func (g *Git) mirrorAt(dir string) *mirror {
	if runtime.GOOS == "windows" || !mirrorEnvSafe() {
		return nil
	}

	m := openMirror(dir)
	if m == nil || !g.configUsable(dir) {
		return nil
	}

	return m
}

// openMirror applies git's checks for a bare repository found at dir
// (is_git_directory, the repository format, the ownership) conservatively.
func openMirror(dir string) *mirror {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || !ownedByCurrentUser(info) {
		return nil
	}

	// git looks for dir/.git before taking dir itself as the repository
	for _, name := range []string{".git", "commondir", "gitdir", "worktrees", "objects/info/alternates", "refs/replace", "info/grafts", "reftable"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			return nil
		}
	}

	for _, name := range []string{"objects", "refs", "objects/pack"} {
		if info, err := os.Lstat(filepath.Join(dir, name)); err != nil || !info.IsDir() {
			return nil
		}
	}

	head, err := os.Lstat(filepath.Join(dir, "HEAD"))
	if err != nil || !head.Mode().IsRegular() {
		return nil
	}

	data, err := os.ReadFile(filepath.Join(dir, "HEAD"))
	if err != nil {
		return nil
	}

	target, ok := strings.CutPrefix(string(data), "ref: refs/heads/")
	if !ok {
		return nil
	}

	target, ok = strings.CutSuffix(target, "\n")
	if !ok || !refnameSafe(target) {
		return nil
	}

	if !mirrorConfigFileSafe(filepath.Join(dir, "config")) {
		return nil
	}

	return &mirror{dir: dir, head: target}
}

// mirrorConfigFileSafe reads the mirror's configuration as git clone
// --mirror writes it, without extensions, includes or anything else that
// may change what git sees or prints there.
func mirrorConfigFileSafe(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	section, bare := "", false

	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)

		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
			continue
		case strings.HasSuffix(line, "\\"):
			return false
		case line == "[core]":
			section = "core"

			continue
		case strings.HasPrefix(line, `[remote "`) && strings.HasSuffix(line, `"]`):
			section = "remote"

			continue
		case line[0] == '[':
			return false
		}

		key, value, _ := strings.Cut(line, "=")
		key = php.Strtolower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		if key == "" || strings.ContainsAny(key, " \t\"[]") {
			return false
		}

		switch section {
		case "remote":
			continue
		case "core":
		default:
			return false
		}

		switch key {
		case "repositoryformatversion":
			if value != "0" && value != "1" {
				return false
			}
		case "bare":
			if value != "true" {
				return false
			}

			bare = true
		case "filemode", "symlinks", "ignorecase", "precomposeunicode", "logallrefupdates":
		default:
			return false
		}
	}

	return bare
}

const refnameChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-+"

// refnameSafe reports whether name (below refs/<kind>/) is a valid
// refname of plain ASCII characters, conservatively: git takes it and
// prints it as it is.
func refnameSafe(name string) bool {
	if name == "" {
		return false
	}

	for component := range strings.SplitSeq(name, "/") {
		if component == "" || component[0] == '.' || strings.HasSuffix(component, ".") || strings.HasSuffix(component, ".lock") || strings.Contains(component, "..") {
			return false
		}

		for _, c := range []byte(component) {
			if !strings.ContainsRune(refnameChars, rune(c)) {
				return false
			}
		}
	}

	return true
}

func isObjectName(s string) bool {
	if len(s) != 40 {
		return false
	}

	for _, c := range []byte(s) {
		if !strings.ContainsRune("0123456789abcdef", rune(c)) {
			return false
		}
	}

	return true
}

// loadRefs reads refs/heads and refs/tags, loose over packed; nil when a
// ref is anything but a valid name holding an object name.
func (m *mirror) loadRefs() map[string]string {
	m.refsOnce.Do(func() {
		refs := map[string]string{}

		if data, err := os.ReadFile(filepath.Join(m.dir, "packed-refs")); err == nil {
			for i, line := range strings.Split(string(data), "\n") {
				switch {
				case line == "":
					continue
				case i == 0 && strings.HasPrefix(line, "# pack-refs with:"):
					continue
				case line[0] == '^':
					if !isObjectName(line[1:]) {
						return
					}

					continue
				}

				oid, name, ok := strings.Cut(line, " ")
				if !ok || !isObjectName(oid) || !strings.HasPrefix(name, "refs/") {
					return
				}

				if _, dup := refs[name]; dup {
					return
				}

				refs[name] = oid
			}
		} else if !os.IsNotExist(err) {
			return
		}

		for _, kind := range []string{"refs/heads", "refs/tags"} {
			if !loadLooseRefs(m.dir, kind, refs) {
				return
			}
		}

		m.refs = refs
	})

	return m.refs
}

func loadLooseRefs(dir, prefix string, refs map[string]string) bool {
	entries, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(prefix)))
	if os.IsNotExist(err) {
		return true
	} else if err != nil {
		return false
	}

	for _, entry := range entries {
		name := prefix + "/" + entry.Name()

		switch {
		case entry.IsDir():
			if !loadLooseRefs(dir, name, refs) {
				return false
			}
		case entry.Type().IsRegular():
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
			if err != nil {
				return false
			}

			oid, ok := strings.CutSuffix(string(data), "\n")
			if !ok || !isObjectName(oid) {
				return false
			}

			refs[name] = oid
		default:
			return false
		}
	}

	return true
}

// list returns the names below prefix (refs/heads/ or refs/tags/),
// sorted as git sorts refnames; false when a ref is not certainly listed
// as git lists it (an odd name, a missing object).
func (m *mirror) list(prefix string) ([]string, bool) {
	refs := m.loadRefs()
	if refs == nil {
		return nil, false
	}

	var names []string

	for name, oid := range refs {
		short, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}

		if !refnameSafe(short) {
			return nil, false
		}

		if _, found := m.objectType(oid); !found {
			return nil, false
		}

		names = append(names, short)
	}

	slices.Sort(names)

	return names, true
}

// branchOutput is the output of git branch in the mirror.
func (m *mirror) branchOutput() (string, bool) {
	names, ok := m.list("refs/heads/")
	if !ok {
		return "", false
	}

	var b strings.Builder

	for _, name := range names {
		if name == m.head {
			b.WriteString("* ")
		} else {
			b.WriteString("  ")
		}

		b.WriteString(name + "\n")
	}

	return b.String(), true
}

// tagOutput is the output of git tag in the mirror.
func (m *mirror) tagOutput() (string, bool) {
	names, ok := m.list("refs/tags/")
	if !ok {
		return "", false
	}

	var b strings.Builder

	for _, name := range names {
		b.WriteString(name + "\n")
	}

	return b.String(), true
}

// isCommit reports whether git rev-parse --verify ref^{commit} certainly
// succeeds: ref is a full object name of a commit in the mirror. False
// means unknown.
func (m *mirror) isCommit(ref string) bool {
	if !isObjectName(ref) {
		return false
	}

	// replace refs (loose ones keep openMirror from answering at all)
	refs := m.loadRefs()
	if refs == nil {
		return false
	}

	for name := range refs {
		if strings.HasPrefix(name, "refs/replace/") {
			return false
		}
	}

	typ, found := m.objectType(ref)

	return found && typ == objCommit
}

const (
	objCommit   = 1
	objOfsDelta = 6
	objRefDelta = 7
	objUnknown  = -1
)

// objectType returns the type of object oid when the mirror certainly
// holds it (objUnknown when its type takes more than reading a header).
func (m *mirror) objectType(oid string) (int, bool) {
	raw, err := hex.DecodeString(oid)
	if err != nil {
		return 0, false
	}

	if typ, found := looseObjectType(filepath.Join(m.dir, "objects", oid[:2], oid[2:])); found {
		return typ, true
	}

	for _, p := range m.loadPacks() {
		if offset, found := p.find(raw); found {
			return p.typeAt(offset), true
		}
	}

	return 0, false
}

func looseObjectType(path string) (int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()

	z, err := zlib.NewReader(bufio.NewReader(f))
	if err != nil {
		// git takes an existing file as the object
		return objUnknown, true
	}
	defer z.Close()

	header := make([]byte, 7)
	if _, err := io.ReadFull(z, header); err == nil && string(header) == "commit " {
		return objCommit, true
	}

	return objUnknown, true
}

// packIndex is a version 2 pack index and its pack.
type packIndex struct {
	idx, pack string
	fanout    [256]uint32
}

// loadPacks opens the indexes of objects/pack whose pack exists.
func (m *mirror) loadPacks() []*packIndex {
	m.packsOnce.Do(func() {
		dir := filepath.Join(m.dir, "objects", "pack")

		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}

		packs := []*packIndex{}

		for _, entry := range entries {
			base, ok := strings.CutSuffix(entry.Name(), ".idx")
			if !ok {
				continue
			}

			if _, err := os.Stat(filepath.Join(dir, base+".pack")); err != nil {
				continue
			}

			if p := openPackIndex(filepath.Join(dir, entry.Name()), filepath.Join(dir, base+".pack")); p != nil {
				packs = append(packs, p)
			}
		}

		m.packs = packs
	})

	return m.packs
}

func openPackIndex(idx, pack string) *packIndex {
	f, err := os.Open(idx)
	if err != nil {
		return nil
	}
	defer f.Close()

	buf := make([]byte, 8+256*4)
	if _, err := io.ReadFull(f, buf); err != nil || !bytes.Equal(buf[:8], []byte{0xff, 't', 'O', 'c', 0, 0, 0, 2}) {
		return nil
	}

	p := &packIndex{idx: idx, pack: pack}
	for i := range p.fanout {
		p.fanout[i] = binary.BigEndian.Uint32(buf[8+4*i:])
	}

	return p
}

// find returns the pack offset of the object named raw.
func (p *packIndex) find(raw []byte) (int64, bool) {
	lo, hi := uint32(0), p.fanout[raw[0]]
	if raw[0] > 0 {
		lo = p.fanout[raw[0]-1]
	}

	if lo >= hi {
		return 0, false
	}

	f, err := os.Open(p.idx)
	if err != nil {
		return 0, false
	}
	defer f.Close()

	const names = 8 + 256*4

	bucket := make([]byte, int(hi-lo)*20)
	if _, err := f.ReadAt(bucket, names+int64(lo)*20); err != nil {
		return 0, false
	}

	n := int(hi - lo)
	k := sort.Search(n, func(j int) bool { return bytes.Compare(bucket[j*20:j*20+20], raw) >= 0 })
	if k >= n || !bytes.Equal(bucket[k*20:k*20+20], raw) {
		return 0, false
	}

	total := int64(p.fanout[255])
	pos := int64(lo) + int64(k)

	var word [8]byte
	if _, err := f.ReadAt(word[:4], names+total*24+pos*4); err != nil {
		return 0, false
	}

	offset := binary.BigEndian.Uint32(word[:4])
	if offset&0x80000000 == 0 {
		return int64(offset), true
	}

	if _, err := f.ReadAt(word[:], names+total*28+int64(offset&0x7fffffff)*8); err != nil {
		return 0, false
	}

	offset64 := binary.BigEndian.Uint64(word[:])
	if offset64 > math.MaxInt64 {
		return 0, false
	}

	return int64(offset64), true
}

// typeAt returns the type of the object at offset in the pack, following
// offset deltas to their base; objUnknown for a delta against an object
// named by its id, a chain too long or an unreadable entry.
func (p *packIndex) typeAt(offset int64) int {
	f, err := os.Open(p.pack)
	if err != nil {
		return objUnknown
	}
	defer f.Close()

	for range 1000 {
		var buf [32]byte

		n, err := f.ReadAt(buf[:], offset)
		if n == 0 || err != nil && err != io.EOF {
			return objUnknown
		}

		data := buf[:n]
		typ := int(data[0]>>4) & 7

		i := 0
		for data[i]&0x80 != 0 {
			i++
			if i >= len(data) {
				return objUnknown
			}
		}

		i++

		if typ != objOfsDelta {
			if typ == objRefDelta || typ == 0 || typ == 5 {
				return objUnknown
			}

			return typ
		}

		if i >= len(data) {
			return objUnknown
		}

		c := data[i]
		back := int64(c & 0x7f)

		for c&0x80 != 0 {
			i++
			if i >= len(data) {
				return objUnknown
			}

			c = data[i]
			back = (back+1)<<7 | int64(c&0x7f)
		}

		if back <= 0 || back > offset {
			return objUnknown
		}

		offset -= back
	}

	return objUnknown
}

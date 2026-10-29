// Ports nothing: the root package's version as guessed from git, kept
// across runs (deliberate deviation 3, speed).

package version

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// keptGuesses is where and how GuessVersion keeps its guesses across
// runs; nil or a dir of "" for nowhere (UseGuessCache).
var keptGuesses atomic.Pointer[guessCacheConfig]

type guessCacheConfig struct {
	dir string
	// trust is how much older than an entry the files it is keyed on must
	// be for the entry to be written: a file changed within the
	// timestamps' granularity may not show it.
	trust fsstate.Margin
}

// UseGuessCache makes GuessVersion keep the versions it guesses from git
// in dir for later runs, "" nowhere (the default). A guess is kept only
// when a *util.ProcessExecutor runs its commands (never a test's mock),
// while it does not log them (-vvv, whose log then shows them run as
// Composer's does), and when git, not a fallback, found the version. An
// entry is one per checkout directory, keyed on everything git's answers
// depend on (repoState): the git binary, the environment git reads, its
// configuration files, and the repository's HEAD, refs, shallow and
// graft files. Linux only, as the git binary's identity is.
func UseGuessCache(dir string) {
	keptGuesses.Store(&guessCacheConfig{dir: dir})
}

// guessFormat is the version of the entries; its Header starts one.
var guessFormat = fsstate.Format{Name: "root-version", Version: 2}

// guessCacheMaxEntries bounds the entries kept, one per checkout: past it,
// the least recently written are removed.
const guessCacheMaxEntries = 256

// keptGuess is the entry of one checkout as its repository is now.
type keptGuess struct {
	// path is the entry's file.
	path string
	// state is the hash of what git's answers depend on (repoState).
	state []byte
	// trusted: every file state depends on is old enough for an entry
	// keyed on it to be written.
	trusted bool
}

// keptGuessFor returns the entry of the checkout at dir, a real path, and
// whether its guesses are kept at all.
func keptGuessFor(dir string) (keptGuess, bool) {
	c := keptGuesses.Load()
	if c == nil || c.dir == "" {
		return keptGuess{}, false
	}
	k := fsstate.NewKeyHash()
	k.String(guessFormat.Header())
	trusted, ok := repoState(k, dir, time.Now(), c.trust)
	if !ok {
		return keptGuess{}, false
	}

	name := fsstate.NewKeyHash()
	name.String(guessFormat.Header())
	name.String(dir)

	return keptGuess{
		path:    filepath.Join(c.dir, hex.EncodeToString(name.Sum(nil)[:16])),
		state:   k.Sum(nil),
		trusted: trusted,
	}, true
}

// holds reports whether the entry was written for the repository as it
// is now, whatever configuration it was guessed for.
func (g keptGuess) holds() bool {
	_, ok := g.read()

	return ok
}

// read returns what the entry holds past its state, if it was written for
// the repository as it is now.
func (g keptGuess) read() ([]byte, bool) {
	data, err := os.ReadFile(g.path)
	if err != nil {
		return nil, false
	}
	rest, ok := bytes.CutPrefix(data, []byte(guessFormat.Header()))
	if !ok {
		return nil, false
	}

	return bytes.CutPrefix(rest, g.state)
}

// load returns the version the entry holds for a configuration of
// guessKey (GuessKey).
func (g keptGuess) load(guessKey string) (*loader.VersionData, bool) {
	rest, ok := g.read()
	if !ok {
		return nil, false
	}
	r := guessReader{rest: rest}
	if key := r.string(); r.err != nil || key != guessKey {
		return nil, false
	}
	data := &loader.VersionData{
		Version:              r.string(),
		PrettyVersion:        r.string(),
		Commit:               r.null(),
		FeatureVersion:       r.null(),
		FeaturePrettyVersion: r.null(),
	}
	if r.err != nil || len(r.rest) != 0 {
		return nil, false
	}

	return data, true
}

// store keeps data as the version guessed for a configuration of guessKey,
// unless a file the state depends on is too recent to be trusted;
// failures only lose it.
func (g keptGuess) store(guessKey string, data *loader.VersionData) {
	if !g.trusted {
		return
	}
	b := append([]byte(guessFormat.Header()), g.state...)
	b = appendGuessString(b, guessKey)
	b = appendGuessString(b, data.Version)
	b = appendGuessString(b, data.PrettyVersion)
	for _, n := range [...]pkg.NullString{data.Commit, data.FeatureVersion, data.FeaturePrettyVersion} {
		if !n.Valid {
			b = append(b, 0)

			continue
		}
		b = appendGuessString(append(b, 1), n.S)
	}
	if fsstate.WriteAtomic(g.path, b) != nil {
		return
	}
	fsstate.Prune(filepath.Dir(g.path), guessCacheMaxEntries, 0)
}

func appendGuessString(b []byte, s string) []byte {
	return append(binary.AppendUvarint(b, uint64(len(s))), s...)
}

var errGuessEntry = errors.New("malformed root version entry")

// guessReader reads what store appended; the first malformed part sets
// err, and what follows reads as empty.
type guessReader struct {
	rest []byte
	err  error
}

func (r *guessReader) string() string {
	if r.err != nil {
		return ""
	}
	n, w := binary.Uvarint(r.rest)
	if w <= 0 || n > uint64(len(r.rest)-w) { //nolint:gosec // a length, never negative
		r.err = errGuessEntry

		return ""
	}
	end := w + int(n) //nolint:gosec // at most len(r.rest)
	s := string(r.rest[w:end])
	r.rest = r.rest[end:]

	return s
}

func (r *guessReader) null() pkg.NullString {
	if r.err != nil || len(r.rest) == 0 {
		r.err = errGuessEntry

		return pkg.NullString{}
	}
	valid := r.rest[0]
	r.rest = r.rest[1:]
	switch valid {
	case 0:
		return pkg.NullString{}
	case 1:
		return pkg.Str(r.string())
	default:
		r.err = errGuessEntry

		return pkg.NullString{}
	}
}

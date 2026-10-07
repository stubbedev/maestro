// Store-backed source installs (deliberate deviations 1 and 3 in
// docs/PORTING.md): a checkout cloned from the mirror cache is
// reproducible, so it is kept in the package store and imported again
// instead of running git.

package vcs

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util"
	vcsutil "github.com/stubbedev/maestro/internal/util/vcs"
)

// Two clones of one commit from one mirror are byte-identical, .git
// included, but for the reflogs (their timestamps) and .git/index (the
// stat data of the work tree files); nothing in them depends on the
// vendor path. doInstall therefore stores the directory a clone from the
// cache produced, under a key covering everything its content depends on
// (gitStoreKey), and on a later install of the same key imports it
// unshared (people edit source checkouts) and fixes up those two files
// (fixupCheckout) instead of running clone, remote, checkout and reset.

// gitHost is what the key takes from git and its environment once per
// downloader: none of it changes while maestro runs.
type gitHost struct {
	key    []byte
	bypass bool
}

// bypassConfig are the configuration keys under which a checkout may
// depend on more than the key covers (other files, hooks, filters,
// templates, a sparse or monitored work tree): the store is not used.
var bypassConfig = []string{"core.hookspath", "init.templatedir", "core.fsmonitor", "core.sparsecheckout"}

// bypassConfigPrefixes are the prefixes of such keys.
var bypassConfigPrefixes = []string{"include.", "includeif.", "filter."}

// host returns the key part of git, its configuration and the
// environment, computed once (running `git config` in the mirror at
// cachePath, whose own configuration a clone does not read).
func (d *GitDownloader) host(cachePath string) gitHost {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.gitHost != nil {
		return *d.gitHost
	}

	h := gitHost{}
	d.gitHost = &h

	for _, name := range []string{"GIT_TEMPLATE_DIR", "GIT_COMMITTER_DATE"} {
		if _, ok := os.LookupEnv(name); ok {
			h.bypass = true

			return h
		}
	}

	version, found, err := vcsutil.GetVersion(d.process)
	if err != nil || !found {
		h.bypass = true

		return h
	}

	var parts []string

	parts = append(parts, "git "+version)

	bin, err := exec.LookPath("git")
	if err != nil {
		h.bypass = true

		return h
	}

	if info, err := os.Stat(bin); err == nil {
		parts = append(parts, bin, strconv.FormatInt(info.Size(), 10), strconv.FormatInt(info.ModTime().UnixNano(), 10), info.Mode().String())
	}

	var output string

	code, err := d.process.Execute(util.Cmd("git", "config", "--list", "--show-origin", "-z"), &output, cachePath)
	if err != nil || code != 0 {
		h.bypass = true

		return h
	}

	// -z: origin NUL key LF value NUL (key NUL without a value)
	records := strings.Split(output, "\x00")
	for i := 0; i+1 < len(records); i += 2 {
		origin, entry := records[i], records[i+1]
		if origin == "file:config" {
			// the mirror's own configuration
			continue
		}

		key, _, _ := strings.Cut(entry, "\n")
		key = php.Strtolower(key)

		if slices.Contains(bypassConfig, key) || slices.ContainsFunc(bypassConfigPrefixes, func(p string) bool { return strings.HasPrefix(key, p) }) {
			h.bypass = true

			return h
		}

		parts = append(parts, origin, entry)
	}

	var env []string

	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GIT_") || name == "EMAIL" || name == "HOME" || name == "XDG_CONFIG_HOME" {
			env = append(env, kv)
		}
	}

	slices.Sort(env)
	parts = append(parts, env...)

	hostname, _ := os.Hostname()
	parts = append(parts, hostname, strconv.Itoa(os.Getuid()))

	h.key = keyParts(parts)

	return h
}

// keyParts serializes parts unambiguously.
func keyParts(parts []string) []byte {
	var b []byte

	for _, p := range parts {
		b = strconv.AppendInt(b, int64(len(p)), 10)
		b = append(b, ':')
		b = append(b, p...)
	}

	return b
}

// storeEligible reports whether doInstall may take p's checkout at path
// from the store: a clone from the mirror cache without
// single_use_clone, nothing forcing the checkout, not on Windows.
func (d *GitDownloader) storeEligible(p pkg.PackageInterface, path string) bool {
	if d.store == nil || util.IsWindows() || php.ToBool(p.TransportOptions().Path("git", "single_use_clone")) {
		return false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	return !d.hasDiscardedChanges[path] && !d.hasStashedChanges[path]
}

// gitStoreKey is the store id of p's checkout cloned from the mirror at
// cachePath for url into path, an existing directory; ok is false when
// the store must not be used.
func (d *GitDownloader) gitStoreKey(p pkg.PackageInterface, url, cachePath, path string) (id [32]byte, ok bool) {
	h := d.host(cachePath)
	if h.bypass {
		return id, false
	}

	traits, err := probeFSTraits(path)
	if err != nil {
		return id, false
	}

	mirror, err := mirrorState(cachePath)
	if err != nil {
		return id, false
	}

	sourceURL := p.SourceURL()

	var protocols []string

	if a, isArray := d.config.Get("github-protocols").(*php.Array); isArray {
		for _, v := range a.All() {
			protocols = append(protocols, php.ToString(v))
		}
	}

	parts := []string{
		"maestro-git-checkout-1", string(h.key), cachePath, string(mirror),
		url, strconv.FormatBool(sourceURL.Valid), sourceURL.S,
		p.SourceReference().S, p.PrettyVersion(),
		strings.Join(protocols, "\x00"), vcsutil.GetGitHubDomainsRegex(d.config),
		traits,
	}

	return sha256.Sum256(keyParts(parts)), true
}

// probeFSTraits returns what git's clone detects about the filesystem of
// dir and writes into the checkout's .git/config (core.filemode,
// core.symlinks, core.ignorecase, core.precomposeunicode), probed as git
// init probes them, in a scratch directory under dir: a checkout stored
// from one filesystem must not be imported onto one where git would have
// written other values, or other files (a case-insensitive filesystem
// folds colliding paths).
var probeFSTraits = func(dir string) (string, error) {
	scratch, err := os.MkdirTemp(dir, ".maestro-fs-")
	if err != nil {
		return "", err
	}

	defer func() { _ = os.RemoveAll(scratch) }()

	file := filepath.Join(scratch, "config")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		return "", err
	}

	// core.filemode: the executable bit can be toggled
	fileMode := false

	if st1, err := os.Lstat(file); err == nil && os.Chmod(file, st1.Mode()^0o100) == nil {
		if st2, err := os.Lstat(file); err == nil && st2.Mode() != st1.Mode() {
			fileMode = os.Chmod(file, st1.Mode()) == nil
		}
	}

	// core.symlinks: a symlink can be created
	symlinks := false

	if os.Symlink("testing", filepath.Join(scratch, "tXXXXXX")) == nil {
		st, err := os.Lstat(filepath.Join(scratch, "tXXXXXX"))
		symlinks = err == nil && st.Mode()&fs.ModeSymlink != 0
	}

	// core.ignorecase: "CoNfIg" names "config"
	_, err = os.Lstat(filepath.Join(scratch, "CoNfIg"))
	ignoreCase := err == nil

	// core.precomposeunicode: the decomposed name finds the precomposed one
	precompose := false

	if os.WriteFile(filepath.Join(scratch, "\u00e4"), nil, 0o644) == nil {
		_, err = os.Lstat(filepath.Join(scratch, "a\u0308"))
		precompose = err == nil
	}

	return strings.Join([]string{
		strconv.FormatBool(fileMode), strconv.FormatBool(symlinks),
		strconv.FormatBool(ignoreCase), strconv.FormatBool(precompose),
	}, " "), nil
}

// mirrorState is what a clone takes from the mirror besides its objects,
// which the refs decide: HEAD, packed-refs, the loose refs and the
// alternates.
func mirrorState(cachePath string) ([]byte, error) {
	var parts []string

	for _, name := range []string{"HEAD", "packed-refs", "objects/info/alternates"} {
		data, err := os.ReadFile(filepath.Join(cachePath, filepath.FromSlash(name)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}

		parts = append(parts, name, strconv.FormatBool(err == nil), string(data))
	}

	refs := filepath.Join(cachePath, "refs")

	err := filepath.WalkDir(refs, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		data, err := os.ReadFile(path) //nolint:gosec // the mirror is maestro's own cache.
		if err != nil {
			return err
		}

		parts = append(parts, filepath.ToSlash(path[len(refs):]), string(data))

		return nil
	})
	if err != nil {
		return nil, err
	}

	return keyParts(parts), nil
}

// installFromStore imports the checkout stored under id at path, the
// empty directory prepare left, and fixes it up; false when the store
// does not hold it (any error is treated so: git then clones as usual
// into the empty path again).
func (d *GitDownloader) installFromStore(id [32]byte, path string) bool {
	r, err := d.store.LookupNamed(id)
	if err != nil {
		return false
	}

	// Materialize renames the assembled tree into place, which os.Rename
	// refuses onto a directory, even an empty one.
	if err := os.Remove(path); err != nil {
		return false
	}

	err = d.store.Materialize(r, path, store.ImportOptions{Unshared: true})
	if err == nil {
		if err = d.fixupCheckout(path); err != nil {
			_ = os.RemoveAll(path)
		}
	}

	if err != nil {
		_ = os.MkdirAll(path, 0o777)

		return false
	}

	return true
}

// fixupCheckout makes an imported checkout what a fresh clone would be:
// its reflog entries carry the current time, its index the stat data of
// the imported files.
func (d *GitDownloader) fixupCheckout(path string) error {
	gitDir := filepath.Join(path, ".git")

	if err := fixupReflogs(filepath.Join(gitDir, "logs"), time.Now()); err != nil {
		return err
	}

	err := refreshIndex(path)
	if errors.Is(err, errIndexFallback) {
		var output string

		_, err = d.execute([]string{"git", "update-index", "-q", "--refresh"}, &output, path)
	}

	return err
}

// fixupReflogs rewrites the time of every reflog entry under dir to now.
func fixupReflogs(dir string, now time.Time) error {
	stamp := []byte(strconv.FormatInt(now.Unix(), 10) + " " + now.Format("-0700"))

	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == dir {
				return nil
			}

			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		data, err := os.ReadFile(path) //nolint:gosec // a checkout maestro just imported.
		if err != nil {
			return err
		}

		out, err := restampReflog(data, stamp)
		if err != nil {
			return err
		}

		return os.WriteFile(path, out, 0o644) //nolint:gosec // as above.
	})
}

// restampReflog replaces the "<seconds> <zone>" before the tab (or the end)
// of every reflog line: "<old> <new> <name> <<email>> <seconds> <zone>\t<msg>".
func restampReflog(data, stamp []byte) ([]byte, error) {
	out := make([]byte, 0, len(data)+16)

	for line := range bytes.SplitAfterSeq(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}

		head, tail := line, []byte(nil)
		if i := bytes.IndexByte(line, '\t'); i >= 0 {
			head, tail = line[:i], line[i:]
		} else if bytes.HasSuffix(line, []byte("\n")) {
			head, tail = line[:len(line)-1], line[len(line)-1:]
		}

		gt := bytes.LastIndexByte(head, '>')
		if gt < 0 || len(head) < gt+2 || head[gt+1] != ' ' {
			return nil, errors.New("unexpected reflog line " + strconv.Quote(string(line)))
		}

		out = append(out, head[:gt+2]...)
		out = append(out, stamp...)
		out = append(out, tail...)
	}

	return out, nil
}

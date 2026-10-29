// Ports nothing: an update whose resolution changes nothing in the lock
// file, remembered so that a later update of the same inputs, its metadata
// unchanged, skips building the pool and solving (deliberate deviation 3,
// speed).

package composer

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/filter"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/repository/composerrepo"
	"github.com/stubbedev/maestro/internal/switches"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// resolutionFormat is the version of the entries; its Header starts one.
var resolutionFormat = fsstate.Format{Name: "resolution", Version: 3}

// resolutionCacheMaxEntries bounds the entries kept: past it, the least
// recently written are removed.
const resolutionCacheMaxEntries = 64

// keptResolution is the entry of an update's inputs (keepResolution): the
// update's repositories, and what an earlier update of the same inputs
// left, if any.
type keptResolution struct {
	path  string
	key   []byte
	repos []*composerrepo.ComposerRepository
	// trust is how old the metadata files must be for an entry to be
	// written (composerrepo.ComposerRepository.Resolved).
	trust fsstate.Margin
	cio   *io.ConsoleIO
	// entry is what an earlier update kept, nil for none
	entry *resolutionEntry
}

// resolutionEntry is a kept resolution: the metadata files each
// repository read, and what the update wrote from where its repositories
// were loaded to where the lock file was written.
type resolutionEntry struct {
	files  [][]composerrepo.MetadataFile
	output *io.Recording
}

// keepResolution returns the entry of the update about to run, nil when
// its resolution is not kept: an update's is when maestro's cache is in
// use for it (CreateInstaller: no plugin registered), its output goes to
// a console below -v (whose output shows how long the resolution took), no
// listener may change the pool, nothing is fixed besides the lock file,
// and every repository is a Composer repository. The entry is keyed on
// everything else the resolution and its output read (resolutionKey); the
// metadata files it read are in the entry, revalidated on use.
func (i *Installer) keepResolution(platformRepo *repository.PlatformRepository) *keptResolution {
	cio, ok := i.io.(*io.ConsoleIO)
	if i.resolutions == "" || !ok || i.io.IsVerbose() || i.additionalFixedRepository != nil ||
		i.listenedTo([]string{eventdispatcher.PrePoolCreate}) {
		return nil
	}
	repos := make([]*composerrepo.ComposerRepository, 0, len(i.repositoryManager.Repositories()))
	for _, repo := range i.repositoryManager.Repositories() {
		cr, ok := repo.(*composerrepo.ComposerRepository)
		if !ok {
			return nil
		}
		repos = append(repos, cr)
	}

	key, ok := i.resolutionKey(platformRepo, repos, cio)
	if !ok {
		return nil
	}
	kept := &keptResolution{
		path:  filepath.Join(i.resolutions, hex.EncodeToString(key[:16])),
		key:   key,
		repos: repos,
		cio:   cio,
	}
	kept.entry = kept.load()

	return kept
}

// resolutionKey hashes what an update's resolution and its output read
// besides the metadata files: the process's arguments and the Composer
// environment (COMPOSER_*; maestro's own variables change no
// resolution), the installer's settings
// (resolutionSettings), the root package, the lock file's data, the
// configuration, the platform packages, the repositories, and the
// console's verbosity and decoration. ok is false when one of them cannot
// be read as data: reading them fails nothing (the configuration's
// values, say, are validated where the update uses them).
func (i *Installer) resolutionKey(platformRepo *repository.PlatformRepository, repos []*composerrepo.ComposerRepository, cio *io.ConsoleIO) (key []byte, ok bool) {
	k := fsstate.NewKeyHash()
	k.String(resolutionFormat.Header())

	for _, arg := range os.Args[1:] {
		k.String(arg)
	}
	env := os.Environ()
	slices.Sort(env)
	for _, kv := range env {
		if strings.HasPrefix(kv, "COMPOSER_") {
			k.String(kv)
		}
	}
	k.String(strconv.Itoa(cio.ConsoleOutput().Verbosity()) + strconv.FormatBool(cio.IsDecorated()))

	settings, ok := i.resolutionSettings()
	if !ok {
		return nil, false
	}
	k.String(settings)

	values := []any{}
	root, err := dumper.ArrayDumper{}.Dump(i.pkg)
	if err != nil {
		return nil, false
	}
	values = append(values, root, i.pkg.Aliases(), i.pkg.StabilityFlags(), i.pkg.References(), i.pkg.PreferStable(), i.pkg.Config())

	locked, err := i.locker.IsLocked()
	if err != nil {
		return nil, false
	}
	if locked {
		data, err := i.locker.LockData()
		if err != nil {
			return nil, false
		}
		values = append(values, data)
	} else {
		values = append(values, nil)
	}

	all, ok := i.config.(interface {
		All(flags int) (*php.Array, error)
	})
	if !ok {
		return nil, false
	}
	cfg, err := all.All(0)
	if err != nil {
		return nil, false
	}
	values = append(values, cfg)

	platform, err := platformRepo.Packages()
	if err != nil {
		return nil, false
	}
	for _, p := range platform {
		dumped, err := dumper.ArrayDumper{}.Dump(p)
		if err != nil {
			return nil, false
		}
		values = append(values, dumped)
	}

	for _, repo := range repos {
		values = append(values, repo.RepoName(), repo.RepoConfig())
	}

	for _, v := range values {
		encoded, err := php.JSONEncode(v, php.JSONUnescapedSlashes|php.JSONUnescapedUnicode)
		if err != nil {
			return nil, false
		}
		k.String(encoded)
	}

	return k.Sum(nil), true
}

// resolutionSettings encodes the installer's settings that change what an
// update resolves or writes before the install from the lock file, and
// false when one of them is not plain data (an unknown platform
// requirement filter). TestResolutionSettingsCovered keeps the list of
// the fields it reads and leaves out complete.
func (i *Installer) resolutionSettings() (string, bool) {
	var b strings.Builder
	for _, flag := range [...]bool{
		i.devMode, i.dryRun, i.verbose, i.update, i.preferStable, i.preferLowest, i.minimalUpdate,
		i.writeLock, i.executeOperations, i.updateMirrors,
	} {
		b.WriteString(strconv.FormatBool(flag))
	}
	b.WriteString(strconv.Itoa(i.updateAllowTransitiveDependencies))

	allowed, set := i.allowedTypes.Get()
	for _, list := range [...][]string{i.ignoredTypes, i.updateAllowList, allowed} {
		b.WriteString(strconv.Itoa(len(list)) + "\x00" + strings.Join(list, "\x00") + "\x00")
	}
	b.WriteString(strconv.FormatBool(set))

	filter, ok := platformFilterKey(i.platformRequirementFilter)
	if !ok {
		return "", false
	}
	b.WriteString(filter)

	for name, constraint := range i.temporaryConstraints.All() {
		b.WriteString(name + "\x00")
		if constraint != nil {
			b.WriteString(constraint.String())
		}
		b.WriteString("\x00")
	}

	return b.String(), true
}

// settingsLeftOut are the Installer fields resolutionSettings does not
// read: the collaborators the key covers otherwise (the configuration, the
// root package, the lock file, the repositories) or that only the install
// from the lock file uses, and what the run sets as it goes.
var settingsLeftOut = []string{
	"io", "config", "pkg", "downloadManager", "repositoryManager", "locker", "installationManager",
	"eventDispatcher", "autoloadGenerator", "runtime", "process", "installPreference", "optimizeAutoloader",
	"classMapAuthoritative", "strictPsrAutoloader", "apcuAutoloader", "apcuAutoloaderPrefix", "downloadOnly",
	"install", "dumpAutoloader", "runScripts", "audit", "errorOnAudit", "auditFormat",
	// built from the configuration and the command's options, which the
	// key covers
	"auditConfig", "policyConfig",
	// the root package without its requirements, which the key covers
	"fixedRootPackage",
	"suggestedPackagesReporter", "additionalFixedRepository", "lockTransaction", "executedOperations",
	"resolutions",
}

// platformFilterKey names a platform requirement filter, false for one it
// does not know.
func platformFilterKey(f version.PlatformRequirementFilter) (string, bool) {
	switch f := f.(type) {
	case filter.IgnoreAll:
		return "all", true
	case filter.IgnoreNothing:
		return "none", true
	case *filter.IgnoreList:
		return "list:" + strings.Join(f.ReqList(), "\x00"), true
	}

	return "", false
}

// load reads the entry kept for the key, nil when there is none.
func (k *keptResolution) load() *resolutionEntry {
	data, err := os.ReadFile(k.path)
	if err != nil {
		return nil
	}
	rest, ok := bytes.CutPrefix(data, []byte(resolutionFormat.Header()))
	if !ok {
		return nil
	}
	if rest, ok = bytes.CutPrefix(rest, k.key); !ok {
		return nil
	}
	entry, err := decodeResolutionEntry(rest, len(k.repos))
	if err != nil {
		return nil
	}

	return entry
}

// revalidate reports whether the kept entry's metadata files are what the
// repositories hold now (composerrepo.ComposerRepository.Revalidate),
// with nothing written meanwhile (rec): a warning a request leads to is
// part of what the update writes. A repository that fails to load is a
// miss: the update then meets the failure where it would without the
// entry.
func (k *keptResolution) revalidate(rec *io.Recorder) bool {
	if k.entry == nil {
		return false
	}
	for j, repo := range k.repos {
		same, err := repo.Revalidate(k.entry.files[j])
		if err != nil || !same || rec.Len() != 0 {
			return false
		}
	}

	return true
}

// store keeps what an update that changed nothing in the lock file read
// and wrote (output), when every repository's files can be told
// unchanged later (Resolved).
func (k *keptResolution) store(output *io.Recording) {
	now := time.Now()
	files := make([][]composerrepo.MetadataFile, len(k.repos))
	for j, repo := range k.repos {
		var ok bool
		if files[j], ok = repo.Resolved(now, k.trust); !ok {
			return
		}
	}
	b := append([]byte(resolutionFormat.Header()), k.key...)
	b = appendResolutionEntry(b, &resolutionEntry{files: files, output: output})
	if fsstate.WriteAtomic(k.path, b) != nil {
		return
	}
	fsstate.Prune(filepath.Dir(k.path), resolutionCacheMaxEntries, 0)
}

// verifyResolution tells a hit to resolve anyway and fail unless the
// resolution changes nothing in the lock file, as the entry says
// (switches.VerifyResolutionCache).
func verifyResolution() bool { return switches.On(switches.VerifyResolutionCache) }

// errResolutionDiffers is the failure of a verified hit whose resolution
// changes the lock file or writes something else.
func errResolutionDiffers() error {
	return &util.LogicError{Message: "The kept resolution of this update differs from its resolution (" + switches.VerifyResolutionCache + ")"}
}

func appendResolutionEntry(b []byte, e *resolutionEntry) []byte {
	str := func(s string) { b = append(binary.AppendUvarint(b, uint64(len(s))), s...) }
	b = binary.AppendUvarint(b, uint64(len(e.files)))
	for _, files := range e.files {
		b = binary.AppendUvarint(b, uint64(len(files)))
		for _, f := range files {
			str(f.URL)
			str(f.CacheKey)
			str(f.LastModified)
			var flags byte
			if f.Root {
				flags |= 1
			}
			if f.NotFound {
				flags |= 2
			}
			b = f.ID.AppendBinary(append(b, flags))
		}
	}

	return e.output.AppendBinary(b)
}

var errResolutionEntry = errors.New("malformed resolution entry")

func decodeResolutionEntry(b []byte, repos int) (*resolutionEntry, error) {
	r := bytes.NewReader(b)
	count := func() (int, error) {
		n, err := binary.ReadUvarint(r)
		if err != nil || n > uint64(r.Len()) { //nolint:gosec // a length, never negative
			return 0, errResolutionEntry
		}

		return int(n), nil //nolint:gosec // at most r.Len()
	}
	str := func() (string, error) {
		n, err := count()
		if err != nil {
			return "", err
		}
		s := make([]byte, n)
		if _, err := r.Read(s); err != nil && n > 0 {
			return "", errResolutionEntry
		}

		return string(s), nil
	}

	n, err := count()
	if err != nil || n != repos {
		return nil, errResolutionEntry
	}
	e := &resolutionEntry{files: make([][]composerrepo.MetadataFile, n)}
	for j := range e.files {
		m, err := count()
		if err != nil {
			return nil, err
		}
		files := make([]composerrepo.MetadataFile, m)
		for x := range files {
			f := &files[x]
			if f.URL, err = str(); err != nil {
				return nil, err
			}
			if f.CacheKey, err = str(); err != nil {
				return nil, err
			}
			if f.LastModified, err = str(); err != nil {
				return nil, err
			}
			flags, err := r.ReadByte()
			if err != nil || flags > 3 {
				return nil, errResolutionEntry
			}
			f.Root, f.NotFound = flags&1 != 0, flags&2 != 0
			if f.ID, err = fsstate.ReadBinary(r); err != nil {
				return nil, errResolutionEntry
			}
		}
		e.files[j] = files
	}
	rest := b[len(b)-r.Len():]
	output, rest, err := io.ReadRecording(rest)
	if err != nil || len(rest) != 0 {
		return nil, errResolutionEntry
	}
	e.output = output

	return e, nil
}

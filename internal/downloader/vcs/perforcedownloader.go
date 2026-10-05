// Ports src/Composer/Downloader/PerforceDownloader.php.

package vcs

import (
	"strings"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	vcsutil "github.com/stubbedev/maestro/internal/util/vcs"
)

// Perforce is the part of Composer\Util\Perforce PerforceDownloader uses.
// *vcs.Perforce (internal/util/vcs) implements it.
type Perforce interface {
	InitializePath(path string) error
	SetStream(stream string)
	P4Login() error
	WriteP4ClientSpec() error
	ConnectClient() error
	SyncCodeBase(sourceReference *string) error
	CleanupClientSpec() error
	GetCommitLogs(fromReference, toReference string) (string, bool, error)
}

var _ Perforce = (*vcsutil.Perforce)(nil)

// PerforceDownloader ports Composer\Downloader\PerforceDownloader.
type PerforceDownloader struct {
	vcsDownloader
	perforce Perforce
}

// NewPerforceDownloader is new PerforceDownloader($io, $config, $process,
// $fs).
func NewPerforceDownloader(deps Deps) *PerforceDownloader {
	d := &PerforceDownloader{}
	d.vcsDownloader = newVcsDownloader(deps, d, `Composer\Downloader\PerforceDownloader`)

	return d
}

func (d *PerforceDownloader) doDownload(pkg.PackageInterface, string, string, pkg.PackageInterface) error {
	return nil
}

func (d *PerforceDownloader) doInstall(p pkg.PackageInterface, path, url string) error {
	return d.DoInstall(p, path, url)
}

// DoInstall is the public doInstall(): syncs the package's stream (and
// label, after an "@" in its reference) into path.
func (d *PerforceDownloader) DoInstall(p pkg.PackageInterface, path, url string) error {
	ref := p.SourceReference().S
	label := labelFromSourceReference(ref)

	d.io.WriteError("Cloning "+ref, true, mio.Normal)

	if err := d.InitPerforce(p, path, url); err != nil {
		return err
	}

	d.perforce.SetStream(ref)

	for _, step := range []func() error{d.perforce.P4Login, d.perforce.WriteP4ClientSpec, d.perforce.ConnectClient} {
		if err := step(); err != nil {
			return err
		}
	}

	if err := d.perforce.SyncCodeBase(label); err != nil {
		return err
	}

	return d.perforce.CleanupClientSpec()
}

// labelFromSourceReference is getLabelFromSourceReference(): what follows
// the first "@", nil for none.
func labelFromSourceReference(ref string) *string {
	if _, label, ok := strings.Cut(ref, "@"); ok {
		return &label
	}

	return nil
}

// InitPerforce is initPerforce(): reuses the Perforce instance for path,
// or creates one from the package's VcsRepository config.
func (d *PerforceDownloader) InitPerforce(p pkg.PackageInterface, path, url string) error {
	if d.perforce != nil {
		return d.perforce.InitializePath(path)
	}

	var repoConfig *php.Array
	if config, ok := vcsRepoConfig(p); ok {
		repoConfig = config
	}

	perforce, err := vcsutil.CreatePerforce(repoConfig, url, path, d.process, d.io)
	if err != nil {
		return err
	}

	d.perforce = perforce

	return nil
}

func (d *PerforceDownloader) doUpdate(_, target pkg.PackageInterface, path, url string) error {
	return d.DoInstall(target, path, url)
}

// LocalChanges is getLocalChanges(): Perforce does not check.
func (d *PerforceDownloader) LocalChanges(pkg.PackageInterface, string) (pkg.NullString, error) {
	d.io.WriteError("Perforce driver does not check for local changes before overriding", true, mio.Normal)

	return pkg.NullString{}, nil
}

// commitLogs is getCommitLogs(); Perforce's null result (where PHP's
// string return type would throw a TypeError) is "".
func (d *PerforceDownloader) commitLogs(fromReference, toReference, _ string) (string, error) {
	logs, _, err := d.perforce.GetCommitLogs(fromReference, toReference)

	return logs, err
}

// SetPerforce is setPerforce().
func (d *PerforceDownloader) SetPerforce(perforce Perforce) { d.perforce = perforce }

func (d *PerforceDownloader) hasMetadataRepository(string) bool { return true }

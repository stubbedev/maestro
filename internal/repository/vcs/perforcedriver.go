// Ports src/Composer/Repository/Vcs/PerforceDriver.php.

package vcs

import (
	"time"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
	uvcs "github.com/stubbedev/maestro/internal/util/vcs"
)

// perforceClient is the part of Composer\Util\Perforce the driver uses;
// *uvcs.Perforce implements it.
type perforceClient interface {
	P4Login() error
	CheckStream() (bool, error)
	WriteP4ClientSpec() error
	ConnectClient() error
	GetComposerInformation(identifier string) (*php.Array, error)
	GetFileContent(file, identifier string) (string, bool, error)
	GetBranches() (*php.Array, error)
	GetTags() (*php.Array, error)
	GetUser() *string
	CleanupClientSpec() error
}

var _ perforceClient = (*uvcs.Perforce)(nil)

// PerforceDriver ports Composer\Repository\Vcs\PerforceDriver: a depot
// of a Perforce server, read through a client workspace in
// cache-vcs-dir.
type PerforceDriver struct {
	vcsDriver
	depot    string
	branch   string
	perforce perforceClient
}

func newPerforceDriver(repoConfig *php.Array, deps Deps) Driver {
	return NewPerforceDriver(repoConfig, deps)
}

// NewPerforceDriver is new PerforceDriver($repoConfig, $io, $config,
// $httpDownloader, $process).
func NewPerforceDriver(repoConfig *php.Array, deps Deps) *PerforceDriver {
	d := &PerforceDriver{}
	d.init(d, repoConfig, deps)

	return d
}

// Class returns the PHP class name.
func (d *PerforceDriver) Class() string { return perforceDriverType.Class }

// Initialize ports PerforceDriver::initialize.
func (d *PerforceDriver) Initialize() error {
	d.depot = pathString(d.repoConfig, "depot")
	d.branch = ""

	if v, _ := d.repoConfig.Get("branch"); php.ToBool(v) {
		d.branch = php.ToString(v)
	}

	if err := d.initPerforce(d.repoConfig); err != nil {
		return err
	}

	if err := d.perforce.P4Login(); err != nil {
		return err
	}

	if _, err := d.perforce.CheckStream(); err != nil {
		return err
	}

	if err := d.perforce.WriteP4ClientSpec(); err != nil {
		return err
	}

	return d.perforce.ConnectClient()
}

// initPerforce ports initPerforce().
func (d *PerforceDriver) initPerforce(repoConfig *php.Array) error {
	if d.perforce != nil {
		return nil
	}

	vcsDir := php.ToString(d.config.Get("cache-vcs-dir"))
	if !cache.IsUsable(vcsDir) {
		return &util.RuntimeError{Site: phperr.At("PerforceDriver.php", 64), Message: "PerforceDriver requires a usable cache directory, and it looks like you set it to be disabled"}
	}

	repoDir := vcsDir + "/" + d.depot

	perforce, err := uvcs.CreatePerforce(repoConfig, d.URL(), repoDir, d.process, d.io)
	if err != nil {
		return err
	}

	d.perforce = perforce

	return nil
}

// FileContent ports PerforceDriver::getFileContent.
func (d *PerforceDriver) FileContent(file, identifier string) (string, bool, error) {
	return d.perforce.GetFileContent(file, identifier)
}

// ChangeDate ports PerforceDriver::getChangeDate.
func (d *PerforceDriver) ChangeDate(string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

// RootIdentifier ports PerforceDriver::getRootIdentifier.
func (d *PerforceDriver) RootIdentifier() (string, error) { return d.branch, nil }

// Branches ports PerforceDriver::getBranches.
func (d *PerforceDriver) Branches() (*php.Array, error) { return d.perforce.GetBranches() }

// Tags ports PerforceDriver::getTags.
func (d *PerforceDriver) Tags() (*php.Array, error) { return d.perforce.GetTags() }

// Dist ports PerforceDriver::getDist.
func (d *PerforceDriver) Dist(string) *php.Array { return nil }

// Source ports PerforceDriver::getSource.
func (d *PerforceDriver) Source(identifier string) *php.Array {
	var user any
	if u := d.perforce.GetUser(); u != nil {
		user = *u
	}

	url, _ := d.repoConfig.Get("url")

	return php.ArrayOf("type", "perforce", "url", url, "reference", identifier, "p4user", user)
}

// URL ports PerforceDriver::getUrl.
func (d *PerforceDriver) URL() string { return d.url }

// HasComposerFile ports PerforceDriver::hasComposerFile.
func (d *PerforceDriver) HasComposerFile(identifier string) (bool, error) {
	composerInfo, err := d.perforce.GetComposerInformation("//" + d.depot + "/" + identifier)
	if err != nil {
		return false, err
	}

	return composerInfo != nil && composerInfo.Len() > 0, nil
}

// GetContents ports PerforceDriver::getContents, which Perforce does not
// support.
func (d *PerforceDriver) GetContents(string) (*http.Response, error) {
	return nil, &util.LogicError{Site: phperr.At("PerforceDriver.php", 155), Message: "Not implemented/used in PerforceDriver"}
}

var perforceURL = php.MustCompile(`#\b(perforce|p4)\b#i`)

// perforceSupports ports PerforceDriver::supports.
func perforceSupports(deps Deps, url string, deep bool) (bool, error) {
	if !deep {
		if ok, err := matches(perforceURL, url); err != nil || !ok {
			return false, err
		}
	}

	return uvcs.CheckServerExists(url, deps.Process)
}

// Cleanup ports PerforceDriver::cleanup.
func (d *PerforceDriver) Cleanup() error {
	if d.perforce == nil {
		return nil
	}

	err := d.perforce.CleanupClientSpec()
	d.perforce = nil

	return err
}

// Depot ports getDepot().
func (d *PerforceDriver) Depot() string { return d.depot }

// Branch ports getBranch().
func (d *PerforceDriver) Branch() string { return d.branch }

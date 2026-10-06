// Ports src/Composer/Repository/ArtifactRepository.php and
// PearRepository.php.

package repository

import (
	"crypto/sha1" //nolint:gosec // the dist shasum Composer records is a sha1
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/util"
)

// ArtifactRepository ports Composer\Repository\ArtifactRepository: the
// packages of the zip and tar archives found below a directory.
//
// The directory is walked in readdir() order, as PHP's
// RecursiveDirectoryIterator does: the package order is the file
// system's.
type ArtifactRepository struct {
	ArrayRepository
	loader     *loader.ArrayLoader
	lookup     string
	repoConfig *php.Array
	io         mio.IO
}

var _ ConfigurableRepository = (*ArtifactRepository)(nil)

// NewArtifactRepository ports new ArtifactRepository($repoConfig, $io);
// repoConfig's "url" is the directory. maestro reads archives natively,
// so PHP's check for the zip extension does not apply.
func NewArtifactRepository(repoConfig *php.Array, out mio.IO) (*ArtifactRepository, error) {
	r := &ArtifactRepository{loader: loader.NewArrayLoader(nil, false), repoConfig: repoConfig, io: out}
	r.bind(r, r)
	urlValue, _ := repoConfig.Get("url")
	url, ok := urlValue.(string)
	if !ok {
		return nil, pkg.ArgumentTypeError(`Composer\Util\Platform::expandPath`, 1, "path", "string", urlValue).
			Called(`Composer\Util\Platform::expandPath`, phperr.At("Platform.php", 158), "ArtifactRepository.php", 50)
	}
	lookup, err := util.ExpandPath(url)
	if err != nil {
		return nil, err
	}
	r.lookup = lookup

	return r, nil
}

// Class returns the PHP class name.
func (r *ArtifactRepository) Class() string { return `Composer\Repository\ArtifactRepository` }

// RepoName ports ArtifactRepository::getRepoName.
func (r *ArtifactRepository) RepoName() string { return "artifact repo (" + r.lookup + ")" }

// RepoConfig ports ArtifactRepository::getRepoConfig.
func (r *ArtifactRepository) RepoConfig() *php.Array { return r.repoConfig }

// initialize ports ArtifactRepository::initialize.
func (r *ArtifactRepository) initialize() error {
	r.baseInitialize()

	return r.scanDirectory(r.lookup)
}

var artifactName = php.MustCompile(`/^.+\.(zip|tar|gz|tgz)$/i`)

// scanDirectory ports ArtifactRepository::scanDirectory: a recursive walk
// following symlinks, as RecursiveDirectoryIterator::FOLLOW_SYMLINKS.
func (r *ArtifactRepository) scanDirectory(path string) error {
	// RecursiveDirectoryIterator drops one trailing slash
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
	}
	entries, err := util.ReadDirOrder(path)
	if err != nil {
		return &util.UnexpectedValueError{Site: phperr.At("ArtifactRepository.php", 76), Message: "RecursiveDirectoryIterator::__construct(" + path + "): Failed to open directory: " + util.Strerror(err)}
	}

	return r.scanEntries(path, entries, map[string]bool{})
}

func (r *ArtifactRepository) scanEntries(dir string, entries []os.DirEntry, active map[string]bool) error {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		if active[real] {
			return nil
		}
		active[real] = true
		defer delete(active, real)
	}

	for _, entry := range entries {
		pathname := dir + "/" + entry.Name()
		info, err := os.Stat(pathname)
		if err != nil {
			continue
		}
		if info.IsDir() {
			children, err := util.ReadDirOrder(pathname)
			if err != nil {
				return &util.UnexpectedValueError{Site: phperr.At("ArtifactRepository.php", 79), Message: "RecursiveDirectoryIterator::__construct(" + pathname + "): Failed to open directory: " + util.Strerror(err)}
			}
			if err := r.scanEntries(pathname, children, active); err != nil {
				return err
			}

			continue
		}
		if matched, _ := artifactName.IsMatch(pathname); !matched || !info.Mode().IsRegular() {
			continue
		}

		p, err := r.composerInformation(pathname)
		if err != nil {
			return err
		}
		basename := entry.Name()
		if p == nil {
			r.io.WriteError("File <comment>"+basename+"</comment> doesn't seem to hold a package", true, mio.Verbose)

			continue
		}

		r.io.WriteError("Found package <info>"+p.Name()+"</info> (<comment>"+p.PrettyVersion()+"</comment>) in file <info>"+basename+"</info>", true, mio.Verbose)

		if err := r.hooks.addPackage(p); err != nil {
			return err
		}
	}

	return nil
}

// composerInformation ports ArtifactRepository::getComposerInformation:
// the package of an archive, nil when it holds no composer.json.
func (r *ArtifactRepository) composerInformation(pathname string) (pkg.PackageInterface, error) {
	var fileType string
	switch ext := filepath.Ext(pathname); ext {
	case ".gz", ".tar", ".tgz":
		fileType = "tar"
	case ".zip":
		fileType = "zip"
	default:
		return nil, &util.RuntimeError{Site: phperr.At("ArtifactRepository.php", 108), Message: `Files with "` + strings.TrimPrefix(ext, ".") + `" extensions aren't supported. Only ZIP and TAR/TAR.GZ/TGZ archives are supported.`}
	}

	var content string
	var ok bool
	var err error
	if fileType == "tar" {
		content, ok, err = util.TarGetComposerJSON(pathname)
	} else {
		content, ok, err = util.ZipGetComposerJSON(pathname)
	}
	if err != nil {
		r.io.Write("Failed loading package "+pathname+": "+err.Error(), false, mio.Verbose)
	}
	if err != nil || !ok {
		return nil, nil
	}

	decoded, err := json.ParseJSON(content, pathname+"#composer.json")
	if err != nil {
		return nil, err
	}
	data, isArray := decoded.(*php.Array)
	if !isArray {
		return nil, &util.ErrorException{Site: phperr.At("ArtifactRepository.php", 126), Message: "Cannot use a scalar value as an array"}
	}
	shasum, err := sha1File(util.Realpath(pathname))
	if err != nil {
		return nil, err
	}
	data.Set("dist", php.ArrayOf(
		"type", fileType,
		"url", strings.ReplaceAll(pathname, `\`, "/"),
		"shasum", shasum,
	))

	p, err := r.loader.Load(data, pkg.ClassCompletePackage)
	if isUnexpectedValue(err) {
		return nil, &wrappedError{err: &util.UnexpectedValueError{Site: phperr.At("ArtifactRepository.php", 135), Message: "Failed loading package in " + pathname + ": " + err.Error()}, previous: err}
	}

	return p, err
}

func sha1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New() //nolint:gosec // see the import
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// NewPearRepository ports new PearRepository(): PEAR repositories are gone.
func NewPearRepository() (RepositoryInterface, error) {
	return nil, &util.InvalidArgumentError{Site: phperr.At("PearRepository.php", 30), Message: "The PEAR repository has been removed from Composer 2.x"}
}

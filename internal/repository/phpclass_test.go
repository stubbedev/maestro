package repository_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/repository/composerrepo"
	"github.com/stubbedev/maestro/internal/repository/vcs"
)

// Every repository type names its own class (get_class($repository)),
// which plugins read: one of Composer's, and never the class of a type it
// embeds (each type has a class no other has).
func TestRepositories_NameTheirOwnPHPClass(t *testing.T) {
	types := map[string]string{}
	for _, r := range []repository.RepositoryInterface{
		&repository.ArrayRepository{}, &repository.WritableArrayRepository{}, &repository.InstalledArrayRepository{},
		&repository.LockArrayRepository{}, &repository.RootPackageRepository{}, &repository.FilesystemRepository{},
		&repository.InstalledFilesystemRepository{}, &repository.PlatformRepository{}, &repository.CompositeRepository{},
		&repository.FilterRepository{}, &repository.InstalledRepository{}, &repository.PackageRepository{},
		&repository.ArtifactRepository{}, &repository.PathRepository{}, &composerrepo.ComposerRepository{}, &vcs.VcsRepository{},
	} {
		class, typ := r.PHPClass(), fmt.Sprintf("%T", r)
		if !strings.HasPrefix(class, `Composer\Repository\`) {
			t.Errorf("%s is of class %q", typ, class)
		}
		if other, ok := types[class]; ok {
			t.Errorf("%s and %s are both of class %q", other, typ, class)
		}
		types[class] = typ
	}
}

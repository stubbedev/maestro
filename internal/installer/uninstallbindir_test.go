package installer

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util"
)

// TestInstallationManager_UninstallBinDirIndependentOfOrder pins deviation
// 7 (docs/PORTING.md, docs/tasks/binflake.md).
//
// LibraryInstaller::uninstall calls BinaryInstaller::removeBinaries once the
// package's directory is gone. Composer's removeBinaries creates the bin dir
// before returning early for a package without binaries, so when a batch
// removes every package, an emptied vendor/bin survives unless the binary
// package's callback happens to run last, which depends on the order its
// `rm -rf` processes are seen finishing. maestro only touches the bin dir
// for packages with binaries, so the outcome is the same in every order: the
// last binary removed takes the empty bin dir away, and removals never
// create it.
func TestInstallationManager_UninstallBinDirIndependentOfOrder(t *testing.T) {
	tests := []struct {
		name         string
		order        []string // removal (start) order; "tool" has a binary
		binDirBefore bool     // vendor/bin exists (holding tool's proxy) before
	}{
		// plugin-captainhook's install --no-dev: captainhook/captainhook,
		// the only package with a binary, is removed last.
		{"binary package started last", []string{"a", "b", "tool"}, true},
		// Composer recreates the bin dir here (a package without binaries
		// settles after the binary package removed it); maestro doesn't.
		{"binary package started first", []string{"tool", "a", "b"}, true},
		// Composer creates an empty bin dir here; maestro doesn't.
		{"no binary packages, no bin dir", []string{"a", "b"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := newLibraryFixture(t)
			binDir := f.vendorDir + "/bin"

			fs := util.NewFilesystem(nil)
			bi := NewBinaryInstaller(newBufferIO(t), binDir, "auto", fs, pkg.Str(f.vendorDir))
			library := f.installer(t, pkg.NullString{}, fs, bi)

			repo := newMockRepo(t)
			repo.fake = false

			// the removals settle in the reverse of their start order,
			// the binary package's first when it started last
			sched := util.NewScheduler()
			started := make(chan func(string), len(tt.order))
			f.dm.result = func(method string, p pkg.PackageInterface) (*downloader.Promise, error) {
				if method != "remove" {
					return util.Resolved(""), nil
				}

				if err := os.RemoveAll(f.vendorDir + "/" + p.Name()); err != nil {
					return nil, err
				}

				promise, resolve, _ := util.NewAsync[string](sched, nil)
				started <- resolve

				return promise, nil
			}

			go func() {
				var resolves []func(string)
				for range tt.order {
					resolves = append(resolves, <-started)
				}

				for _, resolve := range slices.Backward(resolves) {
					resolve("")
					time.Sleep(time.Millisecond)
				}
			}()

			var ops []operation.Operation

			for _, name := range tt.order {
				p := newPackage("v/"+name, "1.0.0")
				p.SetType("library")

				if name == "tool" {
					p.SetBinaries(php.ListOf("bin/tool"))
				}

				mustMkdir(t, f.vendorDir+"/v/"+name)

				if err := repo.AddPackage(p); err != nil {
					t.Fatal(err)
				}

				ops = append(ops, operation.NewUninstallOperation(p))
			}

			if tt.binDirBefore {
				mustMkdir(t, binDir)

				if err := os.WriteFile(binDir+"/tool", []byte("proxy"), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			manager := NewManager(nil, newBufferIO(t), nil)
			manager.AddInstaller(library)

			if err := manager.Execute(repo, ops, false, false, false); err != nil {
				t.Fatal(err)
			}

			if _, err := os.Lstat(binDir); err == nil {
				t.Errorf("vendor/bin exists after removing every package, want it gone")
			}
		})
	}
}

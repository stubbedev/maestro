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

// TestInstallationManager_UninstallBinDirFollowsStartOrder is the root
// cause of the plugin-captainhook e2e flake (docs/tasks/binflake.md).
//
// LibraryInstaller::uninstall calls BinaryInstaller::removeBinaries once
// the package's directory is gone, and removeBinaries starts with
// initializeBinDir(), which creates the bin dir, before it returns early
// for a package without binaries; a package with binaries removes the bin
// dir again when it is left empty. So when a batch removes every package,
// vendor/bin survives (empty) unless the last removeBinaries call is for a
// package with binaries. Composer runs those callbacks in the order its
// `rm -rf` processes are seen finishing, which varies between runs;
// maestro runs them in the order the removals started (util.Scheduler),
// whatever order they finish in. Start order is also Composer's outcome in
// the common case (the process started last is nearly always seen
// finishing last: 76 of 80 fresh runs of the fixture, see the task's
// report).
func TestInstallationManager_UninstallBinDirFollowsStartOrder(t *testing.T) {
	tests := []struct {
		name     string
		order    []string // removal (start) order; "tool" has a binary
		wantBins bool     // vendor/bin left behind (empty)
	}{
		// plugin-captainhook's install --no-dev: captainhook/captainhook,
		// the only package with a binary, is removed last.
		{"binary package started last", []string{"a", "b", "tool"}, false},
		// a package without binaries recreates the bin dir after the
		// binary package removed it.
		{"binary package started first", []string{"tool", "a", "b"}, true},
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

			mustMkdir(t, binDir)

			if err := os.WriteFile(binDir+"/tool", []byte("proxy"), 0o755); err != nil {
				t.Fatal(err)
			}

			manager := NewManager(nil, newBufferIO(t), nil)
			manager.AddInstaller(library)

			if err := manager.Execute(repo, ops, false, false, false); err != nil {
				t.Fatal(err)
			}

			entries, err := os.ReadDir(binDir)

			switch {
			case tt.wantBins && err != nil:
				t.Errorf("vendor/bin was removed, want it left (empty): %v", err)
			case tt.wantBins && len(entries) != 0:
				t.Errorf("vendor/bin holds %v, want it empty", entries)
			case !tt.wantBins && err == nil:
				t.Errorf("vendor/bin was left behind, want it removed")
			}
		})
	}
}

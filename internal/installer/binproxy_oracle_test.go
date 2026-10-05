//go:build unix

package installer

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/testutil"
	"github.com/stubbedev/maestro/internal/util"
)

// binProxyScenario is one scenario of testdata/oracle/binproxy.json,
// written by tools/oracle/installer/binproxy.php.
type binProxyScenario struct {
	Name        string  `json:"name"`
	BinCompat   string  `json:"binCompat"`
	VendorDir   *string `json:"vendorDir"`
	BinDir      string  `json:"binDir"`
	Package     string  `json:"package"`
	InstallPath string  `json:"installPath"`
	Binaries    []string
	Files       map[string]struct {
		Content string `json:"content"`
		Mode    string `json:"mode"`
	} `json:"files"`
	Symlinks  map[string]string    `json:"symlinks"`
	Dirs      []string             `json:"dirs"`
	Steps     [][]any              `json:"steps"`
	Tree      map[string]treeEntry `json:"tree"`
	Output    string               `json:"output"`
	Exception *string              `json:"exception"`
}

type treeEntry struct {
	Type    string `json:"type"`
	Target  string `json:"target,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Content string `json:"content,omitempty"`
}

// TestBinaryInstaller_Oracle checks the vendor/bin proxies, file modes and
// output of BinaryInstaller against Composer's, byte for byte.
func TestBinaryInstaller_Oracle(t *testing.T) {
	var scenarios []binProxyScenario

	testutil.LoadJSONGolden(t, "testdata/oracle/binproxy.json", &scenarios)

	if len(scenarios) == 0 {
		t.Fatal("no scenarios")
	}

	for _, s := range scenarios {
		t.Run(s.Name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}

			for _, dir := range s.Dirs {
				mustMkdir(t, filepath.Join(root, dir))
			}

			for path, f := range s.Files {
				content, err := base64.StdEncoding.DecodeString(f.Content)
				if err != nil {
					t.Fatal(err)
				}

				var mode uint32
				if _, err := fmt.Sscanf(f.Mode, "%o", &mode); err != nil {
					t.Fatal(err)
				}

				full := filepath.Join(root, path)
				mustMkdir(t, filepath.Dir(full))

				if err := os.WriteFile(full, content, 0o600); err != nil {
					t.Fatal(err)
				}

				if err := os.Chmod(full, fs.FileMode(mode)); err != nil {
					t.Fatal(err)
				}
			}

			for path, target := range s.Symlinks {
				full := filepath.Join(root, path)
				mustMkdir(t, filepath.Dir(full))

				if err := os.Symlink(target, full); err != nil {
					t.Fatal(err)
				}
			}

			buf, err := mio.NewBufferIO("", 0, nil)
			if err != nil {
				t.Fatal(err)
			}

			vendorDir := pkg.NullString{}
			if s.VendorDir != nil {
				vendorDir = pkg.Str(root + "/" + *s.VendorDir)
			}

			bi := NewBinaryInstaller(buf, root+"/"+s.BinDir, s.BinCompat, util.NewFilesystem(nil), vendorDir)

			p := pkg.NewPackage(s.Package, "1.0.0.0", "1.0.0")
			bins := make([]any, len(s.Binaries))
			for i, b := range s.Binaries {
				bins[i] = b
			}
			p.SetBinaries(php.ListOf(bins...))

			var exception *string

			for _, step := range s.Steps {
				var err error
				if step[0] == "install" {
					err = bi.InstallBinaries(p, root+"/"+s.InstallPath, step[1] == true)
				} else {
					err = bi.RemoveBinaries(p)
				}

				if err != nil {
					msg := strings.ReplaceAll(err.Error(), root, "%ROOT%")
					exception = &msg

					break
				}
			}

			if (exception == nil) != (s.Exception == nil) || (exception != nil && *exception != *s.Exception) {
				t.Errorf("exception = %v, want %v", deref(exception), deref(s.Exception))
			}

			if got := strings.ReplaceAll(buf.Output(), root, "%ROOT%"); got != s.Output {
				t.Errorf("output:\n%q\nwant\n%q", got, s.Output)
			}

			got := map[string]treeEntry{}

			err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil || path == root {
					return err
				}

				rel := strings.TrimPrefix(path, root+"/")

				switch {
				case d.Type()&fs.ModeSymlink != 0:
					target, err := os.Readlink(path)
					if err != nil {
						return err
					}

					got[rel] = treeEntry{Type: "link", Target: target}
				case d.IsDir():
					got[rel] = treeEntry{Type: "dir"}
				default:
					st, err := os.Stat(path)
					if err != nil {
						return err
					}

					content, err := os.ReadFile(path)
					if err != nil {
						return err
					}

					got[rel] = treeEntry{
						Type:    "file",
						Mode:    fmt.Sprintf("%04o", st.Mode().Perm()),
						Content: base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(string(content), root, "%ROOT%"))),
					}
				}

				return nil
			})
			if err != nil {
				t.Fatal(err)
			}

			for path, want := range s.Tree {
				g, ok := got[path]
				if !ok {
					t.Errorf("%s missing", path)

					continue
				}

				if g != want {
					gc, _ := base64.StdEncoding.DecodeString(g.Content)
					wc, _ := base64.StdEncoding.DecodeString(want.Content)
					t.Errorf("%s: got %s %s %s\n%s\nwant %s %s %s\n%s", path, g.Type, g.Mode, g.Target, gc, want.Type, want.Mode, want.Target, wc)
				}
			}

			for path := range got {
				if _, ok := s.Tree[path]; !ok {
					t.Errorf("%s unexpected", path)
				}
			}
		})
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}

	return *s
}

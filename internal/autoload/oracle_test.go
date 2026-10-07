package autoload

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/testutil"
)

// oracleRepo is the repository of an oracle scenario.
type oracleRepo struct {
	packages []pkg.PackageInterface
	devNames []string
}

func (r *oracleRepo) DevPackageNames() []string                 { return r.devNames }
func (r *oracleRepo) CanonicalPackages() []pkg.PackageInterface { return r.packages }

// oracleLocker is a locked Locker with the given content-hash.
type oracleLocker struct{ hash string }

func (l oracleLocker) IsLocked() (bool, error) { return true, nil }
func (l oracleLocker) LockData() (*php.Array, error) {
	return php.ArrayOf("content-hash", l.hash), nil
}

// TestOracle_Dump replays the scenarios of tools/oracle/autoload/generate.php
// and compares every file written, the class map and the output with what
// Composer's AutoloadGenerator produced.
func TestOracle_Dump(t *testing.T) {
	data := testutil.ReadGolden(t, "testdata/oracle/dump.json.gz")
	decoded, err := php.JSONDecode(string(data), true)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range decoded.(*php.Array).Values() {
		s := v.(*php.Array)
		name, _ := s.GetString("name")
		t.Run(name, func(t *testing.T) { runOracleScenario(t, s) })
	}
}

func runOracleScenario(t *testing.T, s *php.Array) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	str := func(a *php.Array, k string) string { v, _ := a.Get(k); return php.ToString(v) }
	sub := func(a *php.Array, k string) *php.Array { v, _ := a.GetArray(k); return v }
	val := func(a *php.Array, k string) any { v, _ := a.Get(k); return v }

	files := sub(s, "files")
	if foldCollision(files) && caseInsensitive(t, root) {
		// Two fixture paths differ only in case (s234: Legacy/ and legacy/),
		// so they share one directory here and Composer would scan both
		// there too: the golden holds a case-sensitive file system's result.
		t.Skip("fixture paths collide on this case-insensitive file system")
	}
	for path, content := range files.All() {
		writeFile(t, root+"/"+path.String(), php.ToString(content))
	}
	for link, target := range sub(s, "symlinks").All() {
		mkdirAll(t, filepath.Dir(root+"/"+link.String()))
		if err := os.Symlink(root+"/"+php.ToString(target), root+"/"+link.String()); err != nil {
			t.Fatal(err)
		}
	}
	workDir := root + "/" + str(s, "workDir")
	mkdirAll(t, workDir)
	vendorDir := root + "/" + str(s, "vendorDir")
	if dev := val(s, "installedDev"); dev != nil {
		encoded, _ := php.JSONEncode(php.ArrayOf("packages", php.NewArray(), "dev", dev), 0)
		writeFile(t, vendorDir+"/composer/installed.json", encoded)
	}
	if existing := val(s, "existingAutoload"); existing != nil {
		writeFile(t, vendorDir+"/autoload.php", php.ToString(existing))
	}

	l := loader.NewArrayLoader(pkg.NewVersionParser(), false)
	rootPackage, err := l.Load(sub(s, "root"), pkg.ClassRootPackage)
	if err != nil {
		t.Fatal(err)
	}
	repo := &oracleRepo{}
	for _, c := range sub(s, "packages").Values() {
		p, err := l.Load(c.(*php.Array), pkg.ClassCompletePackage)
		if err != nil {
			t.Fatal(err)
		}
		repo.packages = append(repo.packages, p)
		if alias := val(c.(*php.Array), "alias"); alias != nil {
			repo.packages = append(repo.packages, pkg.NewAliasPackage(p, "9999999-dev", php.ToString(alias)))
		}
	}
	for _, n := range sub(s, "devPackageNames").Values() {
		repo.devNames = append(repo.devNames, php.ToString(n))
	}

	bio := newBufferIO(t)
	g := NewGenerator(&testDispatcher{}, bio)
	if dev := val(s, "devMode"); dev != nil {
		g.SetDevMode(dev.(bool))
	}
	g.SetClassMapAuthoritative(val(s, "authoritative").(bool))
	prefix := str(s, "apcuPrefix")
	g.SetApcu(val(s, "apcu").(bool), &prefix)
	switch ignore := val(s, "ignore").(type) {
	case bool:
		if ignore {
			g.SetPlatformRequirementFilter(ignoreAllFilter{})
		}
	case *php.Array:
		var reqs []string
		for _, r := range ignore.Values() {
			reqs = append(reqs, php.ToString(r))
		}
		g.SetPlatformRequirementFilter(newIgnoreListFilter(reqs))
	}
	config := testConfig{"vendor-dir": vendorDir}
	for k, v := range sub(s, "config").All() {
		config[k.String()] = v
	}

	t.Chdir(workDir)
	targetDir := str(s, "targetDir")
	classMap, err := g.Dump(config, repo, rootPackage.(pkg.RootPackageInterface), testIM{vendorDir: func() string { return vendorDir }},
		targetDir, val(s, "scanPsr").(bool), str(s, "suffix"), oracleLocker{str(s, "lockHash")}, val(s, "strictAmbiguous").(bool))

	result := sub(s, "result")
	// The golden ran on Linux. On Windows the paths join with backslashes
	// where Composer's code uses DIRECTORY_SEPARATOR or realpath(), so both
	// sides are compared with every backslash as a slash there.
	golden := func(v string) string {
		if runtime.GOOS == "windows" {
			return strings.ReplaceAll(v, `\`, "/")
		}

		return v
	}
	replace := func(v string) string {
		return golden(strings.ReplaceAll(strings.ReplaceAll(v, root, "%ROOT%"), filepath.ToSlash(root), "%ROOT%"))
	}

	if wantErr := sub(result, "error"); wantErr != nil {
		if err == nil || replace(err.Error()) != golden(str(wantErr, "message")) {
			t.Errorf("error %v, want %s: %s", err, str(wantErr, "class"), str(wantErr, "message"))
		}
	} else if err != nil {
		t.Fatalf("unexpected error %v", err)
	} else {
		var want []string
		for _, c := range sub(result, "classes").Values() {
			want = append(want, php.ToString(c))
		}
		if got := classMap.Classes(); !slices.Equal(got, want) {
			t.Errorf("classes %q, want %q", got, want)
		}
	}

	gotOutput := replace(php.NormalizeEOL(bio.Output()))
	swaps := walkOrderSwaps(golden(str(result, "output")), gotOutput, walkedDirs(s, workDir, vendorDir, replace))
	if got, want := sortedLines(gotOutput), sortedLines(swaps.output(golden(str(result, "output")))); !slices.Equal(got, want) {
		t.Errorf("output:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	wantFiles := sub(result, "files")
	realVendor, _ := filepath.EvalSymlinks(vendorDir)
	for _, name := range append([]string{"autoload.php"}, oracleFiles(targetDir)...) {
		path := vendorDir + "/" + name
		if name != "autoload.php" {
			path = realVendor + "/" + name
		}
		got, readErr := os.ReadFile(path)
		want, ok := wantFiles.GetString(name)
		want = swaps.file(golden(want))
		switch {
		case !ok && readErr == nil:
			t.Errorf("%s written, PHP did not", name)
		case ok && readErr != nil:
			t.Errorf("%s not written: %v", name, readErr)
		case ok && replace(string(got)) != want:
			t.Errorf("%s:\n%s\nwant:\n%s", name, replace(string(got)), want)
		}
	}
}

// walkSwap is a class found twice in one directory walk: the golden kept
// first and dropped second, this run the other way round.
type walkSwap struct{ class, first, second string }

type walkSwaps []walkSwap

// walkOrderSwaps finds the ambiguous classes whose two paths lie under one
// scanned directory and that this run reported in the opposite order to the
// golden. Composer's class-map-generator walks a directory with Symfony
// Finder and does not sort it: like the port's os.File.ReadDir walk, it keeps
// the order the file system returns entries in. Which of two files found in
// one walk wins (s367: legacy/f132.php and legacy/a/f20.php) therefore
// depends on the file system, in PHP as in Go (btrfs and tmpfs differ),
// and either order is Composer's behaviour. Ambiguities across separate
// scans are ordered by the generator and still have to match exactly.
func walkOrderSwaps(want, got string, dirs []string) walkSwaps {
	const prefix = `<warning>Warning: Ambiguous class resolution, "`
	var swaps walkSwaps
	for line := range strings.SplitSeq(want, "\n") {
		rest, ok := strings.CutPrefix(line, prefix)
		if !ok {
			continue
		}
		class, rest, ok := strings.Cut(rest, `" was found in both "`)
		if !ok {
			continue
		}
		first, rest, _ := strings.Cut(rest, `" and "`)
		second, _, _ := strings.Cut(rest, `", the first will be used.`)
		swapped := strings.Replace(line, `"`+first+`" and "`+second+`"`, `"`+second+`" and "`+first+`"`, 1)
		if strings.Contains(got, line) || !strings.Contains(got, swapped) {
			continue
		}
		for _, d := range dirs {
			if strings.HasPrefix(first, d+"/") && strings.HasPrefix(second, d+"/") {
				swaps = append(swaps, walkSwap{class, first, second})

				break
			}
		}
	}

	return swaps
}

// output swaps the two paths of each such warning in the golden output.
func (w walkSwaps) output(s string) string {
	for _, sw := range w {
		s = strings.Replace(s, `"`+sw.first+`" and "`+sw.second+`"`, `"`+sw.second+`" and "`+sw.first+`"`, 1)
	}

	return s
}

// file points each swapped class at the other file in a golden autoload
// file. The paths there are split around $baseDir or __DIR__, so only the
// part after the directory the two paths share is replaced.
func (w walkSwaps) file(s string) string {
	for _, sw := range w {
		common := 0
		for i := range min(len(sw.first), len(sw.second)) {
			if sw.first[i] != sw.second[i] {
				break
			}
			if sw.first[i] == '/' {
				common = i
			}
		}
		key := "'" + strings.ReplaceAll(sw.class, `\`, `\\`) + "' => "
		oldEnd, newEnd := sw.first[common:]+"',", sw.second[common:]+"',"
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), key) && strings.HasSuffix(l, oldEnd) {
				lines[i] = strings.TrimSuffix(l, oldEnd) + newEnd
			}
		}
		s = strings.Join(lines, "\n")
	}

	return s
}

// walkedDirs lists the directories the scenario's autoload rules have
// Composer walk, spelt as in the golden.
func walkedDirs(s *php.Array, workDir, vendorDir string, replace func(string) string) []string {
	var dirs []string
	add := func(base string, p *php.Array) {
		autoload, ok := p.GetArray("autoload")
		if !ok {
			return
		}
		for _, typ := range []string{"classmap", "psr-0", "psr-4"} {
			rules, ok := autoload.GetArray(typ)
			if !ok {
				continue
			}
			for _, v := range rules.Values() {
				paths, ok := v.(*php.Array)
				if !ok {
					paths = php.ListOf(v)
				}
				for _, path := range paths.Values() {
					dirs = append(dirs, replace(strings.TrimSuffix(base+"/"+php.ToString(path), "/")))
				}
			}
		}
	}
	if root, ok := s.GetArray("root"); ok {
		add(workDir, root)
	}
	if packages, ok := s.GetArray("packages"); ok {
		for _, c := range packages.Values() {
			p := c.(*php.Array)
			name, _ := p.GetString("name")
			base := vendorDir + "/" + name
			if td, _ := p.GetString("target-dir"); td != "" {
				base += "/" + td
			}
			add(base, p)
		}
	}

	return dirs
}

// foldCollision reports whether two of the paths (keys) of files differ only
// in case in some directory or file name.
func foldCollision(files *php.Array) bool {
	seen := map[string]string{}
	for path := range files.All() {
		p := path.String()
		for {
			folded := strings.ToLower(p)
			if prev, ok := seen[folded]; ok && prev != p {
				return true
			}
			seen[folded] = p
			i := strings.LastIndexByte(p, '/')
			if i < 0 {
				break
			}
			p = p[:i]
		}
	}

	return false
}

// caseInsensitive reports whether the file system holding dir ignores case
// in names (the default on macOS and Windows).
func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "case-probe")
	writeFile(t, probe, "")
	defer func() { _ = os.Remove(probe) }()
	_, err := os.Stat(filepath.Join(dir, "CASE-PROBE"))

	return err == nil
}

func oracleFiles(targetDir string) []string {
	names := []string{"autoload_real.php", "autoload_static.php", "autoload_namespaces.php", "autoload_psr4.php", "autoload_classmap.php", "autoload_files.php", "include_paths.php", "platform_check.php"}
	for i, n := range names {
		names[i] = targetDir + "/" + n
	}

	return names
}

func sortedLines(s string) []string {
	lines := strings.Split(s, "\n")
	slices.Sort(lines)

	return lines
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}

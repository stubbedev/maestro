//go:build linux

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/switches"
)

// TestInstallFloor holds the work an install does when there is little or
// nothing to do: an install with everything installed writes nothing in
// the project (not even a temporary file it removes again), and an
// install that removes packages starts no process to remove them. Writes
// and processes are taken from strace where it is installed; without it
// the project tree is compared before and after.
func TestInstallFloor(t *testing.T) {
	if !switches.On(switches.E2E) {
		t.Skip("set MAESTRO_E2E=1 to run maestro installs (php for the platform probe)")
	}

	root := t.TempDir()
	bin := filepath.Join(root, "maestro")

	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("building maestro: %v\n%s", err, out)
	}

	for _, name := range []string{"art", "dev"} {
		zipFile(t, filepath.Join(root, "artifacts", "acme-"+name+"-1.0.0.zip"), map[string]string{
			name + "/":                "",
			name + "/composer.json":   libComposerJSON("acme/"+name, `, "version": "1.0.0"`),
			name + "/src/":            "",
			name + "/src/Art.php":     "<?php\n\nnamespace Acme\\Lib;\n\nfinal class Art\n{\n}\n",
			name + "/src/sub/":        "",
			name + "/src/sub/Sub.php": "<?php\n\nnamespace Acme\\Lib\\sub;\n\nfinal class Sub\n{\n}\n",
		})
	}

	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(project, "composer.json"), []byte(`{
    "name": "maestro/floor",
    "repositories": [
        {"type": "artifact", "url": "../artifacts"},
        {"packagist.org": false}
    ],
    "require": {"acme/art": "^1.0"},
    "require-dev": {"acme/dev": "^1.0"},
    "config": {"optimize-autoloader": true}
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"COMPOSER_HOME="+filepath.Join(root, "home"),
		"COMPOSER_CACHE_DIR="+filepath.Join(root, "cache"),
		"MAESTRO_CACHE_DIR="+filepath.Join(root, "mcache"),
		"COMPOSER_DISABLE_NETWORK=1",
	)

	run := func(wrap []string, args ...string) {
		t.Helper()

		cmd := exec.Command(wrap[0], append(append(wrap[1:], bin), args...)...)
		cmd.Dir, cmd.Env = project, env

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	direct := []string{"env"}

	run(direct, "install")

	strace, _ := exec.LookPath("strace")

	// no-op install
	before := floorSnapshot(t, project)

	if strace == "" {
		run(direct, "install")
	} else {
		trace := filepath.Join(root, "noop.strace")
		run([]string{strace, "-f", "-qq", "-s", "4096", "-o", trace, "-e", "trace=%file,%desc"}, "install")

		for _, w := range projectWrites(t, trace, project) {
			t.Errorf("no-op install writes: %s", w)
		}
	}

	if after := floorSnapshot(t, project); after != before {
		t.Errorf("no-op install changed the project:\n%s", diffLines(before, after))
	}

	// switching to --no-dev removes acme/dev without a process
	if strace == "" {
		run(direct, "install", "--no-dev")
	} else {
		trace := filepath.Join(root, "nodev.strace")
		run([]string{strace, "-f", "-qq", "-o", trace, "-e", "trace=execve"}, "install", "--no-dev")

		if spawned := removals(t, trace); len(spawned) > 0 {
			t.Errorf("install --no-dev started processes to remove packages: %q", spawned)
		}
	}

	if _, err := os.Lstat(filepath.Join(project, "vendor", "acme", "dev")); !os.IsNotExist(err) {
		t.Errorf("vendor/acme/dev not removed: %v", err)
	}
}

// floorSnapshot lists every entry of dir with its inode, mode, size and change
// and modification times: any write, chmod, rename or replacement shows.
func floorSnapshot(t *testing.T, dir string) string {
	t.Helper()

	var b strings.Builder

	err := filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		st, _ := fi.Sys().(*syscall.Stat_t)
		if st == nil {
			t.Fatalf("no stat data for %s", path)
		}

		b.WriteString(path)
		b.WriteString(" " + fi.Mode().String() + " " + time.Unix(st.Ctim.Unix()).String() + " " + fi.ModTime().String())
		b.WriteString(" " + strconv.FormatUint(st.Ino, 10) + " " + strconv.FormatInt(fi.Size(), 10) + "\n")

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return b.String()
}

// writeCall matches a traced call that changes the file system, and the
// path it names.
var writeCall = regexp.MustCompile(`^\d+\s+(?:(?:openat|open|creat)\((?:AT_FDCWD, )?"([^"]*)", [^)]*O_(?:WRONLY|RDWR|CREAT|TRUNC)|(?:renameat2?|rename|unlinkat|unlink|mkdirat|mkdir|rmdir|symlinkat|linkat|utimensat|fchmodat|chmod)\((?:AT_FDCWD, |\d+, )?"([^"]*)")`)

// projectWrites are the calls in an strace -f log that change something
// under project.
func projectWrites(t *testing.T, trace, project string) []string {
	t.Helper()

	f, err := os.Open(trace)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var writes []string

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24)

	for sc.Scan() {
		m := writeCall.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}

		path := m[1] + m[2]
		if !filepath.IsAbs(path) {
			path = filepath.Join(project, path)
		}

		if path == project || strings.HasPrefix(path, project+"/") {
			writes = append(writes, sc.Text())
		}
	}

	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	return writes
}

// removals are the rm processes (run directly or through sh) an strace -f
// -e trace=execve log shows started.
func removals(t *testing.T, trace string) []string {
	t.Helper()

	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}

	var progs []string

	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.Contains(line, "execve(") || !strings.Contains(line, `"rm"`) && !strings.Contains(line, "rm' '-rf") {
			continue
		}

		progs = append(progs, line)
	}

	return progs
}

// diffLines is the lines of b not in a, and of a not in b.
func diffLines(a, b string) string {
	in := func(s string) map[string]bool {
		m := map[string]bool{}
		for l := range strings.SplitSeq(s, "\n") {
			m[l] = true
		}

		return m
	}

	ma, mb := in(a), in(b)

	var out strings.Builder

	for l := range strings.SplitSeq(a, "\n") {
		if !mb[l] {
			out.WriteString("- " + l + "\n")
		}
	}

	for l := range strings.SplitSeq(b, "\n") {
		if !ma[l] {
			out.WriteString("+ " + l + "\n")
		}
	}

	return out.String()
}

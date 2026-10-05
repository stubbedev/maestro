//go:build unix

package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// entry is what a step must reproduce for one path.
type entry struct {
	kind string // "dir", "file", "link", "git"
	mode fs.FileMode
	sum  [32]byte
	link string
	// text is the content of small files outside packages' directories
	// (and a .git directory's summary), kept to show diffs.
	text    string
	hasText bool
}

func (e entry) String() string {
	switch e.kind {
	case "link":
		return "symlink -> " + e.link
	case "dir":
		return fmt.Sprintf("dir %v", e.mode)
	case "git":
		return "git working copy " + e.text
	}

	return fmt.Sprintf("file %v sha256 %x", e.mode, e.sum[:6])
}

func (e entry) equal(o entry) bool {
	return e.kind == o.kind && e.mode == o.mode && e.sum == o.sum && e.link == o.link && (e.kind != "git" || e.text == o.text)
}

// skipped are the scenario root's directories that are not compared: the
// two tools' caches differ by design (maestro keeps its store there).
var skipped = map[string]bool{"cache": true, "mcache": true}

// snapshot records every path under root.
func snapshot(t *testing.T, root string) map[string]entry {
	t.Helper()

	tree := map[string]entry{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		if rel == "." {
			return nil
		}

		if skipped[rel] {
			return filepath.SkipDir
		}

		info, err := os.Lstat(path)
		if err != nil {
			return err
		}

		e := entry{mode: info.Mode()}

		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			e.kind = "link"
			e.mode = 0

			if e.link, err = os.Readlink(path); err != nil {
				return err
			}
		case info.IsDir() && d.Name() == ".git":
			e.kind = "git"
			e.mode = 0
			e.text = gitSummary(filepath.Dir(path))
			e.hasText = true
			tree[rel] = e

			return filepath.SkipDir
		case info.IsDir():
			e.kind = "dir"
		default:
			e.kind = "file"

			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			data = normalizeFile(rel, data)
			e.sum = sha256.Sum256(data)

			if len(data) < 256<<10 && !insidePackage(rel) {
				e.text = string(data)
				e.hasText = true
			}
		}

		tree[rel] = e

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return tree
}

// insidePackage tells files extracted from packages (compared by checksum
// only) from the files the tools write themselves.
func insidePackage(rel string) bool {
	_, inVendor, ok := strings.Cut(rel, "vendor/")
	if !ok {
		return false
	}

	return !strings.HasPrefix(inVendor, "composer/") && !strings.HasPrefix(inVendor, "bin/") && strings.Contains(inVendor, "/")
}

// gitSummary describes a git working copy by what Composer controls: the
// checked-out commit, the branch and the remotes.
func gitSummary(dir string) string {
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")

		out, err := cmd.Output()
		if err != nil {
			return "(error)"
		}

		return strings.TrimSpace(string(out))
	}

	return fmt.Sprintf("HEAD=%s branch=%s remotes=[%s] status=[%s]",
		git("rev-parse", "HEAD"),
		git("rev-parse", "--abbrev-ref", "HEAD"),
		strings.ReplaceAll(git("remote", "-v"), "\n", "; "),
		strings.ReplaceAll(git("status", "--porcelain"), "\n", "; "),
	)
}

// compareTrees lists the differences between Composer's and maestro's
// trees.
func compareTrees(want, got map[string]entry) string {
	var paths []string

	for p := range want {
		paths = append(paths, p)
	}

	for p := range got {
		if _, ok := want[p]; !ok {
			paths = append(paths, p)
		}
	}

	slices.Sort(paths)

	var (
		b     strings.Builder
		shown int
	)

	const maxShown = 30

	for _, p := range paths {
		w, inW := want[p]
		g, inG := got[p]

		var msg string

		switch {
		case !inG:
			msg = fmt.Sprintf("missing %s (%v)", p, w)
		case !inW:
			msg = fmt.Sprintf("extra %s (%v)", p, g)
		case !w.equal(g):
			msg = fmt.Sprintf("%s\n  Composer: %v\n  maestro:  %v", p, w, g)
			if w.kind == "file" && g.kind == "file" && w.hasText && g.hasText && w.sum != g.sum {
				msg += "\n" + indent(textDiff(w.text, g.text))
			}
		default:
			continue
		}

		if shown++; shown <= maxShown {
			b.WriteString(msg + "\n")
		}
	}

	if shown > maxShown {
		fmt.Fprintf(&b, "... and %d more\n", shown-maxShown)
	}

	return b.String()
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n    ")
}

// apcuPrefix is the APCu prefix AutoloadGenerator picks when none is
// given: bin2hex(random_bytes(10)).
var apcuPrefix = regexp.MustCompile(`setApcuPrefix\('[0-9a-f]{20}'\)`)

// normalizeFile applies the file normalisations: the random APCu prefix
// of vendor/composer/autoload_real.php (dump-autoload --apcu without
// --apcu-prefix), and archives the archive command wrote under
// archives/, which are compared by their entries (names, modes and
// contents), not their bytes: zip and tar headers hold the time of
// writing.
func normalizeFile(rel string, data []byte) []byte {
	if strings.HasSuffix(rel, "vendor/composer/autoload_real.php") {
		data = apcuPrefix.ReplaceAll(data, []byte("setApcuPrefix('<random>')"))
	}

	if strings.HasPrefix(rel, "archives/") {
		return []byte(archiveListing(rel, data))
	}

	return data
}

// archiveListing lists a zip or tar archive's entries.
func archiveListing(name string, data []byte) string {
	var lines []string

	switch {
	case strings.HasSuffix(name, ".zip"):
		r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "unreadable zip: " + err.Error()
		}

		for _, f := range r.File {
			rc, err := f.Open()
			if err != nil {
				return "unreadable zip entry: " + err.Error()
			}

			content, _ := io.ReadAll(rc)
			rc.Close()

			lines = append(lines, fmt.Sprintf("%s %v %x", f.Name, f.Mode(), sha256.Sum256(content)))
		}
	case strings.HasSuffix(name, ".tar"):
		r := tar.NewReader(bytes.NewReader(data))

		for {
			h, err := r.Next()
			if err != nil {
				break
			}

			content, _ := io.ReadAll(r)
			lines = append(lines, fmt.Sprintf("%s %v %x", h.Name, h.FileInfo().Mode(), sha256.Sum256(content)))
		}
	default:
		return string(data)
	}

	return strings.Join(lines, "\n") + "\n"
}

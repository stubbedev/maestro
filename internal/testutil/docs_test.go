package testutil

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docHistory finds what makes documentation a log of its history rather
// than a description of the project: an issue or pull request number
// (#12; an upstream reference in owner/repo#12 form is fine), a commit
// hash, or a date.
var docHistory = regexp.MustCompile(`(?:^|[^\w/&-])(#[0-9]+)\b|\b([0-9a-f]{7,40})\b|\b(20[0-9]{2}-[01][0-9]-[0-3][0-9])\b`)

// TestDocsDescribeTheProject: README.md and docs/ describe what maestro
// does and how, as facts; history lives in git and the issue tracker.
func TestDocsDescribeTheProject(t *testing.T) {
	root := moduleRoot(t)
	docs := []string{filepath.Join(root, "README.md")}
	more, _ := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	docs = append(docs, more...)
	more, _ = filepath.Glob(filepath.Join(root, "docs", "*", "*.md"))
	docs = append(docs, more...)

	for _, doc := range docs {
		data, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, doc)
		for i, line := range strings.Split(string(data), "\n") {
			for _, m := range docHistory.FindAllStringSubmatch(line, -1) {
				found := m[1] + m[2] + m[3]
				// a hexadecimal word without a digit is a word ("defaced")
				if m[2] != "" && !strings.ContainsAny(m[2], "0123456789") {
					continue
				}
				// nor is a number a hash
				if m[2] != "" && !strings.ContainsAny(m[2], "abcdef") {
					continue
				}
				// a quoted date is a value (Composer::RELEASE_DATE)
				if m[3] != "" && (strings.Contains(line, "'"+m[3]) || strings.Contains(line, `"`+m[3])) {
					continue
				}
				t.Errorf("%s:%d: %q: state what is, not when or in which change it came", filepath.ToSlash(rel), i+1, found)
			}
		}
	}
}

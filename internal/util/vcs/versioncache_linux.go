// Ports nothing: see versioncache.go.

package vcs

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// gitBinaryKey names the kept version of the git binary found: the path
// found, the file it resolves to and that file's identity. "" when its
// version is not kept: nothing found, a file not named git (a multi-call
// binary, such as /usr/bin/snap behind /snap/bin/git, picks what it runs
// by its name), a script, or a file changed within margin of now.
func gitBinaryKey(found string, now time.Time, margin fsstate.Margin) string {
	if found == "" {
		return ""
	}
	resolved, err := php.EvalSymlinks(found)
	if err != nil || filepath.Base(resolved) != "git" {
		return ""
	}
	f, err := os.Open(resolved)
	if err != nil {
		return ""
	}
	defer f.Close()
	if script, err := fsstate.IsScript(f); err != nil || script {
		return ""
	}
	id, ok := fsstate.Fstat(f)
	if !ok || !id.IsRegular() || !id.Trusted(now, margin) {
		return ""
	}

	k := fsstate.NewKeyHash()
	for _, s := range [...]string{versionFormat.Header(), found, resolved} {
		k.String(s)
	}
	k.ID(id)

	return hex.EncodeToString(k.Sum(nil)[:16])
}

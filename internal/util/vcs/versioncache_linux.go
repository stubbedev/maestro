// Ports nothing: see versioncache.go.

package vcs

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// gitBinaryKey names the kept version of the git binary found (see
// keyGitBinary), "" when its version is not kept.
func gitBinaryKey(found string, now time.Time, margin fsstate.Margin) string {
	k := fsstate.NewKeyHash()
	k.String(versionFormat.Header())
	if !keyGitBinary(k, found, now, margin) {
		return ""
	}

	return hex.EncodeToString(k.Sum(nil)[:16])
}

// KeyGitBinary adds the git binary a process would start to k, for a
// cache of what that git answers: the path found, the file it resolves to
// and that file's identity. It reports false, adding nothing, when what
// that git answers is not to be kept (see keyGitBinary).
func KeyGitBinary(k *fsstate.KeyHash, now time.Time, margin fsstate.Margin) bool {
	return keyGitBinary(k, util.DirectToolPath("git"), now, margin)
}

// keyGitBinary is KeyGitBinary for the git binary found. It reports false
// for nothing found, a file not named git (a multi-call binary, such as
// /usr/bin/snap behind /snap/bin/git, picks what it runs by its name), a
// script, or a file changed within margin of now.
func keyGitBinary(k *fsstate.KeyHash, found string, now time.Time, margin fsstate.Margin) bool {
	if found == "" {
		return false
	}
	resolved, err := php.EvalSymlinks(found)
	if err != nil || filepath.Base(resolved) != "git" {
		return false
	}
	f, err := os.Open(resolved)
	if err != nil {
		return false
	}
	defer f.Close()
	if script, err := fsstate.IsScript(f); err != nil || script {
		return false
	}
	id, ok := fsstate.Fstat(f)
	if !ok || !id.IsRegular() || !id.Trusted(now, margin) {
		return false
	}

	k.String(found)
	k.String(resolved)
	k.ID(id)

	return true
}

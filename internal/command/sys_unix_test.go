//go:build unix

package command

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

// get_current_user() names the owner of the running script: for maestro,
// its executable (here the test binary, which the user running the test
// built).
func TestCurrentUser(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skip(err)
	}

	if got := currentUser(); got != u.Username {
		t.Errorf("currentUser() = %q, want %q", got, u.Username)
	}

	file := filepath.Join(t.TempDir(), "owned")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if got := fileOwnerName(file); got != u.Username {
		t.Errorf("fileOwnerName = %q, want %q", got, u.Username)
	}

	if got := fileOwnerName(file + ".missing"); got != "" {
		t.Errorf("fileOwnerName(missing) = %q, want \"\"", got)
	}
}

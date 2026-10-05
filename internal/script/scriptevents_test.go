package script

import "testing"

func TestHasConstant(t *testing.T) {
	for _, name := range []string{"POST_INSTALL_CMD", "PRE_ARCHIVE_CMD", "POST_CREATE_PROJECT_CMD"} {
		if !HasConstant(name) {
			t.Errorf("HasConstant(%q) = false", name)
		}
	}
	for _, name := range []string{"post_install_cmd", "INIT", "PRE_PACKAGE_INSTALL", ""} {
		if HasConstant(name) {
			t.Errorf("HasConstant(%q) = true", name)
		}
	}
}

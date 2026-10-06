//go:build !linux

package vcs

// statEntry leaves the index to git where the stat data git records is
// not known to match lstat's exactly.
func statEntry(string, []byte) error {
	return errIndexFallback
}

func untrackedIdent(string) (string, error) {
	return "", errIndexFallback
}

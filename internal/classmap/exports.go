// Exported faces of the glob() port for the packages above classmap
// (internal/repository's PathRepository).

package classmap

import "slices"

// GlobOnlyDir ports PHP's glob($pattern, GLOB_MARK | GLOB_ONLYDIR |
// GLOB_BRACE) with the marking slashes removed (Composer rtrims them): the
// directories pattern matches, each brace alternative's matches sorted
// byte-wise as glibc sorts them (strcoll in the C locale) and the
// alternatives kept in order.
func GlobOnlyDir(pattern string) []string {
	var dirs []string
	for _, alt := range braceExpand(pattern) {
		start := len(dirs)
		for _, m := range globPattern(nil, alt) {
			// PHP checks GLOB_ONLYDIR itself, as glibc only takes it as a hint.
			if isDirectory(m) {
				dirs = append(dirs, m)
			}
		}
		slices.Sort(dirs[start:])
	}

	return dirs
}

//go:build !linux

package cache

import "io/fs"

// fileIdentity is empty where readFile remembers nothing: other systems'
// file times and ids are not checked here.
type fileIdentity struct{}

func sysIdentity(fs.FileInfo) (fileIdentity, bool) { return fileIdentity{}, false }

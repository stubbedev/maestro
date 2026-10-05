package store

import "io/fs"

// Umask is the process umask, read without changing it where the platform
// allows (umask() as PHP returns it).
func Umask() fs.FileMode { return processUmask() }

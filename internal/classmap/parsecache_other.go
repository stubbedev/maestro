//go:build !(linux || darwin)

package classmap

import "os"

// statKey: no file identities here, so nothing is cached.
func statKey(string) (fileKey, bool) { return fileKey{}, false }

func fstatKey(*os.File) (fileKey, bool) { return fileKey{}, false }

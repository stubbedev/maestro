package command

import "io/fs"

// fileOwner reports no owner: Windows files have no uid
// (warnIfUntrustedPath checks nothing there).
func fileOwner(fs.FileInfo) (int, bool) { return 0, false }

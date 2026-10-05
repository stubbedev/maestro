//go:build unix && !linux && !darwin

package store

import "io/fs"

func processUmask() fs.FileMode {
	return umaskBySetting()
}

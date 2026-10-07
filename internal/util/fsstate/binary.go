package fsstate

import (
	"os"
	"strconv"
	"sync"
)

// BinaryID identifies the running maestro binary (its size and
// modification time), for the headers of what it caches: " <size>
// <mtime>", or "" when unknown.
var BinaryID = sync.OnceValue(func() string {
	if exe, err := os.Executable(); err == nil {
		if info, err := os.Stat(exe); err == nil {
			return " " + strconv.FormatInt(info.Size(), 10) + " " + strconv.FormatInt(info.ModTime().UnixNano(), 10)
		}
	}

	return ""
})

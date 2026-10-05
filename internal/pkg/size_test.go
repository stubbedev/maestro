package pkg

import (
	"testing"
	"unsafe"
)

// TestSizes keeps the hot structs within their allocation size classes.
func TestSizes(t *testing.T) {
	if s := unsafe.Sizeof(Link{}); s > 80 {
		t.Errorf("Link is %d bytes, want at most 80", s)
	}
}

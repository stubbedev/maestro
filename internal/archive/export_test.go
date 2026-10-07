package archive

// SetZipInMemory sets the largest zip archive read into memory, returning
// a function that restores it.
func SetZipInMemory(n int64) func() {
	old := zipInMemory
	zipInMemory = n

	return func() { zipInMemory = old }
}

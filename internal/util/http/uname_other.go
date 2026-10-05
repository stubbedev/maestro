//go:build !unix && !windows

package http

// systemUname reports Unknown where php_uname has nothing to read.
func systemUname() (sysname, release string) {
	return "Unknown", "Unknown"
}

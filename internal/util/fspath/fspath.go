// Ports Composer\Util\Filesystem's path functions that touch no file
// (normalizePath, isAbsolutePath), in a leaf package both internal/util
// and the packages below it (internal/classmap) use.

package fspath

import "strings"

// NormalizePath ports Filesystem::normalizePath: backslashes become slashes,
// redundant separators and up-level references collapse, the trailing slash
// goes, and a drive letter is uppercased. An already normalized path is
// returned as is, without allocating.
func NormalizePath(path string) string {
	orig := path
	if strings.IndexByte(path, '\\') >= 0 {
		path = strings.ReplaceAll(path, `\`, "/")
	}

	absolute := ""

	// Extract Windows UNC paths e.g. \\foo\bar
	if len(path) > 2 && strings.HasPrefix(path, "//") {
		absolute = "//"
		path = path[2:]
	}

	// Extract a prefix being a protocol://, protocol:, protocol://drive: or
	// simply drive:
	prefix := normalizePrefix(path)
	path = path[len(prefix):]

	if strings.HasPrefix(path, "/") {
		absolute = "/"
		path = path[1:]
	}

	// The result is built in buf; segments holds where each kept part
	// starts (at its separator), so ".." can drop the last one.
	var (
		bufArray      [256]byte
		segmentsArray [32]int
	)

	buf := append(bufArray[:0], prefix...)

	// Ensure c: is normalized to C:, as {(^|://)[a-z]:$}i matches.
	if n := len(prefix); n >= 2 && prefix[n-1] == ':' && isASCIIAlpha(prefix[n-2]) && (n == 2 || strings.HasSuffix(prefix[:n-2], "://")) {
		buf[n-2] &^= 0x20
	}

	buf = append(buf, absolute...)
	base := len(buf)
	segments := segmentsArray[:0]
	up := false

	for path != "" {
		var chunk string

		chunk, path, _ = strings.Cut(path, "/")

		switch {
		case chunk == ".." && (absolute != "" || up):
			if n := len(segments); n > 0 {
				buf = buf[:segments[n-1]]
				segments = segments[:n-1]
			}

			up = len(segments) > 0 && !lastSegmentIsUp(buf, segments)
		case chunk != "." && chunk != "":
			segments = append(segments, len(buf))
			if len(buf) > base {
				buf = append(buf, '/')
			}

			buf = append(buf, chunk...)
			up = chunk != ".."
		}
	}

	if string(buf) == orig {
		return orig
	}

	return string(buf)
}

// lastSegmentIsUp reports whether the last part written to buf is "..".
func lastSegmentIsUp(buf []byte, segments []int) bool {
	last := buf[segments[len(segments)-1]:]
	if len(last) > 0 && last[0] == '/' {
		last = last[1:]
	}

	return string(last) == ".."
}

// normalizePrefix matches {^( [0-9a-z]{2,}+: (?: // (?: [a-z]: )? )? | [a-z]: )}ix.
func normalizePrefix(path string) string {
	n := 0
	for n < len(path) && isASCIIAlnum(path[n]) {
		n++
	}

	if n >= 2 && n < len(path) && path[n] == ':' {
		end := n + 1
		if strings.HasPrefix(path[end:], "//") {
			end += 2
			if end+1 < len(path) && isASCIIAlpha(path[end]) && path[end+1] == ':' {
				end += 2
			}
		}

		return path[:end]
	}

	if len(path) >= 2 && isASCIIAlpha(path[0]) && path[1] == ':' {
		return path[:2]
	}

	return ""
}

// IsAbsolutePath ports Filesystem::isAbsolutePath.
func IsAbsolutePath(path string) bool {
	return strings.HasPrefix(path, "/") || (len(path) > 1 && path[1] == ':') || strings.HasPrefix(path, `\\`)
}

func isASCIIAlpha(c byte) bool { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }

func isASCIIAlnum(c byte) bool { return c >= '0' && c <= '9' || isASCIIAlpha(c) }

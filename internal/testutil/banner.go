package testutil

import "regexp"

// banner is the logo and version line heading the output of `list` and a
// bare run: lines of art without letters or digits (Composer's ASCII art,
// maestro's block letters or ASCII art; with --ansi, styled), then
// "Composer version ..." or "Maestro version ..." ("Maestro (...)" for a
// build without a version). Composer's and maestro's differ
// (docs/PORTING.md deviation 8); tools/oracle/errors/errors.sh matches
// the same lines.
var banner = regexp.MustCompile("\\A(?:(?:\x1b\\[[0-9;]*m|[^A-Za-z0-9\n\x1b])+\r?\n)+(?:\x1b\\[[0-9;]*m)*(?:Composer|Maestro)[^\n]*\n")

// BannerPlaceholder is what NormalizeBanner leaves of a banner.
const BannerPlaceholder = "<banner>\n"

// NormalizeBanner replaces the banner starting s, Composer's or maestro's,
// with BannerPlaceholder, so outputs that show it compare the rest
// (tools/oracle/errors/errors.sh does the same to its goldens).
func NormalizeBanner(s string) string {
	return banner.ReplaceAllLiteralString(s, BannerPlaceholder)
}

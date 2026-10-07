package testutil

import "regexp"

// banner is the logo and version line heading the output of `list` and a
// bare run: lines of ASCII art, then "Composer version ..." or
// "maestro version ..." ("maestro (...)" for a build without a version;
// with --ansi, styled). Composer's and maestro's differ (docs/PORTING.md
// deviation 8).
var banner = regexp.MustCompile("\\A(?:[ _/\\\\|().,'`-]+\r?\n)+(?:\x1b\\[[0-9;]*m)*(?:Composer|maestro)[^\n]*\n")

// BannerPlaceholder is what NormalizeBanner leaves of a banner.
const BannerPlaceholder = "<banner>\n"

// NormalizeBanner replaces the banner starting s, Composer's or maestro's,
// with BannerPlaceholder, so outputs that show it compare the rest
// (tools/oracle/errors/errors.sh does the same to its goldens).
func NormalizeBanner(s string) string {
	return banner.ReplaceAllLiteralString(s, BannerPlaceholder)
}

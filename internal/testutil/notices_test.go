package testutil

import (
	"slices"
	"testing"
)

func TestComposerNoticesAndRemoveNotices(t *testing.T) {
	composer := "> Project\\Scripts::deprecation\r\n" +
		"Deprecation Notice: an old API in /p/src/Scripts.php:66\r\n" +
		"Stack trace:\r\n" +
		" /p/src/Scripts.php:66\r\n" +
		" phar:///c/composer.phar/src/Composer/EventDispatcher/EventDispatcher.php:232\r\n" +
		"Deprecation Notice: another old API in /p/src/Scripts.php:67\n" +
		"More deprecation notices were hidden, run again with `-v` to show them.\n" +
		"after the deprecations\n"

	notices, rest := ComposerNotices(composer)
	want := []string{"an old API", "another old API", "More deprecation notices were hidden, run again with `-v` to show them."}
	if !slices.Equal(notices, want) {
		t.Errorf("notices %q, want %q", notices, want)
	}
	if rest != "> Project\\Scripts::deprecation\r\nafter the deprecations\n" {
		t.Errorf("rest %q", rest)
	}

	maestro := "> Project\\Scripts::deprecation\r\n" +
		"Deprecated: an old API\r\n" +
		"Deprecated: another old API\n" +
		"Note: More deprecation notices were hidden, run again with `-v` to show them.\n" +
		"after the deprecations\n"
	got, missing := RemoveNotices(maestro, notices)
	if got != rest || len(missing) != 0 {
		t.Errorf("RemoveNotices: %q, missing %q", got, missing)
	}

	// a notice maestro does not report, and a line that is no diagnostic
	got, missing = RemoveNotices("an old API\nDeprecated: another old API\n", []string{"an old API", "another old API"})
	if got != "an old API\n" || !slices.Equal(missing, []string{"an old API"}) {
		t.Errorf("RemoveNotices: %q, missing %q", got, missing)
	}
}

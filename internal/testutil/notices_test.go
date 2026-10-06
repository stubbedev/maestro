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

// With xdebug.scream on, ErrorHandler appends a warning of its own to the
// message, over several lines, before the location.
func TestNoticesOverSeveralLines(t *testing.T) {
	composer := "Deprecation Notice: an old API\r\n" +
		"\r\n" +
		"Warning: You have xdebug.scream enabled, the warning above may be\r\n" +
		"a legitimately suppressed error that you were not supposed to see. in /p/src/Scripts.php:66\r\n" +
		"Stack trace:\r\n" +
		" /p/src/Scripts.php:66\r\n" +
		"Deprecation Notice: no location\n" +
		"after\n"

	notices, rest := ComposerNotices(composer)
	scream := "an old API\n\nWarning: You have xdebug.scream enabled, the warning above may be\n" +
		"a legitimately suppressed error that you were not supposed to see."
	if !slices.Equal(notices, []string{scream}) {
		t.Errorf("notices %q", notices)
	}
	if rest != "Deprecation Notice: no location\nafter\n" {
		t.Errorf("rest %q", rest)
	}

	maestro := "Deprecated: an old API\r\n" +
		"\r\n" +
		"            Warning: You have xdebug.scream enabled, the warning above may be\r\n" +
		"            a legitimately suppressed error that you were not supposed to see.\r\n" +
		"after\n"
	got, missing := RemoveNotices(maestro, notices)
	if got != "after\n" || len(missing) != 0 {
		t.Errorf("RemoveNotices: %q, missing %q", got, missing)
	}

	// only the first line reported
	got, missing = RemoveNotices("Deprecated: an old API\nafter\n", notices)
	if got != "Deprecated: an old API\nafter\n" || !slices.Equal(missing, notices) {
		t.Errorf("RemoveNotices: %q, missing %q", got, missing)
	}
}

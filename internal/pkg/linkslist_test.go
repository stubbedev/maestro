package pkg_test

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// A list-shaped $links is the \ErrorException Composer 2.10.3's
// ErrorHandler throws for Package::convertLinksToMap's notice (PHP 8.4.25:
// setRequires([new Link(...)]) under ErrorHandler::register()); a map is
// fine.
func TestLinksListError(t *testing.T) {
	l := link("a/b", "c/d", "*")

	if err := pkg.LinksListError("setRequires", pkg.LinksOf(l)); err != nil {
		t.Errorf("map: %v", err)
	}

	err := pkg.LinksListError("setConflicts", pkg.LinkList(l))

	var e *util.ErrorException
	if !errors.As(err, &e) {
		t.Fatalf("list: %v", err)
	}

	if want := "Package::setConflicts must be called with a map of lowercased package name => Link object, got a indexed array, this is deprecated and you should fix your usage."; e.Message != want {
		t.Errorf("message %q", e.Message)
	}
}

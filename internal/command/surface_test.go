// The free surface's styling (docs/PORTING.md "Presentation of free
// output"): every --format a command offers says which surface it is on,
// and a free format's decorated output is its undecorated output plus
// escape sequences, nothing more.

package command_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/ui"
)

// notOutputFormats are the commands whose --format is not a report's:
// archive's is the archive's file type, and help's and list's are
// Symfony's descriptors, frozen with the rest of the CLI surface.
var notOutputFormats = map[string]bool{"archive": true, "help": true, "list": true}

// Every --format (and --audit-format) option suggests exactly the formats
// of its table, and the table follows the contract: the text and table
// formats are for people and free; json, plain, summary and any other are
// parsed and frozen.
func TestOutputFormatsAreClassified(t *testing.T) {
	app := commandtest.NewApplication()
	seen := map[string]bool{}
	for _, nc := range app.All("") {
		c := nc.Command.Base()
		name := c.Name()
		if notOutputFormats[name] || seen[name] {
			continue
		}
		seen[name] = true
		for _, o := range c.Definition().Options() {
			if o.Name() != "format" && o.Name() != "audit-format" {
				continue
			}
			formats, ok := command.OutputFormats[name][o.Name()]
			if !ok {
				t.Errorf("%s --%s: its formats are not classified (internal/command/export_formats_test.go)", name, o.Name())

				continue
			}
			var s console.CompletionSuggestions
			o.Complete(nil, &s)
			var suggested []string
			for _, v := range s.ValueSuggestions() {
				suggested = append(suggested, v.Value)
			}
			if !slices.Equal(suggested, formats.Names()) {
				t.Errorf("%s --%s suggests %q, its formats are %q", name, o.Name(), suggested, formats.Names())
			}
		}
	}
	for name, options := range command.OutputFormats {
		if !seen[name] {
			t.Errorf("%s: classified but not registered", name)
		}
		for option, formats := range options {
			for _, f := range formats {
				want := ui.Frozen
				if f.Name == "text" || f.Name == "table" {
					want = ui.Free
				}
				if f.Surface != want {
					t.Errorf("%s --%s=%s is %s, the contract makes it %s", name, option, f.Name, f.Surface, want)
				}
			}
		}
	}
}

// sgrOrLinkRe matches an SGR sequence or an OSC 8 hyperlink's start or
// end.
var sgrOrLinkRe = regexp.MustCompile("\x1b\\[[0-9;]*m|\x1b\\]8;[^\x1b\a]*(?:\x1b\\\\|\a)")

// surfaceProject is a project whose packages carry every kind of data the
// free text formats style: licences (OSI-approved, not, none), funding,
// suggestions, descriptions, requirements and an unmet platform
// requirement.
func surfaceProject(t *testing.T) {
	t.Helper()
	commandtest.InitTempComposer(t, `{
		"name": "test/pkg",
		"license": "MIT",
		"require": {"first/pkg": "^2.0", "second/pkg": "3.*", "third/pkg": "^1.3", "ext-foobar": "^2.0", "ext-provided": "^1.0"},
		"require-dev": {"ext-barbaz": "~4.0"}
	}`, nil, nil, true)

	first := commandtest.GetPackage(t, "first/pkg", "2.3.4")
	first.SetLicense(php.ListOf("MIT", "proprietary"))
	first.SetDescription(pkg.Str("The first package"))
	first.SetFunding(php.ListOf(php.ArrayOf("type", "custom", "url", "https://example.org/fund")))
	first.SetSuggests(php.ArrayOf("vendor/suggested", "for the </comment> reason", "vendor/other", "helps"))
	commandtest.ConfigureLinks(t, first, `{"require": {"second/pkg": "^3.0"}}`)

	second := commandtest.GetPackage(t, "second/pkg", "3.4.0")
	second.SetLicense(php.ListOf("LGPL-2.0-only"))
	second.SetDescription(pkg.Str("Second, with a </p> tag"))
	second.SetHomepage(pkg.Str("https://example.org"))

	third := commandtest.GetPackage(t, "third/pkg", "1.5.4")
	commandtest.ConfigureLinks(t, third, `{"provide": {"ext-provided": "1.0.0"}}`)

	packages := []pkg.PackageInterface{first, second, third, commandtest.GetPackage(t, "ext-foobar", "2.3.4")}
	devPackages := []pkg.PackageInterface{commandtest.GetPackage(t, "ext-barbaz", "2.3.4.5")}
	commandtest.CreateInstalledJSON(t, packages, devPackages, true)
	commandtest.CreateComposerLock(t, packages, devPackages)
}

// A free text format's decorated output is styled, and without its escape
// sequences it is the undecorated output, byte for byte, on both streams.
func TestFreeTextFormatsAreColourOnly(t *testing.T) {
	muted, accent := ui.RoleMuted.Styled, ui.RoleAccent.Styled
	for _, tc := range []struct {
		args []any
		// styled are pieces of text the decorated output shows in a role.
		styled []string
	}{
		{[]any{"command", "licenses"}, []string{
			muted("Name:"), ui.RoleSuccess.Styled("MIT"), ui.RoleNotice.Styled("proprietary"), ui.RoleDanger.Styled("none"),
		}},
		{[]any{"command", "fund"}, []string{accent("first"), ui.RoleLink.Styled("https://example.org/fund")}},
		{[]any{"command", "check-platform-reqs"}, []string{muted("provided by third/pkg")}},
		{[]any{"command", "suggests"}, []string{accent("first/pkg"), muted("helps")}},
		{[]any{"command", "suggests", "--by-suggestion", true}, []string{accent("vendor/other"), muted("helps")}},
		{[]any{"command", "show"}, []string{muted("The first package")}},
		{[]any{"command", "show", "--tree", true}, []string{muted("The first package"), muted("^3.0")}},
		{[]any{"command", "show", "package", "first/pkg", "--tree", true}, []string{muted("^3.0")}},
	} {
		args := tc.args
		t.Run(fmt.Sprint(args[1:]...), func(t *testing.T) {
			surfaceProject(t)
			run := func(decorated bool) commandtest.Streams {
				tester := commandtest.GetApplicationTester(t)
				code, err := tester.RunArgs(commandtest.Options{Decorated: &decorated, CaptureStderrSeparately: true}, args...)
				if err != nil {
					t.Fatal(err)
				}

				return commandtest.Streams{Code: code, Stdout: tester.Display(true), Stderr: tester.ErrorOutput(true)}
			}
			plain, decorated := run(false), run(true)
			if plain.Code != decorated.Code {
				t.Errorf("exit code %d undecorated, %d decorated", plain.Code, decorated.Code)
			}
			if strings.Contains(plain.Stdout+plain.Stderr, "\x1b") {
				t.Errorf("undecorated output carries escape sequences:\n%q", plain.Stdout+plain.Stderr)
			}
			for _, s := range tc.styled {
				if !strings.Contains(decorated.Stdout, s) {
					t.Errorf("decorated stdout does not show %q:\n%q", s, decorated.Stdout)
				}
			}
			// Composer draws a tree with box-drawing characters when
			// decorated and ASCII when not.
			ascii := strings.NewReplacer("└", "`-", "├", "|-", "──", "-", "│", "|")
			if got := ascii.Replace(sgrOrLinkRe.ReplaceAllString(decorated.Stdout, "")); got != plain.Stdout {
				t.Errorf("decorated stdout without escapes:\n%s\nundecorated:\n%s", got, plain.Stdout)
			}
			if got := sgrOrLinkRe.ReplaceAllString(decorated.Stderr, ""); got != plain.Stderr {
				t.Errorf("decorated stderr without escapes:\n%s\nundecorated:\n%s", got, plain.Stderr)
			}
		})
	}
}

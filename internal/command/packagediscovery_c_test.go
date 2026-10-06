package command_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
)

func TestPackageDiscovery_MinimumStability(t *testing.T) {
	d := command.NewPackageDiscovery(command.NewBaseCommand("init"))

	withOption := func(value any) console.Input {
		def := console.MustDefinition(console.MustOption("stability", "s", console.OptionValueRequired, "", nil))
		params := []console.Param{}
		if value != nil {
			params = append(params, console.P("--stability", value))
		}
		in, err := console.NewArrayInput(params, def)
		if err != nil {
			t.Fatal(err)
		}

		return in
	}
	for _, tc := range []struct {
		value any
		want  string
	}{{nil, "stable"}, {"RC", "RC"}, {"Dev", "dev"}} {
		if got, err := d.MinimumStabilityForTest(withOption(tc.value)); err != nil || got != tc.want {
			t.Errorf("%v: got %q, %v", tc.value, got, err)
		}
	}
	if _, err := d.MinimumStabilityForTest(withOption("foo")); err == nil || err.Error() != `Invalid stability string "foo", expected one of stable, RC, beta, alpha or dev` {
		t.Errorf("got %v", err)
	}

	// without the option: composer.json's minimum-stability
	noOption, _ := console.NewArrayInput(nil, console.MustDefinition())
	commandtest.InitTempComposer(t, `{"minimum-stability": "beta"}`, nil, nil, true)
	if got, err := d.MinimumStabilityForTest(noOption); err != nil || got != "beta" {
		t.Errorf("got %q, %v", got, err)
	}
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	if got, err := d.MinimumStabilityForTest(noOption); err != nil || got != "stable" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestPackageDiscovery_FindSimilarBeforeRepos(t *testing.T) {
	d := command.NewPackageDiscovery(command.NewBaseCommand("require"))
	if _, err := d.FindSimilarForTest("foo"); err == nil || err.Error() != "findSimilar was called before $this->repos was initialized" {
		t.Errorf("got %v", err)
	}
}

func TestPackageDiscovery_DetermineRequirementsSeparateAs(t *testing.T) {
	d := command.NewPackageDiscovery(command.NewBaseCommand("require"))
	in, _ := console.NewArrayInput(nil, console.MustDefinition())
	_, err := d.DetermineRequirements(in, nil, []string{"foo/bar", "AS", "1.0"}, nil, "stable", true, false)
	if err == nil || err.Error() != `Cannot use "AS" as a separate argument. Quote the inline alias as one argument, e.g. "vendor/package:dev-main as 1.2.x-dev".` {
		t.Errorf("got %v", err)
	}
}

func TestPackageDiscovery_PlatformExceptionDetails(t *testing.T) {
	d := command.NewPackageDiscovery(command.NewBaseCommand("require"))
	candidate := pkg.NewCompletePackage("a/a", "1.0.0.0", "1.0.0")
	parser := pkg.NewVersionParser()
	var b pkg.LinksBuilder
	for _, r := range [][2]string{{"php", "^8.0"}, {"ext-foo", "*"}, {"ext-bar", "*"}, {"b/b", "^1"}} {
		c, err := parser.ParseConstraints(r[1])
		if err != nil {
			t.Fatal(err)
		}
		b.Set(r[0], pkg.NewLink("a/a", r[0], c, pkg.TypeRequire, pkg.Str(r[1])))
	}
	candidate.SetRequires(b.Build())

	if got, err := d.PlatformExceptionDetails(candidate, nil); err != nil || got != "" {
		t.Errorf("nil repo: %q %v", got, err)
	}

	repo, err := repository.NewPlatformRepository(nil, php.ArrayOf("php", "7.4.0", "ext-foo", false), repository.PlatformOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.PlatformExceptionDetails(candidate, repo)
	if err != nil {
		t.Fatal(err)
	}
	// getPlatformExceptionDetails joins with PHP_EOL.
	want := ":" + php.EOL +
		"  - a/a 1.0.0 requires php ^8.0 which does not match your installed version 7.4.0 (Package overridden via config.platform)." + php.EOL +
		// ext-foo is only "disabled" when the running PHP has it loaded
		"  - a/a 1.0.0 requires ext-foo * but it is not present." + php.EOL +
		"  - a/a 1.0.0 requires ext-bar * but it is not present."
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

// TestPackageDiscovery_PlatformExceptionDetailsWindowsEOL checks that the
// details end their lines with PHP_EOL, "\r\n" on Windows.
func TestPackageDiscovery_PlatformExceptionDetailsWindowsEOL(t *testing.T) {
	php.SetEOLForTest(t, "\r\n")

	d := command.NewPackageDiscovery(command.NewBaseCommand("require"))
	candidate := pkg.NewCompletePackage("a/a", "1.0.0.0", "1.0.0")
	c, err := pkg.NewVersionParser().ParseConstraints("*")
	if err != nil {
		t.Fatal(err)
	}
	var b pkg.LinksBuilder
	for _, name := range []string{"ext-foo", "ext-bar"} {
		b.Set(name, pkg.NewLink("a/a", name, c, pkg.TypeRequire, pkg.Str("*")))
	}
	candidate.SetRequires(b.Build())

	repo, err := repository.NewPlatformRepository(nil, php.ArrayOf("php", "7.4.0"), repository.PlatformOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.PlatformExceptionDetails(candidate, repo)
	if err != nil {
		t.Fatal(err)
	}
	want := ":\r\n  - a/a 1.0.0 requires ext-foo * but it is not present.\r\n  - a/a 1.0.0 requires ext-bar * but it is not present."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPackageDiscovery_PHPVersionForSelector(t *testing.T) {
	for in, want := range map[string]string{"8.4.25": "8.4.25", "8.5.0-dev": "8.5.0", "8.3.1RC1": "8.3.1"} {
		if got := command.PHPVersionForSelector(in); got != want {
			t.Errorf("%s: got %s", in, got)
		}
	}
}

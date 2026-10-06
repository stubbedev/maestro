package testutil

import (
	"slices"
	"strings"
	"testing"
)

const rendered = "Warning: something\n" +
	"\n" +
	"In ArrayLoader.php line 412:\n" +
	"                                                                               \n" +
	"  [UnexpectedValueException]                                                   \n" +
	"  Link constraint in acme/app requires > x/y should be a valid version constr  \n" +
	"  aint, got \"^^1\"                                                              \n" +
	"                                                                               \n" +
	"\n" +
	"Exception trace:\n" +
	"  at @COMPOSER@/src/Composer/Package/Loader/ArrayLoader.php:412\n" +
	"\n" +
	"In VersionParser.php line 526:\n" +
	"           \n" +
	"  first    \n" +
	"           \n" +
	"  second   \n" +
	"           \n" +
	"\n" +
	"                                          \n" +
	"  Command \"nope-command\" is not defined.  \n" +
	"                                          \n" +
	"\n" +
	"In Preg.php line 1:\n" +
	"                                                                              \n" +
	"  [TypeError]                                                                 \n" +
	"  Composer\\Pcre\\Preg::isMatch(): Argument #2 ($subject) must be of type strin  \n" +
	"  g, int given, called in @COMPOSER@/src/Compo  \n" +
	"  ser/Util/ConfigValidator.php on line 134                                     \n" +
	"                                                                              \n" +
	"\n" +
	"update [--with WITH] [--] [<packages>...]\n" +
	"\n"

func TestErrorRendering(t *testing.T) {
	want := []string{
		`Linkconstraintinacme/apprequires>x/yshouldbeavalidversionconstraint,got"^^1"`,
		"firstsecond",
		`Command"nope-command"isnotdefined.`,
		`Composer\Pcre\Preg::isMatch():Argument#2($subject)mustbeoftypestring,intgiven`,
	}
	for name, output := range map[string]string{
		"\\n":    rendered,
		"\\r\\n": strings.ReplaceAll(rendered, "\n", "\r\n"),
	} {
		got, start := ErrorRendering(output)
		if !slices.Equal(got, want) {
			t.Errorf("%s: messages =\n%q\nwant\n%q", name, got, want)
		}
		if head := strings.ReplaceAll(output[:start], "\r", ""); head != "Warning: something\n" {
			t.Errorf("%s: rendering starts after %q", name, head)
		}
	}

	if got, start := ErrorRendering("Nothing to install\n"); got != nil || start != len("Nothing to install\n") {
		t.Errorf("no rendering: %q from %d", got, start)
	}
}

func TestCompactMessage(t *testing.T) {
	for in, want := range map[string]string{
		"  connect to h:1 after 12 ms: Could not\n  connect   \n": "connecttoh:1after0ms:Couldnotconnect",
		"│ a │\n│ b │": "ab",
		"f(): Argument #1 must be of type string, int given, called in /x/Y.php on line 3": "f():Argument#1mustbeoftypestring,intgiven",
	} {
		if got := CompactMessage(in); got != want {
			t.Errorf("CompactMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

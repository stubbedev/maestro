// Ports Tests/Formatter/*.php and Tests/ColorTest.php (symfony/console).

package console

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// catchPanic runs fn and returns the *Error it panicked with, if any.
func catchPanic(t *testing.T, fn func()) (err *Error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*Error)
			if !ok {
				panic(r)
			}
			err = e
		}
	}()
	fn()

	return nil
}

func eq[T comparable](t *testing.T, got, want T, msg ...string) {
	t.Helper()
	if got != want {
		t.Errorf("%s\nwant %#v\ngot  %#v", strings.Join(msg, " "), want, got)
	}
}

// wantKind asserts err is an *Error of kind (and, if non-empty, message).
func wantKind(t *testing.T, err error, kind Kind, message string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want %s error %q, got %v", kindClass[kind], message, err)
	}
	if e.Kind != kind {
		t.Errorf("want %s, got %s (%q)", kindClass[kind], kindClass[e.Kind], e.Message)
	}
	if message != "" && e.Message != message {
		t.Errorf("want message %q, got %q", message, e.Message)
	}
}

func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func TestOutputFormatter_EmptyTag(t *testing.T) {
	f := NewOutputFormatter(true)
	eq(t, f.Format("foo<>bar"), "foo<>bar")
}

func TestOutputFormatter_LGCharEscaping(t *testing.T) {
	f := NewOutputFormatter(true)
	eq(t, f.Format(`foo\<bar`), "foo<bar")
	eq(t, f.Format("foo << bar"), "foo << bar")
	eq(t, f.Format(`foo << bar \`), `foo << bar \`)
	eq(t, f.Format(`foo << <info>bar \ baz</info> \`), "foo << \033[32mbar \\ baz\033[39m \\")
	eq(t, f.Format(`\<info>some info\</info>`), "<info>some info</info>")
	eq(t, Escape("<info>some info</info>"), `\<info\>some info\</info\>`)
	// every < and > gets escaped if not already escaped, but already escaped ones do not get escaped again
	// and escaped backslashes remain as such, same with backslashes escaping non-special characters
	eq(t, Escape(`foo < bar \< baz \\< foo > bar \> baz \\> \x`), `foo \< bar \< baz \\< foo \> bar \> baz \\> \x`)
	eq(t, f.Format(`<comment>Symfony\Component\Console does work very well!</comment>`), "\033[33mSymfony\\Component\\Console does work very well!\033[39m")
}

func TestOutputFormatter_BundledStyles(t *testing.T) {
	f := NewOutputFormatter(true)
	for _, s := range []string{"error", "info", "comment", "question"} {
		if !f.HasStyle(s) {
			t.Errorf("missing style %s", s)
		}
	}
	eq(t, f.Format("<error>some error</error>"), "\033[37;41msome error\033[39;49m")
	eq(t, f.Format("<info>some info</info>"), "\033[32msome info\033[39m")
	eq(t, f.Format("<comment>some comment</comment>"), "\033[33msome comment\033[39m")
	eq(t, f.Format("<question>some question</question>"), "\033[30;46msome question\033[39;49m")
}

func TestOutputFormatter_NestedStyles(t *testing.T) {
	f := NewOutputFormatter(true)
	eq(t, f.Format("<error>some <info>some info</info> error</error>"),
		"\033[37;41msome \033[39;49m\033[32msome info\033[39m\033[37;41m error\033[39;49m")
}

func TestOutputFormatter_AdjacentStyles(t *testing.T) {
	f := NewOutputFormatter(true)
	eq(t, f.Format("<error>some error</error><info>some info</info>"),
		"\033[37;41msome error\033[39;49m\033[32msome info\033[39m")
}

func TestOutputFormatter_StyleMatchingNotGreedy(t *testing.T) {
	f := NewOutputFormatter(true)
	eq(t, f.Format("(<info>>=2.0,<2.3</info>)"), "(\033[32m>=2.0,<2.3\033[39m)")
}

func TestOutputFormatter_StyleEscaping(t *testing.T) {
	f := NewOutputFormatter(true)
	eq(t, f.Format("(<info>"+Escape(`z>=2.0,<\<<a2.3\`)+"</info>)"), "(\033[32mz>=2.0,<<<a2.3\\\033[39m)")
	eq(t, f.Format("<info>"+Escape("<error>some error</error>")+"</info>"), "\033[32m<error>some error</error>\033[39m")
}

func TestOutputFormatter_DeepNestedStyles(t *testing.T) {
	f := NewOutputFormatter(true)
	eq(t, f.Format("<error>error<info>info<comment>comment</info>error</error>"),
		"\033[37;41merror\033[39;49m\033[32minfo\033[39m\033[33mcomment\033[39m\033[37;41merror\033[39;49m")
}

func TestOutputFormatter_NewStyle(t *testing.T) {
	f := NewOutputFormatter(true)
	style := MustStyle("blue", "white")
	f.SetStyle("test", style)
	got, err := f.Style("test")
	if err != nil || got != Style(style) {
		t.Fatalf("Style(test) = %v, %v", got, err)
	}
	if info, _ := f.Style("info"); info == Style(style) {
		t.Error("info style must differ")
	}
	f.SetStyle("b", MustStyle("blue", "white"))
	eq(t, f.Format("<test>some <b>custom</b> msg</test>"),
		"\033[34;47msome \033[39;49m\033[34;47mcustom\033[39;49m\033[34;47m msg\033[39;49m")
}

func TestOutputFormatter_RedefineStyle(t *testing.T) {
	f := NewOutputFormatter(true)
	f.SetStyle("info", MustStyle("blue", "white"))
	eq(t, f.Format("<info>some custom msg</info>"), "\033[34;47msome custom msg\033[39;49m")
}

func TestOutputFormatter_InlineStyle(t *testing.T) {
	f := NewOutputFormatter(true)
	eq(t, f.Format("<fg=blue;bg=red>some text</>"), "\033[34;41msome text\033[39;49m")
	eq(t, f.Format("<fg=blue;bg=red>some text</fg=blue;bg=red>"), "\033[34;41msome text\033[39;49m")
}

func TestOutputFormatter_InlineStyleOptions(t *testing.T) {
	unsetEnv(t, "COLORTERM")
	cases := []struct {
		tag, expected, input string
		null, truecolor      bool
	}{
		{tag: "<unknown=_unknown_>", null: true},
		{tag: "<unknown=_unknown_;a=1;b>", null: true},
		{"<fg=green;>", "\033[32m[test]\033[39m", "[test]", false, false},
		{"<fg=green;bg=blue;>", "\033[32;44ma\033[39;49m", "a", false, false},
		{"<fg=green;options=bold>", "\033[32;1mb\033[39;22m", "b", false, false},
		{"<fg=green;options=reverse;>", "\033[32;7m<a>\033[39;27m", "<a>", false, false},
		{"<fg=green;options=bold,underscore>", "\033[32;1;4mz\033[39;22;24m", "z", false, false},
		{"<fg=green;options=bold,underscore,reverse;>", "\033[32;1;4;7md\033[39;22;24;27m", "d", false, false},
		{"<fg=#00ff00;bg=#00f>", "\033[38;2;0;255;0;48;2;0;0;255m[test]\033[39;49m", "[test]", false, true},
	}
	for _, c := range cases {
		if c.truecolor {
			t.Setenv("COLORTERM", "truecolor")
		}
		styleString := c.tag[1 : len(c.tag)-1]
		f := NewOutputFormatter(true)
		result := f.createStyleFromString(styleString)
		if c.null {
			if result != nil {
				t.Errorf("%s: want nil style", c.tag)
			}
			expected := c.tag + c.input + "</" + styleString + ">"
			eq(t, f.Format(expected), expected, c.tag)

			continue
		}
		if _, ok := result.(*OutputFormatterStyle); !ok {
			t.Errorf("%s: want *OutputFormatterStyle, got %T", c.tag, result)
		}
		eq(t, f.Format(c.tag+c.input+"</>"), c.expected, c.tag)
		eq(t, f.Format(c.tag+c.input+"</"+styleString+">"), c.expected, c.tag)
	}
}

func TestOutputFormatter_InlineStyleTagsWithUnknownOptions(t *testing.T) {
	for _, c := range []struct{ tag, option string }{
		{"<options=abc;>", "abc"},
		{"<options=abc,def;>", "abc"},
		{"<fg=green;options=xyz;>", "xyz"},
		{"<fg=green;options=efg,abc>", "efg"},
	} {
		f := NewOutputFormatter(true)
		err := catchPanic(t, func() { f.Format(c.tag + "foo</>") })
		if err == nil {
			t.Fatalf("%s: want panic", c.tag)
		}
		wantKind(t, err, KindInvalidArgument, `Invalid option specified: "`+c.option+`". Expected one of (bold, underscore, blink, reverse, conceal).`)
	}
}

func TestOutputFormatter_NonStyleTag(t *testing.T) {
	f := NewOutputFormatter(true)
	eq(t, f.Format("<info>some <tag> <setting=value> styled <p>single-char tag</p></info>"),
		"\033[32msome \033[39m\033[32m<tag>\033[39m\033[32m \033[39m\033[32m<setting=value>\033[39m\033[32m styled \033[39m\033[32m<p>\033[39m\033[32msingle-char tag\033[39m\033[32m</p>\033[39m")
}

func TestOutputFormatter_FormatLongString(t *testing.T) {
	f := NewOutputFormatter(true)
	long := strings.Repeat(`\`, 14000)
	eq(t, f.Format("<error>some error</error>"+long), "\033[37;41msome error\033[39;49m"+long)
}

func TestOutputFormatter_FormatToStringObject(t *testing.T) {
	// A Stringable is formatted through its string form.
	f := NewOutputFormatter(false)
	eq(t, f.Format("<info>some info</info>"), "some info")
}

func TestOutputFormatter_FormatterHasStyles(t *testing.T) {
	f := NewOutputFormatter(false)
	for _, s := range []string{"error", "info", "comment", "question"} {
		if !f.HasStyle(s) {
			t.Errorf("missing style %s", s)
		}
	}
}

var decoratedAndNonDecoratedOutput = []struct {
	input, nonDecorated, decorated string
	flag                           bool
}{
	{"<error>some error</error>", "some error", "\033[37;41msome error\033[39;49m", false},
	{"<info>some info</info>", "some info", "\033[32msome info\033[39m", false},
	{"<comment>some comment</comment>", "some comment", "\033[33msome comment\033[39m", false},
	{"<question>some question</question>", "some question", "\033[30;46msome question\033[39;49m", false},
	{"<fg=red>some text with inline style</>", "some text with inline style", "\033[31msome text with inline style\033[39m", false},
	{"<href=idea://open/?file=/path/SomeFile.php&line=12>some URL</>", "some URL", "\033]8;;idea://open/?file=/path/SomeFile.php&line=12\033\\some URL\033]8;;\033\\", false},
	{`<href=https://example.com/\<woohoo\>>some URL with \<woohoo\></>`, "some URL with <woohoo>", "\033]8;;https://example.com/<woohoo>\033\\some URL with <woohoo>\033]8;;\033\\", false},
	{"<href=idea://open/?file=/path/SomeFile.php&line=12>some URL</>", "some URL", "some URL", true},
}

func TestOutputFormatter_NotDecoratedFormatterOnJediTermEmulator(t *testing.T) {
	unsetEnv(t, "KONSOLE_VERSION", "IDEA_INITIAL_DIRECTORY")
	for _, c := range decoratedAndNonDecoratedOutput {
		if c.flag {
			t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
		} else {
			t.Setenv("TERMINAL_EMULATOR", "Unknown")
		}
		eq(t, NewOutputFormatter(true).Format(c.input), c.decorated, c.input)
		eq(t, NewOutputFormatter(false).Format(c.input), c.nonDecorated, c.input)
	}
}

func TestOutputFormatter_NotDecoratedFormatterOnIDEALikeEnvironment(t *testing.T) {
	unsetEnv(t, "KONSOLE_VERSION", "TERMINAL_EMULATOR", "IDEA_INITIAL_DIRECTORY")
	for _, c := range decoratedAndNonDecoratedOutput {
		if c.flag {
			t.Setenv("IDEA_INITIAL_DIRECTORY", "/tmp")
		} else {
			os.Unsetenv("IDEA_INITIAL_DIRECTORY")
		}
		eq(t, NewOutputFormatter(true).Format(c.input), c.decorated, c.input)
		eq(t, NewOutputFormatter(false).Format(c.input), c.nonDecorated, c.input)
	}
}

func TestOutputFormatter_HrefOnKonsole(t *testing.T) {
	unsetEnv(t, "TERMINAL_EMULATOR", "IDEA_INITIAL_DIRECTORY")
	in := "<href=x>l</>"
	for _, c := range []struct {
		version string
		link    bool
	}{{"", true}, {"0", true}, {"201100", false}, {"220400", true}} {
		t.Setenv("KONSOLE_VERSION", c.version)
		got := NewOutputFormatter(true).Format(in)
		eq(t, strings.Contains(got, "\033]8;;x"), c.link, "KONSOLE_VERSION="+c.version)
	}
}

func TestOutputFormatter_ContentWithLineBreaks(t *testing.T) {
	f := NewOutputFormatter(true)
	eq(t, f.Format("<info>\nsome text</info>"), "\033[32m\nsome text\033[39m")
	eq(t, f.Format("<info>some text\n</info>"), "\033[32msome text\n\033[39m")
	eq(t, f.Format("<info>\nsome text\n</info>"), "\033[32m\nsome text\n\033[39m")
	eq(t, f.Format("<info>\nsome text\nmore text\n</info>"), "\033[32m\nsome text\nmore text\n\033[39m")
}

func TestOutputFormatter_FormatAndWrap(t *testing.T) {
	f := NewOutputFormatter(true)
	eq(t, f.FormatAndWrap("foo<error>bar</error> baz", 2), "fo\no\033[37;41mb\033[39;49m\n\033[37;41mar\033[39;49m\nba\nz")
	eq(t, f.FormatAndWrap("pre <error>foo bar baz</error> post", 2), "pr\ne \033[37;41m\033[39;49m\n\033[37;41mfo\033[39;49m\n\033[37;41mo\033[39;49m\n\033[37;41mba\033[39;49m\n\033[37;41mr\033[39;49m\n\033[37;41mba\033[39;49m\n\033[37;41mz\033[39;49m \npo\nst")
	eq(t, f.FormatAndWrap("pre <error>foo bar baz</error> post", 3), "pre\033[37;41m\033[39;49m\n\033[37;41mfoo\033[39;49m\n\033[37;41mbar\033[39;49m\n\033[37;41mbaz\033[39;49m\npos\nt")
	eq(t, f.FormatAndWrap("pre <error>foo bar baz</error> post", 4), "pre \033[37;41m\033[39;49m\n\033[37;41mfoo\033[39;49m\n\033[37;41mbar\033[39;49m\n\033[37;41mbaz\033[39;49m \npost")
	eq(t, f.FormatAndWrap("pre <error>foo bar baz</error> post", 5), "pre \033[37;41mf\033[39;49m\n\033[37;41moo\033[39;49m\n\033[37;41mbar\033[39;49m\n\033[37;41mbaz\033[39;49m p\nost")
	eq(t, f.FormatAndWrap("Lorem <error>ipsum</error> dolor <info>sit</info> amet", 4), "Lore\nm \033[37;41mip\033[39;49m\n\033[37;41msum\033[39;49m \ndolo\nr \033[32msi\033[39m\n\033[32mt\033[39m am\net")
	eq(t, f.FormatAndWrap("Lorem <error>ipsum</error> dolor <info>sit</info> amet", 8), "Lorem \033[37;41mip\033[39;49m\n\033[37;41msum\033[39;49m dolo\nr \033[32msit\033[39m am\net")
	eq(t, f.FormatAndWrap("Lorem <error>ipsum</error> dolor <info>sit</info>, <error>amet</error> et <info>laudantium</info> architecto", 18), "Lorem \033[37;41mipsum\033[39;49m dolor \033[32m\033[39m\n\033[32msit\033[39m, \033[37;41mamet\033[39;49m et \033[32mlauda\033[39m\n\033[32mntium\033[39m architecto")

	f = NewOutputFormatter(false)
	eq(t, f.FormatAndWrap("foo<error>bar</error> baz", 2), "fo\nob\nar\nba\nz")
	eq(t, f.FormatAndWrap("pre <error>foo bar baz</error> post", 2), "pr\ne \nfo\no\nba\nr\nba\nz \npo\nst")
	eq(t, f.FormatAndWrap("pre <error>foo bar baz</error> post", 3), "pre\nfoo\nbar\nbaz\npos\nt")
	eq(t, f.FormatAndWrap("pre <error>foo bar baz</error> post", 4), "pre \nfoo\nbar\nbaz \npost")
	eq(t, f.FormatAndWrap("pre <error>foo bar baz</error> post", 5), "pre f\noo\nbar\nbaz p\nost")
	eq(t, f.FormatAndWrap("Â rèälly löng tîtlè thät cöüld nèêd múltîplê línès", 10), "Â rèälly\nlöng tîtlè\nthät cöüld\nnèêd\nmúltîplê\nlínès")
	eq(t, f.FormatAndWrap("Â rèälly löng tîtlè thät cöüld nèêd múltîplê\n línès", 10), "Â rèälly\nlöng tîtlè\nthät cöüld\nnèêd\nmúltîplê\n línès")
	eq(t, f.FormatAndWrap("", 5), "")
}

func TestOutputFormatter_FormatAndWrapInvalidUTF8(t *testing.T) {
	f := NewOutputFormatter(false)
	err := catchPanic(t, func() { f.FormatAndWrap("ab\xffcd", 2) })
	wantKind(t, err, KindStringInvalidArgument, `Invalid "UTF-8" string.`)
	// substr() splits the "é" across the line boundary
	err = catchPanic(t, func() { f.FormatAndWrap("xx<info>é</info>", 3) })
	wantKind(t, err, KindStringInvalidArgument, `Invalid "UTF-8" string.`)
	eq(t, f.FormatAndWrap("aé b", 1), "a\né\nb")
}

func TestOutputFormatter_Clone(t *testing.T) {
	f := NewOutputFormatter(true)
	c := f.Clone()
	s, _ := c.Style("info")
	_ = s.SetForeground("red")
	eq(t, f.Format("<info>x</info>"), "\033[32mx\033[39m")
	eq(t, c.Format("<info>x</info>"), "\033[31mx\033[39m")
}

func TestOutputFormatter_UndefinedStyle(t *testing.T) {
	_, err := NewOutputFormatter(false).Style("foo")
	wantKind(t, err, KindInvalidArgument, `Undefined style: "foo".`)
}

func TestOutputFormatterStyle_Constructor(t *testing.T) {
	eq(t, MustStyle("green", "black", "bold", "underscore").Apply("foo"), "\033[32;40;1;4mfoo\033[39;49;22;24m")
	eq(t, MustStyle("red", "", "blink").Apply("foo"), "\033[31;5mfoo\033[39;25m")
	eq(t, MustStyle("", "white").Apply("foo"), "\033[47mfoo\033[49m")
}

func TestOutputFormatterStyle_Foreground(t *testing.T) {
	s := MustStyle("", "")
	_ = s.SetForeground("black")
	eq(t, s.Apply("foo"), "\033[30mfoo\033[39m")
	_ = s.SetForeground("blue")
	eq(t, s.Apply("foo"), "\033[34mfoo\033[39m")
	_ = s.SetForeground("default")
	eq(t, s.Apply("foo"), "\033[39mfoo\033[39m")
	wantKind(t, s.SetForeground("undefined-color"), KindInvalidArgument,
		`Invalid "undefined-color" color; expected one of (black, red, green, yellow, blue, magenta, cyan, white, default, gray, bright-red, bright-green, bright-yellow, bright-blue, bright-magenta, bright-cyan, bright-white).`)
}

func TestOutputFormatterStyle_Background(t *testing.T) {
	s := MustStyle("", "")
	_ = s.SetBackground("black")
	eq(t, s.Apply("foo"), "\033[40mfoo\033[49m")
	_ = s.SetBackground("yellow")
	eq(t, s.Apply("foo"), "\033[43mfoo\033[49m")
	_ = s.SetBackground("default")
	eq(t, s.Apply("foo"), "\033[49mfoo\033[49m")
	wantKind(t, s.SetBackground("undefined-color"), KindInvalidArgument, "")
}

func TestOutputFormatterStyle_Options(t *testing.T) {
	s := MustStyle("", "")
	_ = s.SetOptions([]string{"reverse", "conceal"})
	eq(t, s.Apply("foo"), "\033[7;8mfoo\033[27;28m")
	_ = s.SetOption("bold")
	eq(t, s.Apply("foo"), "\033[7;8;1mfoo\033[27;28;22m")
	_ = s.UnsetOption("reverse")
	eq(t, s.Apply("foo"), "\033[8;1mfoo\033[28;22m")
	_ = s.SetOption("bold")
	eq(t, s.Apply("foo"), "\033[8;1mfoo\033[28;22m")
	_ = s.SetOptions([]string{"bold"})
	eq(t, s.Apply("foo"), "\033[1mfoo\033[22m")

	err := s.SetOption("foo")
	wantKind(t, err, KindInvalidArgument, "")
	if !strings.Contains(err.Error(), `Invalid option specified: "foo"`) {
		t.Errorf("unexpected message %q", err)
	}
}

func TestOutputFormatterStyle_Href(t *testing.T) {
	unsetEnv(t, "TERMINAL_EMULATOR", "KONSOLE_VERSION", "IDEA_INITIAL_DIRECTORY")
	s := MustStyle("", "")
	s.SetHref("idea://open/?file=/path/SomeFile.php&line=12")
	eq(t, s.Apply("some URL"), "\033]8;;idea://open/?file=/path/SomeFile.php&line=12\033\\some URL\033]8;;\033\\")
}

func TestOutputFormatterStyleStack_Push(t *testing.T) {
	st := NewOutputFormatterStyleStack(nil)
	s1, s2, s3 := MustStyle("white", "black"), MustStyle("yellow", "blue"), MustStyle("green", "red")
	st.Push(s1)
	st.Push(s2)
	eq(t, st.Current(), Style(s2))
	st.Push(s3)
	eq(t, st.Current(), Style(s3))
}

func TestOutputFormatterStyleStack_Pop(t *testing.T) {
	st := NewOutputFormatterStyleStack(nil)
	s1, s2 := MustStyle("white", "black"), MustStyle("yellow", "blue")
	st.Push(s1)
	st.Push(s2)
	got, _ := st.Pop(nil)
	eq(t, got, Style(s2))
	got, _ = st.Pop(nil)
	eq(t, got, Style(s1))
}

func TestOutputFormatterStyleStack_PopEmpty(t *testing.T) {
	st := NewOutputFormatterStyleStack(nil)
	got, err := st.Pop(nil)
	if err != nil || got.Apply("x") != "x" {
		t.Errorf("Pop() on empty stack = %v, %v", got, err)
	}
}

func TestOutputFormatterStyleStack_PopNotLast(t *testing.T) {
	st := NewOutputFormatterStyleStack(nil)
	s1, s2, s3 := MustStyle("white", "black"), MustStyle("yellow", "blue"), MustStyle("green", "red")
	st.Push(s1)
	st.Push(s2)
	st.Push(s3)
	got, _ := st.Pop(s2)
	eq(t, got, Style(s2))
	got, _ = st.Pop(nil)
	eq(t, got, Style(s1))
}

func TestOutputFormatterStyleStack_InvalidPop(t *testing.T) {
	st := NewOutputFormatterStyleStack(nil)
	st.Push(MustStyle("white", "black"))
	_, err := st.Pop(MustStyle("yellow", "blue"))
	wantKind(t, err, KindInvalidArgument, "Incorrectly nested style tag found.")
}

func TestNullOutputFormatter_Format(t *testing.T) {
	eq(t, (&NullOutputFormatter{}).Format("this message will be destroyed"), "")
}

func TestNullOutputFormatter_GetStyle(t *testing.T) {
	f := &NullOutputFormatter{}
	s, _ := f.Style("null")
	if _, ok := s.(*NullOutputFormatterStyle); !ok {
		t.Fatalf("want *NullOutputFormatterStyle, got %T", s)
	}
	s2, _ := f.Style("null")
	eq(t, s2, s)
}

func TestNullOutputFormatter_SetStyle(t *testing.T) {
	f := &NullOutputFormatter{}
	style := MustStyle("", "")
	f.SetStyle("null", style)
	if s, _ := f.Style("null"); s == Style(style) {
		t.Error("SetStyle must not register the style")
	}
}

func TestNullOutputFormatter_HasStyle(t *testing.T) {
	eq(t, (&NullOutputFormatter{}).HasStyle("null"), false)
}

func TestNullOutputFormatter_IsDecorated(t *testing.T) {
	eq(t, (&NullOutputFormatter{}).IsDecorated(), false)
}

func TestNullOutputFormatter_SetDecorated(t *testing.T) {
	f := &NullOutputFormatter{}
	f.SetDecorated(true)
	eq(t, f.IsDecorated(), false)
}

func TestNullOutputFormatterStyle_Apply(t *testing.T) {
	eq(t, (&NullOutputFormatterStyle{}).Apply("foo"), "foo")
}

func TestNullOutputFormatterStyle_SetForeground(t *testing.T) {
	s := &NullOutputFormatterStyle{}
	_ = s.SetForeground("black")
	eq(t, s.Apply("foo"), "foo")
}

func TestNullOutputFormatterStyle_SetBackground(t *testing.T) {
	s := &NullOutputFormatterStyle{}
	_ = s.SetBackground("blue")
	eq(t, s.Apply("foo"), "foo")
}

func TestNullOutputFormatterStyle_Options(t *testing.T) {
	s := &NullOutputFormatterStyle{}
	_ = s.SetOptions([]string{"reverse", "conceal"})
	eq(t, s.Apply("foo"), "foo")
	_ = s.SetOption("bold")
	eq(t, s.Apply("foo"), "foo")
	_ = s.UnsetOption("reverse")
	eq(t, s.Apply("foo"), "foo")
}

func TestColor_AnsiColors(t *testing.T) {
	mk := func(fg, bg string, opts ...string) *Color {
		c, err := NewColor(fg, bg, opts)
		if err != nil {
			t.Fatal(err)
		}

		return c
	}
	eq(t, mk("", "").Apply(" "), " ")
	eq(t, mk("red", "yellow").Apply(" "), "\033[31;43m \033[39;49m")
	eq(t, mk("bright-red", "bright-yellow").Apply(" "), "\033[91;103m \033[39;49m")
	eq(t, mk("red", "yellow", "underscore").Apply(" "), "\033[31;43;4m \033[39;49;24m")
}

func TestColor_TrueColors(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	c, _ := NewColor("#fff", "#000", nil)
	eq(t, c.Apply(" "), "\033[38;2;255;255;255;48;2;0;0;0m \033[39;49m")
	c, _ = NewColor("#ffffff", "#000000", nil)
	eq(t, c.Apply(" "), "\033[38;2;255;255;255;48;2;0;0;0m \033[39;49m")
}

func TestColor_DegradedTrueColors(t *testing.T) {
	t.Setenv("COLORTERM", "")
	c, _ := NewColor("#f00", "#ff0", nil)
	eq(t, c.Apply(" "), "\033[31;43m \033[39;49m")
	c, _ = NewColor("#c0392b", "#f1c40f", nil)
	eq(t, c.Apply(" "), "\033[31;43m \033[39;49m")
}

func TestColor_InvalidHex(t *testing.T) {
	_, err := NewColor("#ff", "", nil)
	wantKind(t, err, KindInvalidArgument, `Invalid "ff" color.`)
}

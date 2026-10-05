// Ports Tests/Helper/{HelperTest,HelperSetTest,FormatterHelperTest}.php
// (symfony/console) and Composer's tests/Composer/Test/Console/
// HtmlOutputFormatterTest.php.

package console

import (
	"testing"
)

func TestHelper_FormatTime(t *testing.T) {
	for _, c := range []struct {
		secs     float64
		expected string
	}{
		{0, "< 1 sec"},
		{1, "1 sec"},
		{2, "2 secs"},
		{59, "59 secs"},
		{60, "1 min"},
		{61, "1 min"},
		{119, "1 min"},
		{120, "2 mins"},
		{121, "2 mins"},
		{3599, "59 mins"},
		{3600, "1 hr"},
		{7199, "1 hr"},
		{7200, "2 hrs"},
		{7201, "2 hrs"},
		{86399, "23 hrs"},
		{86400, "1 day"},
		{86401, "1 day"},
		{172799, "1 day"},
		{172800, "2 days"},
		{172801, "2 days"},
		{2.5, "2 secs"},
		{-1, ""},
	} {
		eq(t, FormatTime(c.secs), c.expected)
	}
}

func TestHelper_FormatMemory(t *testing.T) {
	for _, c := range []struct {
		memory   int
		expected string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1 KiB"},
		{1536, "1 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{1572864, "1.5 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
		{5 * 1024 * 1024 * 1024, "5.0 GiB"},
	} {
		eq(t, FormatMemory(c.memory), c.expected)
	}
}

func TestHelper_RemoveDecoration(t *testing.T) {
	for _, c := range []struct{ decorated, undecorated string }{
		{"abc", "abc"},
		{"abc<fg=default;bg=default>", "abc"},
		{"a\033[1;36mbc", "abc"},
		{"a\033]8;;http://url\033\\b\033]8;;\033\\c", "abc"},
	} {
		f := NewOutputFormatter(true)
		eq(t, RemoveDecoration(f, c.decorated), c.undecorated)
		eq(t, f.IsDecorated(), true, "decoration is restored")
	}
}

func TestHelper_WidthLengthSubstr(t *testing.T) {
	for _, c := range []struct {
		s             string
		width, length int
		substr        string
	}{
		{"", 0, 0, ""},
		{"abc", 3, 3, "bc"},
		{"日本語", 6, 3, "本語"},
		{"a\nb", 2, 3, "\nb"},
		{"éx", 2, 2, "́x"},
		{"\xffab", 3, 3, "ab"},
	} {
		eq(t, Width(c.s), c.width, "Width", c.s)
		eq(t, Length(c.s), c.length, "Length", c.s)
		eq(t, Substr(c.s, 1, 2, false), c.substr, "Substr", c.s)
	}
	eq(t, Substr("abcdef", -2, 0, true), "ef")
	eq(t, Substr("abcdef", 1, -2, false), "bcd")
}

type mockHelper struct {
	HelperBase
	name string
}

func (h *mockHelper) Name() string { return h.name }

func TestHelperSet_Constructor(t *testing.T) {
	h := &mockHelper{name: "fake_helper"}
	set := NewHelperSet()
	set.Set(h, "fake_helper_alias")
	got, err := set.Get("fake_helper_alias")
	if err != nil || got != Helper(h) {
		t.Fatal("__construct sets given helper to helpers")
	}
	eq(t, set.Has("fake_helper_alias"), true, "__construct sets helper alias for given helper")
}

func TestHelperSet_Set(t *testing.T) {
	set := NewHelperSet()
	h := &mockHelper{name: "fake_helper"}
	set.Set(h, "")
	eq(t, set.Has("fake_helper"), true, "->set() adds helper to helpers")
	if h.HelperSet() != set {
		t.Error("->set() calls setHelperSet()")
	}

	set = NewHelperSet()
	set.Set(&mockHelper{name: "fake_helper_01"}, "")
	set.Set(&mockHelper{name: "fake_helper_02"}, "")
	eq(t, set.Has("fake_helper_01"), true, "->set() will set multiple helpers on consecutive calls")
	eq(t, set.Has("fake_helper_02"), true, "->set() will set multiple helpers on consecutive calls")

	set = NewHelperSet()
	set.Set(&mockHelper{name: "fake_helper"}, "fake_helper_alias")
	eq(t, set.Has("fake_helper"), true, "->set() adds helper alias when set")
	eq(t, set.Has("fake_helper_alias"), true, "->set() adds helper alias when set")
}

func TestHelperSet_Has(t *testing.T) {
	set := NewHelperSet()
	set.Set(&mockHelper{name: "fake_helper"}, "fake_helper_alias")
	eq(t, set.Has("fake_helper"), true, "->has() finds set helper")
	eq(t, set.Has("fake_helper_alias"), true, "->has() finds set helper by alias")
}

func TestHelperSet_Get(t *testing.T) {
	h1, h2 := &mockHelper{name: "fake_helper_01"}, &mockHelper{name: "fake_helper_02"}
	set := NewHelperSet()
	set.Set(h1, "fake_helper_01_alias")
	set.Set(h2, "fake_helper_02_alias")
	for name, want := range map[string]Helper{"fake_helper_01": h1, "fake_helper_01_alias": h1, "fake_helper_02": h2, "fake_helper_02_alias": h2} {
		got, err := set.Get(name)
		if err != nil || got != want {
			t.Errorf("->get(%s) returns correct helper", name)
		}
	}

	_, err := NewHelperSet().Get("foo")
	wantKind(t, err, KindInvalidArgument, `The helper "foo" is not defined.`)
	eq(t, IsConsoleException(err), true, "->get() throws domain specific exception when helper not found")
}

func TestHelperSet_Iteration(t *testing.T) {
	set := NewHelperSet()
	set.Set(&mockHelper{name: "fake_helper_01"}, "")
	set.Set(&mockHelper{name: "fake_helper_02"}, "")
	helpers := []string{"fake_helper_01", "fake_helper_02"}
	all := set.All()
	eq(t, len(all), 2)
	for i, h := range all {
		eq(t, h.Name(), helpers[i])
	}
}

func TestFormatterHelper_FormatSection(t *testing.T) {
	f := &FormatterHelper{}
	eq(t, f.FormatSection("cli", "Some text to display", "info"), "<info>[cli]</info> Some text to display", "::formatSection() formats a message in a section")
}

func TestFormatterHelper_FormatBlock(t *testing.T) {
	f := &FormatterHelper{}
	eq(t, f.FormatBlock([]string{"Some text to display"}, "error", false), "<error> Some text to display </error>", "::formatBlock() formats a message in a block")
	eq(t, f.FormatBlock([]string{"Some text to display", "foo bar"}, "error", false),
		"<error> Some text to display </error>\n<error> foo bar              </error>", "::formatBlock() formats a message in a block")
	eq(t, f.FormatBlock([]string{"Some text to display"}, "error", true),
		"<error>                        </error>\n<error>  Some text to display  </error>\n<error>                        </error>", "::formatBlock() formats a message in a block")
}

func TestFormatterHelper_FormatBlockWithDiacriticLetters(t *testing.T) {
	f := &FormatterHelper{}
	eq(t, f.FormatBlock([]string{"Du texte à afficher"}, "error", true),
		"<error>                       </error>\n<error>  Du texte à afficher  </error>\n<error>                       </error>")
}

func TestFormatterHelper_FormatBlockWithDoubleWidthDiacriticLetters(t *testing.T) {
	f := &FormatterHelper{}
	eq(t, f.FormatBlock([]string{"表示するテキスト"}, "error", true),
		"<error>                    </error>\n<error>  表示するテキスト  </error>\n<error>                    </error>")
}

func TestFormatterHelper_FormatBlockLGEscaping(t *testing.T) {
	f := &FormatterHelper{}
	eq(t, f.FormatBlock([]string{"<info>some info</info>"}, "error", true),
		"<error>                              </error>\n<error>  \\<info\\>some info\\</info\\>  </error>\n<error>                              </error>", "::formatBlock() escapes '<' chars")
}

func TestFormatterHelper_TruncatingWithShorterLengthThanMessageWithSuffix(t *testing.T) {
	f := &FormatterHelper{}
	message := "testing truncate"
	eq(t, f.Truncate(message, 4, "..."), "test...")
	eq(t, f.Truncate(message, 15, "..."), "testing truncat...")
	eq(t, f.Truncate(message, 16, "..."), "testing truncate...")
	eq(t, f.Truncate("zażółć gęślą jaźń", 12, "..."), "zażółć gęślą...")
}

func TestFormatterHelper_TruncatingMessageWithCustomSuffix(t *testing.T) {
	eq(t, (&FormatterHelper{}).Truncate("testing truncate", 4, "!"), "test!")
}

func TestFormatterHelper_TruncatingWithLongerLengthThanMessageWithSuffix(t *testing.T) {
	eq(t, (&FormatterHelper{}).Truncate("test", 10, "..."), "test")
}

func TestFormatterHelper_TruncatingWithNegativeLength(t *testing.T) {
	f := &FormatterHelper{}
	eq(t, f.Truncate("testing truncate", -5, "..."), "testing tru...")
	eq(t, f.Truncate("testing truncate", -100, "..."), "...")
}

func TestHtmlOutputFormatter_Formatting(t *testing.T) {
	f := NewHTMLOutputFormatter(NamedStyle{"warning", MustStyle("black", "yellow")})
	eq(t, f.Format("text <info>green</info> <comment>yellow</comment> <warning>black w/ yellow bg</warning>"),
		`text <span style="color:green;">green</span> <span style="color:yellow;">yellow</span> <span style="color:black;background-color:yellow;">black w/ yellow bg</span>`)
}

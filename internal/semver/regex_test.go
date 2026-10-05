// Cross-checks the hand-written matchers of regex.go against regexp2
// compilations of the PHP patterns they port, with possessive quantifiers
// written as atomic groups and \s, \d spelled out as PCRE defines them.

package semver

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

const (
	reS   = ` \t\n\x0B\f\r` // PCRE's \s, for use inside a class
	reMod = `[._-]?(?:(stable|beta|b|RC|alpha|a|patch|pl|p)((?>(?:[.-]?[0-9]+)*))?)?([.-]?dev)?`
	reVR  = `v?((?>[0-9]+))(?:\.((?>[0-9]+)))?(?:\.((?>[0-9]+)))?(?:\.((?>[0-9]+)))?(?:` + reMod +
		`|\.([xX*][.-]?dev))(?:\+[^` + reS + `]+)?`
	reStabilities = `stable|RC|beta|alpha|dev`
)

func compileRef(pattern string, ci bool) *regexp2.Regexp {
	opts := regexp2.None
	if ci {
		opts = regexp2.IgnoreCase
	}

	return regexp2.MustCompile(pattern, opts)
}

var refPatterns = sync.OnceValue(func() map[string]*regexp2.Regexp {
	return map[string]*regexp2.Regexp{
		"alias":           compileRef(`^((?>[^,`+reS+`]+))(?> +)as(?> +)((?>[^,`+reS+`]+))$`, false),
		"stabilityFlag":   compileRef(`@(?:`+reStabilities+`)$`, true),
		"buildMetadata":   compileRef(`^((?>[^,`+reS+`+]+))\+(?>[^`+reS+`]+)$`, false),
		"classical":       compileRef(`^v?((?>[0-9]{1,5}))(\.(?>[0-9]+))?(\.(?>[0-9]+))?(\.(?>[0-9]+))?`+reMod+`$`, true),
		"date":            compileRef(`^v?([0-9]{4}(?:[.:-]?[0-9]{2}){1,6}(?:[.:-]?[0-9]{1,3}){0,2})`+reMod+`$`, true),
		"devSuffix":       compileRef(`(.*?)[.-]?dev$`, true),
		"numericAlias":    compileRef(`^(((?>[0-9]+)\.)*(?>[0-9]+))(?:\.x)?-dev$`, true),
		"branch":          compileRef(`^v?((?>[0-9]+))(\.(?:(?>[0-9]+)|[xX*]))?(\.(?:(?>[0-9]+)|[xX*]))?(\.(?:(?>[0-9]+)|[xX*]))?$`, true),
		"stabilitySuffix": compileRef(`^([^,`+reS+`]*?)@(`+reStabilities+`)$`, true),
		"reference":       compileRef(`#.+$`, false),
		"devReference":    compileRef(`^(dev-[^,`+reS+`@]+?|[^,`+reS+`@]+?\.x-dev)#.+$`, true),
		"wildcard":        compileRef(`^(v)?[xX*](\.[xX*])*$`, true),
		"tilde":           compileRef(`^~>?`+reVR+`$`, true),
		"caret":           compileRef(`^\^`+reVR+`($)`, true),
		"xRange":          compileRef(`^v?((?>[0-9]+))(?:\.((?>[0-9]+)))?(?:\.((?>[0-9]+)))?(?>(?:\.[xX*])+)$`, false),
		"hyphen":          compileRef(`^(`+reVR+`) +- +(`+reVR+`)($)`, true),
		"basic":           compileRef(`^(<>|!=|>=?|<=?|==?)?[`+reS+`]*(.*)`, false),
		"simpleDev":       compileRef(`^[0-9a-zA-Z./-]+$`, false),
		"dashModifier":    compileRef(`-`+reMod+`$`, false),
		"stabilityMod":    compileRef(reMod+`(?:\+.*)?$`, true),
		"splitOr":         compileRef(`[`+reS+`]*\|\|?[`+reS+`]*`, false),
		"splitAnd":        compileRef(`(?<!^|as|[=>< ,]) *(?<!-)[, ](?!-) *(?!,|as|$)`, false),
	}
})

// refGroups matches re against s and returns, for each group, its text or
// "<unset>"; nil when there is no match.
func refGroups(t testing.TB, re *regexp2.Regexp, s string) []string {
	t.Helper()
	m, err := re.FindStringMatch(s)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		return nil
	}
	groups := m.Groups()
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = "<unset>"
		if len(g.Captures) > 0 {
			out[i] = g.String()
		}
	}

	return out
}

func spanGroup(s string, sp span) string {
	if !sp.isSet() {
		return "<unset>"
	}

	return sp.text(s)
}

func modGroups(s string, c modCaps) []string {
	return []string{spanGroup(s, c.word), spanGroup(s, c.nums), spanGroup(s, c.dev)}
}

func vrGroups(s string, c vrCaps) []string {
	g := []string{spanGroup(s, c.num[0]), spanGroup(s, c.num[1]), spanGroup(s, c.num[2]), spanGroup(s, c.num[3])}
	g = append(g, modGroups(s, c.mod)...)

	return append(g, spanGroup(s, c.xDev))
}

// refSplit is preg_split() with re.
func refSplit(t testing.TB, re *regexp2.Regexp, s string) []string {
	t.Helper()
	runes := []rune(s)
	var parts []string
	last := 0
	m, err := re.FindStringMatch(s)
	for ; m != nil && err == nil; m, err = re.FindNextMatch(m) {
		parts = append(parts, string(runes[last:m.Index]))
		last = m.Index + m.Length
	}
	if err != nil {
		t.Fatal(err)
	}

	return append(parts, string(runes[last:]))
}

// crossCheck compares every matcher with its reference on s.
func crossCheck(t testing.TB, s string) {
	t.Helper()
	ref := refPatterns()
	check := func(name string, got, want []string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Errorf("%s(%q): got %q, want %q", name, s, got, want)
		}
	}
	// groups returns the reference groups from first on, nil for no match.
	groups := func(name string, first int) []string {
		if g := refGroups(t, ref[name], s); g != nil {
			return g[first:]
		}

		return nil
	}
	// matched returns the captures for a match, nil for none; they are
	// only computed for a match, as failed matches leave junk behind.
	matched := func(ok bool, g func() []string) []string {
		if !ok {
			return nil
		}

		return g()
	}
	is := func(ok bool) []string {
		if !ok {
			return nil
		}

		return []string{}
	}

	alias, ok := matchAlias(s)
	check("alias", matched(ok, func() []string { return []string{alias} }), firstOf(groups("alias", 1)))

	var wantFlag []string
	if g := groups("stabilityFlag", 0); g != nil {
		wantFlag = []string{strconv.Itoa(len(g[0]))}
	}
	n := matchStabilityFlag(s)
	check("stabilityFlag", matched(n > 0, func() []string { return []string{strconv.Itoa(n)} }), wantFlag)

	base, ok := matchBuildMetadata(s)
	check("buildMetadata", matched(ok, func() []string { return []string{base} }), firstOf(groups("buildMetadata", 1)))

	var classical classicalCaps
	ok = matchClassical(s, &classical)
	minor := func(sp span) string {
		if !sp.isSet() {
			return "<unset>"
		}

		return s[sp.start-1 : sp.end]
	}
	check("classical", matched(ok, func() []string {
		return append([]string{
			spanGroup(s, classical.major), minor(classical.minor[0]),
			minor(classical.minor[1]), minor(classical.minor[2]),
		}, modGroups(s, classical.mod)...)
	}), groups("classical", 1))

	var date dateCaps
	ok = matchDate(s, &date)
	check("date", matched(ok, func() []string { return append([]string{spanGroup(s, date.date)}, modGroups(s, date.mod)...) }), groups("date", 1))

	branch, ok := matchDevSuffix(s)
	check("devSuffix", matched(ok, func() []string { return []string{branch} }), groups("devSuffix", 1))

	end, ok := matchNumericAliasPrefix(s, 0)
	check("numericAlias", matched(ok, func() []string { return []string{s[:end]} }), firstOf(groups("numericAlias", 1)))

	var b branchCaps
	ok = matchBranch(s, &b)
	check("branch", matched(ok, func() []string {
		return []string{spanGroup(s, b[0]), spanGroup(s, b[1]), spanGroup(s, b[2]), spanGroup(s, b[3])}
	}), groups("branch", 1))

	rest, stability, ok := matchStabilitySuffix(s)
	check("stabilitySuffix", matched(ok, func() []string { return []string{rest, stability} }), groups("stabilitySuffix", 1))

	stripped, err := ref["reference"].Replace(s, "", -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	check("reference", []string{stripReference(s)}, []string{stripped})

	ref1, ok := matchDevReference(s)
	check("devReference", matched(ok, func() []string { return []string{ref1} }), groups("devReference", 1))

	ok, hasGroup := matchWildcard(s)
	nonEmpty := func(g string) bool { return g != "<unset>" && g != "" }
	if g := groups("wildcard", 1); (g != nil) != ok || (ok && hasGroup != (nonEmpty(g[0]) || nonEmpty(g[1]))) {
		t.Errorf("wildcard(%q): got %v %v, want %q", s, ok, hasGroup, g)
	}

	var vr vrCaps
	ok = matchTilde(s, &vr)
	check("tilde", matched(ok, func() []string { return vrGroups(s, vr) }), groups("tilde", 1))

	ok = matchCaret(s, &vr)
	g := groups("caret", 1)
	if g != nil {
		g = g[:len(g)-1] // ($)
	}
	check("caret", matched(ok, func() []string { return vrGroups(s, vr) }), g)

	var x xRangeCaps
	ok = matchXRange(s, &x)
	check("xRange", matched(ok, func() []string { return []string{spanGroup(s, x[0]), spanGroup(s, x[1]), spanGroup(s, x[2])} }), groups("xRange", 1))

	var h hyphenCaps
	ok = matchHyphen(s, &h)
	var gotHyphen []string
	if ok {
		gotHyphen = append(append([]string{s[:h.fromEnd]}, vrGroups(s, h.from)...), s[h.toStart:h.toEnd])
		gotHyphen = append(gotHyphen, vrGroups(s, h.to)...)
	}
	g = groups("hyphen", 1)
	if g != nil {
		g = g[:len(g)-1] // ($)
	}
	check("hyphen", gotHyphen, g)

	op, version := matchBasicComparator(s)
	if op == "" {
		op = "<unset>"
	}
	check("basic", []string{op, version}, groups("basic", 1))

	check("simpleDev", is(matchSimpleDevChars(s)), is(groups("simpleDev", 0) != nil))
	check("dashModifier", is(matchDashModifier(s)), is(groups("dashModifier", 0) != nil))

	var mod modCaps
	ok = matchStabilityModifier(s, &mod)
	check("stabilityMod", matched(ok, func() []string { return modGroups(s, mod) }), groups("stabilityMod", 1))

	check("splitOr", splitOr(s), refSplit(t, ref["splitOr"], s))
	check("splitAnd", splitAnd(s), refSplit(t, ref["splitAnd"], s))

	// The alias checks of normalize()'s error message, with every split of
	// s into a full version and a version (short inputs only: each split
	// compiles two patterns).
	for i := 0; i <= len(s) && len(s) <= 12; i++ {
		full, v := s[:i], s[i:]
		if !utf8.ValidString(full) || !utf8.ValidString(v) {
			continue // regexp2 works on runes
		}
		quoted := regexp2.Escape(v)
		aliasOf := compileRef(` +as +`+quoted+`(?:@(?:`+reStabilities+`))?$`, false)
		sourceOf := compileRef(`^`+quoted+`(?:@(?:`+reStabilities+`))? +as +`, false)
		if got, want := matchAliasOf(full, v), refGroups(t, aliasOf, full) != nil; got != want {
			t.Errorf("aliasOf(%q, %q): got %v, want %v", full, v, got, want)
		}
		if got, want := matchAliasSourceOf(full, v), refGroups(t, sourceOf, full) != nil; got != want {
			t.Errorf("aliasSourceOf(%q, %q): got %v, want %v", full, v, got, want)
		}
	}
}

func firstOf(g []string) []string {
	if g == nil {
		return nil
	}

	return g[:1]
}

// fuzzTokens make up the generated inputs: the pieces the patterns care
// about, plus bytes they must treat as ordinary ones.
var fuzzTokens = []string{
	"0", "1", "2", "00", "12", "123", "2010", "12345", "999999", ".", ".", ".", "-", "_", ":", "+", "#", "@", ",", " ", "  ",
	"\t", "\n", "\x0b", "\f", "\r", "|", "||", "~", "~>", "^", "*", "x", "X", "v", "V", "<", ">", "=", "!", "/", "as",
	" as ", "dev", "DEV", "dev-", "-dev", "beta", "b", "RC", "rc", "alpha", "a", "patch", "pl", "p", "stable", "STABLE",
	"foo", "é", " ", "\u0085", "\x00",
}

// tokenString builds an input from fuzzer bytes, one token per byte.
func tokenString(data []byte) string {
	var b strings.Builder
	for _, c := range data {
		b.WriteString(fuzzTokens[int(c)%len(fuzzTokens)])
	}

	return b.String()
}

func TestRegex_CrossCheck(t *testing.T) {
	inputs := oracleInputs(t)
	r := rand.New(rand.NewPCG(1, 2))
	for range 5000 {
		data := make([]byte, r.IntN(12))
		for i := range data {
			data[i] = byte(r.IntN(256))
		}
		inputs = append(inputs, tokenString(data))
	}
	slices.Sort(inputs)
	inputs = slices.Compact(inputs)
	for i, s := range inputs {
		// The race detector has nothing to find here and slows regexp2
		// down tenfold.
		if raceEnabled && i%10 != 0 {
			continue
		}
		crossCheck(t, s)
		if t.Failed() {
			return
		}
	}
	t.Logf("%d inputs", len(inputs))
}

func FuzzRegex(f *testing.F) {
	for _, s := range []string{"1.0.0", "~1.2-beta", "1.0 - 2.0", "dev-master#abc", ">=1.0,<2.0 || ^3"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 24 {
			return
		}
		crossCheck(t, tokenString(data))
	})
}

// oracleInputs returns the version and constraint strings of the oracle
// corpus.
func oracleInputs(t testing.TB) []string {
	t.Helper()
	var inputs []string
	var versions []struct {
		V string `json:"v"`
	}
	var constraints []struct {
		C string `json:"c"`
	}
	for name, dst := range map[string]any{"versions.json": &versions, "constraints.json": &constraints} {
		data, err := os.ReadFile("testdata/oracle/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, dst); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range versions {
		inputs = append(inputs, v.V)
	}
	for _, c := range constraints {
		inputs = append(inputs, c.C)
		// What parseConstraint() sees.
		for _, or := range splitOr(phpTrim(c.C)) {
			inputs = append(inputs, splitAnd(or)...)
		}
	}

	return inputs
}

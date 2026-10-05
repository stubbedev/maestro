package php

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Workloads of the regex engine evaluation (internal/php/doc.go): the
// Composer pattern corpus, its semver and class-map-generator subsets,
// JsonManipulator's recursive patterns over a large composer.json, and
// compilation.

type benchPattern struct {
	pattern  string
	sources  []string
	subjects []string
}

var benchCorpus = sync.OnceValue(func() []benchPattern {
	data, err := os.ReadFile("testdata/preg/corpus.json")
	if err != nil {
		panic(err)
	}
	var c struct {
		Patterns []struct {
			Pattern  any      `json:"pattern"`
			Sources  []string `json:"sources"`
			Subjects []any    `json:"subjects"`
			Logged   []any    `json:"logged"`
		} `json:"patterns"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		panic(err)
	}
	var out []benchPattern
	for _, p := range c.Patterns {
		bp := benchPattern{pattern: goldenString(p.Pattern), sources: p.Sources}
		for _, s := range append(p.Subjects, p.Logged...) {
			bp.subjects = append(bp.subjects, goldenString(s))
		}
		if _, err := Compile(bp.pattern); err != nil {
			continue
		}
		out = append(out, bp)
	}
	return out
})

func benchSelect(src string) []benchPattern {
	var out []benchPattern
	for _, p := range benchCorpus() {
		for _, s := range p.sources {
			if strings.Contains(s, src) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

func runCorpus(b *testing.B, ps []benchPattern) {
	res := make([]*Regexp, len(ps))
	for i, p := range ps {
		res[i] = MustCompile(p.pattern)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for i, p := range ps {
			re := res[i]
			for _, s := range p.subjects {
				_, _ = re.Match(s)
				_, _ = re.MatchAll(s)
				_, _, _ = re.Replace(s, "<$0>", -1)
			}
		}
	}
}

// BenchmarkPregCorpus runs preg_match, preg_match_all and preg_replace of
// every Composer pattern on its subjects.
func BenchmarkPregCorpus(b *testing.B)        { runCorpus(b, benchCorpus()) }
func BenchmarkPregSemver(b *testing.B)        { runCorpus(b, benchSelect("semver/src/")) }
func BenchmarkPregClassmapSmall(b *testing.B) { runCorpus(b, benchSelect("class-map-generator/")) }

// jsonManipulatorWorkload is testdata/preg/jsonmanipulator: JsonManipulator's
// recursive (?&json) patterns, a 66 KB composer.json, and PHP's results
// (tools/oracle/php/preg_jsonmanipulator.php).
type jsonManipulatorWorkload struct {
	subject  string
	patterns []string
	results  []struct {
		R      any     `json:"r"`
		Err    string  `json:"err"`
		Groups [][]int `json:"groups"`
	}
	php, pcre string
}

func loadJSONManipulator(tb testing.TB) *jsonManipulatorWorkload {
	tb.Helper()
	dir := "testdata/preg/jsonmanipulator/"
	w := &jsonManipulatorWorkload{}
	subject, err := os.ReadFile(dir + "composer.json")
	if err != nil {
		tb.Fatal(err)
	}
	w.subject = string(subject)
	data, err := os.ReadFile(dir + "patterns.json")
	if err != nil {
		tb.Fatal(err)
	}
	if err := json.Unmarshal(data, &w.patterns); err != nil {
		tb.Fatal(err)
	}
	var golden struct {
		PHP     string `json:"php"`
		PCRE    string `json:"pcre"`
		Results json.RawMessage
	}
	data, err = os.ReadFile(dir + "golden.json")
	if err != nil {
		tb.Fatal(err)
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		tb.Fatal(err)
	}
	if err := json.Unmarshal(golden.Results, &w.results); err != nil {
		tb.Fatal(err)
	}
	if len(w.results) != len(w.patterns) {
		tb.Fatalf("%d results for %d patterns", len(w.results), len(w.patterns))
	}
	w.php, w.pcre = golden.PHP, golden.PCRE
	return w
}

// TestPregJsonManipulatorGolden checks preg_match of JsonManipulator's
// patterns on a 66 KB composer.json against PHP, including which of them
// exhaust the backtrack limit.
func TestPregJsonManipulatorGolden(t *testing.T) {
	t.Parallel()
	w := loadJSONManipulator(t)
	limits := 0
	for i, p := range w.patterns {
		want := w.results[i]
		m, err := MustCompile(p).MatchAt(w.subject, 0)
		got := PregErrorMsg(PregNoError)
		if err != nil {
			var pe *PcreError
			if !errors.As(err, &pe) {
				t.Fatalf("pattern %d: %v", i, err)
			}
			got = PregErrorMsg(pe.Code)
			limits++
		}
		if got != want.Err {
			t.Errorf("pattern %d: error %q, want %q", i, got, want.Err)
			continue
		}
		if err != nil {
			continue
		}
		if matched := m != nil; matched != (want.R == float64(1)) {
			t.Errorf("pattern %d: matched %v, want %v", i, matched, want.R)
			continue
		}
		if m == nil {
			continue
		}
		if m.Groups() != len(want.Groups) {
			t.Errorf("pattern %d: %d groups, want %d", i, m.Groups(), len(want.Groups))
			continue
		}
		for g, wg := range want.Groups {
			st, en := m.offs[2*g], m.offs[2*g+1]
			if wg == nil && st >= 0 || wg != nil && (st != wg[0] || en-st != wg[1]) {
				t.Errorf("pattern %d group %d: [%d, %d), want %v", i, g, st, en, wg)
			}
		}
	}
	t.Logf("%d patterns, %d limit errors, PHP %s, PCRE2 %s", len(w.patterns), limits, w.php, w.pcre)
}

// BenchmarkPregJsonManipulator times the JsonManipulator workload, split
// into the patterns that complete and those that exhaust the backtrack
// limit.
func BenchmarkPregJsonManipulator(b *testing.B) {
	w := loadJSONManipulator(b)
	for _, limit := range []bool{false, true} {
		var sel []*Regexp
		for i, p := range w.patterns {
			if (w.results[i].Err != PregErrorMsg(PregNoError)) == limit {
				sel = append(sel, MustCompile(p))
			}
		}
		name := "completing"
		if limit {
			name = "backtrack-limit"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for _, re := range sel {
					_, _ = re.MatchAt(w.subject, 0)
				}
			}
		})
	}
}

// BenchmarkPregClassmapLarge runs the class-map-generator patterns over
// the largest PHP files of Composer (in .ref, see docs/PORTING.md).
func BenchmarkPregClassmapLarge(b *testing.B) {
	files, _ := filepath.Glob("../../.ref/composer/src/Composer/*/*.php")
	var subjects []string
	for _, f := range files {
		d, err := os.ReadFile(f)
		if err != nil {
			b.Fatal(err)
		}
		if len(d) > 30000 {
			subjects = append(subjects, string(d))
		}
	}
	if len(subjects) == 0 {
		b.Skip("no .ref/composer sources")
	}
	var res []*Regexp
	for _, p := range benchSelect("class-map-generator/") {
		res = append(res, MustCompile(p.pattern))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, re := range res {
			for _, s := range subjects {
				if _, err := re.MatchAll(s); err != nil {
					b.Fatal(re.String(), err)
				}
			}
		}
	}
}

// BenchmarkPregCompile compiles every corpus pattern, bypassing the cache.
func BenchmarkPregCompile(b *testing.B) {
	ps := benchCorpus()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, p := range ps {
			if _, err := compile(p.pattern); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkPregSingleSemverMatch(b *testing.B) {
	re := MustCompile(`{^v?(\d{1,5}+)(\.\d++)?(\.\d++)?(\.\d++)?[._-]?(?:(stable|beta|b|RC|alpha|a|patch|pl|p)((?:[.-]?\d+)*+)?)?([.-]?dev)?$}i`)
	b.ReportAllocs()
	for b.Loop() {
		if m, _ := re.Match("1.2.3-beta2"); m == nil {
			b.Fatal("no match")
		}
	}
}

package php

import (
	"encoding/json"
	"os"
	"runtime"
	"sync"
	"testing"
	"unicode/utf8"
)

// countTolerance bounds the difference from PHP's count. PCRE2 skips
// start positions where no match can begin with heuristics of its own
// (the JIT's two-character search, pcre2_match's start bitmap); this
// engine skips such positions differently, and a skipped position counts
// at most a few frames before it fails.
const countTolerance = 3

// interpStartSlack is the count below which the pcre2_match counts of
// the start positions it skips and this engine tries may make up the
// difference.
const interpStartSlack = 50

// TestPregMatchLimitCounts checks how far each golden case gets against
// pcre.backtrack_limit, with the JIT and with pcre2_match, against PHP
// (tools/oracle/php/preg_counts.php).
func TestPregMatchLimitCounts(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/preg/counts.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Counts [][2]int `json:"counts"`
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	type countCase struct{ pattern, subject string }
	var cases []countCase
	for _, file := range []string{"testdata/preg/golden.json", "testdata/preg/engine_golden.json"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var g pregGoldenFile
		if err := json.Unmarshal(data, &g); err != nil {
			t.Fatal(err)
		}
		for _, p := range g.Patterns {
			if !p.Valid {
				continue
			}
			for _, r := range p.Results {
				cases = append(cases, countCase{goldenString(p.Pattern), goldenString(r["subject"])})
			}
		}
	}
	w := loadJSONManipulator(t)
	for _, p := range w.patterns {
		cases = append(cases, countCase{p, w.subject})
	}
	if len(cases) != len(golden.Counts) {
		t.Fatalf("%d cases, %d counts", len(cases), len(golden.Counts))
	}
	type result struct {
		got  [2]int
		code [2]int
	}
	results := make([]result, len(cases))
	var wg sync.WaitGroup
	next := make(chan int)
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range next {
				c := cases[i]
				re := MustCompile(c.pattern)
				if re.utf && !utf8.ValidString(c.subject) {
					results[i].code = [2]int{-1, -1}
					continue
				}
				for model := range 2 {
					m := re.getMachine(c.subject)
					_, results[i].code[model] = m.execModel(0, false, false, model == 1)
					results[i].got[model] = m.peak
					re.putMachine(m)
				}
			}
		})
	}
	for i := range cases {
		next <- i
	}
	close(next)
	wg.Wait()
	failures := 0
	for i, c := range cases {
		for model, want := range golden.Counts[i] {
			got, code := results[i].got[model], results[i].code[model]
			if code < 0 || code == matchErrRecursion {
				// An invalid subject, or PHP's JIT running out of stack
				// first (left recursion).
				continue
			}
			limited := code == matchErrBacktrack
			ok := limited == (want < 0 || want > matchLimit)
			if ok && !limited {
				ok = got >= want-countTolerance && got <= want+countTolerance
				if model == 1 {
					// pcre2_match also skips start positions with a
					// first code unit and a start bitmap that this engine
					// does not reproduce; PHP uses this count only for
					// the anchored retry, at one position.
					ok = ok || got < interpStartSlack && want < interpStartSlack
				}
			}
			if !ok {
				failures++
				if failures <= 20 {
					t.Errorf("%.80q on %.40q (interp=%v): count %d (limit %v), PHP %d", c.pattern, c.subject, model == 1, got, limited, want)
				}
			}
		}
	}
	if failures > 0 {
		t.Errorf("%d failures", failures)
	}
}

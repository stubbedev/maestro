package php

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"unicode/utf8"
)

// The goldens are written by tools/oracle/php/preg_golden.php; see there
// for the format. They are compared as decoded JSON values.

type pregGoldenFile struct {
	PHPVersion  string `json:"php_version"`
	PCREVersion string `json:"pcre_version"`
	Patterns    []struct {
		Pattern      any    `json:"pattern"`
		Valid        bool   `json:"valid"`
		CompileError string `json:"compile_error"`
		Results      []map[string]any
	} `json:"patterns"`
}

// goldenString decodes a golden string (text or {"base64": ...}).
func goldenString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case map[string]any:
		b, err := base64.StdEncoding.DecodeString(v["base64"].(string))
		if err != nil {
			panic(err)
		}
		return string(b)
	}
	panic(fmt.Sprintf("not a golden string: %#v", v))
}

// encString encodes a string as the goldens do.
func encString(s string) any {
	if utf8.ValidString(s) {
		return s
	}
	return map[string]any{"base64": base64.StdEncoding.EncodeToString([]byte(s))}
}

// encValue encodes a PHP value from a match array.
func encValue(v any) any {
	if s, ok := v.(string); ok {
		return encString(s)
	}
	return v
}

func keyJSON(k Key) any {
	if k.IsInt() {
		return k.Int()
	}
	return k.String()
}

// opResult builds {"error": msg|null, "result": ...} like the oracle.
func opResult(err error, result func() any) map[string]any {
	if err != nil {
		code := PregInternalError
		if pe, ok := err.(*PcreError); ok { //nolint:errorlint // direct errors only
			code = pe.Code
		}
		return map[string]any{"error": PregErrorMsg(code), "result": nil}
	}
	return map[string]any{"error": nil, "result": result()}
}

// normalizeJSON round-trips v through JSON so it compares with decoded
// goldens.
func normalizeJSON(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func pregOps(re *Regexp, subject string) map[string]any {
	pairs := func(r int, m *Array) any {
		ps := []any{}
		for k, v := range m.All() {
			ps = append(ps, []any{keyJSON(k), encValue(v)})
		}
		return []any{r, ps}
	}
	ops := map[string]any{}
	r, m, err := re.MatchArray(subject, 0, 0)
	ops["match"] = opResult(err, func() any { return pairs(r, m) })
	r2, m2, err := re.MatchArray(subject, 0, 2)
	ops["match_at"] = opResult(err, func() any { return pairs(r2, m2) })
	_, all, err := re.MatchAllArray(subject, PregSetOrder|PregUnmatchedAsNull|PregOffsetCapture, 0)
	ops["match_all"] = opResult(err, func() any {
		sets := []any{}
		for _, set := range all.All() {
			triples := []any{}
			for k, pair := range set.(*Array).All() {
				vs := pair.(*Array).Values()
				triples = append(triples, []any{keyJSON(k), encValue(vs[0]), vs[1]})
			}
			sets = append(sets, triples)
		}
		return sets
	})
	for name, limit := range map[string]int{"replace": -1, "replace1": 1} {
		res, count, err := re.Replace(subject, goldenReplacement, limit)
		ops[name] = opResult(err, func() any { return []any{encString(res), count} })
	}
	for name, limit := range map[string]int{"split": -1, "split2": 2} {
		parts, err := re.Split(subject, limit, 0)
		ops[name] = opResult(err, func() any {
			out := []any{}
			for _, p := range parts {
				out = append(out, encString(p))
			}
			return out
		})
	}
	pieces, err := re.SplitWithOffsets(subject, -1, PregSplitDelimCapture|PregSplitNoEmpty)
	ops["split_flags"] = opResult(err, func() any {
		out := []any{}
		for _, p := range pieces {
			out = append(out, []any{encString(p.Value), p.Offset})
		}
		return out
	})
	return ops
}

const goldenReplacement = `<$0|\1|${2}|$10>`

func testPregGolden(t *testing.T, file string) {
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var g pregGoldenFile
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	// The subjects run concurrently; their results are compared in order.
	type job struct {
		re      *Regexp
		subject string
		got     map[string]any
	}
	var jobs []*job
	failures, subjects := 0, 0
	report := func(format string, args ...any) {
		failures++
		if failures <= 40 {
			t.Errorf(format, args...)
		}
	}
	for _, p := range g.Patterns {
		pattern := goldenString(p.Pattern)
		re, err := Compile(pattern)
		if (err == nil) != p.Valid {
			report("%q: compile error %v, want valid=%v (%s)", pattern, err, p.Valid, p.CompileError)
			continue
		}
		for _, r := range p.Results {
			jobs = append(jobs, &job{re: re, subject: goldenString(r["subject"])})
		}
	}
	next := make(chan *job)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for j := range next {
				j.got = pregOps(j.re, j.subject)
			}
		})
	}
	for _, j := range jobs {
		next <- j
	}
	close(next)
	wg.Wait()
	k := 0
	for _, p := range g.Patterns {
		if _, err := Compile(goldenString(p.Pattern)); err != nil {
			continue
		}
		pattern := goldenString(p.Pattern)
		for _, r := range p.Results {
			j := jobs[k]
			k++
			subjects++
			got := normalizeJSON(t, j.got).(map[string]any)
			for name, want := range r {
				if name == "subject" {
					continue
				}
				if g := got[name]; !reflect.DeepEqual(g, want) {
					gj, _ := json.Marshal(g)
					wj, _ := json.Marshal(want)
					report("%q %s %q:\n got %s\nwant %s", pattern, name, j.subject, gj, wj)
				}
			}
		}
	}
	if failures > 0 {
		t.Errorf("%d failures", failures)
	}
	t.Logf("%d patterns, %d subjects, PHP %s, PCRE2 %s", len(g.Patterns), subjects, g.PHPVersion, g.PCREVersion)
}

// TestPregGolden runs every pattern Composer uses (collected by
// tools/oracle/php/preg_collect.php) against PHP's results.
func TestPregGolden(t *testing.T) {
	t.Parallel()
	testPregGolden(t, "testdata/preg/golden.json")
}

// TestPregEngineGolden runs the engine feature corpus of
// tools/oracle/php/preg_engine.php against PHP's results.
func TestPregEngineGolden(t *testing.T) {
	t.Parallel()
	testPregGolden(t, "testdata/preg/engine_golden.json")
}

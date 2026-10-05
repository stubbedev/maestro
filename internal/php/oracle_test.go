package php

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Tests against the goldens of tools/oracle/php/values.php.

func loadOracle(t *testing.T, name string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/oracle/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []map[string]any
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

// unser decodes a golden serialized value.
func unser(t *testing.T, v any) any { return phpUnserialize(t, goldenString(v)) }

// catch runs fn and returns its panic message, if any.
func catch(fn func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	fn()
	return ""
}

// withNext is a result as the oracle records it: with 'next' appended to
// show the next free index.
func withNext(a *Array) string {
	a = a.shallowCopy()
	a.Append("next")
	return phpSerialize(a)
}

// golden returns the expected serialized value or error of an attempt().
func golden(v any) (value string, errMsg string) {
	if m, ok := v.(map[string]any); ok {
		if e, ok := m["error"]; ok {
			return "", fmt.Sprint(e)
		}
	}
	return goldenString(v), ""
}

func TestOracleFloats(t *testing.T) {
	for _, c := range loadOracle(t, "floats") {
		f := unser(t, c["value"]).(float64)
		if got := FloatToString(f); got != c["string"] {
			t.Errorf("(string) %v: got %q, want %q", f, got, c["string"])
		}
		if got := VarExport(f); got != c["var_export"] {
			t.Errorf("var_export %v: got %q, want %q", f, got, c["var_export"])
		}
		for name, flags := range map[string]JSONFlag{"json": 0, "json_zero_fraction": JSONPreserveZeroFraction} {
			got, err := JSONEncode(f, flags)
			want, wantErr := golden(c[name])
			if err != nil && err.Error() != wantErr || err == nil && got != want {
				t.Errorf("json_encode(%v, %s): got %q %v, want %q %q", f, name, got, err, want, wantErr)
			}
		}
		if got := strconv.FormatInt(ToInt(f), 10); got != c["int"] {
			t.Errorf("(int) %v: got %s, want %v", f, got, c["int"])
		}
	}
}

func TestOracleNumericStrings(t *testing.T) {
	for _, c := range loadOracle(t, "strings_numeric") {
		s := goldenString(c["value"])
		if got := strconv.FormatInt(ToInt(s), 10); got != c["int"] {
			t.Errorf("(int) %q: got %s, want %v", s, got, c["int"])
		}
		if got, want := phpSerialize(ToFloat(s)), goldenString(c["float"]); got != want {
			t.Errorf("(float) %q: got %s, want %s", s, got, want)
		}
		if got := ToBool(s); got != c["bool"] {
			t.Errorf("(bool) %q: got %v", s, got)
		}
		if got := IsNumeric(s); got != c["is_numeric"] {
			t.Errorf("is_numeric(%q): got %v", s, got)
		}
		if got, want := phpSerialize(StrKey(s).Value()), goldenString(c["key"]); got != want {
			t.Errorf("key %q: got %s, want %s", s, got, want)
		}
	}
}

func TestOracleCompare(t *testing.T) {
	cases := loadOracle(t, "compare")
	values := make([]any, len(cases))
	for i, c := range cases {
		values[i] = unser(t, c["value"])
	}
	for i, c := range cases {
		for j, r := range c["results"].([]any) {
			row := r.([]any)
			a, b := values[i], values[j]
			if got := Compare(a, b); float64(got) != row[0].(float64) {
				t.Errorf("%s <=> %s: got %d, want %v", phpSerialize(a), phpSerialize(b), got, row[0])
			}
			if got := LooseEquals(a, b); got != row[1] {
				t.Errorf("%s == %s: got %v", phpSerialize(a), phpSerialize(b), got)
			}
			if got := StrictEquals(a, b); got != row[2] {
				t.Errorf("%s === %s: got %v", phpSerialize(a), phpSerialize(b), got)
			}
		}
	}
}

var oracleSortFlags = map[string]SortFlag{
	"regular": SortRegular, "numeric": SortNumeric, "string": SortString, "string_case": SortString | SortFlagCase,
	"natural": SortNatural, "natural_case": SortNatural | SortFlagCase, "locale": SortLocaleString,
}

var oracleSortFuncs = map[string]func(*Array, SortFlag){
	"sort": Sort, "rsort": Rsort, "asort": Asort, "arsort": Arsort, "ksort": Ksort, "krsort": Krsort,
}

func TestOracleSort(t *testing.T) {
	jsonString := func(v any) string {
		s, err := JSONEncode(v, 0)
		if err != nil {
			return ""
		}
		return s
	}
	for _, c := range loadOracle(t, "sort") {
		for name, want := range c["results"].(map[string]any) {
			a := unser(t, c["value"]).(*Array)
			fn, flag, _ := strings.Cut(name, "/")
			switch {
			case fn == "usort" && flag == "spaceship":
				Usort(a, Compare)
			case fn == "usort" && flag == "zero":
				Usort(a, func(any, any) int { return 0 })
			case fn == "usort" && flag == "intransitive":
				Usort(a, func(x, _ any) int {
					if _, ok := x.(int64); ok {
						return -1
					}
					return 1
				})
			case fn == "uasort":
				Uasort(a, func(x, y any) int { return strings.Compare(jsonString(x), jsonString(y)) })
			case fn == "uksort":
				Uksort(a, func(x, y Key) int { return Strnatcasecmp(x.String(), y.String()) })
			default:
				oracleSortFuncs[fn](a, oracleSortFlags[flag])
				a.Append("next")
			}
			if got, want := phpSerialize(a), goldenString(want); got != want {
				t.Errorf("%s(%s):\n got %s\nwant %s", name, goldenString(c["value"]), got, want)
			}
		}
	}
}

func TestOracleArrayBinary(t *testing.T) {
	ops := map[string]func(x, y *Array) *Array{
		"array_merge":             func(x, y *Array) *Array { return ArrayMerge(x, y) },
		"array_merge_recursive":   func(x, y *Array) *Array { return ArrayMergeRecursive(x, y) },
		"array_replace":           func(x, y *Array) *Array { return ArrayReplace(x, y) },
		"array_replace_recursive": func(x, y *Array) *Array { return ArrayReplaceRecursive(x, y) },
		"array_diff":              func(x, y *Array) *Array { return ArrayDiff(x, y) },
		"array_intersect":         func(x, y *Array) *Array { return ArrayIntersect(x, y) },
		"array_diff_key":          func(x, y *Array) *Array { return ArrayDiffKey(x, y) },
		"array_intersect_key":     func(x, y *Array) *Array { return ArrayIntersectKey(x, y) },
		"array_diff_assoc":        func(x, y *Array) *Array { return ArrayDiffAssoc(x, y) },
		"array_combine": func(x, y *Array) *Array {
			return ArrayCombine(ArrayMap(x, func(v any) any {
				if _, ok := v.(*Array); ok {
					return "arr"
				}
				return v
			}), y)
		},
	}
	for _, c := range loadOracle(t, "array_binary") {
		for name, want := range c["results"].(map[string]any) {
			x, y := unser(t, c["x"]).(*Array), unser(t, c["y"]).(*Array)
			xs, ys := phpSerialize(x), phpSerialize(y)
			var got string
			msg := catch(func() { got = withNext(ops[name](x, y)) })
			wantValue, wantErr := golden(want)
			if msg != wantErr || got != wantValue {
				t.Errorf("%s(%s, %s):\n got %s %s\nwant %s %s", name, xs, ys, got, msg, wantValue, wantErr)
			}
			if phpSerialize(x) != xs || phpSerialize(y) != ys {
				t.Errorf("%s modified its arguments", name)
			}
		}
	}
}

func TestOracleArrayUnary(t *testing.T) {
	maxOrNull := func(s string) int {
		if s == "null" {
			return math.MaxInt
		}
		n, _ := strconv.Atoi(s)
		return n
	}
	needles := []any{int64(1), "1", "x", nil, true, int64(0), "01", 1.0}
	for _, c := range loadOracle(t, "array_unary") {
		x := unser(t, c["x"]).(*Array)
		xs := phpSerialize(x)
		for name, want := range c["results"].(map[string]any) {
			a := unser(t, c["x"]).(*Array)
			var got any
			msg := catch(func() {
				parts := strings.Split(name, "/")
				switch parts[0] {
				case "array_unique":
					got = withNext(ArrayUnique(a))
				case "array_flip":
					got = withNext(ArrayFlip(a))
				case "array_values":
					got = withNext(ArrayValues(a))
				case "array_keys":
					got = withNext(ArrayKeys(a))
				case "array_reverse":
					got = withNext(ArrayReverse(a, false))
				case "array_reverse_keys":
					got = withNext(ArrayReverse(a, true))
				case "array_is_list":
					got = a.IsList()
				case "array_fill_keys":
					got = withNext(ArrayFillKeys(ArrayMap(a, func(v any) any {
						if _, ok := v.(*Array); ok {
							return "arr"
						}
						return v
					}), int64(0)))
				case "array_filter":
					got = withNext(ArrayFilter(a, nil))
				case "array_chunk2":
					got = phpSerialize(ArrayChunk(a, 2, false))
				case "array_chunk2_keys":
					got = phpSerialize(ArrayChunk(a, 2, true))
				case "array_pad":
					got = withNext(ArrayPad(a, 7, "p"))
				case "array_pad_left":
					got = withNext(ArrayPad(a, -7, "p"))
				case "array_slice":
					got = withNext(ArraySlice(a, maxOrNull(parts[1]), maxOrNull(parts[2]), false))
				case "array_slice_keys":
					got = withNext(ArraySlice(a, maxOrNull(parts[1]), maxOrNull(parts[2]), true))
				case "array_splice":
					removed := ArraySplice(a, maxOrNull(parts[1]), maxOrNull(parts[2]), "r1", "r2")
					a.Append("next")
					removed.Append("next")
					got = phpSerialize(ListOf(a, removed))
				case "array_pop":
					v, _ := a.Pop()
					a.Append("next")
					got = phpSerialize(ListOf(v, a))
				case "array_shift":
					v, _ := a.Shift()
					a.Append("next")
					got = phpSerialize(ListOf(v, a))
				case "array_unshift":
					a.Unshift("u1", "u2")
					a.Append("next")
					got = phpSerialize(a)
				case "search":
					i, _ := strconv.Atoi(parts[1])
					n := needles[i]
					search := func(strict bool) any {
						if k, ok := ArraySearch(n, a, strict); ok {
							return k.Value()
						}
						return false
					}
					got = phpSerialize(ListOf(search(false), search(true), InArray(n, a, false), InArray(n, a, true)))
				default:
					t.Fatalf("unknown op %s", name)
				}
			})
			if b, ok := want.(bool); ok {
				if got != b {
					t.Errorf("%s(%s): got %v, want %v", name, xs, got, b)
				}
				continue
			}
			wantValue, wantErr := golden(want)
			if msg != wantErr || got != wantValue && wantErr == "" {
				t.Errorf("%s(%s):\n got %v %s\nwant %s %s", name, xs, got, msg, wantValue, wantErr)
			}
		}
		if phpSerialize(x) != xs {
			t.Errorf("unary ops modified their argument")
		}
	}
}

func TestOracleArrayKeysNext(t *testing.T) {
	for _, c := range loadOracle(t, "array_keys_next") {
		script := unser(t, c["script"]).(*Array)
		a := NewArray()
		for _, op := range script.All() {
			vs := op.(*Array).Values()
			switch vs[0] {
			case "set":
				a.Set(vs[1], "v")
			case "append":
				a.Append("next")
			case "unset":
				a.Delete(vs[1])
			case "pop":
				a.Pop()
			case "shift":
				a.Shift()
			}
		}
		if got, want := phpSerialize(a), goldenString(c["result"]); got != want {
			t.Errorf("%s:\n got %s\nwant %s", phpSerialize(script), got, want)
		}
	}
}

func TestOracleJSONDecode(t *testing.T) {
	modes := map[string]struct {
		assoc bool
		flags JSONFlag
		depth int
	}{
		"assoc": {true, 0, 512}, "object": {false, 0, 512}, "bigint": {true, JSONBigintAsString, 512}, "ignore": {true, JSONInvalidUTF8Ignore, 512},
		"substitute": {true, JSONInvalidUTF8Substitute, 512}, "object_bigint": {false, JSONBigintAsString, 512},
		"depth1": {true, 0, 1}, "depth2": {true, 0, 2}, "depth3": {true, 0, 3},
	}
	for _, c := range loadOracle(t, "json_decode") {
		in := goldenString(c["input"])
		for name, want := range c["results"].(map[string]any) {
			m := modes[name]
			flags := m.flags
			if m.assoc {
				flags |= JSONObjectAsArray
			}
			v, err := JSONDecodeFlags(in, flags, m.depth)
			w := want.(map[string]any)
			if e, ok := w["error"]; ok {
				if err == nil || err.Error() != e {
					t.Errorf("json_decode(%q) %s: got %v %v, want error %v", in, name, v, err, e)
				}
				continue
			}
			if err != nil {
				t.Errorf("json_decode(%q) %s: error %v", in, name, err)
				continue
			}
			if got, want := phpSerialize(v), goldenString(w["value"]); got != want {
				t.Errorf("json_decode(%q) %s:\n got %s\nwant %s", in, name, got, want)
			}
		}
	}
}

var oracleEncodeFlags = map[string]JSONFlag{
	"none": 0, "pretty": JSONPrettyPrint, "composer": JSONPrettyPrint | JSONUnescapedSlashes | JSONUnescapedUnicode,
	"slashes_unicode": JSONUnescapedSlashes | JSONUnescapedUnicode, "zero": JSONPreserveZeroFraction, "force": JSONForceObject,
	"force_pretty": JSONForceObject | JSONPrettyPrint, "hex": JSONHexTag | JSONHexAmp | JSONHexApos | JSONHexQuot,
	"ignore": JSONInvalidUTF8Ignore, "substitute": JSONInvalidUTF8Substitute, "substitute_unicode": JSONInvalidUTF8Substitute | JSONUnescapedUnicode,
	"partial": JSONPartialOutputOnError, "numeric": JSONNumericCheck, "line_terminators": JSONUnescapedUnicode | JSONUnescapedLineTerminators,
	"throw": JSONThrowOnError,
}

func TestOracleJSONEncode(t *testing.T) {
	for _, c := range loadOracle(t, "json_encode") {
		v := unser(t, c["value"])
		for name, want := range c["results"].(map[string]any) {
			flags, depth := oracleEncodeFlags[name], JSONDefaultDepth
			if strings.HasPrefix(name, "depth") {
				depth, _ = strconv.Atoi(name[5:])
			}
			got, err := JSONEncodeDepth(v, flags, depth)
			w := want.(map[string]any)
			wantMsg := fmt.Sprint(w["error"])
			r, hasResult := w["result"]
			gotMsg := "No error"
			if err != nil {
				gotMsg = err.Error()
			}
			switch {
			case !hasResult: // JsonException
				if err == nil || gotMsg != wantMsg {
					t.Errorf("json_encode(%s, %s): got %q %v, want exception %s", goldenString(c["value"]), name, got, err, wantMsg)
				}
			case r == false:
				if err == nil || flags&JSONPartialOutputOnError != 0 || gotMsg != wantMsg {
					t.Errorf("json_encode(%s, %s): got %q %v, want false (%s)", goldenString(c["value"]), name, got, err, wantMsg)
				}
			default:
				if got != goldenString(r) || gotMsg != wantMsg {
					t.Errorf("json_encode(%s, %s):\n got %q %s\nwant %q %s", goldenString(c["value"]), name, got, gotMsg, goldenString(r), wantMsg)
				}
			}
		}
	}
}

func TestOracleVarExport(t *testing.T) {
	for _, c := range loadOracle(t, "var_export") {
		v := unser(t, c["value"])
		if got, want := VarExport(v), goldenString(c["var_export"]); got != want {
			t.Errorf("var_export(%s):\n got %q\nwant %q", goldenString(c["value"]), got, want)
		}
	}
}

func TestOracleStrings(t *testing.T) {
	pads := []struct {
		n   int
		pad string
		typ int
	}{{10, " ", StrPadRight}, {10, "-=", StrPadLeft}, {11, "ab", StrPadBoth}, {2, "x", StrPadBoth}, {-1, "x", StrPadLeft}, {5, "", StrPadRight}}
	wraps := []struct {
		w   int
		brk string
		cut bool
	}{{5, "\n", false}, {5, "\n", true}, {10, "<br>", false}, {3, "--", true}, {1, " ", false}, {0, "\n", false}, {0, "\n", true}, {75, "\n", false}, {5, "", false}, {8, "oo", true}}
	substrs := [][2]int{{0, math.MaxInt}, {1, math.MaxInt}, {-2, math.MaxInt}, {1, 2}, {1, -1}, {-3, 2}, {100, math.MaxInt}, {0, -100}, {-100, 2}}
	pairs := map[string]string{"a": "A", "ab": "X", "hello": "bye", "": "E", " ": "_"}
	for _, c := range loadOracle(t, "strings") {
		s := goldenString(c["value"])
		for name, want := range c["results"].(map[string]any) {
			var got string
			msg := catch(func() {
				op, arg, _ := strings.Cut(name, "/")
				i, _ := strconv.Atoi(arg)
				switch op {
				case "strtolower":
					got = Strtolower(s)
				case "strtoupper":
					got = Strtoupper(s)
				case "ucfirst":
					got = Ucfirst(s)
				case "lcfirst":
					got = Lcfirst(s)
				case "ucwords":
					got = Ucwords(s, UcwordsDelimiters)
				case "ucwords_dash":
					got = Ucwords(s, "-")
				case "ucwords_empty":
					got = Ucwords(s, "")
				case "trim", "ltrim", "rtrim":
					if arg == "" && !strings.Contains(name, "/") {
						got = map[string]func(string) string{"trim": Trim, "ltrim": Ltrim, "rtrim": Rtrim}[op](s)
						break
					}
					chars, _ := hex.DecodeString(arg)
					got = map[string]func(string, string) string{"trim": TrimSet, "ltrim": LtrimSet, "rtrim": RtrimSet}[op](s, string(chars))
				case "str_pad":
					p := pads[i]
					got = StrPad(s, p.n, p.pad, p.typ)
				case "wordwrap":
					w := wraps[i]
					got = Wordwrap(s, w.w, w.brk, w.cut)
				case "substr":
					if substrs[i][1] == math.MaxInt {
						got = Substr(s, substrs[i][0])
					} else {
						got = SubstrLen(s, substrs[i][0], substrs[i][1])
					}
				case "strtr":
					got = Strtr(s, "abc", "xy")
				case "strtr_pairs":
					got = StrtrPairs(s, pairs)
				default:
					t.Fatalf("unknown op %s", name)
				}
			})
			wantValue, wantErr := golden(want)
			if msg != wantErr || wantErr == "" && got != wantValue {
				t.Errorf("%s(%q): got %q %s, want %q %s", name, s, got, msg, wantValue, wantErr)
			}
		}
	}
}

func TestOracleStrnatcmp(t *testing.T) {
	cases := loadOracle(t, "strnatcmp")
	for _, c := range cases {
		a := c["value"].(string)
		for j, r := range c["results"].([]any) {
			b := cases[j]["value"].(string)
			row := r.([]any)
			got := []int{Strnatcmp(a, b), Strnatcasecmp(a, b), Strcmp(a, b), Strcasecmp(a, b)}
			for k, name := range []string{"strnatcmp", "strnatcasecmp", "strcmp", "strcasecmp"} {
				if float64(got[k]) != row[k].(float64) {
					t.Errorf("%s(%q, %q): got %d, want %v", name, a, b, got[k], row[k])
				}
			}
		}
	}
}

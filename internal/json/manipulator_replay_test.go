// Replays the JsonManipulator recordings in testdata/manipulator, made by
// the real JsonManipulator (tools/oracle/json/manipulator_tests.php and
// manipulator.php), against the Go port.

package json

import (
	"bytes"
	"encoding/base64"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/stubbedev/maestro/internal/json/jsonlint"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// mstep is one recorded step: creating a manipulator, calling a method, or
// asserting on the result of the previous call.
type mstep struct {
	Op       string               `json:"op"`
	Contents stdjson.RawMessage   `json:"contents"`
	Method   string               `json:"method"`
	Args     []stdjson.RawMessage `json:"args"`
	Result   stdjson.RawMessage   `json:"result"`
	Kind     string               `json:"kind"`
	Expected stdjson.RawMessage   `json:"expected"`
}

// decodeValue decodes the typed value encoding of manipulator_common.php.
func decodeValue(raw stdjson.RawMessage) (any, error) {
	dec := stdjson.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}

	return convertValue(v)
}

// recordedError is an exception in a recording.
type recordedError struct{ class, message string }

func convertValue(v any) (any, error) {
	switch v := v.(type) {
	case nil, bool, string:
		return v, nil
	case stdjson.Number:
		return strconv.ParseInt(string(v), 10, 64)
	case map[string]any:
		switch {
		case v["f"] != nil:
			return strconv.ParseFloat(v["f"].(string), 64)
		case v["b"] != nil:
			b, err := base64.StdEncoding.DecodeString(v["b"].(string))
			return string(b), err
		case v["x"] != nil:
			return recordedError{v["x"].(string), v["m"].(string)}, nil
		case v["a"] != nil, v["o"] != nil:
			pairs, _ := v["a"].([]any)
			o, isObject := v["o"].([]any)
			if isObject {
				pairs = o
			}
			arr := php.NewArray()
			for _, p := range pairs {
				kv := p.([]any)
				val, err := convertValue(kv[1])
				if err != nil {
					return nil, err
				}
				key, err := convertValue(kv[0])
				if err != nil {
					return nil, err
				}
				arr.Set(key, val)
			}
			if isObject {
				return php.ObjectFromArray(arr), nil
			}
			return arr, nil
		}
	}

	return nil, fmt.Errorf("cannot decode %v", v)
}

// errorClass names the PHP exception class a Go error stands for.
func errorClass(err error) string {
	var (
		invalidArgument *util.InvalidArgumentError
		logic           *util.LogicError
		runtime         *util.RuntimeError
		unexpected      *util.UnexpectedValueError
		pcre            *php.PcreError
		parsing         *jsonlint.ParsingError
		phpErr          *phpError
		errorException  *util.ErrorException
	)
	switch {
	case errors.As(err, &invalidArgument):
		return "InvalidArgumentException"
	case errors.As(err, &logic):
		return "LogicException"
	case errors.As(err, &runtime):
		return "RuntimeException"
	case errors.As(err, &unexpected):
		return "UnexpectedValueException"
	case errors.As(err, &pcre):
		return "PcreException"
	case errors.As(err, &parsing):
		return "ParsingException"
	case errors.As(err, &phpErr):
		return phpErr.class
	case errors.As(err, &errorException):
		return "ErrorException"
	}

	return fmt.Sprintf("%T", err)
}

func argInt(v any) int {
	if i, ok := v.(int64); ok {
		return int(i)
	}

	return 0
}

func argBool(args []any, i int, def bool) bool {
	if i < len(args) {
		b, _ := args[i].(bool)
		return b
	}

	return def
}

func argStr(args []any, i int) string {
	if i < len(args) {
		s, _ := args[i].(string)
		return s
	}

	return ""
}

// callManipulator invokes the PHP method name on m with PHP's arguments,
// filling in PHP's defaults.
func callManipulator(m *Manipulator, method string, a []any) (any, error) {
	arg := func(i int) any {
		if i < len(a) {
			return a[i]
		}
		return nil
	}
	switch method {
	case "getContents":
		return m.Contents(), nil
	case "addLink":
		return m.AddLink(argStr(a, 0), argStr(a, 1), argStr(a, 2), argBool(a, 3, false))
	case "addRepository":
		return m.AddRepository(argStr(a, 0), arg(1), argBool(a, 2, true))
	case "setRepositoryUrl":
		return m.SetRepositoryURL(argStr(a, 0), argStr(a, 1))
	case "insertRepository":
		return m.InsertRepository(argStr(a, 0), arg(1), argStr(a, 2), argInt(arg(3)))
	case "removeRepository":
		return m.RemoveRepository(argStr(a, 0))
	case "addConfigSetting":
		return m.AddConfigSetting(argStr(a, 0), arg(1))
	case "removeConfigSetting":
		return m.RemoveConfigSetting(argStr(a, 0))
	case "addProperty":
		return m.AddProperty(argStr(a, 0), arg(1))
	case "removeProperty":
		return m.RemoveProperty(argStr(a, 0))
	case "addSubNode":
		return m.AddSubNode(argStr(a, 0), argStr(a, 1), arg(2), argBool(a, 3, true))
	case "removeSubNode":
		return m.RemoveSubNode(argStr(a, 0), argStr(a, 1))
	case "addListItem":
		return m.AddListItem(argStr(a, 0), arg(1), argBool(a, 2, true))
	case "insertListItem":
		return m.InsertListItem(argStr(a, 0), arg(1), argInt(arg(2)))
	case "removeListItem":
		return m.RemoveListItem(argStr(a, 0), argInt(arg(1)))
	case "addMainKey":
		return m.AddMainKey(argStr(a, 0), arg(1))
	case "removeMainKey":
		return m.RemoveMainKey(argStr(a, 0))
	case "changeEmptyMainKeyFromAssocToList":
		return m.ChangeEmptyMainKeyFromAssocToList(argStr(a, 0))
	case "removeMainKeyIfEmpty":
		return m.RemoveMainKeyIfEmpty(argStr(a, 0))
	case "format":
		return m.Format(arg(0), argInt(arg(1)), argBool(a, 2, false))
	}

	return nil, fmt.Errorf("unknown method %s", method)
}

// checkResult compares a Go result with a recorded one; it reports whether
// they agree (an exception thrown by both agrees).
func checkResult(t *testing.T, where string, got any, err error, want any) bool {
	t.Helper()
	if rec, ok := want.(recordedError); ok {
		if err == nil {
			t.Errorf("%s: got %s, want %s: %s", where, php.VarExport(got), rec.class, rec.message)
			return false
		}
		if class := errorClass(err); class != rec.class || err.Error() != rec.message {
			t.Errorf("%s: got %s: %s\nwant %s: %s", where, class, err, rec.class, rec.message)
			return false
		}
		return true
	}
	if err != nil {
		t.Errorf("%s: unexpected error %s: %v", where, errorClass(err), err)
		return false
	}
	if !php.StrictEquals(got, want) {
		t.Errorf("%s: got %s\nwant %s", where, php.VarExport(got), php.VarExport(want))
		return false
	}

	return true
}

// replaySteps runs one recorded case. With trackContents, call steps
// record the contents whenever they changed and every step checks them.
func replaySteps(t *testing.T, steps []mstep, trackContents bool) {
	t.Helper()
	var (
		m       *Manipulator
		last    any
		current string // the contents after the last call that changed them
	)
	for i, s := range steps {
		where := fmt.Sprintf("step %d (%s %s)", i, s.Op, s.Method)
		switch s.Op {
		case "new":
			contents, err := decodeValue(s.Contents)
			if err != nil {
				t.Fatal(err)
			}
			m, err = NewManipulator(contents.(string))
			if s.Result != nil {
				want, _ := decodeValue(s.Result)
				checkResult(t, where, m, err, want)
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", where, err)
			}
			current = m.Contents()
		case "call":
			args := make([]any, len(s.Args))
			for j, raw := range s.Args {
				v, err := decodeValue(raw)
				if err != nil {
					t.Fatal(err)
				}
				args[j] = v
			}
			want, err := decodeValue(s.Result)
			if err != nil {
				t.Fatal(err)
			}
			got, err := callManipulator(m, s.Method, args)
			if !checkResult(t, where, got, err, want) {
				return
			}
			last = got
			if s.Contents != nil {
				contents, err := decodeValue(s.Contents)
				if err != nil {
					t.Fatal(err)
				}
				current = contents.(string)
			}
			if trackContents && m.Contents() != current {
				t.Errorf("%s: contents\n%q\nwant\n%q", where, m.Contents(), current)
				return
			}
		case "assert":
			expected, err := decodeValue(s.Expected)
			if err != nil {
				t.Fatal(err)
			}
			switch s.Kind {
			case "same":
				if !php.StrictEquals(expected, last) {
					t.Errorf("%s: assertion failed: got %s\nwant %s", where, php.VarExport(last), php.VarExport(expected))
				}
			case "json":
				e, _ := php.JSONDecode(expected.(string), true)
				g, _ := php.JSONDecode(last.(string), true)
				if !php.LooseEquals(e, g) {
					t.Errorf("%s: JSON assertion failed: got %s\nwant %s", where, last, expected)
				}
			}
		}
	}
}

type recordedTest struct {
	Test  string `json:"test"`
	Cases []struct {
		Name  string  `json:"name"`
		Steps []mstep `json:"steps"`
	} `json:"cases"`
}

func loadRecordedTests(t *testing.T) map[string]recordedTest {
	t.Helper()
	data, err := os.ReadFile("testdata/manipulator/tests.json")
	if err != nil {
		t.Fatal(err)
	}
	var tests []recordedTest
	if err := stdjson.Unmarshal(data, &tests); err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]recordedTest, len(tests))
	for _, rt := range tests {
		byName[rt.Test] = rt
	}

	return byName
}

// runManipulatorTest runs every data set of JsonManipulatorTest::name.
func runManipulatorTest(t *testing.T, name string) {
	t.Helper()
	rt, ok := loadRecordedTests(t)[name]
	if !ok {
		t.Fatalf("no recording of %s", name)
	}
	for _, c := range rt.Cases {
		t.Run(c.Name, func(t *testing.T) { replaySteps(t, c.Steps, false) })
	}
}

// TestJsonManipulator_Oracle replays the real JsonManipulator over the
// generated documents and operation sequences of
// tools/oracle/json/manipulator.php.
func TestJsonManipulator_Oracle(t *testing.T) {
	data, err := os.ReadFile("testdata/manipulator/oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases [][]mstep
	if err := stdjson.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for i, steps := range cases {
		if manipulatorRace && i%10 != 0 {
			continue
		}
		t.Run(strconv.Itoa(i), func(t *testing.T) { replaySteps(t, steps, true) })
	}
}

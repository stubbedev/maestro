package console

import (
	"encoding/json"
	"fmt"
	"testing"
)

// inputOracleDefinitions mirrors definitions() in
// tools/oracle/console/input.php.
func inputOracleDefinitions() map[string]*InputDefinition {
	return map[string]*InputDefinition{
		"empty": MustDefinition(),
		"composer": MustDefinition(
			MustArgument("command", ArgumentRequired, "The command to execute", nil),
			opt("--help", "-h", OptionValueNone),
			opt("--quiet", "-q", OptionValueNone),
			opt("--verbose", "-v|vv|vvv", OptionValueNone),
			opt("--version", "-V", OptionValueNone),
			opt("--ansi", "", OptionValueNegatable),
			opt("--no-interaction", "-n", OptionValueNone),
			opt("--profile", "", OptionValueNone),
			opt("--no-plugins", "", OptionValueNone),
			opt("--no-scripts", "", OptionValueNone),
			opt("--working-dir", "-d", OptionValueRequired),
			opt("--no-cache", "", OptionValueNone),
		),
		"require": MustDefinition(
			arg("command", ArgumentRequired),
			arg("packages", ArgumentOptional|ArgumentIsArray),
			opt("dev", "", OptionValueNone),
			opt("dry-run", "", OptionValueNone),
			opt("prefer-source", "", OptionValueNone),
			opt("update-with-dependencies", "w", OptionValueNone),
			opt("with-all-dependencies", "W", OptionValueNone),
			opt("ignore-platform-req", "", OptionValueRequired|OptionValueIsArray),
			opt("optimize-autoloader", "o", OptionValueNone),
			opt("classmap-authoritative", "a", OptionValueNone),
			opt("apcu-autoloader-prefix", "", OptionValueRequired),
			opt("no-interaction", "n", OptionValueNone),
			opt("verbose", "v|vv|vvv", OptionValueNone),
		),
		"mixed": MustDefinition(
			arg("a", ArgumentRequired),
			arg("b", ArgumentOptional, "x"),
			opt("req", "r", OptionValueRequired),
			opt("opt", "o", OptionValueOptional, "d"),
			opt("arr", "A", OptionValueOptional|OptionValueIsArray),
			opt("neg", "", OptionValueNegatable),
			opt("flag", "f", OptionValueNone),
			opt("multi", "m|mm", OptionValueNone),
		),
		"arrayarg": MustDefinition(
			arg("files", ArgumentIsArray),
			opt("x", "x", OptionValueNone),
			opt("val", "V", OptionValueRequired, "dflt"),
		),
	}
}

type oracleErr struct {
	Class   string `json:"class"`
	Message string `json:"message"`
}

type oracleProbe struct {
	values []string
	a, b   any
}

func (p *oracleProbe) UnmarshalJSON(data []byte) error {
	var raw [3]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, v := range raw[0].([]any) {
		p.values = append(p.values, v.(string))
	}
	p.a, p.b = jsonValue(raw[1]), jsonValue(raw[2])

	return nil
}

type oraclePairs []NamedValue

func (p *oraclePairs) UnmarshalJSON(data []byte) error {
	var raw [][2]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, kv := range raw {
		*p = append(*p, NamedValue{phpToString(jsonValue(kv[0])), jsonValue(kv[1])})
	}

	return nil
}

// jsonValue converts a decoded JSON value to the console value model
// (integral numbers become int).
func jsonValue(v any) any {
	switch x := v.(type) {
	case float64:
		if x == float64(int(x)) {
			return int(x)
		}
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = jsonValue(e)
		}

		return out
	}

	return v
}

type inputOracleCase struct {
	Def           string        `json:"def"`
	Argv          []string      `json:"argv"`
	Params        [][2]any      `json:"params"`
	Arguments     *oraclePairs  `json:"arguments"`
	Options       *oraclePairs  `json:"options"`
	Error         *oracleErr    `json:"error"`
	Validate      *oracleErr    `json:"validate"`
	FirstArgument any           `json:"firstArgument"`
	ToString      *string       `json:"toString"`
	ToStringError *oracleErr    `json:"toStringError"`
	Has           []oracleProbe `json:"has"`
	Get           []oracleProbe `json:"get"`
}

type inputOracle struct {
	Argv   []inputOracleCase `json:"argv"`
	Array  []inputOracleCase `json:"array"`
	String []struct {
		Input         string     `json:"input"`
		Tokens        []string   `json:"tokens"`
		ToString      *string    `json:"toString"`
		ToStringError *oracleErr `json:"toStringError"`
		Error         *oracleErr `json:"error"`
	} `json:"string"`
}

func checkOracleErr(t *testing.T, what string, err error, want *oracleErr) {
	t.Helper()
	switch {
	case want == nil && err != nil:
		t.Errorf("%s: unexpected error %v", what, err)
	case want != nil && err == nil:
		t.Errorf("%s: want %s %q, got no error", what, want.Class, want.Message)
	case want != nil:
		e, ok := err.(*Error) //nolint:errorlint // the console returns its errors unwrapped
		if !ok || kindClass[e.Kind] != want.Class || e.Message != want.Message {
			t.Errorf("%s: want %s %q, got %v", what, want.Class, want.Message, err)
		}
	}
}

// checkToString compares __toString(), which panics where PHP throws.
func checkToString(t *testing.T, what string, in Input, want *string, wantErr *oracleErr) {
	t.Helper()
	var got string
	err := catchPanic(t, func() { got = in.String() })
	if err != nil {
		checkOracleErr(t, what+" __toString", err, wantErr)

		return
	}
	checkOracleErr(t, what+" __toString", nil, wantErr)
	if want != nil {
		eq(t, got, *want, what+" __toString")
	}
}

func checkInputState(t *testing.T, what string, in Input, c *inputOracleCase) {
	t.Helper()
	if c.Arguments != nil {
		eqValues(t, in.Arguments(), *c.Arguments, what+" arguments")
		eqValues(t, in.Options(), *c.Options, what+" options")
	}
	eq(t, in.FirstArgument(), phpToString(jsonValue(c.FirstArgument)), what+" firstArgument")
	checkToString(t, what, in, c.ToString, c.ToStringError)
	for _, p := range c.Has {
		eq(t, in.HasParameterOption(p.values, false), p.a.(bool), what, fmt.Sprintf("hasParameterOption(%q)", p.values))
		eq(t, in.HasParameterOption(p.values, true), p.b.(bool), what, fmt.Sprintf("hasParameterOption(%q, true)", p.values))
	}
	for _, p := range c.Get {
		eqValue(t, in.ParameterOption(p.values, "DEFAULT", false), p.a, what, fmt.Sprintf("getParameterOption(%q)", p.values))
		eqValue(t, in.ParameterOption(p.values, false, true), p.b, what, fmt.Sprintf("getParameterOption(%q, false, true)", p.values))
	}
}

func TestOracle_Input(t *testing.T) {
	var o inputOracle
	loadOracle(t, "input.json", &o)
	if len(o.Argv) == 0 || len(o.Array) == 0 || len(o.String) == 0 {
		t.Fatal("empty oracle")
	}

	for i := range o.Argv {
		c := &o.Argv[i]
		what := fmt.Sprintf("ArgvInput %s %q", c.Def, c.Argv)
		in := mustArgv(t, append([]string{"cli.php"}, c.Argv...)...)
		err := in.Bind(inputOracleDefinitions()[c.Def])
		checkOracleErr(t, what, err, c.Error)
		if err == nil {
			checkOracleErr(t, what+" validate", in.Validate(), c.Validate)
		}
		checkInputState(t, what, in, c)
	}

	for i := range o.Array {
		c := &o.Array[i]
		params := make([]Param, len(c.Params))
		for j, kv := range c.Params {
			v := jsonValue(kv[1])
			if k, ok := jsonValue(kv[0]).(int); ok {
				params[j] = PI(k, v)
			} else {
				params[j] = P(kv[0].(string), v)
			}
		}
		what := fmt.Sprintf("ArrayInput %s %v", c.Def, c.Params)
		in, err := NewArrayInput(params, inputOracleDefinitions()[c.Def])
		checkOracleErr(t, what, err, c.Error)
		if err != nil {
			in, _ = NewArrayInput(params, nil)
		}
		checkInputState(t, what, in, c)
	}

	for _, c := range o.String {
		in, err := NewStringInput(c.Input)
		checkOracleErr(t, fmt.Sprintf("StringInput(%q)", c.Input), err, c.Error)
		if err != nil {
			continue
		}
		eq(t, fmt.Sprintf("%q", in.Tokens()), fmt.Sprintf("%q", c.Tokens), "StringInput tokens", c.Input)
		checkToString(t, fmt.Sprintf("StringInput(%q)", c.Input), in, c.ToString, c.ToStringError)
	}
}

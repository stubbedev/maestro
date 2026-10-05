// Ports src/Input/ArrayInput.php (symfony/console).

package console

import (
	"strconv"
	"strings"
)

// Param is one entry of an ArrayInput parameter array: a string key
// ("command", "--opt", "-o") or, when Positional, an integer key.
type Param struct {
	Key        string
	Index      int
	Positional bool
	Value      any
}

// P builds a string-keyed parameter.
func P(key string, value any) Param { return Param{Key: key, Value: value} }

// PI builds an integer-keyed parameter.
func PI(index int, value any) Param {
	return Param{Key: strconv.Itoa(index), Index: index, Positional: true, Value: value}
}

// ArrayInput is an input built from a parameter array.
type ArrayInput struct {
	BaseInput
	parameters []Param
}

// NewArrayInput mirrors new ArrayInput($parameters, $definition).
func NewArrayInput(parameters []Param, definition *InputDefinition) (*ArrayInput, error) {
	in := &ArrayInput{parameters: parameters}
	if err := in.init(definition, in.parseParams); err != nil {
		return in, err
	}

	return in, nil
}

// Clone implements Input.
func (in *ArrayInput) Clone() Input {
	c := &ArrayInput{BaseInput: in.cloneBase(), parameters: in.parameters}
	c.parse = c.parseParams

	return c
}

// FirstArgument implements Input.
func (in *ArrayInput) FirstArgument() string {
	for _, p := range in.parameters {
		if !p.Positional && p.Key != "" && p.Key != "0" && p.Key[0] == '-' {
			continue
		}

		return phpToString(p.Value)
	}

	return ""
}

// HasParameterOption implements Input.
func (in *ArrayInput) HasParameterOption(values []string, onlyParams bool) bool {
	for _, p := range in.parameters {
		var v any = p.Key
		if p.Positional {
			v = p.Value
		}

		if onlyParams && phpIdentical(v, "--") {
			return false
		}

		for _, want := range values {
			if phpLooseEqualsString(v, want) {
				return true
			}
		}
	}

	return false
}

// ParameterOption implements Input.
func (in *ArrayInput) ParameterOption(values []string, def any, onlyParams bool) any {
	for _, p := range in.parameters {
		if onlyParams && ((!p.Positional && p.Key == "--") || (p.Positional && phpIdentical(p.Value, "--"))) {
			return def
		}

		if p.Positional {
			for _, want := range values {
				if phpLooseEqualsString(p.Value, want) {
					return true
				}
			}
		} else {
			for _, want := range values {
				if p.Key == want {
					return p.Value
				}
			}
		}
	}

	return def
}

// String implements Input.
func (in *ArrayInput) String() string {
	params := make([]string, 0, len(in.parameters))
	for _, p := range in.parameters {
		if !p.Positional && p.Key != "" && p.Key != "0" && p.Key[0] == '-' {
			glue := " "
			if len(p.Key) > 1 && p.Key[1] == '-' {
				glue = "="
			}
			if list, ok := arrayValues(p.Value); ok {
				for _, v := range list {
					if phpLooseEqualsString(v, "") {
						params = append(params, p.Key)
					} else {
						params = append(params, p.Key+glue+EscapeToken(phpToString(v)))
					}
				}
			} else if phpLooseEqualsString(p.Value, "") {
				params = append(params, p.Key)
			} else {
				params = append(params, p.Key+glue+EscapeToken(phpToString(p.Value)))
			}
		} else if list, ok := arrayValues(p.Value); ok {
			parts := make([]string, len(list))
			for i, v := range list {
				parts[i] = EscapeToken(phpToString(v))
			}
			params = append(params, strings.Join(parts, " "))
		} else {
			params = append(params, EscapeToken(phpToString(p.Value)))
		}
	}

	return strings.Join(params, " ")
}

func arrayValues(v any) ([]any, bool) {
	switch x := v.(type) {
	case []any:
		return x, true
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}

		return out, true
	}

	return nil, false
}

func (in *ArrayInput) parseParams() error {
	for _, p := range in.parameters {
		if p.Positional {
			if err := in.addPositionalArgument(p.Index, p.Value); err != nil {
				return err
			}

			continue
		}
		key := p.Key
		if key == "--" {
			return nil
		}
		var err error
		switch {
		case strings.HasPrefix(key, "--"):
			err = in.addLongOption(key[2:], p.Value)
		case strings.HasPrefix(key, "-"):
			err = in.addShortOption(key[1:], p.Value)
		default:
			err = in.addArgument(key, p.Value)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (in *ArrayInput) addShortOption(shortcut string, value any) error {
	n, ok := in.definition.shortcuts[shortcut]
	if !ok {
		return newError(KindInvalidOption, "ArrayInput.php", 154, `The "-%s" option does not exist.`, shortcut)
	}

	return in.addLongOption(n, value)
}

func (in *ArrayInput) addLongOption(name string, value any) error {
	o := in.definition.opt(name)
	if o == nil {
		optionName, ok := in.definition.negations[name]
		if !ok {
			return newError(KindInvalidOption, "ArrayInput.php", 170, `The "--%s" option does not exist.`, name)
		}
		in.options[optionName] = false

		return nil
	}

	if value == nil {
		if o.IsValueRequired() {
			return newError(KindInvalidOption, "ArrayInput.php", 183, `The "--%s" option requires a value.`, name)
		}

		if !o.IsValueOptional() {
			value = true
		}
	}

	in.options[name] = value

	return nil
}

func (in *ArrayInput) addArgument(name string, value any) error {
	if !in.definition.HasArgument(name) {
		return newError(KindInvalidArgument, "ArrayInput.php", 205, `The "%s" argument does not exist.`, name)
	}
	in.arguments[name] = value

	return nil
}

// addPositionalArgument handles integer keys: hasArgument(int) looks the
// argument up by position, but the value is stored under the integer key.
func (in *ArrayInput) addPositionalArgument(index int, value any) error {
	if !in.definition.HasArgumentAt(index) {
		return newError(KindInvalidArgument, "ArrayInput.php", 205, `The "%d" argument does not exist.`, index)
	}
	in.arguments[strconv.Itoa(index)] = value

	return nil
}

// phpLooseEqualsString is PHP 8's $v == $s for a string $s.
func phpLooseEqualsString(v any, s string) bool {
	switch x := v.(type) {
	case nil:
		return s == ""
	case bool:
		return x == (s != "" && s != "0")
	case string:
		if x == s {
			return true
		}
		a, okA := phpNumeric(x)
		b, okB := phpNumeric(s)

		return okA && okB && a == b
	case int:
		if b, ok := phpNumeric(s); ok {
			return float64(x) == b
		}

		return strconv.Itoa(x) == s
	case float64:
		if b, ok := phpNumeric(s); ok {
			return x == b
		}

		return phpFloatString(x) == s
	}

	return false
}

// phpNumeric reports whether s is a PHP numeric string, with its value.
func phpNumeric(s string) (float64, bool) {
	t := strings.TrimLeft(s, " \t\n\r\v\f")
	t = strings.TrimRight(t, " \t\n\r\v\f")
	if t == "" {
		return 0, false
	}
	// Reject forms ParseFloat accepts but PHP does not.
	for i := range len(t) {
		c := t[i]
		if (c < '0' || c > '9') && c != '.' && c != 'e' && c != 'E' && c != '+' && c != '-' {
			return 0, false
		}
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, false
	}

	return f, true
}

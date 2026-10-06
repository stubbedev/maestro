// Ports src/Input/ArrayInput.php (symfony/console).

package console

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// Param is one entry of an ArrayInput parameter array: a string key
// ("command", "--opt", "-o") or, when Positional, an integer key. As in a
// PHP array, a key that is a canonical decimal integer ("5") is an integer
// key, and a repeated key keeps its first position with the last value.
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
	in := &ArrayInput{parameters: normalizeParams(parameters)}
	if err := in.init(definition, in.parseParams, `Symfony\Component\Console\Input\ArrayInput`); err != nil {
		return in, err
	}

	return in, nil
}

// normalizeParams applies PHP's array key semantics to parameters.
func normalizeParams(parameters []Param) []Param {
	out := make([]Param, 0, len(parameters))
	for _, p := range parameters {
		if !p.Positional {
			if k := php.StrKey(p.Key); k.IsInt() {
				p = PI(int(k.Int()), p.Value)
			}
		}
		if j := slices.IndexFunc(out, func(q Param) bool { return q.Positional == p.Positional && q.Key == p.Key }); j >= 0 {
			out[j].Value = p.Value

			continue
		}
		out = append(out, p)
	}

	return out
}

// Parameters returns the parameter array (ArrayInput's $parameters).
func (in *ArrayInput) Parameters() []Param { return in.parameters }

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
				if phpLooseEqualsString(p.Key, want) {
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
		if p.Positional && p.Index >= 0 {
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
		return newError(KindInvalidOption, `The "-%s" option does not exist.`, shortcut)
	}

	return in.addLongOption(n, value)
}

func (in *ArrayInput) addLongOption(name string, value any) error {
	o := in.definition.opt(name)
	if o == nil {
		optionName, ok := in.definition.negations[name]
		if !ok {
			return newError(KindInvalidOption, `The "--%s" option does not exist.`, name)
		}
		in.options[optionName] = false

		return nil
	}

	if value == nil {
		if o.IsValueRequired() {
			return newError(KindInvalidOption, `The "--%s" option requires a value.`, name)
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
		return newError(KindInvalidArgument, `The "%s" argument does not exist.`, name)
	}
	in.arguments[name] = value

	return nil
}

// addPositionalArgument handles integer keys: hasArgument(int) looks the
// argument up by position, but the value is stored under the integer key.
func (in *ArrayInput) addPositionalArgument(index int, value any) error {
	if !in.definition.HasArgumentAt(index) {
		return newError(KindInvalidArgument, `The "%d" argument does not exist.`, index)
	}
	key := strconv.Itoa(index)
	if _, ok := in.arguments[key]; !ok && !in.definition.HasArgument(key) {
		in.extraArgs = append(in.extraArgs, key)
	}
	in.arguments[key] = value

	return nil
}

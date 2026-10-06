// Ports src/Input/InputInterface.php, StreamableInputInterface.php and
// Input.php (symfony/console).

package console

import (
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Input is InputInterface plus StreamableInputInterface.
type Input interface {
	// FirstArgument returns the first argument token ("" when none).
	FirstArgument() string
	HasParameterOption(values []string, onlyParams bool) bool
	ParameterOption(values []string, def any, onlyParams bool) any
	Bind(definition *InputDefinition) error
	Validate() error
	Arguments() []NamedValue
	// Argument panics with an InvalidArgument *Error for unknown names.
	Argument(name string) any
	SetArgument(name string, value any)
	HasArgument(name string) bool
	Options() []NamedValue
	// Option panics with an InvalidArgument *Error for unknown names.
	Option(name string) any
	SetOption(name string, value any)
	HasOption(name string) bool
	IsInteractive() bool
	SetInteractive(interactive bool)
	SetStream(stream io.Reader)
	Stream() io.Reader
	// String renders the input for display (__toString).
	String() string
	// Clone returns a shallow copy, like PHP's clone.
	Clone() Input
}

// BaseInput is the abstract Input class.
type BaseInput struct {
	definition *InputDefinition
	stream     io.Reader
	options    map[string]any
	arguments  map[string]any
	// extraArgs lists, in insertion order, the integer keys of arguments
	// stored by an ArrayInput's positional parameters; array_merge() in
	// getArguments() appends them after the named ones.
	extraArgs   []string
	interactive bool
	parse       func() error
	// parseClass declares parse() (the frame of its exceptions' traces)
	parseClass string
}

func (in *BaseInput) init(definition *InputDefinition, parse func() error, parseClass string) error {
	in.interactive = true
	in.parse = parse
	in.parseClass = parseClass
	in.options = map[string]any{}
	in.arguments = map[string]any{}
	if definition == nil {
		in.definition = &InputDefinition{}

		return nil
	}
	if err := in.Bind(definition); err != nil {
		return err
	}

	return in.Validate()
}

func (in *BaseInput) cloneBase() BaseInput {
	c := *in
	c.options = maps.Clone(in.options)
	c.arguments = maps.Clone(in.arguments)
	c.extraArgs = slices.Clone(in.extraArgs)

	return c
}

// Definition returns the bound definition.
func (in *BaseInput) Definition() *InputDefinition { return in.definition }

// Bind implements Input.
func (in *BaseInput) Bind(definition *InputDefinition) error {
	in.arguments = map[string]any{}
	in.options = map[string]any{}
	in.extraArgs = nil
	in.definition = definition

	return in.parse()
}

// Validate implements Input.
func (in *BaseInput) Validate() error {
	var missing []string
	for _, a := range in.definition.Arguments() {
		if _, ok := in.arguments[a.Name()]; !ok && a.IsRequired() {
			missing = append(missing, a.Name())
		}
	}

	if len(missing) > 0 {
		return newError(KindRuntime, `Not enough arguments (missing: "%s").`, strings.Join(missing, ", "))
	}

	return nil
}

// IsInteractive implements Input.
func (in *BaseInput) IsInteractive() bool { return in.interactive }

// SetInteractive implements Input.
func (in *BaseInput) SetInteractive(interactive bool) { in.interactive = interactive }

// Arguments implements Input.
func (in *BaseInput) Arguments() []NamedValue {
	out := in.definition.ArgumentDefaults()
	for i, nv := range out {
		if v, ok := in.arguments[nv.Name]; ok {
			out[i].Value = v
		}
	}
	for i, k := range in.extraArgs {
		out = append(out, NamedValue{strconv.Itoa(i), in.arguments[k]})
	}

	return out
}

// Argument implements Input.
func (in *BaseInput) Argument(name string) any {
	a := in.definition.arg(name)
	if a == nil {
		panic(newError(KindInvalidArgument, `The "%s" argument does not exist.`, name))
	}
	// $this->arguments[$name] ?? default: a null value falls back.
	if v, ok := in.arguments[name]; ok && v != nil {
		return v
	}

	return a.Default()
}

// SetArgument implements Input.
func (in *BaseInput) SetArgument(name string, value any) {
	if !in.definition.HasArgument(name) {
		panic(newError(KindInvalidArgument, `The "%s" argument does not exist.`, name))
	}
	in.arguments[name] = value
}

// HasArgument implements Input.
func (in *BaseInput) HasArgument(name string) bool { return in.definition.HasArgument(name) }

// Options implements Input.
func (in *BaseInput) Options() []NamedValue {
	out := in.definition.OptionDefaults()
	for i, nv := range out {
		if v, ok := in.options[nv.Name]; ok {
			out[i].Value = v
		}
	}

	return out
}

// Option implements Input.
func (in *BaseInput) Option(name string) any {
	if in.definition.HasNegation(name) {
		n, _ := in.definition.NegationToName(name)
		v := in.Option(n)
		if v == nil {
			return nil
		}

		return !phpTruthy(v)
	}

	o := in.definition.opt(name)
	if o == nil {
		panic(newError(KindInvalidArgument, `The "%s" option does not exist.`, name))
	}

	if v, ok := in.options[name]; ok {
		return v
	}

	return o.Default()
}

// SetOption implements Input.
func (in *BaseInput) SetOption(name string, value any) {
	if in.definition.HasNegation(name) {
		n, _ := in.definition.NegationToName(name)
		in.options[n] = !phpTruthy(value)

		return
	}
	if !in.definition.HasOption(name) {
		panic(newError(KindInvalidArgument, `The "%s" option does not exist.`, name))
	}
	in.options[name] = value
}

// HasOption implements Input.
func (in *BaseInput) HasOption(name string) bool {
	return in.definition.HasOption(name) || in.definition.HasNegation(name)
}

// EscapeToken quotes a token for display unless it is a plain word.
func EscapeToken(token string) string {
	// preg_match('{^[\w-]+$}', $token): "$" also matches before a final "\n".
	t := strings.TrimSuffix(token, "\n")
	if t != "" {
		plain := true
		for i := range len(t) {
			c := t[i]
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
				plain = false

				break
			}
		}
		if plain {
			return token
		}
	}

	return escapeShellArg(token)
}

// SetStream implements Input.
func (in *BaseInput) SetStream(stream io.Reader) { in.stream = stream }

// Stream implements Input.
func (in *BaseInput) Stream() io.Reader { return in.stream }

// StringOption returns an option value converted like PHP's (string) cast.
func StringOption(in Input, name string) string { return phpToString(in.Option(name)) }

// BoolOption returns an option value converted like PHP's (bool) cast.
func BoolOption(in Input, name string) bool { return phpTruthy(in.Option(name)) }

// StringsOption returns an array option value.
func StringsOption(in Input, name string) []string { return toStrings(in.Option(name)) }

// StringArgument returns an argument value converted like PHP's (string) cast.
func StringArgument(in Input, name string) string { return phpToString(in.Argument(name)) }

// StringsArgument returns an array argument value.
func StringsArgument(in Input, name string) []string { return toStrings(in.Argument(name)) }

func toStrings(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, len(x))
		for i, e := range x {
			out[i] = phpToString(e)
		}

		return out
	case nil:
		return nil
	}

	return []string{phpToString(v)}
}

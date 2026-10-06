// Ports src/Input/InputArgument.php, InputOption.php and InputDefinition.php
// (symfony/console), with the suggested-values backport of Composer's
// src/Composer/Console/Input/InputArgument.php and InputOption.php.

package console

import (
	"math"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// InputArgument modes.
const (
	ArgumentRequired = 1
	ArgumentOptional = 2
	ArgumentIsArray  = 4
)

// InputOption modes.
const (
	OptionValueNone      = 1
	OptionValueRequired  = 2
	OptionValueOptional  = 4
	OptionValueIsArray   = 8
	OptionValueNegatable = 16
)

// SuggestFunc computes suggested values for completion. It returns
// []string or []Suggestion values.
type SuggestFunc func(input *CompletionInput, suggestions *CompletionSuggestions) []Suggestion

// InputArgument is a command line argument.
type InputArgument struct {
	name        string
	mode        int
	description string
	def         any

	suggestedValues []string
	suggestFunc     SuggestFunc
}

// NewInputArgument mirrors new InputArgument($name, $mode, $description,
// $default). Mode 0 stands for PHP's null mode (OPTIONAL); an explicit 0,
// which PHP rejects, cannot be expressed.
func NewInputArgument(name string, mode int, description string, def any) (*InputArgument, error) {
	if mode == 0 {
		mode = ArgumentOptional
	} else if mode > 7 || mode < 1 {
		return nil, newError(KindInvalidArgument, "InputArgument.php", 46, `Argument mode "%d" is not valid.`, mode)
	}

	a := &InputArgument{name: name, mode: mode, description: description}
	if err := a.SetDefault(def); err != nil {
		return nil, err
	}

	return a, nil
}

// MustArgument is NewInputArgument for static definitions; it panics on
// invalid input.
func MustArgument(name string, mode int, description string, def any) *InputArgument {
	a, err := NewInputArgument(name, mode, description, def)
	if err != nil {
		panic(err)
	}

	return a
}

// Name returns the argument name.
func (a *InputArgument) Name() string { return a.name }

// IsRequired reports whether the argument is required.
func (a *InputArgument) IsRequired() bool { return a.mode&ArgumentRequired != 0 }

// IsArray reports whether the argument takes multiple values.
func (a *InputArgument) IsArray() bool { return a.mode&ArgumentIsArray != 0 }

// SetDefault sets the default value.
func (a *InputArgument) SetDefault(def any) error {
	if a.IsRequired() && def != nil {
		return newError(KindLogic, "InputArgument.php", 96, "Cannot set a default value except for InputArgument::OPTIONAL mode.")
	}

	if a.IsArray() {
		if def == nil {
			def = []string{}
		} else if !isArrayValue(def) {
			return newError(KindLogic, "InputArgument.php", 103, "A default value for an array argument must be an array.")
		}
	}

	a.def = def

	return nil
}

// Default returns the default value.
func (a *InputArgument) Default() any { return a.def }

// Description returns the description.
func (a *InputArgument) Description() string { return a.description }

// WithSuggestedValues sets a static completion list (Composer's backport).
func (a *InputArgument) WithSuggestedValues(values ...string) *InputArgument {
	a.suggestedValues = values

	return a
}

// WithSuggestFunc sets a completion callback (Composer's backport).
func (a *InputArgument) WithSuggestFunc(fn SuggestFunc) *InputArgument {
	a.suggestFunc = fn

	return a
}

// HasCompletion reports whether suggested values were configured.
func (a *InputArgument) HasCompletion() bool {
	return a.suggestFunc != nil || len(a.suggestedValues) > 0
}

// Complete adds the configured suggestions (Composer's InputArgument::complete).
func (a *InputArgument) Complete(input *CompletionInput, suggestions *CompletionSuggestions) {
	completeSuggested(a.suggestedValues, a.suggestFunc, input, suggestions)
}

func completeSuggested(values []string, fn SuggestFunc, input *CompletionInput, suggestions *CompletionSuggestions) {
	if fn != nil {
		if s := fn(input, suggestions); len(s) > 0 {
			suggestions.SuggestValues(s...)
		}

		return
	}
	if len(values) > 0 {
		suggestions.SuggestStrings(values...)
	}
}

// InputOption is a command line option.
type InputOption struct {
	name        string
	shortcut    string
	mode        int
	def         any
	description string

	suggestedValues []string
	suggestFunc     SuggestFunc
}

// NewInputOption mirrors new InputOption($name, $shortcut, $mode,
// $description, $default). An empty shortcut means none; several shortcuts
// are separated by "|". Mode 0 stands for PHP's null mode (VALUE_NONE); an
// explicit 0, which PHP rejects, cannot be expressed.
func NewInputOption(name, shortcut string, mode int, description string, def any) (*InputOption, error) {
	name = strings.TrimPrefix(name, "--")

	if name == "" || name == "0" {
		return nil, newError(KindInvalidArgument, "InputOption.php", 69, "An option name cannot be empty.")
	}

	if shortcut != "" {
		// array_filter(preg_split('{(\|)-?}', ltrim($shortcut, '-')), 'strlen'):
		// only empty parts are dropped ("0" is a valid shortcut).
		parts := strings.Split(strings.TrimLeft(shortcut, "-"), "|")
		kept := parts[:0]
		for i, p := range parts {
			if i > 0 {
				p = strings.TrimPrefix(p, "-")
			}
			if p != "" {
				kept = append(kept, p)
			}
		}
		shortcut = strings.Join(kept, "|")
		if shortcut == "" {
			return nil, newError(KindInvalidArgument, "InputOption.php", 85, "An option shortcut cannot be empty.")
		}
	}

	if mode == 0 {
		mode = OptionValueNone
	} else if mode >= OptionValueNegatable<<1 || mode < 1 {
		return nil, newError(KindInvalidArgument, "InputOption.php", 92, `Option mode "%d" is not valid.`, mode)
	}

	o := &InputOption{name: name, shortcut: shortcut, mode: mode, description: description}

	if o.IsArray() && !o.AcceptValue() {
		return nil, newError(KindInvalidArgument, "InputOption.php", 101, "Impossible to have an option mode VALUE_IS_ARRAY if the option does not accept a value.")
	}
	if o.IsNegatable() && o.AcceptValue() {
		return nil, newError(KindInvalidArgument, "InputOption.php", 104, "Impossible to have an option mode VALUE_NEGATABLE if the option also accepts a value.")
	}

	if err := o.SetDefault(def); err != nil {
		return nil, err
	}

	return o, nil
}

// MustOption is NewInputOption for static definitions; it panics on
// invalid input.
func MustOption(name, shortcut string, mode int, description string, def any) *InputOption {
	o, err := NewInputOption(name, shortcut, mode, description, def)
	if err != nil {
		panic(err)
	}

	return o
}

// Shortcut returns the shortcuts ("" when none).
func (o *InputOption) Shortcut() string { return o.shortcut }

// shortcutIsTruthy is PHP's `if ($option->getShortcut())`: the shortcut "0"
// is falsy, so such an option gets no usable shortcut.
func (o *InputOption) shortcutIsTruthy() bool { return o.shortcut != "" && o.shortcut != "0" }

// Name returns the option name.
func (o *InputOption) Name() string { return o.name }

// AcceptValue reports whether the option takes a value.
func (o *InputOption) AcceptValue() bool { return o.IsValueRequired() || o.IsValueOptional() }

// IsValueRequired reports VALUE_REQUIRED.
func (o *InputOption) IsValueRequired() bool { return o.mode&OptionValueRequired != 0 }

// IsValueOptional reports VALUE_OPTIONAL.
func (o *InputOption) IsValueOptional() bool { return o.mode&OptionValueOptional != 0 }

// IsArray reports VALUE_IS_ARRAY.
func (o *InputOption) IsArray() bool { return o.mode&OptionValueIsArray != 0 }

// IsNegatable reports VALUE_NEGATABLE.
func (o *InputOption) IsNegatable() bool { return o.mode&OptionValueNegatable != 0 }

// SetDefault sets the default value.
func (o *InputOption) SetDefault(def any) error {
	if o.mode&OptionValueNone != 0 && def != nil {
		return newError(KindLogic, "InputOption.php", 181, "Cannot set a default value when using InputOption::VALUE_NONE mode.")
	}

	if o.IsArray() {
		if def == nil {
			def = []string{}
		} else if !isArrayValue(def) {
			return newError(KindLogic, "InputOption.php", 188, "A default value for an array option must be an array.")
		}
	}

	if o.AcceptValue() || o.IsNegatable() {
		o.def = def
	} else {
		o.def = false
	}

	return nil
}

// Default returns the default value.
func (o *InputOption) Default() any { return o.def }

// Description returns the description.
func (o *InputOption) Description() string { return o.description }

// Equals compares two options like InputOption::equals().
func (o *InputOption) Equals(other *InputOption) bool {
	return other.name == o.name &&
		other.shortcut == o.shortcut &&
		phpIdentical(other.def, o.def) &&
		other.IsNegatable() == o.IsNegatable() &&
		other.IsArray() == o.IsArray() &&
		other.IsValueRequired() == o.IsValueRequired() &&
		other.IsValueOptional() == o.IsValueOptional()
}

// WithSuggestedValues sets a static completion list (Composer's backport).
// It fails like Composer when the option accepts no value.
func (o *InputOption) WithSuggestedValues(values ...string) (*InputOption, error) {
	if len(values) > 0 && !o.AcceptValue() {
		return nil, newError(KindLogic, "src/Composer/Console/Input/InputOption.php", 53, "Cannot set suggested values if the option does not accept a value.")
	}
	o.suggestedValues = values

	return o, nil
}

// WithSuggestFunc sets a completion callback (Composer's backport).
func (o *InputOption) WithSuggestFunc(fn SuggestFunc) (*InputOption, error) {
	if fn != nil && !o.AcceptValue() {
		return nil, newError(KindLogic, "src/Composer/Console/Input/InputOption.php", 53, "Cannot set suggested values if the option does not accept a value.")
	}
	o.suggestFunc = fn

	return o, nil
}

// HasCompletion reports whether suggested values were configured.
func (o *InputOption) HasCompletion() bool {
	return o.suggestFunc != nil || len(o.suggestedValues) > 0
}

// Complete adds the configured suggestions (Composer's InputOption::complete).
func (o *InputOption) Complete(input *CompletionInput, suggestions *CompletionSuggestions) {
	completeSuggested(o.suggestedValues, o.suggestFunc, input, suggestions)
}

// InputDefinition is a collection of arguments and options.
type InputDefinition struct {
	arguments            []*InputArgument
	argIndex             map[string]int
	requiredCount        int
	lastArrayArgument    *InputArgument
	lastOptionalArgument *InputArgument

	options   []*InputOption
	optIndex  map[string]int
	negations map[string]string
	shortcuts map[string]string
}

// NewInputDefinition builds a definition from *InputArgument and
// *InputOption items.
func NewInputDefinition(items ...any) (*InputDefinition, error) {
	d := &InputDefinition{}
	if err := d.SetDefinition(items...); err != nil {
		return nil, err
	}

	return d, nil
}

// MustDefinition is NewInputDefinition for static definitions.
func MustDefinition(items ...any) *InputDefinition {
	d, err := NewInputDefinition(items...)
	if err != nil {
		panic(err)
	}

	return d
}

// SetDefinition replaces arguments and options.
func (d *InputDefinition) SetDefinition(items ...any) error {
	var args []*InputArgument
	var opts []*InputOption
	for _, it := range items {
		switch v := it.(type) {
		case *InputOption:
			opts = append(opts, v)
		case *InputArgument:
			args = append(args, v)
		}
	}
	if err := d.SetArguments(args...); err != nil {
		return err
	}

	return d.SetOptions(opts...)
}

// SetArguments replaces the arguments.
func (d *InputDefinition) SetArguments(arguments ...*InputArgument) error {
	d.arguments = nil
	d.argIndex = map[string]int{}
	d.requiredCount = 0
	d.lastOptionalArgument = nil
	d.lastArrayArgument = nil

	return d.AddArguments(arguments...)
}

// AddArguments adds arguments.
func (d *InputDefinition) AddArguments(arguments ...*InputArgument) error {
	for _, a := range arguments {
		if err := d.AddArgument(a); err != nil {
			return err
		}
	}

	return nil
}

// AddArgument adds one argument.
func (d *InputDefinition) AddArgument(argument *InputArgument) error {
	if d.argIndex == nil {
		d.argIndex = map[string]int{}
	}
	if _, ok := d.argIndex[argument.Name()]; ok {
		return newError(KindLogic, "InputDefinition.php", 100, `An argument with name "%s" already exists.`, argument.Name())
	}

	if d.lastArrayArgument != nil {
		return newError(KindLogic, "InputDefinition.php", 104, `Cannot add a required argument "%s" after an array argument "%s".`, argument.Name(), d.lastArrayArgument.Name())
	}

	if argument.IsRequired() && d.lastOptionalArgument != nil {
		return newError(KindLogic, "InputDefinition.php", 108, `Cannot add a required argument "%s" after an optional one "%s".`, argument.Name(), d.lastOptionalArgument.Name())
	}

	if argument.IsArray() {
		d.lastArrayArgument = argument
	}

	if argument.IsRequired() {
		d.requiredCount++
	} else {
		d.lastOptionalArgument = argument
	}

	d.argIndex[argument.Name()] = len(d.arguments)
	d.arguments = append(d.arguments, argument)

	return nil
}

// Argument returns the named argument.
func (d *InputDefinition) Argument(name string) (*InputArgument, error) {
	if a := d.arg(name); a != nil {
		return a, nil
	}

	return nil, newError(KindInvalidArgument, "InputDefinition.php", 136, `The "%s" argument does not exist.`, name)
}

// ArgumentAt returns the argument at position i.
func (d *InputDefinition) ArgumentAt(i int) (*InputArgument, error) {
	if i >= 0 && i < len(d.arguments) {
		return d.arguments[i], nil
	}

	return nil, newError(KindInvalidArgument, "InputDefinition.php", 136, `The "%d" argument does not exist.`, i)
}

func (d *InputDefinition) arg(name string) *InputArgument {
	if i, ok := d.argIndex[name]; ok {
		return d.arguments[i]
	}

	return nil
}

// HasArgument reports whether the named argument exists.
func (d *InputDefinition) HasArgument(name string) bool {
	_, ok := d.argIndex[name]

	return ok
}

// HasArgumentAt reports whether an argument exists at position i.
func (d *InputDefinition) HasArgumentAt(i int) bool { return i >= 0 && i < len(d.arguments) }

// Arguments returns the arguments in order.
func (d *InputDefinition) Arguments() []*InputArgument { return d.arguments }

// ArgumentCount returns the maximum number of arguments (PHP_INT_MAX with
// an array argument).
func (d *InputDefinition) ArgumentCount() int {
	if d.lastArrayArgument != nil {
		return math.MaxInt // PHP_INT_MAX
	}

	return len(d.arguments)
}

// ArgumentRequiredCount returns the number of required arguments.
func (d *InputDefinition) ArgumentRequiredCount() int { return d.requiredCount }

// ArgumentDefaults returns the default values in order.
func (d *InputDefinition) ArgumentDefaults() []NamedValue {
	out := make([]NamedValue, len(d.arguments))
	for i, a := range d.arguments {
		out[i] = NamedValue{a.Name(), a.Default()}
	}

	return out
}

// SetOptions replaces the options.
func (d *InputDefinition) SetOptions(options ...*InputOption) error {
	d.options = nil
	d.optIndex = map[string]int{}
	d.shortcuts = map[string]string{}
	d.negations = map[string]string{}

	return d.AddOptions(options...)
}

// AddOptions adds options.
func (d *InputDefinition) AddOptions(options ...*InputOption) error {
	for _, o := range options {
		if err := d.AddOption(o); err != nil {
			return err
		}
	}

	return nil
}

// AddOption adds one option.
func (d *InputDefinition) AddOption(option *InputOption) error {
	if d.optIndex == nil {
		d.optIndex = map[string]int{}
		d.shortcuts = map[string]string{}
		d.negations = map[string]string{}
	}
	if i, ok := d.optIndex[option.Name()]; ok && !option.Equals(d.options[i]) {
		return newError(KindLogic, "InputDefinition.php", 232, `An option named "%s" already exists.`, option.Name())
	}
	if _, ok := d.negations[option.Name()]; ok {
		return newError(KindLogic, "InputDefinition.php", 235, `An option named "%s" already exists.`, option.Name())
	}

	if option.shortcutIsTruthy() {
		for s := range strings.SplitSeq(option.Shortcut(), "|") {
			if n, ok := d.shortcuts[s]; ok && !option.Equals(d.options[d.optIndex[n]]) {
				return newError(KindLogic, "InputDefinition.php", 241, `An option with shortcut "%s" already exists.`, s)
			}
		}
	}

	if i, ok := d.optIndex[option.Name()]; ok {
		d.options[i] = option
	} else {
		d.optIndex[option.Name()] = len(d.options)
		d.options = append(d.options, option)
	}

	if option.shortcutIsTruthy() {
		for s := range strings.SplitSeq(option.Shortcut(), "|") {
			d.shortcuts[s] = option.Name()
		}
	}

	if option.IsNegatable() {
		negatedName := "no-" + option.Name()
		if _, ok := d.optIndex[negatedName]; ok {
			return newError(KindLogic, "InputDefinition.php", 256, `An option named "%s" already exists.`, negatedName)
		}
		d.negations[negatedName] = option.Name()
	}

	return nil
}

// Option returns the named option.
func (d *InputDefinition) Option(name string) (*InputOption, error) {
	if o := d.opt(name); o != nil {
		return o, nil
	}

	return nil, newError(KindInvalidArgument, "InputDefinition.php", 272, `The "--%s" option does not exist.`, name)
}

func (d *InputDefinition) opt(name string) *InputOption {
	if i, ok := d.optIndex[name]; ok {
		return d.options[i]
	}

	return nil
}

// HasOption reports whether the named option exists.
func (d *InputDefinition) HasOption(name string) bool {
	_, ok := d.optIndex[name]

	return ok
}

// Options returns the options in order.
func (d *InputDefinition) Options() []*InputOption { return d.options }

// HasShortcut reports whether the shortcut exists.
func (d *InputDefinition) HasShortcut(name string) bool {
	_, ok := d.shortcuts[name]

	return ok
}

// HasNegation reports whether name is a negated option name ("no-foo").
func (d *InputDefinition) HasNegation(name string) bool {
	_, ok := d.negations[name]

	return ok
}

// OptionForShortcut returns the option behind a shortcut.
func (d *InputDefinition) OptionForShortcut(shortcut string) (*InputOption, error) {
	name, err := d.ShortcutToName(shortcut)
	if err != nil {
		return nil, err
	}

	return d.Option(name)
}

// OptionDefaults returns the option defaults in order.
func (d *InputDefinition) OptionDefaults() []NamedValue {
	out := make([]NamedValue, len(d.options))
	for i, o := range d.options {
		out[i] = NamedValue{o.Name(), o.Default()}
	}

	return out
}

// ShortcutToName returns the option name of a shortcut.
func (d *InputDefinition) ShortcutToName(shortcut string) (string, error) {
	n, ok := d.shortcuts[shortcut]
	if !ok {
		return "", newError(KindInvalidArgument, "InputDefinition.php", 352, `The "-%s" option does not exist.`, shortcut)
	}

	return n, nil
}

// NegationToName returns the option name of a negation.
func (d *InputDefinition) NegationToName(negation string) (string, error) {
	n, ok := d.negations[negation]
	if !ok {
		return "", newError(KindInvalidArgument, "InputDefinition.php", 368, `The "--%s" option does not exist.`, negation)
	}

	return n, nil
}

// Synopsis returns the definition synopsis.
func (d *InputDefinition) Synopsis(short bool) string {
	elements := make([]string, 0, len(d.options)+len(d.arguments)+1)

	if short && len(d.options) > 0 {
		elements = append(elements, "[options]")
	} else if !short {
		for _, o := range d.options {
			value := ""
			if o.AcceptValue() {
				if o.IsValueOptional() {
					value = " [" + php.Strtoupper(o.Name()) + "]"
				} else {
					value = " " + php.Strtoupper(o.Name())
				}
			}

			shortcut := ""
			if o.shortcutIsTruthy() {
				shortcut = "-" + o.Shortcut() + "|"
			}
			negation := ""
			if o.IsNegatable() {
				negation = "|--no-" + o.Name()
			}

			elements = append(elements, "["+shortcut+"--"+o.Name()+value+negation+"]")
		}
	}

	if len(elements) > 0 && len(d.arguments) > 0 {
		elements = append(elements, "[--]")
	}

	optional := 0
	for _, a := range d.arguments {
		element := "<" + a.Name() + ">"
		if a.IsArray() {
			element += "..."
		}

		if !a.IsRequired() {
			element = "[" + element
			optional++
		}

		elements = append(elements, element)
	}

	return strings.Join(elements, " ") + strings.Repeat("]", optional)
}

// NamedValue is one entry of an ordered name => value list.
type NamedValue struct {
	Name  string
	Value any
}

func isArrayValue(v any) bool {
	switch v.(type) {
	case []string, []any:
		return true
	}

	return false
}

// phpIdentical is PHP's === for input values.
func phpIdentical(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)

		return ok && x == y
	case string:
		y, ok := b.(string)

		return ok && x == y
	case int:
		y, ok := b.(int)

		return ok && x == y
	case float64:
		y, ok := b.(float64)

		return ok && x == y
	case []string:
		switch y := b.(type) {
		case []string:
			if len(x) != len(y) {
				return false
			}
			for i := range x {
				if x[i] != y[i] {
					return false
				}
			}

			return true
		case []any:
			if len(x) != len(y) {
				return false
			}
			for i := range x {
				if !phpIdentical(x[i], y[i]) {
					return false
				}
			}

			return true
		}

		return false
	case []any:
		switch y := b.(type) {
		case []string:
			return phpIdentical(y, x)
		case []any:
			if len(x) != len(y) {
				return false
			}
			for i := range x {
				if !phpIdentical(x[i], y[i]) {
					return false
				}
			}

			return true
		}

		return false
	}

	return false
}

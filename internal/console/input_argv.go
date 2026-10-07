// Ports src/Input/ArgvInput.php and StringInput.php (symfony/console).

package console

import (
	"errors"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/maestro/internal/php"
)

// ArgvInput parses command line tokens.
type ArgvInput struct {
	BaseInput
	tokens []string
	parsed []string
	pos    int
	// suppressErrors makes parseToken swallow RuntimeExceptions
	// (CompletionInput::parseToken).
	suppressErrors bool
}

// NewArgvInput mirrors new ArgvInput($argv, $definition): argv[0] (the
// application name) is dropped. A nil argv uses os.Args.
func NewArgvInput(argv []string, definition *InputDefinition) (*ArgvInput, error) {
	if argv == nil {
		argv = os.Args
	}
	in := &ArgvInput{}
	if len(argv) > 0 {
		in.tokens = append([]string(nil), argv[1:]...)
	}
	if err := in.init(definition, in.parseTokens, `Symfony\Component\Console\Input\ArgvInput`); err != nil {
		return in, err
	}

	return in, nil
}

// Tokens returns the raw tokens.
func (in *ArgvInput) Tokens() []string { return in.tokens }

// SetTokens replaces the raw tokens.
func (in *ArgvInput) SetTokens(tokens []string) { in.tokens = tokens }

// Clone implements Input.
func (in *ArgvInput) Clone() Input {
	c := &ArgvInput{BaseInput: in.cloneBase(), tokens: in.tokens, suppressErrors: in.suppressErrors}
	c.parse = c.parseTokens

	return c
}

func (in *ArgvInput) parseTokens() error {
	parseOptions := true
	in.parsed = append(in.parsed[:0], in.tokens...)
	in.pos = 0
	for in.pos < len(in.parsed) {
		token := in.parsed[in.pos]
		in.pos++
		var err error
		parseOptions, err = in.parseToken(token, parseOptions)
		if err != nil {
			return err
		}
	}

	return nil
}

func (in *ArgvInput) shift() (string, bool) {
	if in.pos >= len(in.parsed) {
		return "", false
	}
	t := in.parsed[in.pos]
	in.pos++

	return t, true
}

func (in *ArgvInput) unshift(token string) {
	if in.pos > 0 {
		in.pos--
		in.parsed[in.pos] = token

		return
	}
	in.parsed = append([]string{token}, in.parsed...)
}

func (in *ArgvInput) parseToken(token string, parseOptions bool) (bool, error) {
	var err error
	switch {
	case parseOptions && token == "":
		err = in.parseArgument(token)
	case parseOptions && token == "--":
		return false, nil
	case parseOptions && strings.HasPrefix(token, "--"):
		err = in.parseLongOption(token)
	case parseOptions && token[0] == '-' && token != "-":
		err = in.parseShortOption(token)
	default:
		err = in.parseArgument(token)
	}
	if err != nil && in.suppressErrors {
		var e *Error
		if errors.As(err, &e) && (e.Kind == KindRuntime || e.Kind == KindMissingInput) {
			// suppress errors, completed input is almost never valid
			return parseOptions, nil
		}
	}

	return parseOptions, err
}

func (in *ArgvInput) parseShortOption(token string) error {
	name := token[1:]

	if len(name) > 1 {
		if o := in.optionForShortcut(name[:1]); o != nil && o.AcceptValue() {
			// an option with a value (with no space)
			return in.addShortOption(name[:1], name[1:], true)
		}

		return in.parseShortOptionSet(name)
	}

	return in.addShortOption(name, "", false)
}

func (in *ArgvInput) optionForShortcut(s string) *InputOption {
	if n, ok := in.definition.shortcuts[s]; ok {
		return in.definition.opt(n)
	}

	return nil
}

func (in *ArgvInput) parseShortOptionSet(name string) error {
	l := len(name)
	for i := range l {
		o := in.optionForShortcut(name[i : i+1])
		if o == nil {
			c := name[i : i+1]
			if utf8.ValidString(name) {
				// mb_substr($name, $i, 1): $i is a byte offset used as a
				// character offset.
				c = Substr(name, i, 1, false)
			}

			return newError(KindRuntime, `The "-%s" option does not exist.`, c)
		}

		if o.AcceptValue() {
			if i == l-1 {
				return in.addLongOption(o.Name(), "", false)
			}

			return in.addLongOption(o.Name(), name[i+1:], true)
		}
		if err := in.addLongOption(o.Name(), "", false); err != nil {
			return err
		}
	}

	return nil
}

func (in *ArgvInput) parseLongOption(token string) error {
	name := token[2:]

	if optName, value, ok := strings.Cut(name, "="); ok {
		if value == "" {
			in.unshift(value)
		}

		return in.addLongOption(optName, value, true)
	}

	return in.addLongOption(name, "", false)
}

func (in *ArgvInput) parseArgument(token string) error {
	c := len(in.arguments)
	def := in.definition

	// if input is expecting another argument, add it
	if c < len(def.arguments) {
		arg := def.arguments[c]
		if arg.IsArray() {
			in.arguments[arg.Name()] = []string{token}
		} else {
			in.arguments[arg.Name()] = token
		}

		return nil
	}

	// if last argument isArray(), append token to last argument
	if c-1 >= 0 && c-1 < len(def.arguments) && def.arguments[c-1].IsArray() {
		arg := def.arguments[c-1]
		list, _ := in.arguments[arg.Name()].([]string)
		in.arguments[arg.Name()] = append(list, token)

		return nil
	}

	// unexpected argument
	all := def.arguments
	// $symfonyCommandName is only used when truthy ("0" is not).
	symfonyCommandName := ""
	if len(all) > 0 && all[0].Name() == "command" {
		if v := in.arguments["command"]; inputTruthy(v) {
			symfonyCommandName = phpToString(v)
		}
		all = all[1:]
	}

	var message string
	switch {
	case len(all) > 0:
		names := make([]string, len(all))
		for i, a := range all {
			names[i] = a.Name()
		}
		if symfonyCommandName != "" {
			message = `Too many arguments to "` + symfonyCommandName + `" command, expected arguments "` + strings.Join(names, `" "`) + `".`
		} else {
			message = `Too many arguments, expected arguments "` + strings.Join(names, `" "`) + `".`
		}
	case symfonyCommandName != "":
		message = `No arguments expected for "` + symfonyCommandName + `" command, got "` + token + `".`
	default:
		message = `No arguments expected, got "` + token + `".`
	}

	return newError(KindRuntime, "%s", message)
}

func (in *ArgvInput) addShortOption(shortcut, value string, hasValue bool) error {
	o := in.optionForShortcut(shortcut)
	if o == nil {
		return newError(KindRuntime, `The "-%s" option does not exist.`, shortcut)
	}

	return in.addLongOption(o.Name(), value, hasValue)
}

// addLongOption takes the value as (value, hasValue) where !hasValue is PHP's null.
func (in *ArgvInput) addLongOption(name, value string, hasValue bool) error {
	def := in.definition
	o := def.opt(name)
	if o == nil {
		optionName, ok := def.negations[name]
		if !ok {
			e := newError(KindRuntime, `The "--%s" option does not exist.`, name)
			e.Alternatives = optionAlternatives(def, name)

			return e
		}
		if hasValue {
			return newError(KindRuntime, `The "--%s" option does not accept a value.`, name)
		}
		in.options[optionName] = false

		return nil
	}

	if hasValue && !o.AcceptValue() {
		return newError(KindRuntime, `The "--%s" option does not accept a value.`, name)
	}

	if (!hasValue || value == "") && o.AcceptValue() && in.pos < len(in.parsed) {
		// if option accepts an optional or mandatory argument
		// let's see if there is one provided
		next, _ := in.shift()
		if next == "" || next[0] != '-' {
			value, hasValue = next, true
		} else {
			in.unshift(next)
		}
	}

	var v any
	if hasValue {
		v = value
	} else {
		if o.IsValueRequired() {
			return newError(KindRuntime, `The "--%s" option requires a value.`, name)
		}

		if !o.IsArray() && !o.IsValueOptional() {
			v = true
		}
	}

	if o.IsArray() {
		list, _ := in.options[name].([]any)
		in.options[name] = append(list, v)
	} else {
		in.options[name] = v
	}

	return nil
}

// FirstArgument implements Input.
func (in *ArgvInput) FirstArgument() string {
	isOption := false
	for i, token := range in.tokens {
		if token != "" && token[0] == '-' {
			if strings.Contains(token, "=") || i+1 >= len(in.tokens) {
				continue
			}

			// If it's a long option, consider that everything after "--" is the option name.
			// Otherwise, use the last char (if it's a short option set, only the last one can take a value with space separator)
			var name string
			if len(token) > 1 && token[1] == '-' {
				name = token[2:]
			} else {
				name = token[len(token)-1:]
			}
			v, set := in.options[name]
			set = set && v != nil
			switch {
			case !set && !in.definition.HasShortcut(name):
				// noop
			default:
				if !set {
					name = in.definition.shortcuts[name]
					v, set = in.options[name]
					set = set && v != nil
				}
				if s, ok := v.(string); set && ok && in.tokens[i+1] == s {
					isOption = true
				}
			}

			continue
		}

		if isOption {
			isOption = false

			continue
		}

		return token
	}

	return ""
}

// HasParameterOption implements Input.
func (in *ArgvInput) HasParameterOption(values []string, onlyParams bool) bool {
	for _, token := range in.tokens {
		if onlyParams && token == "--" {
			return false
		}
		for _, value := range values {
			// Options with values:
			//   For long options, test for '--option=' at beginning
			//   For short options, test for '-o' at beginning
			leading := value
			if strings.HasPrefix(value, "--") {
				leading = value + "="
			}
			if token == value || (leading != "" && strings.HasPrefix(token, leading)) {
				return true
			}
		}
	}

	return false
}

// ParameterOption implements Input.
func (in *ArgvInput) ParameterOption(values []string, def any, onlyParams bool) any {
	tokens := in.tokens
	for len(tokens) > 0 {
		token := tokens[0]
		tokens = tokens[1:]
		if onlyParams && token == "--" {
			return def
		}

		for _, value := range values {
			if token == value {
				if len(tokens) == 0 {
					return nil
				}

				return tokens[0]
			}
			// Options with values:
			//   For long options, test for '--option=' at beginning
			//   For short options, test for '-o' at beginning
			leading := value
			if strings.HasPrefix(value, "--") {
				leading = value + "="
			}
			if leading != "" && strings.HasPrefix(token, leading) {
				return token[len(leading):]
			}
		}
	}

	return def
}

// String implements Input.
func (in *ArgvInput) String() string {
	parts := make([]string, len(in.tokens))
	for i, token := range in.tokens {
		parts[i] = argvTokenString(token)
	}

	return strings.Join(parts, " ")
}

func argvTokenString(token string) string {
	// preg_match('{^(-[^=]+=)(.+)}', $token, $match)
	if len(token) > 0 && token[0] == '-' {
		if eq := strings.IndexByte(token[1:], '='); eq > 0 {
			eq++
			rest := token[eq+1:]
			if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
				rest = rest[:nl]
			}
			if rest != "" {
				return token[:eq+1] + EscapeToken(rest)
			}
		}
	}

	if php.Truthy(token) && token[0] != '-' {
		return EscapeToken(token)
	}

	return token
}

// StringInput parses a command line string.
type StringInput struct {
	*ArgvInput
}

// NewStringInput mirrors new StringInput($input).
func NewStringInput(input string) (*StringInput, error) {
	argv, _ := NewArgvInput([]string{}, nil)
	tokens, err := tokenizeString(input)
	if err != nil {
		return nil, err
	}
	argv.SetTokens(tokens)

	return &StringInput{ArgvInput: argv}, nil
}

// Clone implements Input.
func (in *StringInput) Clone() Input {
	a, _ := in.ArgvInput.Clone().(*ArgvInput)

	return &StringInput{ArgvInput: a}
}

// StringInput::REGEX_UNQUOTED_STRING and REGEX_QUOTED_STRING.
const regexUnquotedString = `([^\s\\]+?)`

const regexQuotedString = `(?:"([^"\\]*(?:\\.[^"\\]*)*)"|'([^'\\]*(?:\\.[^'\\]*)*)')`

// StringInput's patterns, compiled once.
var (
	stringInputSpace        = php.MustCompile(`/\s+/A`)
	stringInputQuotedOption = php.MustCompile(`/([^="'\s]+?)(=?)(` + regexQuotedString + `+)/A`)
	stringInputQuoted       = php.MustCompile(`/` + regexQuotedString + `/A`)
	stringInputUnquoted     = php.MustCompile(`/` + regexUnquotedString + `/A`)
)

var quoteJoins = []string{`"'`, `'"`, `''`, `""`}

func tokenizeString(input string) ([]string, error) {
	var tokens []string
	length := len(input)
	cursor := 0
	var token strings.Builder
	hasToken := false
	for cursor < length {
		if input[cursor] == '\\' {
			cursor++
			if cursor < length {
				token.WriteByte(input[cursor])
			}
			hasToken = true
			cursor++

			continue
		}

		// Raw preg_match(): a failed match (false) tries the next branch.
		var matched int
		if m, _ := stringInputSpace.MatchAt(input, cursor); m != nil {
			if hasToken {
				tokens = append(tokens, token.String())
				token.Reset()
				hasToken = false
			}
			matched = len(m.Get(0))
		} else if m, _ := stringInputQuotedOption.MatchAt(input, cursor); m != nil {
			inner := php.SubstrLen(m.Get(3), 1, -1)
			for _, q := range quoteJoins {
				inner = strings.ReplaceAll(inner, q, "")
			}
			token.WriteString(m.Get(1) + m.Get(2) + php.Stripcslashes(inner))
			hasToken = true
			matched = len(m.Get(0))
		} else if m, _ := stringInputQuoted.MatchAt(input, cursor); m != nil {
			token.WriteString(php.Stripcslashes(php.SubstrLen(m.Get(0), 1, -1)))
			hasToken = true
			matched = len(m.Get(0))
		} else if m, _ := stringInputUnquoted.MatchAt(input, cursor); m != nil {
			token.WriteString(m.Get(1))
			hasToken = true
			matched = len(m.Get(0))
		} else {
			// should never happen
			return nil, newError(KindInvalidArgument, `Unable to parse input near "... %s ...".`, php.SubstrLen(input, cursor, 10))
		}

		cursor += matched
	}

	if hasToken {
		tokens = append(tokens, token.String())
	}

	return tokens, nil
}

// optionAlternatives are the long options of def, negations included,
// that a mistyped name may have meant (FindAlternatives, as for
// commands), each with its "--": maestro's suggestion, which Symfony's
// exception does not make, so its message stays Symfony's.
func optionAlternatives(def *InputDefinition, name string) []string {
	names := make([]string, 0, len(def.options)+len(def.negations))
	for _, o := range def.options {
		names = append(names, o.Name())
	}
	for negation := range def.negations {
		names = append(names, negation)
	}
	alternatives := FindAlternatives(name, names)
	for i, a := range alternatives {
		alternatives[i] = "--" + a
	}

	return alternatives
}

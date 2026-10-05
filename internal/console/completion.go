// Ports src/Completion/CompletionInput.php, CompletionSuggestions.php and
// Suggestion.php (symfony/console).

package console

import (
	"strings"
)

// Completion types.
const (
	CompletionTypeArgumentValue = "argument_value"
	CompletionTypeOptionValue   = "option_value"
	CompletionTypeOptionName    = "option_name"
	CompletionTypeNone          = "none"
)

// Suggestion is a suggested completion value.
type Suggestion struct {
	Value string
}

// String returns the value.
func (s Suggestion) String() string { return s.Value }

// CompletionSuggestions collects completion suggestions.
type CompletionSuggestions struct {
	values  []Suggestion
	options []*InputOption
}

// SuggestValues adds value suggestions.
func (s *CompletionSuggestions) SuggestValues(values ...Suggestion) *CompletionSuggestions {
	s.values = append(s.values, values...)

	return s
}

// SuggestStrings adds plain string value suggestions.
func (s *CompletionSuggestions) SuggestStrings(values ...string) *CompletionSuggestions {
	for _, v := range values {
		s.values = append(s.values, Suggestion{v})
	}

	return s
}

// SuggestOptions adds option suggestions.
func (s *CompletionSuggestions) SuggestOptions(options ...*InputOption) *CompletionSuggestions {
	s.options = append(s.options, options...)

	return s
}

// OptionSuggestions returns the suggested options.
func (s *CompletionSuggestions) OptionSuggestions() []*InputOption { return s.options }

// ValueSuggestions returns the suggested values.
func (s *CompletionSuggestions) ValueSuggestions() []Suggestion { return s.values }

// CompletionInput is an ArgvInput for the command line being completed.
type CompletionInput struct {
	*ArgvInput
	tokens          []string
	currentIndex    int
	completionType  string
	completionName  string
	hasName         bool
	completionValue string
}

// CompletionInputFromString splits a shell command line into tokens.
func CompletionInputFromString(inputStr string, currentIndex int) *CompletionInput {
	// preg_match_all('/(?<=^|\s)([\'"]?)(.+?)(?<!\\\\)\1(?=$|\s)/', $inputStr, $tokens)
	var tokens []string
	n := len(inputStr)
	isSpace := func(c byte) bool {
		return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
	}
	for start := 0; start < n; {
		if start > 0 && !isSpace(inputStr[start-1]) {
			start++

			continue
		}
		end := matchCompletionToken(inputStr, start)
		if end < 0 {
			start++

			continue
		}
		tokens = append(tokens, inputStr[start:end])
		if end == start {
			end++
		}
		start = end
	}

	return CompletionInputFromTokens(tokens, currentIndex)
}

// matchCompletionToken matches ([\'"]?)(.+?)(?<!\\)\1(?=$|\s) at start and
// returns the end offset, or -1.
func matchCompletionToken(s string, start int) int {
	n := len(s)
	try := func(quote string) int {
		bodyStart := start + len(quote)
		// .+? : at least one char, no newlines, shortest first.
		for end := bodyStart + 1; end <= n; end++ {
			if s[end-1] == '\n' {
				return -1
			}
			if end+len(quote) > n || s[end:end+len(quote)] != quote {
				continue
			}
			// (?<!\\) before the closing quote (or before the end when unquoted)
			if s[end-1] == '\\' {
				continue
			}
			after := end + len(quote)
			if after == n || after == n-1 && s[after] == '\n' || s[after] == ' ' || s[after] == '\t' ||
				s[after] == '\n' || s[after] == '\v' || s[after] == '\f' || s[after] == '\r' {
				return after
			}
		}

		return -1
	}
	if start < n && (s[start] == '\'' || s[start] == '"') {
		if e := try(s[start : start+1]); e >= 0 {
			return e
		}
	}

	return try("")
}

// CompletionInputFromTokens mirrors CompletionInput::fromTokens().
func CompletionInputFromTokens(tokens []string, currentIndex int) *CompletionInput {
	if tokens == nil {
		tokens = []string{}
	}
	// new self($tokens): like any ArgvInput the first token is dropped as
	// the application name; the completion logic sees all of them.
	argv, _ := NewArgvInput(tokens, nil)
	argv.suppressErrors = true

	return &CompletionInput{ArgvInput: argv, tokens: tokens, currentIndex: currentIndex}
}

// Bind implements Input.
func (in *CompletionInput) Bind(definition *InputDefinition) error {
	if err := in.ArgvInput.Bind(definition); err != nil {
		return err
	}

	relevantToken := in.relevantToken()
	if relevantToken != "" && relevantToken[0] == '-' {
		// the current token is an input option: complete either option name or option value
		optionToken, optionValue, _ := strings.Cut(relevantToken, "=")

		option := in.optionFromToken(optionToken)
		if option == nil && !in.isCursorFree() {
			in.completionType = CompletionTypeOptionName
			in.completionValue = relevantToken

			return nil
		}

		if option != nil && option.AcceptValue() {
			in.completionType = CompletionTypeOptionValue
			in.completionName, in.hasName = option.Name(), true
			switch {
			case optionValue != "" && optionValue != "0":
				in.completionValue = optionValue
			case !strings.HasPrefix(optionToken, "--"):
				in.completionValue = phpSubstrFrom(optionToken, 2)
			default:
				in.completionValue = ""
			}

			return nil
		}
	}

	previousToken := ""
	if in.currentIndex-1 >= 0 && in.currentIndex-1 < len(in.tokens) {
		previousToken = in.tokens[in.currentIndex-1]
	}
	if previousToken != "" && previousToken[0] == '-' && strings.Trim(previousToken, "-") != "" {
		// check if previous option accepted a value
		previousOption := in.optionFromToken(previousToken)
		if previousOption != nil && previousOption.AcceptValue() {
			in.completionType = CompletionTypeOptionValue
			in.completionName, in.hasName = previousOption.Name(), true
			in.completionValue = relevantToken

			return nil
		}
	}

	// complete argument value
	in.completionType = CompletionTypeArgumentValue

	argumentName := ""
	var lastArg *InputArgument
	for _, argument := range in.definition.Arguments() {
		argumentName = argument.Name()
		lastArg = argument
		v, ok := in.arguments[argumentName]
		if !ok || v == nil {
			break
		}

		in.completionName, in.hasName = argumentName, true
		if list, isList := v.([]string); isList {
			if len(list) > 0 {
				in.completionValue = list[len(list)-1]
			} else {
				in.completionValue = ""
			}
		} else {
			in.completionValue = phpToString(v)
		}
	}

	if in.currentIndex >= len(in.tokens) {
		v, ok := in.arguments[argumentName]
		if !ok || v == nil || (lastArg != nil && lastArg.IsArray()) {
			in.completionName, in.hasName = argumentName, true
			in.completionValue = ""
		} else {
			// we've reached the end
			in.completionType = CompletionTypeNone
			in.completionName, in.hasName = "", false
			in.completionValue = ""
		}
	}

	return nil
}

// CompletionType returns the completion type.
func (in *CompletionInput) CompletionType() string { return in.completionType }

// CompletionName returns the argument or option name being completed ("" for none).
func (in *CompletionInput) CompletionName() string { return in.completionName }

// CompletionValue returns the value typed so far.
func (in *CompletionInput) CompletionValue() string { return in.completionValue }

// MustSuggestOptionValuesFor reports whether option values must be suggested.
func (in *CompletionInput) MustSuggestOptionValuesFor(optionName string) bool {
	return in.completionType == CompletionTypeOptionValue && in.hasName && optionName == in.completionName
}

// MustSuggestArgumentValuesFor reports whether argument values must be suggested.
func (in *CompletionInput) MustSuggestArgumentValuesFor(argumentName string) bool {
	return in.completionType == CompletionTypeArgumentValue && in.hasName && argumentName == in.completionName
}

func (in *CompletionInput) optionFromToken(optionToken string) *InputOption {
	optionName := strings.TrimLeft(optionToken, "-")
	if optionName == "" || optionName == "0" {
		return nil
	}

	if len(optionToken) > 1 && optionToken[1] == '-' {
		// long option name
		return in.definition.opt(optionName)
	}

	// short option name
	return in.optionForShortcut(optionName[:1])
}

func (in *CompletionInput) relevantToken() string {
	i := in.currentIndex
	if in.isCursorFree() {
		i--
	}
	if i < 0 || i >= len(in.tokens) {
		return ""
	}

	return in.tokens[i]
}

func (in *CompletionInput) isCursorFree() bool {
	if in.currentIndex > len(in.tokens) {
		panic(newError(KindSPLLogic, "CompletionInput.php", 231, "Current index is invalid, it must be the number of input tokens or one more."))
	}

	return in.currentIndex >= len(in.tokens)
}

// Clone implements Input.
func (in *CompletionInput) Clone() Input {
	a, _ := in.ArgvInput.Clone().(*ArgvInput)
	c := *in
	c.ArgvInput = a

	return &c
}

// String implements Input.
func (in *CompletionInput) String() string {
	var b strings.Builder
	last := -1
	for i, token := range in.tokens {
		b.WriteString(token)
		if in.currentIndex == i {
			b.WriteByte('|')
		}
		b.WriteByte(' ')
		last = i
	}
	if in.currentIndex > last {
		b.WriteByte('|')
	}

	return strings.TrimRight(b.String(), " \t\n\r\x00\x0B")
}

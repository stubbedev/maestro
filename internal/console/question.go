// Ports src/Question/Question.php, ConfirmationQuestion.php and
// ChoiceQuestion.php (symfony/console), and Composer's
// src/Composer/Question/StrictConfirmationQuestion.php.
//
// Answers and defaults use the internal/php value model (nil, bool, int64,
// float64, string, *php.Array): ChoiceQuestion choices are PHP arrays whose
// keys matter, and a multiselect answer is a list.

package console

import (
	"iter"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// Questioner is implemented by every question type; QuestionHelper
// dispatches on the concrete type like PHP's instanceof checks.
type Questioner interface {
	Q() *Question
}

// Validator is a question validator: it returns the validated answer or an
// error (PHP throws, usually an InvalidArgumentException).
type Validator func(answer any) (any, error)

// Normalizer normalizes a raw answer before validation. A normalizer that
// returns an error throws it (QuestionHelper::ask returns it).
type Normalizer func(answer any) any

// Question is a question asked to the user.
type Question struct {
	question       string
	attempts       int // 0 means unlimited (PHP null)
	hidden         bool
	hiddenFallback bool
	autocompleter  func(input string) []string
	validator      Validator
	def            any
	normalizer     Normalizer
	trimmable      bool
	multiline      bool
}

// NewQuestion mirrors new Question($question, $default).
func NewQuestion(question string, def any) *Question {
	return &Question{question: question, def: def, hiddenFallback: true, trimmable: true}
}

// Q implements Questioner.
func (q *Question) Q() *Question { return q }

// Question returns the question text.
func (q *Question) Question() string { return q.question }

// Default returns the default answer.
func (q *Question) Default() any { return q.def }

// IsMultiline reports whether the answer may span several lines.
func (q *Question) IsMultiline() bool { return q.multiline }

// SetMultiline sets whether the answer may span several lines.
func (q *Question) SetMultiline(multiline bool) *Question {
	q.multiline = multiline

	return q
}

// IsHidden reports whether the answer is hidden.
func (q *Question) IsHidden() bool { return q.hidden }

// SetHidden hides the answer; a hidden question cannot autocomplete.
func (q *Question) SetHidden(hidden bool) error {
	if q.autocompleter != nil {
		return newError(KindLogic, "Question.php", 105, "A hidden question cannot use the autocompleter.")
	}
	q.hidden = hidden

	return nil
}

// IsHiddenFallback reports whether a hidden question falls back to a
// visible one when the response cannot be hidden.
func (q *Question) IsHiddenFallback() bool { return q.hiddenFallback }

// SetHiddenFallback sets the hidden fallback.
func (q *Question) SetHiddenFallback(fallback bool) *Question {
	q.hiddenFallback = fallback

	return q
}

// AutocompleterValues returns the autocompleter values (nil when none).
func (q *Question) AutocompleterValues() []string {
	if q.autocompleter == nil {
		return nil
	}

	return q.autocompleter("")
}

// SetAutocompleterValues sets the autocompleter values from a PHP array:
// for an associative array its keys followed by its values, otherwise its
// values. A nil array removes the autocompleter.
func (q *Question) SetAutocompleterValues(values *php.Array) error {
	if values == nil {
		return q.SetAutocompleterCallback(nil)
	}

	list := make([]string, 0, 2*values.Len())
	if isAssoc(values) {
		for k := range values.All() {
			list = append(list, k.String())
		}
	}
	for _, v := range values.All() {
		list = append(list, php.ToString(v))
	}

	return q.SetAutocompleterCallback(func(string) []string { return list })
}

// SetAutocompleterSeq sets the autocompleter values from a Traversable: its
// values (keys are dropped), collected on first use and cached.
func (q *Question) SetAutocompleterSeq(values iter.Seq[string]) error {
	var cache []string
	cached := false

	return q.SetAutocompleterCallback(func(string) []string {
		if !cached {
			cache, cached = []string{}, true
			for v := range values {
				cache = append(cache, v)
			}
		}

		return cache
	})
}

// AutocompleterCallback returns the autocompleter callback.
func (q *Question) AutocompleterCallback() func(input string) []string { return q.autocompleter }

// SetAutocompleterCallback sets the autocompleter callback.
func (q *Question) SetAutocompleterCallback(callback func(input string) []string) error {
	if q.hidden && callback != nil {
		return newError(KindLogic, "Question.php", 192, "A hidden question cannot use the autocompleter.")
	}
	q.autocompleter = callback

	return nil
}

// SetValidator sets the validator.
func (q *Question) SetValidator(validator Validator) *Question {
	q.validator = validator

	return q
}

// Validator returns the validator.
func (q *Question) Validator() Validator { return q.validator }

// SetMaxAttempts sets the maximum number of attempts, which must be
// positive. ResetMaxAttempts is setMaxAttempts(null).
func (q *Question) SetMaxAttempts(attempts int) error {
	if attempts < 1 {
		return newError(KindInvalidArgument, "Question.php", 234, "Maximum number of attempts must be a positive value.")
	}
	q.attempts = attempts

	return nil
}

// ResetMaxAttempts allows unlimited attempts (setMaxAttempts(null)).
func (q *Question) ResetMaxAttempts() { q.attempts = 0 }

// MaxAttempts returns the maximum number of attempts; 0 means unlimited.
func (q *Question) MaxAttempts() int { return q.attempts }

// SetNormalizer sets the normalizer.
func (q *Question) SetNormalizer(normalizer Normalizer) *Question {
	q.normalizer = normalizer

	return q
}

// Normalizer returns the normalizer.
func (q *Question) Normalizer() Normalizer { return q.normalizer }

// IsTrimmable reports whether the answer is trimmed.
func (q *Question) IsTrimmable() bool { return q.trimmable }

// SetTrimmable sets whether the answer is trimmed.
func (q *Question) SetTrimmable(trimmable bool) *Question {
	q.trimmable = trimmable

	return q
}

// isAssoc is Question::isAssoc(): whether any key is a string.
func isAssoc(a *php.Array) bool {
	for k := range a.All() {
		if k.IsString() {
			return true
		}
	}

	return false
}

// ConfirmationQuestion is a yes/no question.
type ConfirmationQuestion struct {
	*Question
	trueAnswerRegex *php.Regexp
}

// defaultTrueAnswerRegex is '/^y/i'.
var defaultTrueAnswerRegex = php.MustCompile(`/^y/i`)

// NewConfirmationQuestion mirrors new ConfirmationQuestion($question,
// $default, $trueAnswerRegex); a nil regex is the default '/^y/i'.
func NewConfirmationQuestion(question string, def bool, trueAnswerRegex *php.Regexp) *ConfirmationQuestion {
	if trueAnswerRegex == nil {
		trueAnswerRegex = defaultTrueAnswerRegex
	}
	q := &ConfirmationQuestion{Question: NewQuestion(question, def), trueAnswerRegex: trueAnswerRegex}
	q.SetNormalizer(func(answer any) any {
		if b, ok := answer.(bool); ok {
			return b
		}
		s := php.ToString(answer)
		// (bool) preg_match(): a failed match (false) is not true.
		answerIsTrue, _ := trueAnswerRegex.IsMatch(s)
		if !def {
			return php.ToBool(answer) && answerIsTrue
		}

		return s == "" || answerIsTrue
	})

	return q
}

// StrictConfirmationQuestion is Composer's yes/no question that only
// accepts yes, y, no and n (by default).
type StrictConfirmationQuestion struct {
	*Question
	trueAnswerRegex  *php.Regexp
	falseAnswerRegex *php.Regexp
}

// The default Composer regexes.
var (
	strictTrueAnswerRegex  = php.MustCompile(`/^y(?:es)?$/i`)
	strictFalseAnswerRegex = php.MustCompile(`/^no?$/i`)
)

// NewStrictConfirmationQuestion mirrors new
// StrictConfirmationQuestion($question, $default, $trueRegex, $falseRegex);
// nil regexes use Composer's defaults.
func NewStrictConfirmationQuestion(question string, def bool, trueAnswerRegex, falseAnswerRegex *php.Regexp) *StrictConfirmationQuestion {
	if trueAnswerRegex == nil {
		trueAnswerRegex = strictTrueAnswerRegex
	}
	if falseAnswerRegex == nil {
		falseAnswerRegex = strictFalseAnswerRegex
	}
	q := &StrictConfirmationQuestion{
		Question:         NewQuestion(question, def),
		trueAnswerRegex:  trueAnswerRegex,
		falseAnswerRegex: falseAnswerRegex,
	}
	q.SetNormalizer(func(answer any) any {
		if b, ok := answer.(bool); ok {
			return b
		}
		if !php.ToBool(answer) && def {
			return true
		}
		s := php.ToString(answer)
		// Preg::isMatch throws a PcreException when matching fails.
		if ok, err := trueAnswerRegex.IsMatch(s); err != nil {
			return err
		} else if ok {
			return true
		}
		if ok, err := falseAnswerRegex.IsMatch(s); err != nil {
			return err
		} else if ok {
			return false
		}

		return nil
	})
	q.SetValidator(func(answer any) (any, error) {
		if _, ok := answer.(bool); !ok {
			return nil, newError(KindInvalidArgument, "StrictConfirmationQuestion.php", 87, "Please answer yes, y, no, or n.")
		}

		return answer, nil
	})

	return q
}

// ChoiceQuestion asks the user to pick among choices.
type ChoiceQuestion struct {
	*Question
	choices      *php.Array
	multiselect  bool
	prompt       string
	errorMessage string
}

// NewChoiceQuestion mirrors new ChoiceQuestion($question, $choices,
// $default).
func NewChoiceQuestion(question string, choices *php.Array, def any) (*ChoiceQuestion, error) {
	if choices == nil || choices.Len() == 0 {
		return nil, newError(KindSPLLogic, "ChoiceQuestion.php", 36, "Choice question must have at least 1 choice available.")
	}

	q := &ChoiceQuestion{
		Question:     NewQuestion(question, def),
		choices:      choices,
		prompt:       " > ",
		errorMessage: `Value "%s" is invalid`,
	}
	q.SetValidator(q.defaultValidator())
	if err := q.SetAutocompleterValues(choices); err != nil {
		return nil, err
	}

	return q, nil
}

// Choices returns the choices.
func (q *ChoiceQuestion) Choices() *php.Array { return q.choices }

// SetMultiselect sets whether several comma separated choices are accepted.
func (q *ChoiceQuestion) SetMultiselect(multiselect bool) *ChoiceQuestion {
	q.multiselect = multiselect
	q.SetValidator(q.defaultValidator())

	return q
}

// IsMultiselect reports whether several choices are accepted.
func (q *ChoiceQuestion) IsMultiselect() bool { return q.multiselect }

// Prompt returns the prompt for choices.
func (q *ChoiceQuestion) Prompt() string { return q.prompt }

// SetPrompt sets the prompt for choices.
func (q *ChoiceQuestion) SetPrompt(prompt string) *ChoiceQuestion {
	q.prompt = prompt

	return q
}

// SetErrorMessage sets the error message for invalid values; "%s" is
// replaced with the value.
func (q *ChoiceQuestion) SetErrorMessage(errorMessage string) *ChoiceQuestion {
	q.errorMessage = errorMessage
	q.SetValidator(q.defaultValidator())

	return q
}

// multiselectRegex is ChoiceQuestion's pattern.
var multiselectRegex = php.MustCompile(`/^[^,]+(?:,[^,]+)*$/`)

func (q *ChoiceQuestion) defaultValidator() Validator {
	choices := q.choices
	errorMessage := q.errorMessage
	multiselect := q.multiselect
	assoc := isAssoc(choices)

	return func(selected any) (any, error) {
		var selectedChoices []any
		if multiselect {
			// Check for a separated comma values
			s := php.ToString(selected)
			// !preg_match(): a failed match (false) also throws.
			if ok, _ := multiselectRegex.IsMatch(s); !ok {
				return nil, newError(KindInvalidArgument, "ChoiceQuestion.php", 129, "%s", phpSprintf(errorMessage, s))
			}
			for part := range strings.SplitSeq(s, ",") {
				selectedChoices = append(selectedChoices, part)
			}
		} else {
			selectedChoices = []any{selected}
		}

		if q.IsTrimmable() {
			for k, v := range selectedChoices {
				selectedChoices[k] = php.Trim(php.ToString(v))
			}
		}

		multiselectChoices := make([]any, 0, len(selectedChoices))
		for _, value := range selectedChoices {
			var results []string
			for key, choice := range choices.All() {
				if php.StrictEquals(choice, value) {
					results = append(results, key.String())
				}
			}

			if len(results) > 1 {
				return nil, newError(KindInvalidArgument, "ChoiceQuestion.php", 153, `The provided answer is ambiguous. Value should be one of "%s".`, strings.Join(results, `" or "`))
			}

			key, found := php.ArraySearch(value, choices, false)
			var result any
			if !assoc {
				if found {
					result, _ = choices.GetKey(key)
				} else if v, ok := choiceAt(choices, value); ok {
					result, found = v, true
				}
			} else if !found {
				if _, ok := choiceAt(choices, value); ok {
					result, found = value, true
				}
			} else {
				result = key.Value()
			}

			if !found {
				return nil, newError(KindInvalidArgument, "ChoiceQuestion.php", 169, "%s", phpSprintf(errorMessage, php.ToString(value)))
			}

			// For associative choices, consistently return the key as string:
			if assoc {
				result = php.ToString(result)
			}
			multiselectChoices = append(multiselectChoices, result)
		}

		if multiselect {
			return php.ListOf(multiselectChoices...), nil
		}

		return multiselectChoices[0], nil
	}
}

// choiceAt is isset($choices[$value]): the value is used as an offset and
// must hold a non-null value.
func choiceAt(choices *php.Array, value any) (any, bool) {
	switch value.(type) {
	case *php.Array, *php.Object:
		return nil, false
	}
	v, ok := choices.Get(value)

	return v, ok && v != nil
}

// Package jsonschema ports the subset of justinrainbow/json-schema 6.10.0
// (JsonSchema\Validator in its default, draft-04 mode) that Composer
// exercises: src/JsonSchema/Validator.php, Constraints/BaseConstraint.php,
// Constraints/Constraint.php, Constraints/SchemaConstraint.php,
// ConstraintError.php and Tool/DeepComparer.php. The constraints live in
// constraints.go and format.go, schema storage and URI handling in
// storage.go.
//
// Deliberately not reproduced: the check modes other than the default
// (type casting, coercion, defaults, exceptions, schema validation, the
// strict draft-06+ validators), the translation of json-schema.org
// meta-schema URIs to the copies bundled with the PHP package (such URIs go
// to the Retriever like any other), and the content-type check of HTTP
// retrievals.
package jsonschema

import (
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// Error is one entry of Validator::getErrors().
type Error struct {
	Property   string
	Pointer    string
	Message    string
	Constraint Constraint
	Context    int
}

// Constraint is the "constraint" entry of an error: its name and params.
type Constraint struct {
	Name   string
	Params *php.Array
}

// Retriever returns the JSON text of the schema document at uri (without
// fragment), as JsonSchema\Uri\UriRetriever would load it.
type Retriever func(uri string) (string, error)

// ErrorDocumentValidation is Validator::ERROR_DOCUMENT_VALIDATION, the
// context of every error in the default check mode.
const ErrorDocumentValidation = 1

// Validator is JsonSchema\Validator. It is not safe for concurrent use.
type Validator struct {
	retrieve   Retriever
	schemas    map[string]any // SchemaStorage::$schemas
	loaded     map[string]any // UriRetriever::$schemaCache
	regexps    map[string]*php.Regexp
	segs       []string // the property paths of the current depth
	noEmbedded bool
}

// NewValidator returns a Validator resolving external $refs with retrieve.
func NewValidator(retrieve Retriever) *Validator {
	return &Validator{retrieve: retrieve, schemas: map[string]any{}, loaded: map[string]any{}}
}

// Validate ports Validator::validate($data, $schema) followed by
// getErrors(). data and schema are PHP values as json_decode($x) (objects
// as *php.Object) yields; neither is modified. A non-nil error is a
// failure to load or resolve the schema (an exception in PHP): a
// *SchemaError, or an error of the Retriever.
func (v *Validator) Validate(data, schema any) (errs []Error, err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case *SchemaError:
			errs, err = nil, r
		case retrieverError:
			errs, err = nil, r.err
		default:
			panic(r)
		}
	}()

	// add provided schema to SchemaStorage with internal URI to allow internal $ref resolution
	schemaURI := InternalProvidedSchemaURI
	if id, ok := loosePropertyGet(schema, "id"); ok {
		schemaURI = php.ToString(id)
	}
	if id, ok := loosePropertyGet(schema, "$id"); ok {
		schemaURI = php.ToString(id)
	}
	v.addSchema(schemaURI, cloneValue(schema), false)

	resolved := v.getSchema(schemaURI)

	// Boolean schema requires no further validation
	if b, ok := resolved.(bool); ok {
		if !b {
			return []Error{v.newError(errFalse, 0, php.NewArray())}, nil
		}
		return nil, nil
	}

	errs = v.checkSchema(nil, data, resolved)
	if len(errs) == 0 {
		return nil, nil
	}

	return unique(errs), nil
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case *php.Object:
		return x.Clone()
	case *php.Array:
		return x.Clone()
	}

	return v
}

// loosePropertyGet ports LooseTypeCheck::propertyExists + propertyGet.
func loosePropertyGet(v any, name string) (any, bool) {
	switch x := v.(type) {
	case *php.Object:
		return x.Get(name)
	case *php.Array:
		return x.Get(name)
	}

	return nil, false
}

// checkSchema ports SchemaConstraint::check in the default check mode.
func (v *Validator) checkSchema(errs []Error, element, schema any) []Error {
	validationSchema := schema
	if schema == nil {
		s, ok := strictPropertyGet(element, "$schema")
		if !ok {
			fail("no schema found to verify against")
		}
		validationSchema = s
	}
	if a, ok := validationSchema.(*php.Array); ok {
		validationSchema = arrayToObjectRecursive(a)
	}

	return v.checkUndefined(errs, element, validationSchema, 0, noKey)
}

// unique ports array_unique($errors, SORT_REGULAR): the first of errors
// that compare equal (==) is kept.
func unique(errs []Error) []Error {
	if len(errs) < 2 {
		return errs
	}
	out := errs[:1]
outer:
	for _, e := range errs[1:] {
		for _, kept := range out {
			if errorsEqual(kept, e) {
				continue outer
			}
		}
		out = append(out, e)
	}

	return out
}

func errorsEqual(a, b Error) bool {
	return php.LooseEquals(a.Property, b.Property) &&
		php.LooseEquals(a.Pointer, b.Pointer) &&
		php.LooseEquals(a.Message, b.Message) &&
		php.LooseEquals(a.Constraint.Name, b.Constraint.Name) &&
		php.LooseEquals(a.Constraint.Params, b.Constraint.Params) &&
		a.Context == b.Context
}

// key is the $i a constraint is called with: null, a string or an int.
type key struct {
	kind uint8
	s    string
	n    int64
}

const (
	kindNull uint8 = iota
	kindStr
	kindInt
)

var noKey = key{}

func strKey(s string) key { return key{kind: kindStr, s: s} }

func intKey(n int) key { return key{kind: kindInt, n: int64(n)} }

// empty reports $i === null || $i === ”.
func (k key) empty() bool { return k.kind == kindNull || k.kind == kindStr && k.s == "" }

func (k key) String() string {
	if k.kind == kindInt {
		return strconv.FormatInt(k.n, 10)
	}

	return k.s
}

// value returns $i as a PHP value.
func (k key) value() any {
	switch k.kind {
	case kindStr:
		return k.s
	case kindInt:
		return k.n
	}

	return nil
}

// depth is a JsonPointer built by Constraint::incrementPath: the number of
// property paths it has, taken from Validator.segs. Constraints push their
// path segment at their depth on entry, so the segments below a depth stay
// unchanged while it is in use.
type depth int

// propertyPath ports BaseConstraint::convertJsonPointerIntoPropertyPath.
func propertyPath(segs []string) string {
	var b strings.Builder
	for _, s := range segs {
		if php.IsNumeric(s) {
			b.WriteByte('[')
			b.WriteString(strconv.FormatInt(php.ToInt(s), 10))
			b.WriteByte(']')
		} else {
			b.WriteByte('.')
			b.WriteString(s)
		}
	}

	return php.TrimSet(b.String(), ".")
}

// Constraint names (ConstraintError values).
const (
	errAdditionalItems      = "additionalItems"
	errAdditionalProperties = "additionalProp"
	errAllOf                = "allOf"
	errAnyOf                = "anyOf"
	errDependencies         = "dependencies"
	errDisallow             = "disallow"
	errDivisibleBy          = "divisibleBy"
	errEnum                 = "enum"
	errConstant             = "const"
	errExclusiveMinimum     = "exclusiveMinimum"
	errExclusiveMaximum     = "exclusiveMaximum"
	errFalse                = "false"
	errFormatColor          = "colorFormat"
	errFormatDate           = "dateFormat"
	errFormatDateTime       = "dateTimeFormat"
	errFormatDateUTC        = "dateUtcFormat"
	errFormatEmail          = "emailFormat"
	errFormatHostname       = "styleHostName"
	errFormatIP             = "ipFormat"
	errFormatPhone          = "phoneFormat"
	errFormatRegex          = "regexFormat"
	errFormatStyle          = "styleFormat"
	errFormatTime           = "timeFormat"
	errFormatURL            = "urlFormat"
	errLengthMax            = "maxLength"
	errLengthMin            = "minLength"
	errMaxItems             = "maxItems"
	errMaximum              = "maximum"
	errMinItems             = "minItems"
	errMinimum              = "minimum"
	errMissingMaximum       = "missingMaximum"
	errMissingMinimum       = "missingMinimum"
	errMultipleOf           = "multipleOf"
	errNot                  = "not"
	errOneOf                = "oneOf"
	errRequired             = "required"
	errRequires             = "requires"
	errPattern              = "pattern"
	errPregexInvalid        = "pregrex"
	errPropertiesMin        = "minProperties"
	errPropertiesMax        = "maxProperties"
	errType                 = "type"
	errUniqueItems          = "uniqueItems"
)

// message ports ConstraintError::getMessage.
func message(name string) string {
	switch name {
	case errAdditionalItems:
		return "The item %s[%s] is not defined and the definition does not allow additional items"
	case errAdditionalProperties:
		return "The property %s is not defined and the definition does not allow additional properties"
	case errAllOf:
		return "Failed to match all schemas"
	case errAnyOf:
		return "Failed to match at least one schema"
	case errDependencies:
		return "%s depends on %s, which is missing"
	case errDisallow:
		return "Disallowed value was matched"
	case errDivisibleBy:
		return "Is not divisible by %d"
	case errEnum:
		return "Does not have a value in the enumeration %s"
	case errConstant:
		return "Does not have a value equal to %s"
	case errExclusiveMinimum:
		return "Must have a minimum value greater than %d"
	case errExclusiveMaximum:
		return "Must have a maximum value less than %d"
	case errFalse:
		return "Boolean schema false"
	case errFormatColor:
		return "Invalid color"
	case errFormatDate:
		return "Invalid date %s, expected format YYYY-MM-DD"
	case errFormatDateTime:
		return "Invalid date-time %s, expected format YYYY-MM-DDThh:mm:ssZ or YYYY-MM-DDThh:mm:ss+hh:mm"
	case errFormatDateUTC:
		return "Invalid time %s, expected integer of milliseconds since Epoch"
	case errFormatEmail:
		return "Invalid email"
	case errFormatHostname:
		return "Invalid hostname"
	case errFormatIP:
		return "Invalid IP address"
	case errFormatPhone:
		return "Invalid phone number"
	case errFormatRegex:
		return "Invalid regex format %s"
	case errFormatStyle:
		return "Invalid style"
	case errFormatTime:
		return "Invalid time %s, expected format hh:mm:ss"
	case errFormatURL:
		return "Invalid URL format"
	case errLengthMax:
		return "Must be at most %d characters long"
	case errLengthMin:
		return "Must be at least %d characters long"
	case errMaxItems:
		return "There must be a maximum of %d items in the array, %d found"
	case errMaximum:
		return "Must have a maximum value less than or equal to %d"
	case errMinItems:
		return "There must be a minimum of %d items in the array, %d found"
	case errMinimum:
		return "Must have a minimum value greater than or equal to %d"
	case errMissingMaximum:
		return "Use of exclusiveMaximum requires presence of maximum"
	case errMissingMinimum:
		return "Use of exclusiveMinimum requires presence of minimum"
	case errMultipleOf:
		return "Must be a multiple of %s"
	case errNot:
		return "Matched a schema which it should not"
	case errOneOf:
		return "Failed to match exactly one schema"
	case errRequired:
		return "The property %s is required"
	case errRequires:
		return "The presence of the property %s requires that %s also be present"
	case errPattern:
		return "Does not match the regex pattern %s"
	case errPregexInvalid:
		return "The pattern %s is invalid"
	case errPropertiesMin:
		return "Must contain a minimum of %d properties"
	case errPropertiesMax:
		return "Must contain no more than %d properties"
	case errType:
		return "%s value found, but %s is required"
	case errUniqueItems:
		return "There are no duplicates allowed in the array"
	}
	panic("Missing error message for " + name)
}

// newError ports BaseConstraint::addError (the error array it builds).
func (v *Validator) newError(name string, p depth, params *php.Array) Error {
	segs := v.segs[:p]

	return Error{
		Property:   propertyPath(segs),
		Pointer:    php.LtrimSet(encodePointerPaths(segs), "#"),
		Message:    php.Ucfirst(vsprintf(message(name), params)),
		Constraint: Constraint{Name: name, Params: params},
		Context:    ErrorDocumentValidation,
	}
}

// vsprintf ports vsprintf($format, array_map(..., array_values($more))) for
// the %s and %d conversions the messages use: booleans are var_export()ed,
// other scalars kept, everything else json_encode()d.
func vsprintf(format string, params *php.Array) string {
	args := params.Values()
	var b strings.Builder
	n := 0
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' || i+1 == len(format) {
			b.WriteByte(c)
			continue
		}
		i++
		var arg any
		if n < len(args) {
			arg = messageArg(args[n])
		}
		n++
		switch format[i] {
		case 'd':
			b.WriteString(strconv.FormatInt(php.ToInt(arg), 10))
		case 's':
			b.WriteString(php.ToString(arg))
		default:
			b.WriteByte('%')
			b.WriteByte(format[i])
		}
	}

	return b.String()
}

func messageArg(v any) any {
	switch x := v.(type) {
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string, int64, float64:
		return x
	}
	s, err := php.JSONEncode(v, 0)
	if err != nil {
		return false
	}

	return s
}

// isEqual ports DeepComparer::isEqual.
func isEqual(left, right any) bool {
	if left == nil && right == nil {
		return true
	}

	leftScalar, rightScalar := isScalar(left), isScalar(right)
	if leftScalar && rightScalar {
		if isNumber(left) && isNumber(right) && php.ToFloat(left) == php.ToFloat(right) {
			return true
		}

		return php.StrictEquals(left, right)
	}
	if leftScalar != rightScalar {
		return false
	}

	switch l := left.(type) {
	case *php.Array:
		r, ok := right.(*php.Array)
		if !ok || l.Len() != r.Len() {
			return false
		}
		for k, lv := range l.All() {
			rv, ok := r.GetKey(k)
			if !ok || !isEqual(lv, rv) {
				return false
			}
		}
		return true
	case *php.Object:
		r, ok := right.(*php.Object)
		if !ok || l.Len() != r.Len() {
			return false
		}
		for k, lv := range l.All() {
			rv, ok := r.Get(k)
			if !ok || !isEqual(lv, rv) {
				return false
			}
		}
		return true
	}

	return false
}

func isScalar(v any) bool {
	switch v.(type) {
	case bool, int64, float64, string:
		return true
	}

	return false
}

func isNumber(v any) bool {
	switch v.(type) {
	case int64, float64:
		return true
	}

	return false
}

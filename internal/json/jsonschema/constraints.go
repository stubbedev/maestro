// Ports src/JsonSchema/Constraints/UndefinedConstraint.php,
// TypeConstraint.php, ObjectConstraint.php, CollectionConstraint.php,
// StringConstraint.php, NumberConstraint.php, EnumConstraint.php,
// ConstConstraint.php and TypeCheck/{StrictTypeCheck,LooseTypeCheck}.php of
// justinrainbow/json-schema 6.10.0, in the default check mode.
//
// Every check takes the errors of the constraint instance PHP would be
// running ("$this->errors", from start on) appended to errs, and returns
// errs with its own errors appended, so that only failures allocate.

package jsonschema

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/maestro/internal/php"
)

// undefined is the UndefinedConstraint instance json-schema passes as the
// value of a property that is absent from the document.
type undefined struct{}

// undefinedProperties are the properties of an UndefinedConstraint
// instance, which property_exists() sees.
func undefinedHasProperty(name string) bool {
	switch name {
	case "appliedDefaults", "inlineSchemaProperty", "errors", "errorMask", "factory":
		return true
	}

	return false
}

// sget ports isset($schema->name): the property, when set and not null.
func sget(schema *php.Object, name string) (any, bool) {
	v, ok := schema.Get(name)

	return v, ok && v != nil
}

// strictIsObject is StrictTypeCheck::isObject (is_object).
func strictIsObject(v any) bool {
	switch v.(type) {
	case *php.Object, undefined:
		return true
	}

	return false
}

// looseIsObject is LooseTypeCheck::isObject.
func looseIsObject(v any) bool {
	switch x := v.(type) {
	case *php.Object, undefined:
		return true
	case *php.Array:
		return x.Len() == 0 || !x.IsList()
	}

	return false
}

// strictPropertyExists is StrictTypeCheck::propertyExists (property_exists).
func strictPropertyExists(v any, name string) bool {
	switch x := v.(type) {
	case *php.Object:
		return x.Has(name)
	case undefined:
		return undefinedHasProperty(name)
	}

	return false
}

func strictPropertyGet(v any, name string) (any, bool) {
	if o, ok := v.(*php.Object); ok {
		return o.Get(name)
	}

	return nil, false
}

// propertyCount is StrictTypeCheck::propertyCount.
func propertyCount(v any) int {
	switch x := v.(type) {
	case *php.Object:
		return x.Len()
	case undefined:
		return 5
	}

	return 0
}

// each ports foreach over an array or object; other values iterate
// nothing (PHP warns).
func each(v any, fn func(k key, val any)) {
	switch x := v.(type) {
	case *php.Array:
		for k, val := range x.All() {
			if k.IsInt() {
				fn(key{kind: kindInt, n: k.Int()}, val)
			} else {
				fn(strKey(k.String()), val)
			}
		}
	case *php.Object:
		for k, val := range x.All() {
			fn(strKey(k), val)
		}
	}
}

// checkUndefined ports Constraint::checkUndefined followed by
// UndefinedConstraint::check.
func (v *Validator) checkUndefined(errs []Error, value, schema any, p depth, i key) []Error {
	o, ok := v.resolveRefSchema(schema, nil).(*php.Object)
	if !ok {
		return errs
	}

	if !i.empty() {
		v.segs = append(v.segs[:p], i.String())
		p++
	}
	start := len(errs)

	// check special properties
	errs = v.validateCommonProperties(errs, value, o, p, i)

	// check allOf, anyOf, and oneOf properties
	errs = v.validateOfProperties(errs, start, value, o, p)

	// check known types
	return v.validateTypes(errs, value, o, p, i)
}

// validateTypes ports UndefinedConstraint::validateTypes.
func (v *Validator) validateTypes(errs []Error, value any, schema *php.Object, p depth, i key) []Error {
	// check array
	if a, ok := value.(*php.Array); ok {
		errs = v.checkArray(errs, a, schema, p, i)
	}

	// check object
	if looseIsObject(value) {
		properties, _ := sget(schema, "properties")
		additionalProperties, _ := sget(schema, "additionalProperties")
		patternProperties, _ := sget(schema, "patternProperties")
		errs = v.checkObject(errs, value, schema, p, properties, additionalProperties, patternProperties)
	}

	// check string
	if s, ok := value.(string); ok {
		errs = v.checkString(errs, value, s, schema, p)
	}

	// check numeric
	if isNumericValue(value) {
		errs = v.checkNumber(errs, value, schema, p)
	}

	// check enum
	if _, ok := sget(schema, "enum"); ok {
		errs = v.checkEnum(errs, value, schema, p)
	}

	// check const
	if _, ok := sget(schema, "const"); ok {
		errs = v.checkConst(errs, value, schema, p)
	}

	return errs
}

func isNumericValue(v any) bool {
	switch v.(type) {
	case int64, float64, string:
		return php.IsNumeric(v)
	}

	return false
}

// validateCommonProperties ports UndefinedConstraint::validateCommonProperties
// (without defaults, which the default check mode does not apply).
func (v *Validator) validateCommonProperties(errs []Error, value any, schema *php.Object, p depth, i key) []Error {
	_, isUndefined := value.(undefined)

	// if it extends another schema, it must pass that schema as well
	if extends, ok := sget(schema, "extends"); ok {
		if s, ok := extends.(string); ok {
			extends = v.validateURI(schema, s)
		}
		if a, ok := extends.(*php.Array); ok {
			for _, e := range a.All() {
				errs = v.checkUndefined(errs, value, e, p, i)
			}
		} else {
			errs = v.checkUndefined(errs, value, extends, p, i)
		}
	}

	// Verify required values
	if strictIsObject(value) {
		required, hasRequired := sget(schema, "required")
		requiredList, isList := required.(*php.Array)
		switch {
		case !isUndefined && hasRequired && isList:
			// Draft 4 - Required is an array of strings - e.g. "required": ["foo", ...]
			for _, req := range requiredList.All() {
				if !strictPropertyExists(value, php.ToString(req)) {
					errPath := p
					if k := keyOf(req); !k.empty() {
						v.segs = append(v.segs[:p], k.String())
						errPath++
					}
					errs = append(errs, v.newError(errRequired, errPath, php.ArrayOf("property", req)))
				}
			}
		case hasRequired && !isList:
			// Draft 3 - Required attribute - e.g. "foo": {"type": "string", "required": true}
			if php.ToBool(required) && isUndefined {
				var name any = false
				if p > 0 {
					name = v.segs[p-1]
				}
				errs = append(errs, v.newError(errRequired, p, php.ArrayOf("property", name)))
			}
		default:
			// if the value is both undefined and not required, skip remaining checks
			// in this method which assume an actual, defined instance when validating.
			if isUndefined {
				return errs
			}
		}
	}

	// Verify type
	if !isUndefined {
		errs = v.checkType(errs, value, schema, p)
	}

	// Verify disallowed items
	if disallow, ok := sget(schema, "disallow"); ok {
		initErrors := len(errs)
		typeSchema := php.NewObject()
		typeSchema.Set("type", disallow)
		errs = v.checkType(errs, value, typeSchema, p)

		// if no new errors were raised it must be a disallowed value
		if len(errs) == initErrors {
			errs = append(errs, v.newError(errDisallow, p, php.NewArray()))
		} else {
			errs = errs[:initErrors]
		}
	}

	if not, ok := sget(schema, "not"); ok {
		initErrors := len(errs)
		errs = v.checkUndefined(errs, value, not, p, i)

		// if no new errors were raised then the instance validated against the "not" schema
		if len(errs) == initErrors {
			errs = append(errs, v.newError(errNot, p, php.NewArray()))
		} else {
			errs = errs[:initErrors]
		}
	}

	// Verify that dependencies are met
	if dependencies, ok := sget(schema, "dependencies"); ok && strictIsObject(value) {
		errs = v.validateDependencies(errs, value, dependencies, p)
	}

	return errs
}

// keyOf turns a PHP value used as a path element into a key.
func keyOf(v any) key {
	switch x := v.(type) {
	case nil:
		return noKey
	case int64:
		return key{kind: kindInt, n: x}
	}

	return strKey(php.ToString(v))
}

// validateOfProperties ports UndefinedConstraint::validateOfProperties.
func (v *Validator) validateOfProperties(errs []Error, start int, value any, schema *php.Object, p depth) []Error {
	// Verify type
	if _, ok := value.(undefined); ok {
		return errs
	}

	if allOf, ok := sget(schema, "allOf"); ok {
		isValid := true
		each(allOf, func(_ key, sub any) {
			initErrors := len(errs)
			errs = v.checkUndefined(errs, value, sub, p, noKey)
			isValid = isValid && len(errs) == initErrors
		})
		if !isValid {
			errs = append(errs, v.newError(errAllOf, p, php.NewArray()))
		}
	}

	if anyOf, ok := sget(schema, "anyOf"); ok {
		isValid := false
		startErrors := len(errs)
		each(anyOf, func(_ key, sub any) {
			if isValid {
				return
			}
			initErrors := len(errs)
			errs = v.checkUndefined(errs, value, sub, p, noKey)
			isValid = len(errs) == initErrors
		})
		if !isValid {
			errs = append(errs, v.newError(errAnyOf, p, php.NewArray()))
		} else {
			errs = errs[:startErrors]
		}
	}

	if oneOf, ok := sget(schema, "oneOf"); ok {
		var allErrors []Error
		matched := 0
		startErrors := append([]Error(nil), errs[start:]...)
		each(oneOf, func(_ key, sub any) {
			errs = v.checkUndefined(errs[:start], value, sub, p, noKey)
			if len(errs) == start {
				matched++
			}
			allErrors = append(allErrors, errs[start:]...)
		})
		if matched != 1 {
			errs = append(errs, allErrors...)
			errs = append(errs, startErrors...)
			errs = append(errs, v.newError(errOneOf, p, php.NewArray()))
		} else {
			errs = append(errs[:start], startErrors...)
		}
	}

	return errs
}

// validateDependencies ports UndefinedConstraint::validateDependencies.
func (v *Validator) validateDependencies(errs []Error, value, dependencies any, p depth) []Error {
	each(dependencies, func(k key, dependency any) {
		if !strictPropertyExists(value, k.String()) {
			return
		}
		switch d := dependency.(type) {
		case string:
			// Draft 3 string is allowed - e.g. "dependencies": {"bar": "foo"}
			if !strictPropertyExists(value, d) {
				errs = append(errs, v.newError(errDependencies, p, php.ArrayOf("key", k.value(), "dependency", d)))
			}
		case *php.Array:
			// Draft 4 must be an array - e.g. "dependencies": {"bar": ["foo"]}
			for _, item := range d.All() {
				if !strictPropertyExists(value, php.ToString(item)) {
					errs = append(errs, v.newError(errDependencies, p, php.ArrayOf("key", k.value(), "dependency", d)))
				}
			}
		case *php.Object:
			// Schema - e.g. "dependencies": {"bar": {"properties": {"foo": {...}}}}
			errs = v.checkUndefined(errs, value, d, p, strKey(""))
		}
	})

	return errs
}

// validateURI ports UndefinedConstraint::validateUri (draft-03 "extends").
func (v *Validator) validateURI(schema *php.Object, schemaURI string) any {
	if parts := parseURI(schemaURI); parts == (uriParts{}) {
		return nil
	}
	base, hasBase := "", false
	if id, ok := schema.Get("id"); ok && id != nil {
		base, hasBase = php.ToString(id), true
	}
	resolved, _ := resolveURI(schemaURI, base, hasBase)

	return v.retrieveURI(resolved)
}

// typeWording is TypeConstraint::$wording.
func typeWording(t any) (string, bool) {
	switch x := t.(type) {
	case string:
		switch x {
		case "integer":
			return "an integer", true
		case "number":
			return "a number", true
		case "boolean":
			return "a boolean", true
		case "object":
			return "an object", true
		case "array":
			return "an array", true
		case "string":
			return "a string", true
		case "null":
			return "a null", true
		case "any", "0":
			return "", true
		}
	case int64:
		return "", x == 0
	case bool:
		return "", !x
	case float64:
		return "", x == 0
	}

	return "", false
}

// validateTypeNameWording ports TypeConstraint::validateTypeNameWording.
func validateTypeNameWording(t any) string {
	w, ok := typeWording(t)
	if !ok {
		fail("No wording for " + php.VarExport(t) + " available, expected wordings are: [an integer, a number, a boolean, an object, an array, a string, a null]")
	}

	return w
}

// checkType ports Constraint::checkType and TypeConstraint::check.
func (v *Validator) checkType(errs []Error, value any, schema *php.Object, p depth) []Error {
	t, _ := sget(schema, "type")
	isValid := false
	var buf [8]string
	wording := buf[:0]

	switch x := t.(type) {
	case *php.Array:
		for _, tp := range x.All() {
			// already valid, so no need to waste cycles looping over everything
			if isValid {
				break
			}
			if tpo, ok := tp.(*php.Object); ok {
				subSchema := php.NewObject()
				subSchema.Set("type", tpo)
				isValid = len(v.checkType(nil, value, subSchema, p)) == 0
				wording = append(wording, "an object")
			} else {
				wording = append(wording, validateTypeNameWording(tp))
				isValid = validateType(value, tp)
			}
		}
	case *php.Object:
		return v.checkUndefined(errs, value, x, p, noKey)
	default:
		isValid = validateType(value, t)
	}

	if !isValid {
		if _, ok := t.(*php.Array); !ok {
			wording = append(wording, validateTypeNameWording(t))
		}
		errs = append(errs, v.newError(errType, p, php.ArrayOf("found", php.GetType(value), "expected", implodeWith(wording, ", ", "or"))))
	}

	return errs
}

// implodeWith ports TypeConstraint::implodeWith.
func implodeWith(elements []string, delimiter, listEnd string) string {
	if len(elements) < 2 {
		return strings.Join(elements, delimiter)
	}

	return strings.Join(elements[:len(elements)-1], delimiter) + " " + listEnd + " " + elements[len(elements)-1]
}

// validateType ports TypeConstraint::validateType without coercion.
func validateType(value, t any) bool {
	// mostly the case for inline schema
	if !php.ToBool(t) {
		return true
	}

	s, _ := t.(string)
	switch s {
	case "any":
		return true
	case "object":
		return strictIsObject(value)
	case "array":
		_, ok := value.(*php.Array)
		return ok
	case "integer":
		_, ok := value.(int64)
		return ok
	case "number":
		return isNumber(value)
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "null":
		return value == nil
	}

	var desc string
	switch value.(type) {
	case *php.Object, undefined:
		desc = "object"
	case *php.Array:
		desc = "Array"
	default:
		desc = php.ToString(value)
	}
	fail(desc + " is an invalid type for " + php.ToString(t))

	return false
}

// checkObject ports Constraint::checkObject and ObjectConstraint::check.
func (v *Validator) checkObject(errs []Error, element any, schema *php.Object, p depth, properties, additionalProp, patternProperties any) []Error {
	if _, ok := element.(undefined); ok {
		return errs
	}

	var matches []string
	if php.ToBool(patternProperties) {
		// validate the element pattern properties
		errs, matches = v.validatePatternProperties(errs, element, p, patternProperties)
	}

	if php.ToBool(properties) {
		// validate the element properties
		errs = v.validateProperties(errs, element, properties, p)
	}

	// validate additional element properties & constraints
	return v.validateElement(errs, element, matches, schema, p, properties, additionalProp)
}

// validatePatternProperties ports ObjectConstraint::validatePatternProperties.
func (v *Validator) validatePatternProperties(errs []Error, element any, p depth, patternProperties any) ([]Error, []string) {
	var matches []string
	each(patternProperties, func(pk key, schema any) {
		pregex := pk.String()
		re := v.jsonPattern(pregex)

		// Validate the pattern before using it to test for matches
		if re == nil {
			errs = append(errs, v.newError(errPregexInvalid, p, php.ArrayOf("pregex", pk.value())))
			return
		}
		each(element, func(i key, value any) {
			if ok, _ := re.IsMatch(i.String()); ok {
				matches = append(matches, i.String())
				if !php.ToBool(schema) {
					schema = php.NewObject()
				}
				errs = v.checkUndefined(errs, value, schema, p, i)
			}
		})
	})

	return errs, matches
}

// jsonPattern compiles BaseConstraint::jsonPatternToPhpRegex($pattern); nil
// when PCRE rejects it.
func (v *Validator) jsonPattern(pattern string) *php.Regexp {
	if re, ok := compiledPatterns()[pattern]; ok {
		return re
	}
	if re, ok := v.regexps[pattern]; ok {
		return re
	}
	re := compileJSONPattern(pattern)
	if v.regexps == nil {
		v.regexps = map[string]*php.Regexp{}
	}
	v.regexps[pattern] = re

	return re
}

func compileJSONPattern(pattern string) *php.Regexp {
	re, err := php.Compile("~" + strings.ReplaceAll(pattern, "~", `\~`) + "~u")
	if err != nil {
		return nil
	}
	if _, err := re.IsMatch(""); err != nil {
		return nil
	}

	return re
}

// validateElement ports ObjectConstraint::validateElement.
func (v *Validator) validateElement(errs []Error, element any, matches []string, schema *php.Object, p depth, properties, additionalProp any) []Error {
	errs = v.validateMinMaxConstraint(errs, element, schema, p)

	each(element, func(i key, value any) {
		definition, hasDefinition := getProperty(properties, i)
		definitionTruthy := hasDefinition && php.ToBool(definition)
		matched := inMatches(i, matches)

		// no additional properties allowed
		isInlineSchema := i.kind == kindStr && i.s == "$schema"
		if !matched && additionalProp == false && !isInlineSchema && !definitionTruthy {
			errs = append(errs, v.newError(errAdditionalProperties, p, php.ArrayOf("property", i.value())))
		}

		// additional properties defined
		if !matched && php.ToBool(additionalProp) && !definitionTruthy {
			if additionalProp == true {
				errs = v.checkUndefined(errs, value, nil, p, i)
			} else {
				errs = v.checkUndefined(errs, value, additionalProp, p, i)
			}
		}

		// property requires presence of another
		if require, _ := getProperty(definition, strKey("requires")); php.ToBool(require) {
			if req, _ := getProperty(element, keyOf(require)); !php.ToBool(req) {
				errs = append(errs, v.newError(errRequires, p, php.ArrayOf("property", i.value(), "requiredProperty", require)))
			}
		}

		if property, ok := value.(*php.Object); ok {
			errs = v.validateMinMaxConstraint(errs, property, definition, p)
		}
	})

	return errs
}

// inMatches ports in_array($i, $matches) (loose).
func inMatches(i key, matches []string) bool {
	for _, m := range matches {
		if php.LooseEquals(i.value(), m) {
			return true
		}
	}

	return false
}

// validateProperties ports ObjectConstraint::validateProperties.
func (v *Validator) validateProperties(errs []Error, element, properties any, p depth) []Error {
	each(properties, func(i key, definition any) {
		property, ok := getProperty(element, i)
		if !ok {
			property = undefined{}
		}
		if _, ok := definition.(*php.Object); ok {
			// Undefined constraint will check for is_object() and quit if is not - so why pass it?
			errs = v.checkUndefined(errs, property, definition, p, i)
		}
	})

	return errs
}

// getProperty ports ObjectConstraint::getProperty.
func getProperty(element any, property key) (any, bool) {
	switch e := element.(type) {
	case *php.Array:
		if property.kind == kindInt {
			return e.GetKey(php.IntKey(property.n))
		}
		return e.Get(property.s)
	case *php.Object:
		return e.Get(property.String())
	case undefined:
		if undefinedHasProperty(property.String()) {
			return nil, true
		}
	}

	return nil, false
}

// validateMinMaxConstraint ports ObjectConstraint::validateMinMaxConstraint.
func (v *Validator) validateMinMaxConstraint(errs []Error, element, objectDefinition any, p depth) []Error {
	if !strictIsObject(element) {
		return errs
	}
	def, ok := objectDefinition.(*php.Object)
	if !ok {
		return errs
	}

	// Verify minimum number of properties
	if minProps, ok := sget(def, "minProperties"); ok {
		if n, ok := minProps.(int64); ok && int64(propertyCount(element)) < max(0, n) {
			errs = append(errs, v.newError(errPropertiesMin, p, php.ArrayOf("minProperties", n)))
		}
	}
	// Verify maximum number of properties
	if maxProps, ok := sget(def, "maxProperties"); ok {
		if n, ok := maxProps.(int64); ok && int64(propertyCount(element)) > max(0, n) {
			errs = append(errs, v.newError(errPropertiesMax, p, php.ArrayOf("maxProperties", n)))
		}
	}

	return errs
}

// checkArray ports Constraint::checkArray and CollectionConstraint::check.
func (v *Validator) checkArray(errs []Error, value *php.Array, schema *php.Object, p depth, i key) []Error {
	count := int64(value.Len())

	// Verify minItems
	if minItems, ok := sget(schema, "minItems"); ok && php.Compare(count, minItems) < 0 {
		errs = append(errs, v.newError(errMinItems, p, php.ArrayOf("minItems", minItems, "found", count)))
	}

	// Verify maxItems
	if maxItems, ok := sget(schema, "maxItems"); ok && php.Compare(maxItems, count) < 0 {
		errs = append(errs, v.newError(errMaxItems, p, php.ArrayOf("maxItems", maxItems, "found", count)))
	}

	// Verify uniqueItems
	if unique, ok := sget(schema, "uniqueItems"); ok && php.ToBool(unique) {
	outer:
		for x := range count - 1 {
			for y := x + 1; y < count; y++ {
				vx, _ := value.GetKey(php.IntKey(x))
				vy, _ := value.GetKey(php.IntKey(y))
				if isEqual(vx, vy) {
					errs = append(errs, v.newError(errUniqueItems, p, php.NewArray()))
					break outer
				}
			}
		}
	}

	return v.validateItems(errs, value, schema, p, i)
}

// validateItems ports CollectionConstraint::validateItems.
func (v *Validator) validateItems(errs []Error, value *php.Array, schema *php.Object, p depth, i key) []Error {
	items, ok := sget(schema, "items")
	if !ok || items == true {
		return errs
	}
	start := len(errs)

	if itemsObj, ok := items.(*php.Object); ok {
		// just one type definition for the whole array
		additionalItems, hasAdditional := sget(schema, "additionalItems")
		var secondErrors []Error
		hasSecond := false
		for k, val := range value.All() {
			initErrors := len(errs)

			// First check if its defined in "items"
			errs = v.checkUndefined(errs, val, itemsObj, p, phpKey(k))

			// Recheck with "additionalItems" if the first test fails
			if initErrors < len(errs) && hasAdditional && additionalItems != false {
				secondErrors = append(secondErrors[:0], errs[start:]...)
				hasSecond = true
				errs = v.checkUndefined(errs, val, additionalItems, p, phpKey(k))
			}

			// Reset errors if needed
			if hasSecond && start+len(secondErrors) < len(errs) {
				errs = append(errs[:start], secondErrors...)
			} else if hasSecond && start+len(secondErrors) == len(errs) {
				errs = errs[:initErrors]
			}
		}

		return errs
	}

	// Defined item type definitions
	itemList, _ := items.(*php.Array)
	itemCount := 0
	if itemList != nil {
		itemCount = itemList.Len()
	}
	for k, val := range value.All() {
		if itemList != nil {
			if def, ok := itemList.GetKey(k); ok {
				errs = v.checkUndefined(errs, val, def, p, phpKey(k))
				continue
			}
		}
		// Additional items
		if additionalItems, ok := schema.Get("additionalItems"); ok {
			if additionalItems != false {
				errs = v.checkUndefined(errs, val, additionalItems, p, phpKey(k))
			} else {
				errs = append(errs, v.newError(errAdditionalItems, p, php.ArrayOf("item", i.value(), "property", k.Value(), "additionalItems", additionalItems)))
			}
		} else {
			// Should be valid against an empty schema
			errs = v.checkUndefined(errs, val, php.NewObject(), p, phpKey(k))
		}
	}

	// Treat when we have more schema definitions than values, not for empty arrays
	if value.Len() > 0 {
		for k := value.Len(); k < itemCount; k++ {
			def, _ := itemList.GetKey(php.IntKey(int64(k)))
			errs = v.checkUndefined(errs, undefined{}, def, p, intKey(k))
		}
	}

	return errs
}

func phpKey(k php.Key) key {
	if k.IsInt() {
		return key{kind: kindInt, n: k.Int()}
	}

	return strKey(k.String())
}

// checkString ports Constraint::checkString and StringConstraint::check.
// value is element as an interface, saving its conversion for checkFormat.
func (v *Validator) checkString(errs []Error, value any, element string, schema *php.Object, p depth) []Error {
	// Verify maxLength
	if maxLength, ok := sget(schema, "maxLength"); ok && php.Compare(maxLength, int64(mbStrlen(element))) < 0 {
		errs = append(errs, v.newError(errLengthMax, p, php.ArrayOf("maxLength", maxLength)))
	}

	// verify minLength
	if minLength, ok := sget(schema, "minLength"); ok && php.Compare(int64(mbStrlen(element)), minLength) < 0 {
		errs = append(errs, v.newError(errLengthMin, p, php.ArrayOf("minLength", minLength)))
	}

	// Verify a regex pattern
	if pattern, ok := sget(schema, "pattern"); ok {
		matched := false
		if re := v.jsonPattern(php.ToString(pattern)); re != nil {
			matched, _ = re.IsMatch(element)
		}
		if !matched {
			errs = append(errs, v.newError(errPattern, p, php.ArrayOf("pattern", pattern)))
		}
	}

	return v.checkFormat(errs, value, schema, p)
}

// mbStrlen ports mb_strlen($s, mb_detect_encoding($s)) for ASCII and UTF-8
// input; invalid UTF-8 counts bytes.
func mbStrlen(s string) int {
	if utf8.ValidString(s) {
		return utf8.RuneCountInString(s)
	}

	return len(s)
}

// checkNumber ports Constraint::checkNumber and NumberConstraint::check.
func (v *Validator) checkNumber(errs []Error, element any, schema *php.Object, p depth) []Error {
	// Verify minimum
	minimum, hasMinimum := sget(schema, "minimum")
	if exclusiveMinimum, ok := sget(schema, "exclusiveMinimum"); ok {
		if hasMinimum {
			if php.ToBool(exclusiveMinimum) && php.Compare(element, minimum) <= 0 {
				errs = append(errs, v.newError(errExclusiveMinimum, p, php.ArrayOf("minimum", minimum)))
			} else if php.Compare(element, minimum) < 0 {
				errs = append(errs, v.newError(errMinimum, p, php.ArrayOf("minimum", minimum)))
			}
		} else {
			errs = append(errs, v.newError(errMissingMinimum, p, php.NewArray()))
		}
	} else if hasMinimum && php.Compare(element, minimum) < 0 {
		errs = append(errs, v.newError(errMinimum, p, php.ArrayOf("minimum", minimum)))
	}

	// Verify maximum
	maximum, hasMaximum := sget(schema, "maximum")
	if exclusiveMaximum, ok := sget(schema, "exclusiveMaximum"); ok {
		if hasMaximum {
			if php.ToBool(exclusiveMaximum) && php.Compare(maximum, element) <= 0 {
				errs = append(errs, v.newError(errExclusiveMaximum, p, php.ArrayOf("maximum", maximum)))
			} else if php.Compare(maximum, element) < 0 {
				errs = append(errs, v.newError(errMaximum, p, php.ArrayOf("maximum", maximum)))
			}
		} else {
			errs = append(errs, v.newError(errMissingMaximum, p, php.NewArray()))
		}
	} else if hasMaximum && php.Compare(maximum, element) < 0 {
		errs = append(errs, v.newError(errMaximum, p, php.ArrayOf("maximum", maximum)))
	}

	// Verify divisibleBy - Draft v3
	if divisibleBy, ok := sget(schema, "divisibleBy"); ok && fmodNonZero(element, divisibleBy) {
		errs = append(errs, v.newError(errDivisibleBy, p, php.ArrayOf("divisibleBy", divisibleBy)))
	}

	// Verify multipleOf - Draft v4
	if multipleOf, ok := sget(schema, "multipleOf"); ok && fmodNonZero(element, multipleOf) {
		errs = append(errs, v.newError(errMultipleOf, p, php.ArrayOf("multipleOf", multipleOf)))
	}

	return v.checkFormat(errs, element, schema, p)
}

// fmodNonZero ports NumberConstraint::fmod($a, $b) != 0.
func fmodNonZero(number1, number2 any) bool {
	n1, n2 := php.ToFloat(number1), php.ToFloat(number2)
	if n2 == 0 {
		fail("Division by zero")
	}
	// PHP rounds the product before the subtraction (separate opcodes); the
	// conversion stops Go fusing them into one FMA on arm64 and other FMA
	// targets, whose exact remainder would differ (1e20 divisibleBy 3).
	modulus := n1 - float64(math.Round(n1/n2)*n2)
	const precision = 0.0000000001

	return !(-precision < modulus && modulus < precision) && modulus != 0
}

// checkEnum ports Constraint::checkEnum and EnumConstraint::check.
func (v *Validator) checkEnum(errs []Error, element any, schema *php.Object, p depth) []Error {
	// Only validate enum if the attribute exists
	if _, ok := element.(undefined); ok {
		if required, ok := sget(schema, "required"); !ok || !php.ToBool(required) {
			return errs
		}
	}

	enum, _ := schema.Get("enum")
	typ := gettype(element)
	found := false
	each(enum, func(_ key, e any) {
		if found {
			return
		}
		if typ == gettype(e) && isEqual(element, e) {
			found = true
			return
		}
		if isNumericValue(element) && isNumericValue(e) && isEqual(php.ToFloat(element), php.ToFloat(e)) {
			found = true
		}
	})
	if found {
		return errs
	}

	return append(errs, v.newError(errEnum, p, php.ArrayOf("enum", enum)))
}

func gettype(v any) string {
	if _, ok := v.(undefined); ok {
		return "object"
	}

	return php.GetType(v)
}

// checkConst ports Constraint::checkConst and ConstConstraint::check.
func (v *Validator) checkConst(errs []Error, element any, schema *php.Object, p depth) []Error {
	// Only validate const if the attribute exists
	if _, ok := element.(undefined); ok {
		if required, ok := sget(schema, "required"); !ok || !php.ToBool(required) {
			return errs
		}
	}

	c, _ := schema.Get("const")
	if isEqual(element, c) {
		return errs
	}

	return append(errs, v.newError(errConstant, p, php.ArrayOf("const", c)))
}

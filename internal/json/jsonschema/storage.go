// Ports src/JsonSchema/SchemaStorage.php, src/JsonSchema/Uri/UriResolver.php,
// src/JsonSchema/Uri/UriRetriever.php and src/JsonSchema/Entity/JsonPointer.php
// of justinrainbow/json-schema 6.10.0.

package jsonschema

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/json/res"
	"github.com/stubbedev/maestro/internal/php"
)

// InternalProvidedSchemaURI is SchemaStorage::INTERNAL_PROVIDED_SCHEMA_URI.
const InternalProvidedSchemaURI = "internal://provided-schema/"

// Draft identifiers (DraftIdentifiers::DRAFT_3, DRAFT_4) without fragment.
const (
	draft3NoFragment = "http://json-schema.org/draft-03/schema"
	draft4NoFragment = "http://json-schema.org/draft-04/schema"
)

// SchemaError is an exception json-schema throws while loading or resolving
// a schema (ResourceNotFoundException, UnresolvableJsonPointerException,
// UriResolverException, JsonDecodingException, InvalidArgumentException,
// UnexpectedValueException).
type SchemaError struct{ Message string }

func (e *SchemaError) Error() string { return e.Message }

// fail aborts the validation with err, as a PHP exception would; Validate
// recovers it.
func fail(msg string) { panic(&SchemaError{Message: msg}) }

// failErr aborts the validation with an error returned by the Retriever.
func failErr(err error) { panic(retrieverError{err}) }

type retrieverError struct{ err error }

// addSchema ports SchemaStorage::addSchema. schema is owned by the
// storage: callers clone what they must not see modified.
func (v *Validator) addSchema(id string, schema any, retrieve bool) {
	if retrieve {
		if cached, ok := v.embeddedFor(id); ok {
			v.schemas[id] = cached
			return
		}
		schema = v.retrieveURI(id)
	}

	if a, ok := schema.(*php.Array); ok {
		schema = arrayToObjectRecursive(a)
	}

	// workaround for bug in draft-03 & draft-04 meta-schemas (id & $ref defined with incorrect format)
	if o, ok := schema.(*php.Object); ok {
		if id, ok := o.Get("id"); ok {
			if s, _ := id.(string); s == draft4NoFragment+"#" || s == draft3NoFragment+"#" {
				setPath(o, "properties", "id", "format")
				if s == draft3NoFragment+"#" {
					setPath(o, "properties", "$ref", "format")
				}
			}
		}
	}

	v.scanForSubschemas(schema, id)
	v.expandRefs(schema, id, "")
	v.schemas[id] = schema
}

// setPath performs $o->a->b->c = 'uri-reference' where the objects exist.
func setPath(o *php.Object, a, b, c string) {
	x, _ := o.Get(a)
	xo, ok := x.(*php.Object)
	if !ok {
		return
	}
	y, _ := xo.Get(b)
	if yo, ok := y.(*php.Object); ok {
		yo.Set(c, "uri-reference")
	}
}

// arrayToObjectRecursive ports BaseConstraint::arrayToObjectRecursive.
func arrayToObjectRecursive(a *php.Array) any {
	json, err := php.JSONEncode(a, 0)
	if err != nil {
		fail("Unable to encode schema array as JSON: " + err.Error())
	}
	v, _ := php.JSONDecode(json, false)
	if arr, ok := v.(*php.Array); ok {
		return php.ObjectFromArray(arr)
	}

	return v
}

// expandRefs ports SchemaStorage::expandRefs; parentProperty is the last
// entry of PHP's $propertyStack ("" when empty).
func (v *Validator) expandRefs(schema any, parentID, parentProperty string) {
	o, ok := schema.(*php.Object)
	if !ok {
		if a, ok := schema.(*php.Array); ok {
			for _, member := range a.All() {
				v.expandRefs(member, parentID, "")
			}
		}
		return
	}

	if ref, ok := o.Get("$ref"); ok {
		if s, ok := ref.(string); ok {
			resolved, isNull := resolveURI(s, parentID, true)
			if isNull {
				fail("Ref value must be a string")
			}
			o.Set("$ref", parsePointer(resolved).String())
		}
	}

	for name, member := range o.All() {
		if parentProperty != "properties" && (name == "enum" || name == "const") {
			continue
		}
		childID := parentID
		if schemaID, ok := findSchemaIDInObject(o); ok && childID != schemaID {
			childID, _ = resolveURI(schemaID, childID, true)
		}
		v.expandRefs(member, childID, name)
	}
}

// scanForSubschemas ports SchemaStorage::scanForSubschemas.
func (v *Validator) scanForSubschemas(schema any, parentID string) {
	var all func(yield func(string, any) bool)
	switch s := schema.(type) {
	case *php.Object:
		all = s.All()
	case *php.Array:
		all = func(yield func(string, any) bool) {
			for k, val := range s.All() {
				if !yield(k.String(), val) {
					return
				}
			}
		}
	default:
		return
	}

	for name, sub := range all {
		so, ok := sub.(*php.Object)
		if !ok {
			if a, ok := sub.(*php.Array); ok {
				for _, item := range a.All() {
					v.scanForSubschemas(item, parentID)
				}
			}
			continue
		}
		if id, ok := findSchemaIDInObject(so); ok && so.Has("type") {
			if name == "enum" || name == "const" {
				continue
			}
			resolved, _ := resolveURI(id, parentID, true)
			v.addSchema(resolved, so, false)
		}
		v.scanForSubschemas(so, parentID)
	}
}

// findSchemaIDInObject ports SchemaStorage::findSchemaIdInObject.
func findSchemaIDInObject(o *php.Object) (string, bool) {
	if id, ok := o.Get("id"); ok {
		if s, ok := id.(string); ok {
			return s, true
		}
	}
	if id, ok := o.Get("$id"); ok {
		if s, ok := id.(string); ok {
			return s, true
		}
	}

	return "", false
}

// getSchema ports SchemaStorage::getSchema.
func (v *Validator) getSchema(id string) any {
	s, ok := v.schemas[id]
	if !ok {
		v.addSchema(id, nil, true)
		s = v.schemas[id]
	}

	return s
}

// resolveRef ports SchemaStorage::resolveRef. stack holds the schemas whose
// $ref is being followed, to detect loops.
func (v *Validator) resolveRef(ref string, stack *refFrame) any {
	filename, fragment, _ := strings.Cut(ref, "#")
	if filename == "" {
		fail("Could not resolve fragment '" + parsePointer(ref).pathString() + "': no file is defined")
	}

	refSchema := v.getSchema(filename)
	for part := range strings.SplitSeq(php.TrimSet(fragment, "/"), "/") {
		if part = decodePointerPath(part); part == "" {
			continue
		}
		part = phpURLDecode(part)
		var child any
		found := false
		switch s := refSchema.(type) {
		case *php.Object:
			child, found = s.Get(part)
		case *php.Array:
			child, found = s.Get(part)
		}
		if !found {
			fail("File: " + filename + " is found, but could not resolve fragment: " + parsePointer(ref).pathString())
		}
		refSchema = v.resolveRefSchema(child, stack)
	}

	return refSchema
}

// refFrame is an entry of resolveRefSchema's $resolveStack.
type refFrame struct {
	schema *php.Object
	next   *refFrame
}

// phpURLDecode ports urldecode: "+" is a space, %XX a byte, and
// malformed % sequences are kept verbatim.
func phpURLDecode(s string) string {
	if !strings.ContainsAny(s, "%+") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '+':
			b.WriteByte(' ')
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
		default:
			b.WriteByte(c)
		}
	}

	return b.String()
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	}

	return c - '0'
}

// resolveRefSchema ports SchemaStorage::resolveRefSchema.
func (v *Validator) resolveRefSchema(refSchema any, stack *refFrame) any {
	o, ok := refSchema.(*php.Object)
	if !ok {
		return refSchema
	}
	if ref, ok := o.Get("$ref"); ok {
		if s, ok := ref.(string); ok {
			for f := stack; f != nil; f = f.next {
				if f.schema == o {
					fail("Dereferencing a pointer to " + s + " results in an infinite loop")
				}
			}

			return v.resolveRef(s, &refFrame{schema: o, next: stack})
		}
	}
	if o.Len() == 1 {
		if inner, ok := o.Get(""); ok {
			return inner
		}
	}

	return refSchema
}

// retrieveURI ports UriRetriever::retrieve($uri) (no base URI).
func (v *Validator) retrieveURI(uri string) any {
	resolvedURI, _ := resolveURI(uri, "", false)
	fetchURI := resolvedURI
	parts := parseURI(resolvedURI)
	if parts.hasFragment {
		parts.hasFragment = false
		fetchURI = parts.generate()
	}

	jsonSchema := v.loadSchema(fetchURI)

	usesDollarID := true
	if o, ok := jsonSchema.(*php.Object); ok {
		if d, ok := o.Get("$schema"); ok && d != nil {
			dialect := php.RtrimSet(php.ToString(d), "#")
			usesDollarID = dialect != draft3NoFragment && dialect != draft4NoFragment
		}
	}

	jsonSchema = resolvePointer(jsonSchema, resolvedURI)
	if o, ok := jsonSchema.(*php.Object); ok {
		if usesDollarID {
			o.Set("$id", resolvedURI)
		} else {
			o.Set("id", resolvedURI)
		}
	}

	return jsonSchema
}

// loadSchema ports UriRetriever::loadSchema.
func (v *Validator) loadSchema(fetchURI string) any {
	if s, ok := v.loaded[fetchURI]; ok {
		return s
	}
	if v.retrieve == nil {
		fail("JSON schema not found at " + fetchURI)
	}
	contents, err := v.retrieve(fetchURI)
	if err != nil {
		failErr(err)
	}
	schema, err := php.JSONDecode(contents, false)
	if err != nil {
		code := php.JSONErrorSyntax
		if jsonErr, ok := errors.AsType[*php.JSONError](err); ok {
			code = jsonErr.Code
		}
		fail(jsonDecodingMessage(code))
	}
	v.loaded[fetchURI] = schema

	return schema
}

// jsonDecodingMessage ports JsonDecodingException's messages.
func jsonDecodingMessage(code int) string {
	switch code {
	case php.JSONErrorDepth:
		return "The maximum stack depth has been exceeded"
	case php.JSONErrorStateMismatch:
		return "Invalid or malformed JSON"
	case php.JSONErrorCtrlChar:
		return "Control character error, possibly incorrectly encoded"
	case php.JSONErrorUTF8:
		return "Malformed UTF-8 characters, possibly incorrectly encoded"
	case php.JSONErrorSyntax:
		return "JSON syntax is malformed"
	}

	return "Syntax error"
}

// resolvePointer ports UriRetriever::resolvePointer.
func resolvePointer(jsonSchema any, uri string) any {
	parsed := parseURI(uri)
	if !parsed.hasFragment || parsed.fragment == "" || parsed.fragment == "0" {
		return jsonSchema
	}
	for el := range strings.SplitSeq(parsed.fragment, "/") {
		if el == "" || el == "0" {
			continue
		}
		el = strings.ReplaceAll(el, "~1", "/")
		el = strings.ReplaceAll(el, "~0", "~")
		o, _ := jsonSchema.(*php.Object)
		var next any
		if o != nil {
			next, _ = o.Get(el)
		}
		if !php.ToBool(next) {
			fail(`Fragment "` + parsed.fragment + `" not found in ` + uri)
		}
		jsonSchema = next
		if _, ok := jsonSchema.(*php.Object); !ok {
			fail(`Fragment part "` + el + `" is no object  in ` + uri)
		}
	}

	return jsonSchema
}

// uriParts is the result of UriResolver::parse.
type uriParts struct {
	scheme, authority, path, query, fragment string
	hasQuery, hasFragment                    bool
}

var uriRegexp = php.MustCompile(`|^(([^:/?#]+):)?(//([^/?#]*))?([^?#]*)(\?([^#]*))?(#(.*))?|`)

// parseURI ports UriResolver::parse.
func parseURI(uri string) uriParts {
	var p uriParts
	m, err := uriRegexp.Match(uri)
	if err != nil || m == nil {
		return p
	}
	// count($match) in PHP: trailing groups that did not participate are dropped.
	count := 0
	for i := m.Groups() - 1; i >= 0; i-- {
		if _, ok := m.Group(i); ok {
			count = i + 1
			break
		}
	}
	if count > 5 {
		p.scheme, p.authority, p.path = m.Get(2), m.Get(4), m.Get(5)
	}
	if count > 7 {
		p.query, p.hasQuery = m.Get(7), true
	}
	if count > 9 {
		p.fragment, p.hasFragment = m.Get(9), true
	}

	return p
}

// generate ports UriResolver::generate.
func (p uriParts) generate() string {
	uri := p.scheme + "://" + p.authority + p.path
	if p.hasQuery && p.query != "" {
		uri += "?" + p.query
	}
	if p.hasFragment {
		uri += "#" + p.fragment
	}

	return uri
}

var schemeSlashes = php.MustCompile(`|^[^/]+://|u`)

// resolveURI ports UriResolver::resolve; hasBase false is a null base. The
// second result reports a null return (empty uri and null base).
func resolveURI(uri, baseURI string, hasBase bool) (string, bool) {
	// treat non-uri base as local file path
	if hasBase && !isFilterURL(baseURI) {
		if ok, _ := schemeSlashes.IsMatch(baseURI); !ok {
			baseURI = localFileURI(baseURI)
		}
	}

	if uri == "" {
		return baseURI, !hasBase
	}

	components := parseURI(uri)
	if components.scheme != "" && components.scheme != "0" {
		return uri, false
	}

	base := parseURI(baseURI)
	base.path = combineRelativePathWithBasePath(components.path, base.path)
	if components.hasFragment {
		base.fragment, base.hasFragment = components.fragment, true
	}

	return base.generate(), false
}

// isFilterURL approximates filter_var($uri, FILTER_VALIDATE_URL) for the
// URIs that do not contain "://": only mailto:, news: and file: URLs pass.
func isFilterURL(uri string) bool {
	scheme, rest, ok := strings.Cut(uri, ":")
	if !ok || rest == "" {
		return false
	}
	switch strings.ToLower(scheme) {
	case "mailto", "news", "file":
		return true
	}

	return false
}

// localFileURI turns a base that is no URI into a file:// URI as
// UriResolver::resolve does.
func localFileURI(base string) string {
	if fi, err := os.Stat(base); err == nil {
		real := realpath(base)
		if fi.Mode().IsRegular() {
			return "file://" + real
		}
		if fi.IsDir() {
			return "file://" + real + "/"
		}
	}
	cwd, _ := os.Getwd()

	return "file://" + cwd + "/" + base
}

func realpath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}

	return abs
}

var (
	dotSlashes    = php.MustCompile(`|((?<!\.)\./)*|`)
	doubleSlashes = php.MustCompile(`|//|`)
)

// combineRelativePathWithBasePath ports
// UriResolver::combineRelativePathWithBasePath.
func combineRelativePathWithBasePath(relativePath, basePath string) string {
	relativePath, _, _ = dotSlashes.Replace(relativePath, "", -1)
	relativePath, _, _ = doubleSlashes.Replace(relativePath, "/", -1)
	if relativePath == "" || relativePath == "0" {
		return basePath
	}
	if relativePath[0] == '/' {
		return relativePath
	}
	if basePath == "" || basePath == "0" {
		fail("Unable to resolve URI '" + relativePath + "' from base '" + basePath + "'")
	}

	dirname := basePath
	if basePath[len(basePath)-1] != '/' {
		dirname = phpDirname(basePath)
	}
	combined := php.RtrimSet(dirname, "/") + "/" + php.LtrimSet(relativePath, "/")
	collapsed := make([]string, 0, strings.Count(combined, "/")+1)
	for segment := range strings.SplitSeq(combined, "/") {
		if segment == ".." {
			if len(collapsed) <= 1 {
				fail("Unable to resolve URI '" + relativePath + "' from base '" + basePath + "'")
			}
			collapsed = collapsed[:len(collapsed)-1]
		} else {
			collapsed = append(collapsed, segment)
		}
	}

	return strings.Join(collapsed, "/")
}

// phpDirname ports dirname() for Unix paths.
func phpDirname(path string) string {
	p := php.RtrimSet(path, "/")
	if p == "" {
		if strings.HasPrefix(path, "/") {
			return "/"
		}
		return "."
	}
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return "."
	}
	d := php.RtrimSet(p[:i], "/")
	if d == "" {
		return "/"
	}

	return d
}

// pointer is JsonSchema\Entity\JsonPointer built from a string.
type pointer struct {
	filename string
	paths    []string
}

// parsePointer ports JsonPointer::__construct.
func parsePointer(value string) pointer {
	filename, fragment, ok := strings.Cut(value, "#")
	p := pointer{filename: filename}
	if ok {
		for part := range strings.SplitSeq(php.TrimSet(fragment, "/"), "/") {
			if part = decodePointerPath(part); part != "" {
				p.paths = append(p.paths, part)
			}
		}
	}

	return p
}

var (
	pointerDecoder = strings.NewReplacer("~1", "/", "~0", "~", "%25", "%")
	pointerEncoder = strings.NewReplacer("/", "~1", "~", "~0", "%", "%25")
)

func decodePointerPath(p string) string {
	if !strings.ContainsAny(p, "~%") {
		return p
	}

	return pointerDecoder.Replace(p)
}

// pathString ports JsonPointer::getPropertyPathAsString.
func (p pointer) pathString() string { return encodePointerPaths(p.paths) }

func encodePointerPaths(paths []string) string {
	var b strings.Builder
	b.WriteString("#/")
	for i, s := range paths {
		if i > 0 {
			b.WriteByte('/')
		}
		_, _ = pointerEncoder.WriteString(&b, s)
	}

	return php.RtrimSet(b.String(), "/")
}

// String ports JsonPointer::__toString.
func (p pointer) String() string { return p.filename + p.pathString() }

// embedded holds Composer's own schemas as SchemaStorage stores them after
// retrieving them by their res URI (decoded, $id set, $refs expanded). They
// are never modified afterwards, so validators share them. (A sync.Once
// rather than a sync.OnceValue initializer, which would be an
// initialization cycle through addSchema.)
var embedded struct {
	once    sync.Once
	schemas map[string]any
}

func embeddedSchemas() map[string]any {
	embedded.once.Do(func() { embedded.schemas = buildEmbedded() })

	return embedded.schemas
}

func buildEmbedded() map[string]any {
	m := make(map[string]any, 3)
	for _, uri := range []string{res.ComposerSchemaURI, res.LockSchemaURI, res.RepositorySchemaURI} {
		text, _ := res.Lookup(uri)
		v := &Validator{
			retrieve:   func(string) (string, error) { return text, nil },
			schemas:    map[string]any{},
			loaded:     map[string]any{},
			noEmbedded: true,
		}
		v.addSchema(uri, nil, true)
		if len(v.schemas) == 1 {
			m[uri] = v.schemas[uri]
		}
	}

	return m
}

// embeddedFor returns the shared, prepared copy of Composer's schema at uri
// when the retriever serves the embedded text for it.
func (v *Validator) embeddedFor(uri string) (any, bool) {
	text, ok := res.Lookup(uri)
	if !ok || v.retrieve == nil || v.noEmbedded {
		return nil, false
	}
	got, err := v.retrieve(uri)
	if err != nil || got != text {
		return nil, false
	}
	s, ok := embeddedSchemas()[uri]

	return s, ok
}

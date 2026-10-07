// Ports src/Composer/Json/JsonFile.php and
// src/Composer/Json/JsonValidationException.php.

package json

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json/jsonlint"
	"github.com/stubbedev/maestro/internal/json/jsonschema"
	"github.com/stubbedev/maestro/internal/json/res"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// JsonFile::*_SCHEMA.
const (
	LaxSchema    = 1
	StrictSchema = 2
	AuthSchema   = 3
	LockSchema   = 4
)

// IndentDefault is JsonFile::INDENT_DEFAULT.
const IndentDefault = "    "

// DefaultEncodeFlags are the json_encode options JsonFile::encode and
// JsonFile::write default to.
const DefaultEncodeFlags = php.JSONUnescapedSlashes | php.JSONPrettyPrint | php.JSONUnescapedUnicode

// draft04 is the $schema JsonFile::validateJsonSchema declares.
const draft04 = "https://json-schema.org/draft-04/schema#"

// HTTPDownloader is the part of Composer\Util\HttpDownloader JsonFile uses:
// the body of a GET request. Its errors are TransportExceptions; Read
// rethrows them as RuntimeExceptions with the same message.
type HTTPDownloader interface {
	Get(url string) (string, error)
}

// ValidationError is Composer\Json\JsonValidationException.
type ValidationError struct {
	Message string
	Errors  []string
	Prev    error
}

func (e *ValidationError) Error() string { return e.Message }

func (e *ValidationError) Unwrap() error { return e.Prev }

// PHPClass implements util.PHPClasser.
func (*ValidationError) PHPClass() (string, int) { return `Composer\Json\JsonValidationException`, 0 }

// File is Composer\Json\JsonFile.
type File struct {
	path           string
	httpDownloader HTTPDownloader
	io             io.IO
	indent         string
	// content is what the last read decoded (hasContent), for Content.
	content    string
	hasContent bool
}

var httpURL = php.MustCompile(`{^https?://}i`)

// NewFile ports JsonFile::__construct. httpDownloader and io may be nil.
func NewFile(path string, httpDownloader HTTPDownloader, io io.IO) (*File, error) {
	if httpDownloader == nil {
		if ok, err := httpURL.IsMatch(path); err != nil {
			return nil, err
		} else if ok {
			return nil, &util.InvalidArgumentError{Message: "http urls require a HttpDownloader instance to be passed"}
		}
	}

	return &File{path: path, httpDownloader: httpDownloader, io: io, indent: IndentDefault}, nil
}

// Path ports JsonFile::getPath.
func (f *File) Path() string { return f.path }

// Exists ports JsonFile::exists (is_file).
func (f *File) Exists() bool { return isFile(f.path) }

func isFile(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.Mode().IsRegular()
}

// Read ports JsonFile::read: the decoded file (json_decode with assoc), or
// a *jsonlint.ParsingError, *util.UnexpectedValueError or
// *util.RuntimeError.
func (f *File) Read() (any, error) {
	json, err := f.readContent()
	if err != nil {
		return nil, err
	}

	return f.decode(json)
}

// Content is the content of the file the last Read or ReadIfChanged
// decoded; ok is false before any, or for a file read over HTTP.
func (f *File) Content() (string, bool) { return f.content, f.hasContent }

// ReadIfChanged is Read for a caller holding what content decodes to (the
// data an earlier Read returned with that Content): when the file still
// holds content, it is read but not decoded again, and changed is false
// (deliberate deviation 3: the same content decodes to the same data).
func (f *File) ReadIfChanged(content string) (data any, changed bool, err error) {
	json, err := f.readContent()
	if err != nil {
		return nil, true, err
	}

	if f.httpDownloader == nil && json == content {
		return nil, false, nil
	}

	data, err = f.decode(json)

	return data, true, err
}

// decode is the end of read(): json's indentation detected and its value
// decoded, the content kept for Content.
func (f *File) decode(json string) (any, error) {
	indent, err := detectIndenting(json)
	if err != nil {
		return nil, err
	}
	f.indent = indent

	data, err := ParseJSON(json, f.path)
	if err == nil && f.httpDownloader == nil {
		f.content, f.hasContent = json, true
	}

	return data, err
}

// readContent is the start of read(): the file's content.
func (f *File) readContent() (string, error) {
	var json string
	if f.httpDownloader != nil {
		body, err := f.httpDownloader.Get(f.path)
		if err != nil {
			if _, ok := errors.AsType[*util.TransportError](err); ok {
				return "", &util.RuntimeError{Message: err.Error(), Prev: err}
			}

			return "", &util.RuntimeError{Message: "Could not read " + f.path + "\n\n" + err.Error()}
		}
		json = body
	} else {
		// the exceptions thrown here are wrapped by read()'s catch
		if !util.IsReadable(f.path) {
			return "", &util.RuntimeError{Message: "Could not read " + f.path + "\n\n" + `The file "` + f.path + `" is not readable.`}
		}
		if f.io != nil && f.io.IsDebug() {
			realpathInfo := ""
			if realpath := util.Realpath(f.path); realpath != f.path {
				realpathInfo = " (" + realpath + ")"
			}
			f.io.WriteError("Reading "+f.path+realpathInfo, true, io.Normal)
		}
		data, err := os.ReadFile(f.path)
		if err != nil {
			return "", &util.RuntimeError{Message: "Could not read " + f.path}
		}
		json = string(data)
	}

	return json, nil
}

// Write ports JsonFile::write; options are json_encode flags
// (DefaultEncodeFlags in PHP's default).
func (f *File) Write(hash any, options php.JSONFlag) error {
	if f.path == "php://memory" {
		return nil
	}

	dir := filepath.Dir(f.path)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		if err == nil {
			return &util.UnexpectedValueError{Message: util.Realpath(dir) + " exists and is not a directory."}
		}
		if os.MkdirAll(dir, 0o777) != nil {
			return &util.UnexpectedValueError{Message: dir + " does not exist and could not be created."}
		}
	}

	for retries := 2; ; retries-- {
		err := f.write(hash, options)
		if err == nil || retries == 0 {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (f *File) write(hash any, options php.JSONFlag) error {
	encoded, err := f.Encoded(hash, options)
	if err != nil {
		return err
	}
	_, err = util.FilePutContentsIfModified(f.path, []byte(encoded))

	return err
}

// Encoded is what Write(hash, options) writes to the file.
func (f *File) Encoded(hash any, options php.JSONFlag) (string, error) {
	encoded, err := Encode(hash, options, f.indent)
	if err != nil {
		return "", err
	}
	if options&php.JSONPrettyPrint != 0 {
		encoded += "\n"
	}

	return encoded, nil
}

// ValidateSchema ports JsonFile::validateSchema; schemaFile "" is null.
// The error is a *ValidationError, a *jsonlint.ParsingError, a
// *util.UnexpectedValueError or a *util.RuntimeError.
func (f *File) ValidateSchema(schema int, schemaFile string) error {
	if !util.IsReadable(f.path) {
		return &util.RuntimeError{Message: `The file "` + f.path + `" is not readable.`}
	}
	content, err := os.ReadFile(f.path)
	if err != nil {
		return &util.RuntimeError{Message: `The file "` + f.path + `" is not readable.`}
	}
	data, decodeErr := php.JSONDecode(string(content), false)
	if data == nil && string(content) != "null" {
		if err := validateSyntax(string(content), f.path, decodeErr); err != nil {
			return err
		}
	}

	return ValidateJSONSchema(f.path, data, schema, schemaFile)
}

// ValidateJSONSchema ports JsonFile::validateJsonSchema; schemaFile "" is
// null. data may hold arrays: like PHP, it is converted to objects through
// a JSON round trip first.
func ValidateJSONSchema(source string, data any, schema int, schemaFile string) error {
	isComposerSchemaFile := false
	if schemaFile == "" {
		if schema == LockSchema {
			schemaFile = res.LockSchemaURI
		} else {
			isComposerSchemaFile = true
			schemaFile = res.ComposerSchemaURI
		}
	}

	if !strings.Contains(schemaFile, "://") {
		schemaFile = "file://" + schemaFile
	}

	var schemaData any
	switch {
	case schema == StrictSchema && isComposerSchemaFile:
		text, err := retrieveSchema(schemaFile)
		if err != nil {
			return err
		}
		decoded, _ := php.JSONDecode(text, false)
		obj, ok := decoded.(*php.Object)
		if !ok {
			return &util.RuntimeError{Message: "Could not load the schema " + schemaFile}
		}
		obj.Set("additionalProperties", false)
		obj.Set("required", php.ListOf("name", "description"))
		schemaData = obj
	case schema == AuthSchema && isComposerSchemaFile:
		schemaData = refSchema(schemaFile + "#/properties/config")
	default:
		schemaData = refSchema(schemaFile)
	}

	// convert assoc arrays to objects
	encoded, err := php.JSONEncode(data, 0)
	if err != nil {
		encoded = ""
	}
	data, _ = php.JSONDecode(encoded, false)

	errs, err := jsonschema.NewValidator(retrieveSchema).Validate(data, schemaData)
	if err != nil {
		return err
	}
	if len(errs) == 0 {
		return nil
	}

	messages := make([]string, len(errs))
	for i, e := range errs {
		if e.Property != "" {
			messages[i] = e.Property + " : " + e.Message
		} else {
			messages[i] = e.Message
		}
	}

	return &ValidationError{Message: `"` + source + `" does not match the expected JSON schema`, Errors: messages}
}

func refSchema(ref string) *php.Object {
	o := php.NewObject()
	o.Set("$ref", ref)
	o.Set("$schema", draft04)

	return o
}

// retrieveSchema loads a schema document: Composer's own from res, anything
// else from a file:// URI.
func retrieveSchema(uri string) (string, error) {
	if text, ok := res.Lookup(uri); ok {
		return text, nil
	}
	if path, ok := strings.CutPrefix(uri, "file://"); ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", &util.RuntimeError{Message: "Could not load the schema " + uri}
		}

		return string(data), nil
	}

	return "", &util.RuntimeError{Message: "Could not load the schema " + uri}
}

var multiSpaceIndent = php.MustCompile(`#^ {4,}#m`)

// Encode ports JsonFile::encode: json_encode with options, re-indented with
// indent when pretty printing.
func Encode(data any, options php.JSONFlag, indent string) (string, error) {
	json, err := php.JSONEncode(data, options)
	if err != nil {
		code := 0
		if jsonErr, ok := errors.AsType[*php.JSONError](err); ok {
			code = jsonErr.Code
		}

		return "", encodeError(code)
	}

	if options&php.JSONPrettyPrint != 0 && indent != IndentDefault {
		json, _, err = multiSpaceIndent.ReplaceCallback(json, func(m *php.Match) string {
			return strings.Repeat(indent, len(m.Get(0))/4)
		}, -1)
		if err != nil {
			return "", err
		}
	}

	return json, nil
}

// EncodeDefault is Encode with JsonFile::encode's default options and
// indentation.
func EncodeDefault(data any) (string, error) { return Encode(data, DefaultEncodeFlags, IndentDefault) }

// encodeError ports JsonFile::throwEncodeError.
func encodeError(code int) error {
	var msg string
	switch code {
	case php.JSONErrorDepth:
		msg = "Maximum stack depth exceeded"
	case php.JSONErrorStateMismatch:
		msg = "Underflow or the modes mismatch"
	case php.JSONErrorCtrlChar:
		msg = "Unexpected control character found"
	case php.JSONErrorUTF8:
		msg = "Malformed UTF-8 characters, possibly incorrectly encoded"
	default:
		msg = "Unknown error"
	}

	return &util.RuntimeError{Message: "JSON encoding failed: " + msg}
}

var lockMergeConflict = php.MustCompile(`{\r?\n<<<<<<< [^\r\n]+\r?\n\s+"content-hash": *"[0-9a-f]+", *\r?\n(?:\|{7} [^\r\n]+\r?\n\s+"content-hash": *"[0-9a-f]+", *\r?\n)?=======\r?\n\s+"content-hash": *"[0-9a-f]+", *\r?\n>>>>>>> [^\r\n]+(\r?\n)}`)

// ParseJSON ports JsonFile::parseJson: json_decode($json, true), with
// jsonlint's diagnosis when that fails. file "" is null.
func ParseJSON(json, file string) (any, error) {
	data, err := php.JSONDecode(json, true)
	if data != nil || err == nil {
		return data, nil
	}

	// attempt resolving simple conflicts in lock files so that one can run `composer update --lock` and get a valid lock file
	if file != "" && strings.HasSuffix(file, ".lock") && strings.Contains(json, `"content-hash"`) {
		replaced, count, rerr := lockMergeConflict.Replace(json, "    \"content-hash\": \"VCS merge conflict detected. Please run `composer update --lock`.\",$1", 1)
		if rerr != nil {
			return nil, rerr
		}
		if count == 1 {
			if data, _ := php.JSONDecode(replaced, true); data != nil {
				return data, nil
			}
		}
	}

	if err := validateSyntax(json, file, err); err != nil {
		return nil, err
	}

	return nil, nil
}

// validateSyntax ports JsonFile::validateSyntax; decodeErr is the error of
// the json_decode that failed (json_last_error()).
func validateSyntax(json, file string, decodeErr error) error {
	lintErr := jsonlint.Lint(json, 0)
	if lintErr == nil {
		var jsonErr *php.JSONError
		if errors.As(decodeErr, &jsonErr) && jsonErr.Code == php.JSONErrorUTF8 {
			if file == "" {
				return &util.UnexpectedValueError{Message: "The input is not UTF-8, could not parse as JSON"}
			}

			return &util.UnexpectedValueError{Message: `"` + file + `" is not UTF-8, could not parse as JSON`}
		}

		return nil
	}

	var result *jsonlint.ParsingError
	if !errors.As(lintErr, &result) {
		return lintErr
	}
	if file == "" {
		return &jsonlint.ParsingError{Message: "The input does not contain valid JSON\n" + result.Message, Details: result.Details}
	}

	return &jsonlint.ParsingError{Message: `"` + file + `" does not contain valid JSON` + "\n" + result.Message, Details: result.Details}
}

var indentPrefix = php.MustCompile(`#^([ \t]+)"#m`)

// DetectIndenting ports JsonFile::detectIndenting. The pattern cannot fail
// (its [ \t]+ is possessive before the quote); detectIndenting returns the
// PcreException Preg would throw anyway.
func DetectIndenting(json string) string {
	indent, _ := detectIndenting(json)

	return indent
}

func detectIndenting(json string) (string, error) {
	m, err := indentPrefix.MatchStrictGroups(json)
	if err != nil {
		return "", err
	}
	if m != nil {
		return m.Get(1), nil
	}

	return IndentDefault, nil
}

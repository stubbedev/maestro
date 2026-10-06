// Ports src/Composer/Util/ConfigValidator.php.

package command

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/json/jsonlint"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/spdx"
	"github.com/stubbedev/maestro/internal/util"
)

// ConfigValidatorCheckVersion is ConfigValidator::CHECK_VERSION.
const ConfigValidatorCheckVersion = 1

// ConfigValidator is Composer\Util\ConfigValidator. It lives here rather
// than in internal/util because it needs the JSON schema and the package
// loaders, which sit above internal/util.
type ConfigValidator struct {
	io io.IO
}

// NewConfigValidator ports ConfigValidator::__construct.
func NewConfigValidator(out io.IO) *ConfigValidator {
	return &ConfigValidator{io: out}
}

var (
	cvDeprecatedGPLPlus = php.MustCompile(`{^[AL]?GPL-[123](\.[01])?\+$}i`)
	cvDeprecatedGPL     = php.MustCompile(`{^[AL]?GPL-[123](\.[01])?$}i`)
	cvUpper             = php.MustCompile(`{[A-Z]}`)
	cvCamelSplit        = php.MustCompile(`{(?:([a-z])([A-Z])|([A-Z])([A-Z][a-z]))}`)
	cvHash              = php.MustCompile(`/#/`)
)

// Validate ports ConfigValidator::validate: the errors, publishable errors
// and warnings for the file. The error result is what PHP lets escape the
// method (a TypeError on wrongly typed manifest values, or a loader
// failure other than InvalidPackageException).
func (v *ConfigValidator) Validate(file string, arrayLoaderValidationFlags, flags int) (errs, publishErrors, warnings []string, err error) {
	errs = []string{}
	publishErrors = []string{}
	warnings = []string{}

	// validate json schema
	laxValid := false
	var manifest any
	jsonFile, err := json.NewFile(file, nil, v.io)
	if err == nil {
		manifest, err = jsonFile.Read()
	}
	if err == nil {
		err = jsonFile.ValidateSchema(json.LaxSchema, "")
		if err == nil {
			laxValid = true
			err = jsonFile.ValidateSchema(json.StrictSchema, "")
		}
	}
	if err != nil {
		var ve *json.ValidationError
		if !errors.As(err, &ve) {
			errs = append(errs, err.Error())

			return errs, publishErrors, warnings, nil
		}
		for _, message := range ve.Errors {
			if laxValid {
				publishErrors = append(publishErrors, message)
			} else {
				errs = append(errs, message)
			}
		}
	}

	m, isArray := manifest.(*php.Array)
	if isArray {
		content, _ := os.ReadFile(file)
		var dup *jsonlint.DuplicateKeyError
		if _, perr := jsonlint.Parse(string(content), jsonlint.DetectKeyConflicts); errors.As(perr, &dup) {
			warnings = append(warnings, "Key "+dup.Details.Key+" is a duplicate in "+file+" at line "+strconv.Itoa(dup.Details.Line))
		}
	} else {
		// PHP indexes a scalar manifest: reads give null
		m = php.NewArray()
	}

	// validate actual data
	if license, _ := m.Get("license"); !php.ToBool(license) {
		warnings = append(warnings, `No license specified, it is recommended to do so. For closed-source software you may use "proprietary" as license.`)
	} else {
		var licenses []any
		if a, ok := license.(*php.Array); ok {
			licenses = a.Values()
		} else {
			licenses = []any{license}
		}

		licenseValidator := spdx.New()
		for _, l := range licenses {
			// strip proprietary since it's not a valid SPDX identifier, but is accepted by composer
			if s, ok := l.(string); ok && s == "proprietary" {
				continue
			}
			s, ok := l.(string)
			if !ok {
				// getLicenseByIdentifier($identifier) has no type: the
				// strtolower() of SpdxLicenses.php (strict) fails
				e := &pkg.TypeError{Message: "strtolower(): Argument #1 ($string) must be of type string, " + php.ZvalValueName(l) + " given"}

				return nil, nil, nil, e
			}
			info, found := licenseValidator.GetLicenseByIdentifier(s)
			if !found || !info.Deprecated {
				continue
			}
			plus, err := cvDeprecatedGPLPlus.IsMatch(s)
			if err != nil {
				return nil, nil, nil, err
			}
			if plus {
				// the license is part of the format, as in PHP
				w, err := php.Sprintf(`License "%s" is a deprecated SPDX license identifier, use "`+strings.ReplaceAll(s, "+", "")+`-or-later" instead`, s)
				if err != nil {
					return nil, nil, nil, err
				}
				warnings = append(warnings, w)

				continue
			}
			plain, err := cvDeprecatedGPL.IsMatch(s)
			if err != nil {
				return nil, nil, nil, err
			}
			if plain {
				w, err := php.Sprintf(`License "%s" is a deprecated SPDX license identifier, use "`+s+`-only" or "`+s+`-or-later" instead`, s)
				if err != nil {
					return nil, nil, nil, err
				}
				warnings = append(warnings, w)
			} else {
				warnings = append(warnings, `License "`+s+`" is a deprecated SPDX license identifier, see https://spdx.org/licenses/`)
			}
		}
	}

	if flags&ConfigValidatorCheckVersion != 0 && isset(m, "version") {
		warnings = append(warnings, "The version field is present, it is recommended to leave it out if the package is published on Packagist.")
	}

	if name, _ := m.Get("name"); php.ToBool(name) {
		s, ok := name.(string)
		if !ok {
			return nil, nil, nil, pkg.ArgumentTypeError(`Composer\Pcre\Preg::isMatch`, 2, "subject", "string", name)
		}
		upper, err := cvUpper.IsMatch(s)
		if err != nil {
			return nil, nil, nil, err
		}
		if upper {
			suggestName, _, err := cvCamelSplit.Replace(s, `\1\3-\2\4`, -1)
			if err != nil {
				return nil, nil, nil, err
			}
			suggestName = strings.ToLower(suggestName)

			publishErrors = append(publishErrors, `Name "`+s+`" does not match the best practice (e.g. lower-cased/with-dashes). We suggest using "`+suggestName+`" instead. As such you will not be able to submit it to Packagist.`)
		}
	}

	if typ, _ := m.Get("type"); php.ToBool(typ) && typ == "composer-installer" {
		warnings = append(warnings, "The package type 'composer-installer' is deprecated. Please distribute your custom installers as plugins from now on. See https://getcomposer.org/doc/articles/plugins.md for plugin documentation.")
	}

	// check for require-dev overrides
	if isset(m, "require") && isset(m, "require-dev") {
		require, ok1 := arrayValue(m, "require")
		requireDev, ok2 := arrayValue(m, "require-dev")
		if !ok1 || !ok2 {
			// array_intersect_key()'s first parameter is named, the
			// variadic rest is not
			arg, key := "Argument #1 ($array)", "require"
			if ok1 {
				arg, key = "Argument #2", "require-dev"
			}
			v, _ := m.Get(key)

			return nil, nil, nil, &pkg.TypeError{Message: "array_intersect_key(): " + arg + " must be of type array, " + php.ZvalValueName(v) + " given"}
		}
		var overrides []string
		for k := range require.All() {
			if requireDev.Has(k) {
				overrides = append(overrides, k.String())
			}
		}
		if len(overrides) > 0 {
			plural := "is"
			if len(overrides) > 1 {
				plural = "are"
			}
			warnings = append(warnings, strings.Join(overrides, ", ")+" "+plural+" required both in require and require-dev, this can lead to unexpected behavior")
		}
	}

	// check for meaningless provide/replace satisfying requirements
	for _, linkType := range []string{"provide", "replace"} {
		if !isset(m, linkType) {
			continue
		}
		for _, requireType := range []string{"require", "require-dev"} {
			if !isset(m, requireType) {
				continue
			}
			links, ok := arrayValue(m, linkType)
			if !ok {
				// the warning Composer's ErrorHandler turns into an exception
				v, _ := m.Get(linkType)

				return nil, nil, nil, &util.ErrorException{Message: "foreach() argument must be of type array|object, " + php.ZvalValueName(v) + " given"}
			}
			reqs, _ := arrayValue(m, requireType)
			for provide := range links.All() {
				if reqs != nil && isset(reqs, provide) {
					warnings = append(warnings, "The package "+provide.String()+" in "+requireType+" is also listed in "+linkType+" which satisfies the requirement. Remove it from "+linkType+" if you wish to install it.")
				}
			}
		}
	}

	// check for commit references
	require, ok1 := arrayOrEmpty(m, "require")
	requireDev, ok2 := arrayOrEmpty(m, "require-dev")
	if !ok1 || !ok2 {
		n, key := 1, "require"
		if ok1 {
			n, key = 2, "require-dev"
		}
		v, _ := m.Get(key)

		// an internal function's TypeError
		return nil, nil, nil, &pkg.TypeError{Message: "array_merge(): Argument #" + strconv.Itoa(n) + " must be of type array, " + php.ZvalValueName(v) + " given"}
	}
	for name, version := range php.ArrayMerge(require, requireDev).All() {
		s, ok := version.(string)
		if !ok {
			return nil, nil, nil, pkg.ArgumentTypeError(`Composer\Pcre\Preg::isMatch`, 2, "subject", "string", version)
		}
		// a single literal cannot exceed the match limit
		if hit, _ := cvHash.IsMatch(s); hit {
			warnings = append(warnings, `The package "`+name.String()+`" is pointing to a commit-ref, this is bad practice and can cause unforeseen issues.`)
		}
	}

	// report scripts-descriptions for non-existent scripts
	scripts, scriptsOK := arrayOrEmpty(m, "scripts")
	// foreach over a non-array warns (the ErrorException Composer's
	// ErrorHandler throws); array_key_exists() on a non-array $scripts is
	// a TypeError
	foreachError := func(key string) error {
		v, _ := m.Get(key)

		return &util.ErrorException{Message: "foreach() argument must be of type array|object, " + php.ZvalValueName(v) + " given"}
	}
	hasScript := func(name php.Key) (bool, error) {
		if !scriptsOK {
			v, _ := m.Get("scripts")

			return false, &pkg.TypeError{Message: "array_key_exists(): Argument #2 ($array) must be of type array, " + php.ZvalValueName(v) + " given"}
		}

		return scripts.Has(name), nil
	}
	scriptsDescriptions, ok := arrayOrEmpty(m, "scripts-descriptions")
	if !ok {
		return nil, nil, nil, foreachError("scripts-descriptions")
	}
	for scriptName := range scriptsDescriptions.All() {
		has, err := hasScript(scriptName)
		if err != nil {
			return nil, nil, nil, err
		}
		if !has {
			warnings = append(warnings, `Description for non-existent script "`+scriptName.String()+`" found in "scripts-descriptions"`)
		}
	}

	// report scripts-aliases for non-existent scripts
	scriptAliases, ok := arrayOrEmpty(m, "scripts-aliases")
	if !ok {
		return nil, nil, nil, foreachError("scripts-aliases")
	}
	for scriptName := range scriptAliases.All() {
		has, err := hasScript(scriptName)
		if err != nil {
			return nil, nil, nil, err
		}
		if !has {
			warnings = append(warnings, `Aliases for non-existent script "`+scriptName.String()+`" found in "scripts-aliases"`)
		}
	}

	// check for empty psr-0/psr-4 namespace prefixes
	if autoload, ok := arrayValue(m, "autoload"); ok {
		if psr0, ok := arrayValue(autoload, "psr-0"); ok && isset(psr0, "") {
			warnings = append(warnings, "Defining autoload.psr-0 with an empty namespace prefix is a bad idea for performance")
		}
		if psr4, ok := arrayValue(autoload, "psr-4"); ok && isset(psr4, "") {
			warnings = append(warnings, "Defining autoload.psr-4 with an empty namespace prefix is a bad idea for performance")
		}
	}

	if !isArray {
		// isset($manifest['version']) is false, so $manifest['version'] =
		// '1.0.0' writes into the scalar: an error, or, for false (and
		// null), a new array after the deprecation notice
		arr, _, deprecated, e := php.WritableArray(manifest)
		if e != nil {
			return nil, nil, nil, e
		}
		if deprecated {
			util.RaiseDeprecation(php.FalseToArrayDeprecation)
		}
		m = arr
	}
	l := loader.NewValidatingArrayLoader(loader.NewArrayLoader(nil, true), nil, arrayLoaderValidationFlags)
	if !isset(m, "version") {
		m.Set("version", "1.0.0")
	}
	if !isset(m, "name") {
		m.Set("name", "dummy/dummy")
	}
	_, lerr := l.Load(m, pkg.ClassCompletePackage)
	if lerr != nil {
		var ipe *loader.InvalidPackageError
		if !errors.As(lerr, &ipe) {
			return nil, nil, nil, lerr
		}
		errs = append(errs, ipe.Errors()...)
	}

	warnings = append(warnings, l.Warnings()...)

	return errs, publishErrors, warnings, nil
}

// isset is PHP's isset($a[$k]): present and not null.
func isset(a *php.Array, k any) bool {
	v, ok := a.Get(k)

	return ok && v != nil
}

// arrayValue returns $a[$k] when it is an array.
func arrayValue(a *php.Array, k any) (*php.Array, bool) {
	v, ok := a.Get(k)
	if !ok {
		return nil, false
	}
	arr, ok := v.(*php.Array)

	return arr, ok
}

// arrayOrEmpty is `$a[$k] ?? []`; ok is false when the value is set but not
// an array (PHP then fails with a TypeError).
func arrayOrEmpty(a *php.Array, k any) (*php.Array, bool) {
	v, ok := a.Get(k)
	if !ok || v == nil {
		return php.NewArray(), true
	}
	arr, ok := v.(*php.Array)

	return arr, ok
}

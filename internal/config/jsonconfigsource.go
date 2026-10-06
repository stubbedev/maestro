// Ports src/Composer/Config/JsonConfigSource.php.

package config

import (
	"errors"
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// JSONConfigSource is Composer\Config\JsonConfigSource: a ConfigSource
// backed by a composer.json, config.json or (authConfig) auth.json file.
// It edits the file with json.Manipulator, keeping its formatting, and
// falls back to rewriting the whole file when that fails.
type JSONConfigSource struct {
	file       *json.File
	authConfig bool
}

var _ ConfigSource = (*JSONConfigSource)(nil)

// NewJSONConfigSource ports JsonConfigSource::__construct.
func NewJSONConfigSource(file *json.File, authConfig bool) *JSONConfigSource {
	return &JSONConfigSource{file: file, authConfig: authConfig}
}

// Name ports JsonConfigSource::getName: the file's path.
func (s *JSONConfigSource) Name() string { return s.file.Path() }

// File returns the source's file.
func (s *JSONConfigSource) File() *json.File { return s.file }

// fallback edits the decoded file when the manipulator could not; args are
// the arguments manipulateJson was called with, as PHP passes them on.
type fallback func(config *php.Array, args []any) error

// AddRepository ports JsonConfigSource::addRepository.
func (s *JSONConfigSource) AddRepository(name string, config any, appendRepo bool) error {
	return s.manipulateJSON("addRepository", func(cfg *php.Array, args []any) error {
		repo := arg(args, 0)
		repoConfig := arg(args, 1)
		if err := convertRepositoriesToList(cfg); err != nil {
			return err
		}

		if repoConfig == false {
			if isset(cfg, "repositories") {
				repos, _ := cfg.Get("repositories")
				if list, ok := repos.(*php.Array); ok {
					disabled := php.ArrayOf(repo, repoConfig)
					for k, repository := range list.All() {
						if repoName(repository) == repo {
							list.SetKey(k, disabled)

							return nil
						}
						if php.StrictEquals(repository, php.ArrayOf(repo, false)) {
							return nil
						}
					}
				}
			} else {
				cfg.Set("repositories", php.NewArray())
			}

			list, err := child(cfg, "repositories")
			if err != nil {
				return err
			}
			list.Append(php.ArrayOf(repo, repoConfig))

			return nil
		}

		if rc, ok := repoConfig.(*php.Array); ok && repo != "" && !isset(rc, "name") {
			repoConfig = prependName(repo, rc)
		}

		// ensure uniqueness by removing any existing entries which use the same name
		filtered, err := filterRepositoriesByName(cfg, repo)
		if err != nil {
			return err
		}
		if arg(args, 2) == true {
			filtered.Append(repoConfig)
		} else {
			filtered.Unshift(repoConfig)
		}
		cfg.Set("repositories", filtered)

		return nil
	}, name, config, appendRepo)
}

// InsertRepository ports JsonConfigSource::insertRepository.
func (s *JSONConfigSource) InsertRepository(name string, config any, referenceName string, offset int) error {
	return s.manipulateJSON("insertRepository", func(cfg *php.Array, args []any) error {
		name := arg(args, 0)
		repoConfig := arg(args, 1)
		referenceName := arg(args, 2)
		offset := php.ToInt(arg(args, 3))
		if err := convertRepositoriesToList(cfg); err != nil {
			return err
		}

		// ensure uniqueness by removing any existing entries which use the same name
		repos, err := filterRepositoriesByName(cfg, name)
		if err != nil {
			return err
		}
		cfg.Set("repositories", repos)

		indexToInsert := int64(-1)
		for k, repository := range repos.All() {
			if repoName(repository) == referenceName || php.StrictEquals(php.ArrayOf(referenceName, false), repository) {
				indexToInsert = k.Int()

				break
			}
		}

		if indexToInsert < 0 {
			return &util.RuntimeError{Message: `The referenced repository "` + php.ToString(referenceName) + `" does not exist.`}
		}

		if rc, ok := repoConfig.(*php.Array); ok && name != "" && !isset(rc, "name") {
			repoConfig = prependName(name, rc)
		}

		php.ArraySplice(repos, int(indexToInsert+offset), 0, repoConfig)

		return nil
	}, name, config, referenceName, int64(offset))
}

// SetRepositoryURL ports JsonConfigSource::setRepositoryUrl.
func (s *JSONConfigSource) SetRepositoryURL(name, url string) error {
	return s.manipulateJSON("setRepositoryUrl", func(cfg *php.Array, args []any) error {
		name := arg(args, 0)
		url := arg(args, 1)
		repos, _ := cfg.Get("repositories")
		list, ok := repos.(*php.Array)
		if !ok {
			if repos == nil {
				return nil
			}

			return &util.ErrorException{Message: "foreach() argument must be of type array|object, " + zvalName(repos) + " given"}
		}
		for index, repository := range list.All() {
			if php.StrictEquals(name, index.Value()) {
				return setIn(cfg, url, "repositories", index.Value(), "url")
			}
			if php.StrictEquals(name, repoName(repository)) {
				return setIn(cfg, url, "repositories", index.Value(), "url")
			}
		}

		return nil
	}, name, url)
}

// RemoveRepository ports JsonConfigSource::removeRepository.
func (s *JSONConfigSource) RemoveRepository(name string) error {
	return s.manipulateJSON("removeRepository", func(cfg *php.Array, args []any) error {
		repo := arg(args, 0)
		if getIn(cfg, "repositories", repo) != nil {
			if err := unsetIn(cfg, "repositories", repo); err != nil {
				return err
			}
		} else {
			filtered, err := filterRepositoriesByName(cfg, repo)
			if err != nil {
				return err
			}
			cfg.Set("repositories", filtered)
		}

		if v, _ := cfg.Get("repositories"); isEmptyArray(v) {
			cfg.Delete("repositories")
		}

		return nil
	}, name)
}

var authSettingPattern = php.MustCompile(`{^(bitbucket-oauth|github-oauth|gitlab-oauth|gitlab-token|bearer|http-basic|custom-headers|forgejo-token|platform)\.}`)

// AddConfigSetting ports JsonConfigSource::addConfigSetting.
func (s *JSONConfigSource) AddConfigSetting(name string, value any) error {
	authConfig := s.authConfig

	return s.manipulateJSON("addConfigSetting", func(cfg *php.Array, args []any) error {
		key := php.ToString(arg(args, 0))
		val := arg(args, 1)
		if m, err := authSettingPattern.IsMatch(key); err != nil {
			return err
		} else if m {
			key, host, _ := strings.Cut(key, ".")
			if authConfig {
				return setIn(cfg, val, key, host)
			}

			return setIn(cfg, val, "config", key, host)
		}

		if strings.HasPrefix(key, "policy.") {
			if _, ok := cfg.GetArray("config"); !ok {
				cfg.Set("config", php.NewArray())
			}

			bits := strings.Split(key, ".")
			last := bits[len(bits)-1]
			arr, _ := cfg.GetArray("config")
			for _, bit := range bits[:len(bits)-1] {
				next, ok := arr.GetArray(bit)
				if !ok {
					next = php.NewArray()
					arr.Set(bit, next)
				}
				arr = next
			}
			arr.Set(last, val)

			return nil
		}

		return setIn(cfg, val, "config", key)
	}, name, value)
}

// RemoveConfigSetting ports JsonConfigSource::removeConfigSetting.
func (s *JSONConfigSource) RemoveConfigSetting(name string) error {
	authConfig := s.authConfig

	return s.manipulateJSON("removeConfigSetting", func(cfg *php.Array, args []any) error {
		key := php.ToString(arg(args, 0))
		if m, err := authSettingPattern.IsMatch(key); err != nil {
			return err
		} else if m {
			key, host, _ := strings.Cut(key, ".")
			if authConfig {
				return unsetIn(cfg, key, host)
			}

			return unsetIn(cfg, "config", key, host)
		}

		if strings.HasPrefix(key, "policy.") {
			config, ok := cfg.GetArray("config")
			if !ok {
				return nil
			}
			bits := strings.Split(key, ".")
			last := bits[len(bits)-1]
			bits = bits[:len(bits)-1]
			arr := config
			for _, bit := range bits {
				next, ok := arr.GetArray(bit)
				if !ok {
					return nil
				}
				arr = next
			}
			arr.Delete(last)

			// cascade: drop now-empty ancestors within the policy subtree (stops before config itself)
			for len(bits) > 0 {
				leafKey := bits[len(bits)-1]
				bits = bits[:len(bits)-1]
				parent := config
				for _, bit := range bits {
					v, _ := parent.Get(bit)
					if v == nil {
						return nil
					}
					next, ok := v.(*php.Array)
					if !ok {
						// PHP takes a reference to a scalar here, and
						// isset($parent[$leafKey]) on it is false.
						return nil
					}
					parent = next
				}
				if v, _ := parent.Get(leafKey); v != nil && isEmptyArray(v) {
					parent.Delete(leafKey)
				} else {
					break
				}
			}

			return nil
		}

		return unsetIn(cfg, "config", key)
	}, name)
}

// AddProperty ports JsonConfigSource::addProperty.
func (s *JSONConfigSource) AddProperty(name string, value any) error {
	return s.manipulateJSON("addProperty", func(cfg *php.Array, args []any) error {
		key := php.ToString(arg(args, 0))
		val := arg(args, 1)
		if !strings.HasPrefix(key, "extra.") && !strings.HasPrefix(key, "scripts.") {
			cfg.Set(key, val)

			return nil
		}

		bits := strings.Split(key, ".")
		last := bits[len(bits)-1]
		bits = bits[:len(bits)-1]
		// $arr = &$config[reset($bits)], then every bit, the first one
		// included, below it: the reference is holder[hkey]
		holder, hkey := cfg, any(bits[0])
		for _, bit := range bits {
			v, _ := holder.Get(hkey)
			// if (!isset($arr[$bit])) { $arr[$bit] = []; }
			a, ok := v.(*php.Array)
			if !ok || getIn(a, bit) == nil {
				var err error
				if a, err = child(holder, hkey); err != nil {
					return err
				}
				a.Set(bit, php.NewArray())
			}
			// $arr = &$arr[$bit]
			holder, hkey = a, bit
		}
		arr, err := child(holder, hkey)
		if err != nil {
			return err
		}
		arr.Set(last, val)

		return nil
	}, name, value)
}

// RemoveProperty ports JsonConfigSource::removeProperty.
func (s *JSONConfigSource) RemoveProperty(name string) error {
	return s.manipulateJSON("removeProperty", func(cfg *php.Array, args []any) error {
		key := php.ToString(arg(args, 0))
		lower := php.Strtolower(key)
		if !strings.HasPrefix(key, "extra.") && !strings.HasPrefix(key, "scripts.") && !strings.HasPrefix(lower, "autoload.") && !strings.HasPrefix(lower, "autoload-dev.") {
			cfg.Delete(key)

			return nil
		}

		bits := strings.Split(key, ".")
		last := bits[len(bits)-1]
		bits = bits[:len(bits)-1]
		// $arr = &$config[reset($bits)] creates the key, as null, when it
		// is missing
		if !cfg.Has(bits[0]) {
			cfg.Set(bits[0], nil)
		}
		var cur any
		cur, _ = cfg.Get(bits[0])
		for _, bit := range bits {
			arr, ok := cur.(*php.Array)
			if !ok {
				// isset() of a string offset or on a scalar; the bits
				// are never numeric where it could be true
				return nil
			}
			next, _ := arr.Get(bit)
			if next == nil {
				return nil
			}
			cur = next
		}

		return unsetIn(php.ArrayOf("v", cur), "v", last)
	}, name)
}

// AddLink ports JsonConfigSource::addLink.
func (s *JSONConfigSource) AddLink(typ, name, value string) error {
	return s.manipulateJSON("addLink", func(cfg *php.Array, args []any) error {
		return setIn(cfg, arg(args, 2), arg(args, 0), arg(args, 1))
	}, typ, name, value)
}

// RemoveLink ports JsonConfigSource::removeLink.
func (s *JSONConfigSource) RemoveLink(typ, name string) error {
	err := s.manipulateJSON("removeSubNode", func(cfg *php.Array, args []any) error {
		return unsetIn(cfg, arg(args, 0), arg(args, 1))
	}, typ, name)
	if err != nil {
		return err
	}

	return s.manipulateJSON("removeMainKeyIfEmpty", func(cfg *php.Array, args []any) error {
		typ := arg(args, 0)
		v, set := cfg.Get(typ)
		if !set {
			// $config[$type] read without isset: the warning comes first
			return &util.ErrorException{Message: "Undefined array key " + undefinedKey(typ)}
		}
		a, ok := v.(*php.Array)
		if !ok {
			return &php.EngineError{Class: "TypeError", Message: "count(): Argument #1 ($value) must be of type Countable|array, " + php.ZvalValueName(v) + " given"}
		}
		if a.Len() == 0 {
			cfg.Delete(typ)
		}

		return nil
	}, typ)
}

// manipulateJSON ports JsonConfigSource::manipulateJson: method is the
// JsonManipulator method to try, fb the whole-file fallback, args their
// arguments.
func (s *JSONConfigSource) manipulateJSON(method string, fb fallback, args ...any) error {
	return s.manipulate(method, fb, args)
}

func (s *JSONConfigSource) manipulate(method string, fb fallback, args []any) error {
	path := s.file.Path()
	var contents string
	exists := s.file.Exists()
	switch {
	case exists:
		if !util.IsWritable(path) {
			return &util.RuntimeError{Message: `The file "` + path + `" is not writable.`}
		}
		if !util.IsReadable(path) {
			return &util.RuntimeError{Message: `The file "` + path + `" is not readable.`}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return &util.RuntimeError{Message: `The file "` + path + `" is not readable.`}
		}
		contents = string(data)
	case s.authConfig:
		contents = "{\n}\n"
	default:
		contents = "{\n    \"config\": {\n    }\n}\n"
	}

	manipulator, err := json.NewManipulator(contents)
	if err != nil {
		return err
	}

	newFile := !exists

	// override manipulator method for auth config files
	if s.authConfig && (method == "addConfigSetting" || method == "removeConfigSetting") {
		mainNode, name, found := strings.Cut(php.ToString(args[0]), ".")
		if !found {
			// [$mainNode, $name] = explode('.', $args[0], 2)
			return &util.ErrorException{Message: "Undefined array key 1"}
		}
		if method == "addConfigSetting" {
			method = "addSubNode"
			args = []any{mainNode, name, arg(args, 1)}
		} else {
			method = "removeSubNode"
			args = []any{mainNode, name}
		}
	}

	// try to update cleanly
	ok, err := callManipulator(manipulator, method, args)
	if err != nil {
		return err
	}
	if ok {
		if _, err := util.FilePutContentsIfModified(path, []byte(manipulator.Contents())); err != nil {
			return err
		}
	} else if err := s.rewrite(fb, args); err != nil {
		return err
	}

	if err := s.file.ValidateSchema(json.LaxSchema, ""); err != nil {
		var ve *json.ValidationError
		if !errors.As(err, &ve) {
			return err
		}
		// restore contents to the original state
		_, _ = util.FilePutContentsIfModified(path, []byte(contents))

		return &util.RuntimeError{Message: "Failed to update composer.json with a valid format, reverting to the original content. Please report an issue to us with details (command you run and a copy of your composer.json). " + php.EOL + strings.Join(ve.Errors, php.EOL), Prev: ve}
	}

	if newFile {
		_ = os.Chmod(path, 0o600)
	}

	return nil
}

// rewrite is the fallback of manipulateJson: decode the file, apply fb and
// write the whole file back.
func (s *JSONConfigSource) rewrite(fb fallback, args []any) error {
	decoded, err := s.file.Read()
	if err != nil {
		return err
	}
	// new JsonManipulator($contents) has already rejected a file that is
	// not a JSON object
	config, ok := decoded.(*php.Array)
	if !ok {
		config = php.NewArray()
	}

	// $fallback(...$args)
	err = fb(config, args)
	if err != nil {
		return err
	}

	// avoid ending up with arrays for keys that should be objects
	// (apply bottom-up so parent coercion to stdClass doesn't hide deeper sub-keys)
	if policy, ok := getIn(config, "config", "policy").(*php.Array); ok {
		for listName, listValue := range policy.All() {
			if isEmptyArray(listValue) {
				policy.SetKey(listName, php.NewObject())
			}
		}
		if policy.Len() == 0 {
			_ = setIn(config, php.NewObject(), "config", "policy")
		}
	}
	for _, prop := range [...]string{"platform", "http-basic", "bearer", "gitlab-token", "gitlab-oauth", "github-oauth", "custom-headers", "forgejo-token", "preferred-install"} {
		if isEmptyArray(getIn(config, "config", prop)) {
			_ = setIn(config, php.NewObject(), "config", prop)
		}
	}
	for _, prop := range [...]string{"psr-0", "psr-4"} {
		if isEmptyArray(getIn(config, "autoload", prop)) {
			_ = setIn(config, php.NewObject(), "autoload", prop)
		}
		if isEmptyArray(getIn(config, "autoload-dev", prop)) {
			_ = setIn(config, php.NewObject(), "autoload-dev", prop)
		}
	}
	for _, prop := range [...]string{"require", "require-dev", "conflict", "provide", "replace", "suggest", "config", "autoload", "autoload-dev", "scripts", "scripts-descriptions", "scripts-aliases", "support"} {
		if v, _ := config.Get(prop); isEmptyArray(v) {
			config.Set(prop, php.NewObject())
		}
	}

	return s.file.Write(config, json.DefaultEncodeFlags)
}

// callManipulator is call_user_func_array([$manipulator, $method], $args),
// PHP's defaults filling the arguments not given.
func callManipulator(m *json.Manipulator, method string, args []any) (bool, error) {
	str := func(i int) string { return php.ToString(args[i]) }
	switch method {
	case "addRepository":
		return m.AddRepository(str(0), args[1], args[2] == true)
	case "insertRepository":
		return m.InsertRepository(str(0), args[1], str(2), php.ToNativeInt(args[3]))
	case "setRepositoryUrl":
		return m.SetRepositoryURL(str(0), str(1))
	case "removeRepository":
		return m.RemoveRepository(str(0))
	case "addConfigSetting":
		return m.AddConfigSetting(str(0), args[1])
	case "removeConfigSetting":
		return m.RemoveConfigSetting(str(0))
	case "addSubNode":
		return m.AddSubNode(str(0), str(1), args[2], true)
	case "removeSubNode":
		return m.RemoveSubNode(str(0), str(1))
	case "addProperty":
		return m.AddProperty(str(0), args[1])
	case "removeProperty":
		return m.RemoveProperty(str(0))
	case "addLink":
		return m.AddLink(str(0), str(1), str(2), false)
	case "removeMainKeyIfEmpty":
		return m.RemoveMainKeyIfEmpty(str(0))
	}

	panic("unknown JsonManipulator method " + method)
}

// arg is $args[$i] (null when missing, as PHP passes too few arguments
// only to closures that ignore them).
func arg(args []any, i int) any {
	if i < len(args) {
		return args[i]
	}

	return nil
}

// repoName is $repository['name'] ?? null.
func repoName(repository any) any {
	if a, ok := repository.(*php.Array); ok {
		v, _ := a.Get("name")

		return v
	}

	return nil
}

// prependName is ['name' => $name] + $config.
func prependName(name any, config *php.Array) *php.Array {
	r := php.NewArrayCap(config.Len() + 1)
	r.Set("name", name)
	for k, v := range config.All() {
		if !r.Has(k.Value()) {
			r.SetKey(k, v)
		}
	}

	return r
}

// undefinedKey is how PHP's "Undefined array key" warning names key.
func undefinedKey(key any) string {
	k := php.ToKey(key)
	if k.IsInt() {
		return k.String()
	}

	return `"` + k.String() + `"`
}

// convertRepositoriesToList ports the conversion of an associative
// "repositories" to a list that addRepository and insertRepository start
// with.
func convertRepositoriesToList(cfg *php.Array) error {
	repos, ok := cfg.Get("repositories")
	if !ok || repos == nil {
		repos = php.NewArray()
	}
	isList, err := arrayIsList(repos)
	if err != nil || isList {
		return err
	}

	assoc, _ := repos.(*php.Array) // array_is_list accepted it
	list := php.NewArrayCap(assoc.Len())
	for index, repository := range assoc.All() {
		switch r, isArr := repository.(*php.Array); {
		case index.IsString() && isArr:
			// convert to list entry with name
			if !isset(r, "name") {
				r = prependName(index.String(), r)
			}
			list.Append(r)
		case index.IsString():
			// keep boolean entries (e.g. 'packagist.org' => false)
			list.Append(php.ArrayOf(index.String(), repository))
		default:
			list.Append(repository)
		}
	}
	cfg.Set("repositories", list)

	return nil
}

// filterRepositoriesByName is array_values(array_filter($config['repositories']
// ?? [], ...)) with the uniqueness predicate of JsonConfigSource.
func filterRepositoriesByName(cfg *php.Array, name any) (*php.Array, error) {
	repos, _ := cfg.Get("repositories")
	if repos == nil {
		return php.NewArray(), nil
	}
	list, ok := repos.(*php.Array)
	if !ok {
		return nil, &php.EngineError{Class: "TypeError", Message: "array_filter(): Argument #1 ($array) must be of type array, " + php.ZvalValueName(repos) + " given"}
	}

	disabled := php.ArrayOf(name, false)
	filtered := php.NewArrayCap(list.Len())
	for _, val := range list.All() {
		valName := repoName(val)
		if valName == nil || !php.StrictEquals(valName, name) || !php.StrictEquals(val, disabled) {
			filtered.Append(val)
		}
	}

	return filtered, nil
}

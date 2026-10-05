// Ports src/Composer/Config.php.

package config

import (
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/util"
)

// Config::SOURCE_*.
const (
	SourceDefault = "default"
	SourceCommand = "command"
	SourceUnknown = "unknown"
)

// RelativePaths is Config::RELATIVE_PATHS, a Get flag.
const RelativePaths = 1

// DefaultConfig returns a fresh copy of Config::$defaultConfig.
func DefaultConfig() *php.Array {
	return php.ArrayOf(
		"process-timeout", 300,
		"use-include-path", false,
		"allow-plugins", php.NewArray(),
		"use-parent-dir", "prompt",
		"preferred-install", "dist",
		"audit", php.ArrayOf("ignore", php.NewArray(), "abandoned", policy.AuditFail),
		"policy", true,
		"notify-on-install", true,
		"github-protocols", php.ListOf("https", "ssh", "git"),
		"gitlab-protocol", nil,
		"vendor-dir", "vendor",
		"bin-dir", "{$vendor-dir}/bin",
		"cache-dir", "{$home}/cache",
		"data-dir", "{$home}",
		"cache-files-dir", "{$cache-dir}/files",
		"cache-repo-dir", "{$cache-dir}/repo",
		"cache-vcs-dir", "{$cache-dir}/vcs",
		"cache-ttl", 15552000, // 6 months
		"cache-files-ttl", nil, // fallback to cache-ttl
		"cache-files-maxsize", "300MiB",
		"cache-read-only", false,
		"bin-compat", "auto",
		"discard-changes", false,
		"autoloader-suffix", nil,
		"sort-packages", false,
		"optimize-autoloader", false,
		"classmap-authoritative", false,
		"apcu-autoloader", false,
		"prepend-autoloader", true,
		"update-with-minimal-changes", false,
		"github-domains", php.ListOf("github.com"),
		"bitbucket-expose-hostname", true,
		"disable-tls", false,
		"secure-http", true,
		"secure-svn-domains", php.NewArray(),
		"cafile", nil,
		"capath", nil,
		"github-expose-hostname", true,
		"gitlab-domains", php.ListOf("gitlab.com"),
		"store-auths", "prompt",
		"platform", php.NewArray(),
		"archive-format", "tar",
		"archive-dir", ".",
		"htaccess-protect", true,
		"use-github-api", true,
		"lock", true,
		"platform-check", "php-only",
		"bitbucket-oauth", php.NewArray(),
		"github-oauth", php.NewArray(),
		"gitlab-oauth", php.NewArray(),
		"gitlab-token", php.NewArray(),
		"http-basic", php.NewArray(),
		"bearer", php.NewArray(),
		"custom-headers", php.NewArray(),
		"bump-after-update", false,
		"allow-missing-requirements", false,
		"client-certificate", php.NewArray(),
		"forgejo-domains", php.ListOf("codeberg.org"),
		"forgejo-token", php.NewArray(),
		"source-fallback", false,
	)
}

// DefaultRepositories returns a fresh copy of Config::$defaultRepositories.
func DefaultRepositories() *php.Array {
	return php.ArrayOf("packagist.org", php.ArrayOf(
		"type", "composer",
		"url", "https://repo.packagist.org",
	))
}

// Config is Composer\Config.
//
// Get, All, Raw, Has, SourceOfValue and ProhibitURLByConfig may be called
// concurrently; the methods that change the configuration (Merge and the
// setters) must not run concurrently with anything else. Arrays returned by
// Get, Raw and Repositories are the configuration's own (PHP would hand out
// copies): Clone them before modifying.
type Config struct {
	config                *php.Array
	baseDir               string // "" is null
	repositories          *php.Array
	configSource          ConfigSource
	authConfigSource      ConfigSource
	localAuthConfigSource ConfigSource
	useEnvironment        bool
	rev                   uint64

	// mu guards the state Get and ProhibitURLByConfig record.
	mu                   sync.Mutex
	warnedHosts          map[string]struct{}
	sslVerifyWarnedHosts map[string]struct{}
	sourceOfConfigValue  map[string]string
}

// New ports Config::__construct: useEnvironment makes COMPOSER_* environment
// variables override settings; baseDir "" is null.
func New(useEnvironment bool, baseDir string) *Config {
	c := &Config{
		config:              DefaultConfig(),
		repositories:        DefaultRepositories(),
		useEnvironment:      useEnvironment,
		baseDir:             baseDir,
		sourceOfConfigValue: make(map[string]string, 128),
	}

	for k, v := range c.config.All() {
		c.setSourceOfConfigValue(v, k.String(), SourceDefault)
	}
	for k, v := range c.repositories.All() {
		c.setSourceOfConfigValue(v, "repositories."+k.String(), SourceDefault)
	}

	return c
}

// Rev is a counter every change to the configuration increments, for the
// plugin shim's mirrors.
func (c *Config) Rev() uint64 { return c.rev }

// SetBaseDir ports Config::setBaseDir ("" is null). Changing it can break
// path resolution for relative config paths.
func (c *Config) SetBaseDir(baseDir string) {
	c.baseDir = baseDir
	c.rev++
}

// BaseDir is the private Config::$baseDir ("" for null).
func (c *Config) BaseDir() string { return c.baseDir }

// SetConfigSource ports Config::setConfigSource.
func (c *Config) SetConfigSource(source ConfigSource) {
	c.configSource = source
	c.rev++
}

// ConfigSource ports Config::getConfigSource.
func (c *Config) ConfigSource() ConfigSource { return c.configSource }

// SetAuthConfigSource ports Config::setAuthConfigSource.
func (c *Config) SetAuthConfigSource(source ConfigSource) {
	c.authConfigSource = source
	c.rev++
}

// AuthConfigSource ports Config::getAuthConfigSource.
func (c *Config) AuthConfigSource() ConfigSource { return c.authConfigSource }

// SetLocalAuthConfigSource ports Config::setLocalAuthConfigSource.
func (c *Config) SetLocalAuthConfigSource(source ConfigSource) {
	c.localAuthConfigSource = source
	c.rev++
}

// LocalAuthConfigSource ports Config::getLocalAuthConfigSource; nil is null.
func (c *Config) LocalAuthConfigSource() ConfigSource { return c.localAuthConfigSource }

// isset is isset($a[$k]) on an array.
func isset(a *php.Array, k any) bool {
	v, ok := a.Get(k)

	return ok && v != nil
}

// arrayArgError is the TypeError PHP throws when an array function gets a
// non-array argument.
func arrayArgError(fn string, arg int, v any) error {
	return &php.EngineError{Class: "TypeError", Message: fn + "(): Argument #" + strconv.Itoa(arg) + " must be of type array, " + zvalName(v) + " given"}
}

var authKeys = [...]string{"bitbucket-oauth", "github-oauth", "gitlab-oauth", "gitlab-token", "http-basic", "bearer", "client-certificate", "forgejo-token"}

// policyDeepMergeKeys are the inner keys of a policy list that are deep
// merged so ignore rules from global and project sources both apply.
var policyDeepMergeKeys = [...]string{"ignore", "ignore-id", "ignore-severity", "ignore-source"}

var packagistURL = php.MustCompile(`{^https?://(?:[a-z0-9-.]+\.)?packagist.org(/|$)}`)

// Merge ports Config::merge: config's "config" and "repositories" entries
// override the current values. The error is the TypeError PHP throws on
// values of the wrong type (merging stops there, as in PHP).
func (c *Config) Merge(config *php.Array, source string) error {
	c.rev++
	// PHP receives a copy; keep none of the caller's arrays.
	config = config.Clone()

	if cfg, ok := config.GetArray("config"); ok && cfg.Len() > 0 {
		for k, val := range cfg.All() {
			if err := c.mergeKey(k, val, source); err != nil {
				return err
			}
		}
	}

	if repos, ok := config.GetArray("repositories"); ok && repos.Len() > 0 {
		return c.mergeRepositories(repos, source)
	}

	return nil
}

func (c *Config) mergeKey(k php.Key, val any, source string) error {
	key := k.String()
	isStr := k.IsString()

	switch {
	case isStr && contains(authKeys[:], key) && isset(c.config, key):
		cur, _ := c.config.Get(key)
		merged, err := arrayMerge(cur, val)
		if err != nil {
			return err
		}
		c.config.Set(key, merged)
	case isStr && key == "allow-plugins" && isset(c.config, key) && isArray(val) && isArray(c.get(key)):
		// merging $val first to get the local config on top of the global one, then appending the global config,
		// then merging local one again to make sure the values from local win over global ones for keys present in both
		v, _ := val.(*php.Array)
		cur, _ := c.get(key).(*php.Array)
		c.config.Set(key, php.ArrayMerge(v, cur, v))
	case isStr && (key == "gitlab-domains" || key == "github-domains") && isset(c.config, key):
		merged, err := arrayMerge(c.get(key), val)
		if err != nil {
			return err
		}
		// array_unique compares string forms
		for _, v := range merged.All() {
			if isArray(v) {
				return errArrayToString
			}
		}
		c.config.Set(key, php.ArrayUnique(merged))
	case isStr && key == "preferred-install" && isset(c.config, key):
		cur := c.get(key)
		if !isArray(val) && !isArray(cur) {
			c.config.Set(key, val)

			break
		}
		if s, ok := val.(string); ok {
			val = php.ArrayOf("*", s)
		}
		if s, ok := cur.(string); ok {
			cur = php.ArrayOf("*", s)
			c.setSource(key+"*", source)
		}
		merged, err := arrayMerge(cur, val)
		if err != nil {
			return err
		}
		// the full match pattern needs to be last
		if wildcard, ok := merged.Get("*"); ok && wildcard != nil {
			merged.Delete("*")
			merged.Set("*", wildcard)
		}
		c.config.Set(key, merged)
	case isStr && key == "audit":
		var currentIgnores any
		if audit, ok := c.config.GetArray("audit"); ok {
			currentIgnores, _ = audit.Get("ignore")
		}
		merged, err := arrayMerge(c.get("audit"), val)
		if err != nil {
			return err
		}
		c.config.Set(key, merged)
		c.setSourceOfConfigValue(val, key, source)
		var incoming any = php.NewArray()
		valArr, _ := val.(*php.Array) // an array, as array_merge accepted it
		if ign, ok := valArr.Get("ignore"); ok && ign != nil {
			incoming = ign
		}
		ignores, err := arrayMerge(currentIgnores, incoming)
		if err != nil {
			return err
		}
		merged.Set("ignore", ignores)

		return nil
	case isStr && key == "policy":
		c.mergePolicy(val)
	default:
		c.config.SetKey(k, val)
		if !isStr {
			return &php.EngineError{Class: "TypeError", Message: "Composer\\Config::setSourceOfConfigValue(): Argument #2 ($path) must be of type string, int given"}
		}
	}

	c.setSourceOfConfigValue(val, key, source)

	return nil
}

// mergePolicy ports the 'policy' branch of Config::merge.
func (c *Config) mergePolicy(val any) {
	// The schema accepts `true`, `{}` as equivalent.
	// Canonicalise `true` to `[]` here so both shapes share a single merge code path and layer identically across config sources.
	if val == true {
		val = php.NewArray()
	}

	if val == false {
		c.config.Set("policy", false)

		return
	}
	in, ok := val.(*php.Array)
	if !ok {
		return
	}

	current, ok := c.get("policy").(*php.Array)
	if ok {
		current = current.Clone()
	} else {
		current = php.NewArray()
	}

	for listName, listConfig := range in.All() {
		if listName.IsString() && contains(policy.NonListKeys[:], listName.String()) {
			current.SetKey(listName, listConfig)

			continue
		}

		// Per-list canonicalisation: `true` ≡ `[]` ≡ "use defaults".
		if listConfig == true {
			listConfig = php.NewArray()
		}

		existing, _ := current.GetKey(listName)
		if existing == true {
			existing = php.NewArray()
		}

		existingArr, existingIsArr := existing.(*php.Array)
		incomingArr, incomingIsArr := listConfig.(*php.Array)

		switch {
		case listConfig == false:
			// Explicit disable always overrides any prior shape.
			current.SetKey(listName, false)
		case existing == nil || existing == false:
			// No prior layer (or it was disabled and is being re-enabled);
			// store the new value as-is.
			current.SetKey(listName, listConfig)
		case existingIsArr && incomingIsArr:
			merged := php.ArrayMerge(existingArr, incomingArr)
			for _, innerKey := range policyDeepMergeKeys {
				existingInner, _ := existingArr.GetArray(innerKey)
				incomingInner, _ := incomingArr.GetArray(innerKey)
				if existingInner != nil && incomingInner != nil {
					merged.Set(innerKey, php.ArrayMerge(existingInner, incomingInner))
				}
			}
			current.SetKey(listName, merged)
		default:
			// Should not be reachable after the canonicalisations above,
			// but keep a deterministic fallback: incoming wins.
			current.SetKey(listName, listConfig)
		}
	}

	c.config.Set("policy", current)
}

// mergeRepositories ports the repositories half of Config::merge.
func (c *Config) mergeRepositories(repos *php.Array, source string) error {
	c.repositories = php.ArrayReverse(c.repositories, true)
	newRepos := php.ArrayReverse(repos, true)
	for name, repository := range newRepos.All() {
		// disable a repository by name
		// this is a code path, that will be used less as the next check will be preferred
		if repository == false {
			c.disableRepoByName(name.String())

			continue
		}

		repo, isArr := repository.(*php.Array)

		// disable a repository with an anonymous {"name": false} repo
		if isArr && repo.Len() == 1 {
			if k, v, _ := repo.First(); v == false {
				c.disableRepoByName(k.String())

				continue
			}
		}

		// auto-deactivate the default packagist.org repo if it gets redefined
		if isArr && isset(repo, "type") && isset(repo, "url") {
			if typ, _ := repo.Get("type"); typ == "composer" {
				u, _ := repo.Get("url")
				url, ok := u.(string)
				if !ok {
					return &php.EngineError{Class: "TypeError", Message: "Composer\\Pcre\\Preg::isMatch(): Argument #2 ($subject) must be of type string, " + zvalName(u) + " given"}
				}
				if m, err := packagistURL.IsMatch(url); err != nil {
					return err
				} else if m {
					c.disableRepoByName("packagist.org")
				}
			}
		}

		// store repo
		switch {
		case name.IsInt():
			if !isset(c.repositories, name.Value()) {
				c.repositories.SetKey(name, repository)
			} else {
				c.repositories.Append(repository)
			}
			found := ""
			if k, ok := php.ArraySearch(repository, c.repositories, true); ok {
				found = k.String()
			}
			c.setSourceOfConfigValue(repository, "repositories."+found, source)
		case name.String() == "packagist": // BC support for default "packagist" named repo
			c.repositories.Set("packagist.org", repository)
			c.setSourceOfConfigValue(repository, "repositories.packagist.org", source)
		default:
			c.repositories.SetKey(name, repository)
			c.setSourceOfConfigValue(repository, "repositories."+name.String(), source)
		}
	}
	c.repositories = php.ArrayReverse(c.repositories, true)

	return nil
}

// Repositories ports Config::getRepositories.
func (c *Config) Repositories() *php.Array { return c.repositories }

// get is $this->config[$key] (null when missing).
func (c *Config) get(key string) any {
	v, _ := c.config.Get(key)

	return v
}

// Get ports Config::get: the setting key, with environment overrides and
// {$refs} replaced. flags is a combination of RelativePaths.
func (c *Config) Get(key string, flags int) (any, error) {
	switch key {
	// strings/paths with env var and {$refs} support
	case "vendor-dir", "bin-dir", "process-timeout", "data-dir", "cache-dir", "cache-files-dir", "cache-repo-dir", "cache-vcs-dir", "cafile", "capath":
		// convert foo-bar to COMPOSER_FOO_BAR and check if it exists since it overrides the local config
		env := envName(key)

		envVal, set := c.getComposerEnv(env)
		if set {
			c.setSourceOfConfigValue(envVal, key, env)
		}

		if key == "process-timeout" {
			if set {
				return phpMax0(php.ToInt(envVal)), nil
			}

			return phpMax0(c.get(key)), nil
		}

		var raw any = envVal
		if !set {
			raw = c.get(key)
		}
		processed, err := c.process(raw, flags)
		if err != nil {
			return nil, err
		}
		str, err := toString(processed)
		if err != nil {
			return nil, err
		}
		val, err := util.ExpandPath(php.RtrimSet(str, "/\\"))
		if err != nil {
			return nil, err
		}

		if !strings.HasSuffix(key, "-dir") {
			return val, nil
		}
		if flags&RelativePaths == RelativePaths {
			return val, nil
		}

		return c.realpath(val), nil

	// booleans with env var support
	case "cache-read-only", "htaccess-protect":
		// convert foo-bar to COMPOSER_FOO_BAR and check if it exists since it overrides the local config
		env := envName(key)

		var val any
		if envVal, set := c.getComposerEnv(env); set {
			c.setSourceOfConfigValue(envVal, key, env)
			val = envVal
		} else {
			val = c.get(key)
		}

		return val != "false" && php.ToBool(val), nil

	// booleans without env var support
	case "disable-tls", "secure-http", "use-github-api", "lock", "source-fallback":
		// special case for secure-http
		if key == "secure-http" {
			if disableTLS, _ := c.Get("disable-tls", 0); disableTLS == true {
				return false, nil
			}
		}
		val := c.get(key)

		return val != "false" && php.ToBool(val), nil

	// ints without env var support
	case "cache-ttl":
		return phpMax0(php.ToInt(c.get(key))), nil

	// numbers with kb/mb/gb support, without env var support
	case "cache-files-maxsize":
		return c.cacheFilesMaxsize()

	// special cases below
	case "cache-files-ttl":
		if v := c.get(key); v != nil {
			return phpMax0(php.ToInt(v)), nil
		}

		return c.Get("cache-ttl", 0)

	case "home":
		v, exists := c.config.Get(key)
		if !exists {
			return nil, &util.ErrorException{Message: `Undefined array key "home"`}
		}
		s, ok := v.(string)
		if !ok {
			return nil, &php.EngineError{Class: "TypeError", Message: "Composer\\Util\\Platform::expandPath(): Argument #1 ($path) must be of type string, " + zvalName(v) + " given"}
		}
		expanded, err := util.ExpandPath(s)
		if err != nil {
			return nil, err
		}
		processed, err := c.process(expanded, flags)
		if err != nil {
			return nil, err
		}

		return php.RtrimSet(php.ToString(processed), "/\\"), nil

	case "bin-compat":
		return c.binCompat()

	case "discard-changes":
		return c.discardChanges()

	case "github-protocols":
		return c.githubProtocols()

	case "autoloader-suffix":
		v := c.get(key)
		if v == "" { // we need to guarantee null or non-empty-string
			return nil, nil
		}

		return c.process(v, flags)

	case "audit":
		return c.audit()

	case "policy":
		policyConfig := c.get(key)
		// Only the main switch (COMPOSER_POLICY) lives here, since it
		// can flip the whole config to `false`. Per-list block toggles
		// are applied in PolicyConfig::fromConfig against the parsed
		// objects.
		policyEnv, set, err := util.GetBoolEnv("COMPOSER_POLICY")
		if err != nil {
			return nil, err
		}
		if set && !policyEnv {
			policyConfig = false
		} else if set && policyEnv && policyConfig == false {
			// Re-enable a config-disabled policy. Existing array configs
			// are left untouched — the env var only flips the kill switch.
			policyConfig = true
		}

		return policyConfig, nil

	default:
		v, ok := c.config.Get(key)
		if !ok || v == nil {
			return nil, nil
		}

		return c.process(v, flags)
	}
}

// envName converts foo-bar to COMPOSER_FOO_BAR.
func envName(key string) string {
	return "COMPOSER_" + php.Strtoupper(strings.ReplaceAll(key, "-", "_"))
}

// phpMax0 is max(0, $v).
func phpMax0(v any) any {
	if php.Compare(v, int64(0)) > 0 {
		return v
	}

	return int64(0)
}

var maxsizePattern = php.MustCompile(`/^\s*([0-9.]+)\s*(?:([kmg])(?:i?b)?)?\s*$/i`)

func (c *Config) cacheFilesMaxsize() (any, error) {
	raw, err := toString(c.get("cache-files-maxsize"))
	if err != nil {
		return nil, err
	}
	m, err := maxsizePattern.Match(raw)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, &util.RuntimeError{Message: "Could not parse the value of 'cache-files-maxsize': " + raw}
	}
	size := php.ToFloat(m.Get(1))
	if unit, ok := m.Group(2); ok {
		switch php.Strtolower(unit) {
		case "g":
			size *= 1024 * 1024 * 1024
		case "m":
			size *= 1024 * 1024
		case "k":
			size *= 1024
		}
	}

	return phpMax0(php.ToInt(size)), nil
}

func (c *Config) binCompat() (any, error) {
	var value any
	if env, set := c.getComposerEnv("COMPOSER_BIN_COMPAT"); set && php.ToBool(env) {
		value = env
	} else {
		value = c.get("bin-compat")
	}

	valid := false
	for _, allowed := range [...]string{"auto", "full", "proxy", "symlink"} {
		if php.LooseEquals(value, allowed) {
			valid = true

			break
		}
	}
	if !valid {
		str, err := toString(value)
		if err != nil {
			return nil, err
		}

		return nil, &util.RuntimeError{Message: "Invalid value for 'bin-compat': " + str + ". Expected auto, full or proxy"}
	}

	// PHP raises an E_USER_DEPRECATED notice for "symlink" here, which
	// Composer's ErrorHandler only reports with the PHP file and line it
	// came from; maestro does not reproduce it.
	return value, nil
}

func (c *Config) discardChanges() (any, error) {
	if env, set := c.getComposerEnv("COMPOSER_DISCARD_CHANGES"); set {
		switch env {
		case "stash":
			return "stash", nil
		case "true", "false", "1", "0":
			// convert string value to bool
			return env != "false" && php.ToBool(env), nil
		}

		return nil, &util.RuntimeError{Message: "Invalid value for COMPOSER_DISCARD_CHANGES: " + env + ". Expected 1, 0, true, false or stash"}
	}

	v := c.get("discard-changes")
	if v != true && v != false && v != "stash" {
		str, err := toString(v)
		if err != nil {
			return nil, err
		}

		return nil, &util.RuntimeError{Message: "Invalid value for 'discard-changes': " + str + ". Expected true, false or stash"}
	}

	return v, nil
}

func (c *Config) githubProtocols() (any, error) {
	v := c.get("github-protocols")
	protos, ok := v.(*php.Array)
	if !ok {
		return nil, &php.EngineError{Class: "TypeError", Message: "array_search(): Argument #2 ($haystack) must be of type array, " + zvalName(v) + " given"}
	}
	if php.ToBool(c.get("secure-http")) {
		if index, found := php.ArraySearch("git", protos, false); found {
			protos = protos.Clone()
			protos.DeleteKey(index)
		}
	}
	if _, first, ok := protos.First(); ok && first == "http" {
		return nil, &util.RuntimeError{Message: `The http protocol for github is not available anymore, update your config's github-protocols to use "https", "git" or "ssh"`}
	}

	return protos, nil
}

func (c *Config) audit() (any, error) {
	result := c.get("audit")

	abandonedEnv, abandonedSet := c.getComposerEnv("COMPOSER_AUDIT_ABANDONED")
	if abandonedSet {
		if !contains(policy.Audits[:], abandonedEnv) {
			return nil, &util.RuntimeError{Message: "Invalid value for COMPOSER_AUDIT_ABANDONED: " + abandonedEnv + ". Expected one of " + strings.Join(policy.Audits[:], ", ") + "."}
		}
	}
	_, blockAbandonedSet := c.getComposerEnv("COMPOSER_SECURITY_BLOCKING_ABANDONED")
	if !abandonedSet && !blockAbandonedSet {
		return result, nil
	}

	arr, _ := result.(*php.Array)
	if arr == nil {
		arr = php.NewArray()
	} else {
		arr = arr.Clone()
	}
	if abandonedSet {
		arr.Set("abandoned", abandonedEnv)
	}
	if blockAbandonedSet {
		v, set, err := util.GetBoolEnv("COMPOSER_SECURITY_BLOCKING_ABANDONED")
		if err != nil {
			return nil, err
		}
		if set {
			arr.Set("block-abandoned", v)
		} else {
			arr.Set("block-abandoned", nil)
		}
	}

	return arr, nil
}

// All ports Config::all.
func (c *Config) All(flags int) (*php.Array, error) {
	cfg := php.NewArrayCap(c.config.Len())
	for k := range c.config.All() {
		if k.IsInt() {
			return nil, &php.EngineError{Class: "TypeError", Message: "Composer\\Config::get(): Argument #1 ($key) must be of type string, int given"}
		}
		v, err := c.Get(k.String(), flags)
		if err != nil {
			return nil, err
		}
		cfg.SetKey(k, v)
	}

	all := php.ArrayOf("repositories", c.Repositories())
	if cfg.Len() > 0 {
		all.Set("config", cfg)
	}

	return all, nil
}

// SourceOfValue ports Config::getSourceOfValue.
func (c *Config) SourceOfValue(key string) (string, error) {
	if _, err := c.Get(key, 0); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if source, ok := c.sourceOfConfigValue[key]; ok {
		return source, nil
	}

	return SourceUnknown, nil
}

func (c *Config) setSource(path, source string) {
	c.mu.Lock()
	c.sourceOfConfigValue[path] = source
	c.mu.Unlock()
}

// setSourceOfConfigValue records source for path and, for arrays, every
// path below it.
func (c *Config) setSourceOfConfigValue(value any, path, source string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setSourceLocked(value, path, source)
}

func (c *Config) setSourceLocked(value any, path, source string) {
	c.sourceOfConfigValue[path] = source

	if a, ok := value.(*php.Array); ok {
		for k, v := range a.All() {
			c.setSourceLocked(v, path+"."+k.String(), source)
		}
	}
}

// Raw ports Config::raw.
func (c *Config) Raw() *php.Array {
	return php.ArrayOf("repositories", c.Repositories(), "config", c.config)
}

// Has ports Config::has.
func (c *Config) Has(key string) bool { return c.config.Has(key) }

var refPattern = php.MustCompile(`#\{\$(.+)\}#`)

// process ports Config::process: replaces {$refs} inside a config string.
func (c *Config) process(value any, flags int) (any, error) {
	s, ok := value.(string)
	if !ok {
		return value, nil
	}

	var cbErr error
	out, _, err := refPattern.ReplaceCallback(s, func(m *php.Match) string {
		if cbErr != nil {
			return ""
		}
		v, err := c.Get(m.Get(1), flags)
		if err == nil {
			var str string
			if str, err = toString(v); err == nil {
				return str
			}
		}
		cbErr = err

		return ""
	}, -1)
	if cbErr != nil {
		return nil, cbErr
	}
	if err != nil {
		return nil, err
	}

	return out, nil
}

var absolutePathPattern = php.MustCompile(`{^(?:/|[a-z]:|[a-z0-9.]+://|\\\\)}i`)

// realpath ports Config::realpath: turns relative paths in absolute paths
// without realpath(), since the dirs might not exist yet.
func (c *Config) realpath(path string) string {
	// Anchored, and [a-z0-9.]+ is possessive before ':': Preg::isMatch
	// cannot throw on this pattern.
	if m, _ := absolutePathPattern.IsMatch(path); m {
		return path
	}
	if c.baseDir != "" {
		return c.baseDir + "/" + path
	}

	return path
}

// getComposerEnv ports Config::getComposerEnv: the value of a COMPOSER_
// environment variable overloading a config value, when the environment is
// used.
func (c *Config) getComposerEnv(name string) (string, bool) {
	if c.useEnvironment {
		return util.GetEnv(name)
	}

	return "", false
}

func (c *Config) disableRepoByName(name string) {
	if isset(c.repositories, name) {
		c.repositories.Delete(name)
	} else if name == "packagist" { // BC support for default "packagist" named repo
		c.repositories.Delete("packagist.org")
	}
}

// ProhibitURLByConfig ports Config::prohibitUrlByConfig: a
// *util.TransportError when the configuration does not allow accessing url.
// io may be nil; repoOptions may be nil.
func (c *Config) ProhibitURLByConfig(url string, out io.IO, repoOptions *php.Array) error {
	// Return right away if the URL is malformed or custom (see issue #5173), but only for non-HTTP(S) URLs
	if !util.FilterValidateURL(url) {
		// Anchored and fixed-length: Preg::isMatch cannot throw.
		if m, _ := httpURLPattern.IsMatch(url); !m {
			return nil
		}
	}

	// Extract scheme and throw exception on known insecure protocols
	parts, _ := util.ParseURL(url)
	scheme := php.Strtolower(parts.Scheme)
	hostname, hasHostname := parts.Host, parts.HasHost
	if scheme == "http" || scheme == "git" || scheme == "ftp" || scheme == "svn" {
		secureHTTP, err := c.Get("secure-http", 0)
		if err != nil {
			return err
		}
		if php.ToBool(secureHTTP) {
			if scheme == "svn" {
				domains, err := c.Get("secure-svn-domains", 0)
				if err != nil {
					return err
				}
				if d, ok := domains.(*php.Array); ok {
					var needle any
					if hasHostname {
						needle = hostname
					}
					if php.InArray(needle, d, true) {
						return nil
					}
				}

				return util.NewTransportError("Your configuration does not allow connections to "+util.SanitizeURL(url)+". See https://getcomposer.org/doc/06-config.md#secure-svn-domains for details.", 400)
			}

			return util.NewTransportError("Your configuration does not allow connections to "+util.SanitizeURL(url)+". See https://getcomposer.org/doc/06-config.md#secure-http for details.", 400)
		}
		if out != nil && hasHostname {
			c.mu.Lock()
			_, warned := c.warnedHosts[hostname]
			if c.warnedHosts == nil {
				c.warnedHosts = make(map[string]struct{})
			}
			c.warnedHosts[hostname] = struct{}{}
			c.mu.Unlock()
			if !warned {
				out.WriteError("<warning>Warning: Accessing "+hostname+" over "+scheme+" which is an insecure protocol.</warning>", true, io.Normal)
			}
		}
	}

	if out == nil || !hasHostname {
		return nil
	}
	c.mu.Lock()
	_, warned := c.sslVerifyWarnedHosts[hostname]
	c.mu.Unlock()
	if warned {
		return nil
	}

	warning := ""
	if ssl, ok := repoOptionsSSL(repoOptions); ok {
		if v, ok := ssl.Get("verify_peer"); ok && v != nil && !php.ToBool(v) {
			warning = "verify_peer"
		}
		if v, ok := ssl.Get("verify_peer_name"); ok && v != nil && !php.ToBool(v) {
			if warning == "" {
				warning = "verify_peer_name"
			} else {
				warning += " and verify_peer_name"
			}
		}
	}

	if warning != "" {
		out.WriteError("<warning>Warning: Accessing "+hostname+" with "+warning+" disabled.</warning>", true, io.Normal)
		c.mu.Lock()
		if c.sslVerifyWarnedHosts == nil {
			c.sslVerifyWarnedHosts = make(map[string]struct{})
		}
		c.sslVerifyWarnedHosts[hostname] = struct{}{}
		c.mu.Unlock()
	}

	return nil
}

func repoOptionsSSL(repoOptions *php.Array) (*php.Array, bool) {
	if repoOptions == nil {
		return nil, false
	}

	return repoOptions.GetArray("ssl")
}

var httpURLPattern = php.MustCompile(`{^https?://}i`)

// DisableProcessTimeout ports Config::disableProcessTimeout, used by
// long-running custom scripts in composer.json: it overrides the global
// process timeout set earlier by environment or config.
func DisableProcessTimeout() {
	util.SetProcessTimeout(0)
}

func contains(list []string, s string) bool { return slices.Contains(list, s) }

func isArray(v any) bool {
	_, ok := v.(*php.Array)

	return ok
}

// arrayMerge is array_merge($a, $b) with PHP's TypeError for non-arrays.
func arrayMerge(a, b any) (*php.Array, error) {
	x, ok := a.(*php.Array)
	if !ok {
		return nil, arrayArgError("array_merge", 1, a)
	}
	y, ok := b.(*php.Array)
	if !ok {
		return nil, arrayArgError("array_merge", 2, b)
	}

	return php.ArrayMerge(x, y), nil
}

var errArrayToString = &util.ErrorException{Message: "Array to string conversion"}

// toString is (string) $v, failing with the ErrorException Composer's error
// handler turns PHP's "Array to string conversion" warning into.
func toString(v any) (string, error) {
	if _, ok := v.(*php.Array); ok {
		return "", errArrayToString
	}

	return php.ToString(v), nil
}

// zvalName is zend_zval_value_name, the type PHP's TypeErrors name: like
// get_debug_type, except that booleans are "true" and "false".
func zvalName(v any) string {
	if b, ok := v.(bool); ok {
		if b {
			return "true"
		}

		return "false"
	}

	return php.TypeName(v)
}

// Ports src/Composer/Command/ConfigCommand.php.

package command

import (
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/json/res"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

func init() {
	registerCommand(OrderConfig, func() console.Commander { return NewConfigCommand() })
}

// configurablePackageProperties is CONFIGURABLE_PACKAGE_PROPERTIES.
var configurablePackageProperties = []string{
	"name",
	"type",
	"description",
	"homepage",
	"version",
	"minimum-stability",
	"prefer-stable",
	"keywords",
	"license",
	"repositories",
	"suggest",
	"extra",
}

// ConfigCommand is Composer\Command\ConfigCommand.
type ConfigCommand struct {
	*BaseConfigCommand

	// AuthFile and AuthConfigSource are $authConfigFile and
	// $authConfigSource.
	AuthFile         *json.File
	AuthConfigSource *config.JSONConfigSource
}

const configHelp = `This command allows you to edit composer config settings and repositories
in either the local composer.json file or the global config.json file.

Additionally it lets you edit most properties in the local composer.json.

To set a config setting:

    <comment>%command.full_name% bin-dir bin/</comment>

To read a config setting:

    <comment>%command.full_name% bin-dir</comment>
    Outputs: <info>bin</info>

To edit the global config.json file:

    <comment>%command.full_name% --global</comment>

To add a repository:

    <comment>%command.full_name% repositories.foo vcs https://bar.com</comment>

To remove a repository (repo is a short alias for repositories):

    <comment>%command.full_name% --unset repo.foo</comment>

To disable packagist.org:

    <comment>%command.full_name% repo.packagist.org false</comment>

You can alter repositories in the global config.json file by passing in the
<info>--global</info> option.

To add or edit suggested packages you can use:

    <comment>%command.full_name% suggest.package reason for the suggestion</comment>

To add or edit extra properties you can use:

    <comment>%command.full_name% extra.property value</comment>

Or to add a complex value you can use json with:

    <comment>%command.full_name% extra.property --json '{"foo":true, "bar": []}'</comment>

To edit the file in an external editor:

    <comment>%command.full_name% --editor</comment>

To choose your editor you can set the "EDITOR" env variable.

To get a list of configuration values in the file:

    <comment>%command.full_name% --list</comment>

You can always pass more than one option. As an example, if you want to edit the
global config.json file.

    <comment>%command.full_name% --editor --global</comment>

Read more at https://getcomposer.org/doc/03-cli.md#config`

// NewConfigCommand ports new ConfigCommand().
func NewConfigCommand() *ConfigCommand {
	c := &ConfigCommand{BaseConfigCommand: NewBaseConfigCommand("")}
	c.SetImpl(c)
	c.SetName("config").
		SetDescription("Sets config options").
		SetDefinitionItems(
			console.MustOption("global", "g", console.OptionValueNone, "Apply command to the global config file", nil),
			console.MustOption("editor", "e", console.OptionValueNone, "Open editor", nil),
			console.MustOption("auth", "a", console.OptionValueNone, "Affect auth config file (only used for --editor)", nil),
			console.MustOption("unset", "", console.OptionValueNone, "Unset the given setting-key", nil),
			console.MustOption("list", "l", console.OptionValueNone, "List configuration settings", nil),
			console.MustOption("file", "f", console.OptionValueRequired, "If you want to choose a different composer.json or config.json", nil),
			console.MustOption("absolute", "", console.OptionValueNone, "Returns absolute paths when fetching *-dir config values instead of relative", nil),
			console.MustOption("json", "j", console.OptionValueNone, "JSON decode the setting value, to be used with extra.* keys", nil),
			console.MustOption("merge", "m", console.OptionValueNone, "Merge the setting value with the current value, to be used with extra.* or audit.ignore[-abandoned] keys in combination with --json", nil),
			console.MustOption("append", "", console.OptionValueNone, "When adding a repository, append it (lowest priority) to the existing ones instead of prepending it (highest priority)", nil),
			console.MustOption("source", "", console.OptionValueNone, "Display where the config value is loaded from", nil),
			console.MustArgument("setting-key", console.ArgumentOptional, "Setting key", nil).WithSuggestFunc(c.suggestSettingKeys()),
			console.MustArgument("setting-value", console.ArgumentIsArray, "Setting value", nil),
		).
		SetHelp(configHelp)

	return c
}

// ClassName implements console.ClassNamer.
func (*ConfigCommand) ClassName() string { return `Composer\Command\ConfigCommand` }

// Initialize ports initialize.
func (c *ConfigCommand) Initialize(in console.Input, out console.Output) error {
	if err := c.BaseConfigCommand.Initialize(in, out); err != nil {
		return err
	}

	authConfigFile, err := c.AuthConfigFile(in, c.Config)
	if err != nil {
		return err
	}

	if c.AuthFile, err = json.NewFile(authConfigFile, nil, c.IO()); err != nil {
		return err
	}
	c.AuthConfigSource = config.NewJSONConfigSource(c.AuthFile, true)

	// Initialize the global file if it's not there, ignoring any warnings or notices
	if console.BoolOption(in, "global") && !c.AuthFile.Exists() {
		path := c.AuthFile.Path()
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o666); err == nil { //nolint:gosec // touch()
			_ = f.Close()
		}
		data := php.NewArray()
		for _, k := range []string{"bitbucket-oauth", "github-oauth", "gitlab-oauth", "gitlab-token", "http-basic", "bearer", "forgejo-token"} {
			data.Set(k, php.NewObject())
		}
		if err := c.AuthFile.Write(data, json.DefaultEncodeFlags); err != nil {
			return err
		}
		_ = os.Chmod(path, 0o600)
	}

	return nil
}

func configErr(class, message string) error {
	return NewError(class, message)
}

// readArray is JsonFile::read() where the caller needs an array.
func readArray(f *json.File) (*php.Array, error) {
	data, err := f.Read()
	if err != nil {
		return nil, err
	}
	if a, ok := data.(*php.Array); ok {
		return a, nil
	}

	return php.NewArray(), nil
}

// issetDim is isset($data[$key]) followed by the value.
func issetDim(data any, key string) (any, bool) {
	switch d := data.(type) {
	case *php.Array:
		v, ok := d.Get(key)
		if !ok || v == nil {
			return nil, false
		}

		return v, true
	case string:
		// string offsets: only integer-like keys within the string
		i, err := strconv.Atoi(key)
		if err != nil || strconv.Itoa(i) != key {
			return nil, false
		}
		if i < 0 {
			i += len(d)
		}
		if i < 0 || i >= len(d) {
			return nil, false
		}

		return d[i : i+1], true
	}

	return nil, false
}

func stringList(values []string) *php.Array {
	a := php.NewArrayCap(len(values))
	for _, v := range values {
		a.Append(v)
	}

	return a
}

// arrayMerge is array_merge for two arrays.
func arrayMerge(a, b *php.Array) *php.Array {
	out := php.NewArray()
	for _, src := range []*php.Array{a, b} {
		for k, v := range src.All() {
			if k.IsInt() {
				out.Append(v)
			} else {
				out.SetKey(k, v)
			}
		}
	}

	return out
}

// arrayUnion is $a + $b.
func arrayUnion(a, b *php.Array) *php.Array {
	out := a.Clone()
	for k, v := range b.All() {
		if _, ok := out.GetKey(k); !ok {
			out.SetKey(k, v)
		}
	}

	return out
}

var (
	reposPattern            = php.MustCompile(`/^repos?(?:itories)?(?:\.(.+))?/`)
	reposKeyPattern         = php.MustCompile(`/^repos?(?:itories)?\.(.+)/`)
	cacheMaxsizePattern     = php.MustCompile(`/^\s*([0-9.]+)\s*(?:([kmg])(?:i?b)?)?\s*$/i`)
	policyListPattern       = php.MustCompile(`/^policy\.([^.]+)$/`)
	policyCustomPattern     = php.MustCompile(`/^policy\.([^.]+)\.(block|audit)$/`)
	policySourcesPattern    = php.MustCompile(`/^policy\.([^.]+)\.sources$/`)
	preferredInstallPattern = php.MustCompile(`/^preferred-install\.(.+)/`)
	allowPluginsPattern     = php.MustCompile(`{^allow-plugins\.([a-zA-Z0-9/*-]+)}`)
	extraPattern            = php.MustCompile(`/^extra\.(.+)/`)
	suggestPattern          = php.MustCompile(`/^suggest\.(.+)/`)
	platformPattern         = php.MustCompile(`/^platform\.(.+)/`)
	authPattern             = php.MustCompile(`/^(bitbucket-oauth|github-oauth|gitlab-oauth|gitlab-token|http-basic|custom-headers|bearer|forgejo-token)\.(.+)/`)
	headerPattern           = php.MustCompile(`/^[^:]+:\s*.+$/`)
	scriptsPattern          = php.MustCompile(`/^scripts\.(.+)/`)
	configPrefixPattern     = php.MustCompile(`{^config\.}`)
	dotSuffixPattern        = php.MustCompile(`{\..*$}`)
	nonAlnumPattern         = php.MustCompile(`{[^a-z0-9]}i`)
	dashesPattern           = php.MustCompile(`{-+}`)
)

// configValidator is a validator/normalizer pair of a single value.
type configValidator struct {
	validate  func(val string) (bool, error)
	normalize func(val string) (any, error)
}

// multiValidator is a validator/normalizer pair of a list of values; the
// validator returns "" for true, else its message.
type multiValidator struct {
	validate  func(vals []string) string
	normalize func(vals []string) any
}

func booleanValidator(val string) (bool, error) {
	return val == "true" || val == "false" || val == "1" || val == "0", nil
}

func phpBool(val string) bool { return val != "" && val != "0" }

func booleanNormalizer(val string) (any, error) { return val != "false" && phpBool(val), nil }

func keepAsIs(val string) (any, error) { return val, nil }

func oneOf(values ...string) func(string) (bool, error) {
	return func(val string) (bool, error) { return slices.Contains(values, val), nil }
}

func isString(string) (bool, error) { return true, nil }

func nullIfNull(val string) (any, error) {
	if val == "null" {
		return nil, nil
	}

	return val, nil
}

var (
	boolPair  = configValidator{booleanValidator, booleanNormalizer}
	auditPair = configValidator{oneOf(policy.AuditIgnore, policy.AuditReport, policy.AuditFail), keepAsIs}
	strPair   = configValidator{isString, keepAsIs}
)

func keywordOrBool(keyword ...string) func(string) (any, error) {
	return func(val string) (any, error) {
		if slices.Contains(keyword, val) {
			return val, nil
		}

		return val != "false" && phpBool(val), nil
	}
}

func numericPair() configValidator {
	return configValidator{
		func(val string) (bool, error) { return php.IsNumeric(val), nil },
		func(val string) (any, error) { return php.ToInt(val), nil },
	}
}

// uniqueConfigValues are $uniqueConfigValues.
var uniqueConfigValues = map[string]configValidator{
	"process-timeout":   numericPair(),
	"use-include-path":  boolPair,
	"use-github-api":    boolPair,
	"preferred-install": {oneOf("auto", "source", "dist"), keepAsIs},
	"gitlab-protocol":   {oneOf("git", "http", "https"), keepAsIs},
	"store-auths":       {oneOf("true", "false", "prompt"), keywordOrBool("prompt")},
	"notify-on-install": boolPair,
	"vendor-dir":        strPair,
	"bin-dir":           strPair,
	"archive-dir":       strPair,
	"archive-format":    strPair,
	"data-dir":          strPair,
	"cache-dir":         strPair,
	"cache-files-dir":   strPair,
	"cache-repo-dir":    strPair,
	"cache-vcs-dir":     strPair,
	"cache-ttl":         numericPair(),
	"cache-files-ttl":   numericPair(),
	"cache-files-maxsize": {
		func(val string) (bool, error) { return cacheMaxsizePattern.IsMatch(val) },
		keepAsIs,
	},
	"bin-compat":                  {oneOf("auto", "full", "proxy", "symlink"), keepAsIs},
	"discard-changes":             {oneOf("stash", "true", "false", "1", "0"), keywordOrBool("stash")},
	"autoloader-suffix":           {isString, nullIfNull},
	"sort-packages":               boolPair,
	"optimize-autoloader":         boolPair,
	"classmap-authoritative":      boolPair,
	"apcu-autoloader":             boolPair,
	"prepend-autoloader":          boolPair,
	"update-with-minimal-changes": boolPair,
	"disable-tls":                 boolPair,
	"secure-http":                 boolPair,
	"source-fallback":             boolPair,
	"bump-after-update":           {oneOf("dev", "no-dev", "true", "false", "1", "0"), keywordOrBool("dev", "no-dev")},
	"cafile": {
		func(val string) (bool, error) { return fileExists(val) && util.IsReadable(val), nil },
		nullIfNull,
	},
	"capath": {
		func(val string) (bool, error) {
			st, err := os.Stat(val)

			return err == nil && st.IsDir() && util.IsReadable(val), nil
		},
		nullIfNull,
	},
	"github-expose-hostname":     boolPair,
	"htaccess-protect":           boolPair,
	"lock":                       boolPair,
	"allow-plugins":              boolPair,
	"platform-check":             {oneOf("php-only", "true", "false", "1", "0"), keywordOrBool("php-only")},
	"use-parent-dir":             {oneOf("true", "false", "prompt"), keywordOrBool("prompt")},
	"audit.abandoned":            auditPair,
	"audit.ignore-unreachable":   boolPair,
	"audit.block-insecure":       boolPair,
	"audit.block-abandoned":      boolPair,
	"policy.advisories.block":    boolPair,
	"policy.advisories.audit":    auditPair,
	"policy.malware.block":       boolPair,
	"policy.malware.block-scope": {oneOf("all", "update", "install"), keepAsIs},
	"policy.malware.audit":       auditPair,
	"policy.abandoned.block":     boolPair,
	"policy.abandoned.audit":     auditPair,
	"policy.ignore-unreachable":  boolPair,
}

func listOf(vals []string) any { return stringList(vals) }

func allIn(message string, allowed ...string) func([]string) string {
	return func(vals []string) string {
		for _, v := range vals {
			if !slices.Contains(allowed, v) {
				return message
			}
		}

		return ""
	}
}

var ignoreSeverityPair = multiValidator{allIn("valid severities include: low, medium, high, critical", "low", "medium", "high", "critical"), listOf}

// multiConfigValues are $multiConfigValues (every value is an array, so
// the "array expected" checks never fail).
var multiConfigValues = map[string]multiValidator{
	"github-protocols":                  {allIn("valid protocols include: git, https, ssh", "git", "https", "ssh"), listOf},
	"github-domains":                    {func([]string) string { return "" }, listOf},
	"gitlab-domains":                    {func([]string) string { return "" }, listOf},
	"audit.ignore-severity":             ignoreSeverityPair,
	"policy.advisories.ignore-severity": ignoreSeverityPair,
	"policy.malware.ignore-source":      {func([]string) string { return "" }, listOf},
}

// uniqueProps are $uniqueProps.
var uniqueProps = map[string]configValidator{
	"name":        strPair,
	"type":        strPair,
	"description": strPair,
	"homepage":    strPair,
	"version":     strPair,
	"minimum-stability": {
		func(val string) (bool, error) {
			s, err := normalizeStability(val)
			if err != nil {
				return false, err
			}
			_, ok := pkg.StabilityValue(s)

			return ok, nil
		},
		func(val string) (any, error) {
			s, err := normalizeStability(val)
			if err != nil {
				return nil, err
			}

			return s, nil
		},
	},
	"prefer-stable": boolPair,
}

// multiProps are $multiProps.
var multiProps = map[string]multiValidator{
	"keywords": {func([]string) string { return "" }, listOf},
	"license":  {func([]string) string { return "" }, listOf},
}

// normalizeStability is VersionParser::normalizeStability, its error an
// InvalidArgumentException.
func normalizeStability(val string) (string, error) {
	s, err := semver.NormalizeStability(val)
	if err != nil {
		return "", NewError(ClassInvalidArgument, err.Error())
	}

	return s, nil
}

// Execute implements console.Executor.
func (c *ConfigCommand) Execute(in console.Input, out console.Output) (int, error) {
	// Open file in editor
	if in.Option("editor") == true {
		return c.openEditor(in)
	}

	if in.Option("global") == false {
		read, err := c.ConfigFile.Read()
		if err != nil {
			return 0, err
		}
		// Config::merge(array $config) under strict_types: a file
		// holding a scalar is its TypeError
		data, ok := read.(*php.Array)
		if !ok {
			return 0, pkg.ArgumentTypeError(`Composer\Config::merge`, 1, "config", "array", read)
		}
		if err := c.Config.Merge(data, c.ConfigFile.Path()); err != nil {
			return 0, err
		}
		authData := php.NewArray()
		if c.AuthFile.Exists() {
			if authData, err = readArray(c.AuthFile); err != nil {
				return 0, err
			}
		}
		if err := c.Config.Merge(php.ArrayOf("config", authData), c.AuthFile.Path()); err != nil {
			return 0, err
		}
	}

	if err := c.IO().LoadConfiguration(c.Config.ForIO(), util.SetProcessTimeout); err != nil {
		return 0, err
	}

	// List the configuration of the file settings
	if in.Option("list") == true {
		all, err := c.Config.All(0)
		if err != nil {
			return 0, err
		}
		if err := c.listConfiguration(all, c.Config.Raw(), "", false, console.BoolOption(in, "source")); err != nil {
			return 0, err
		}

		return 0, nil
	}

	settingKey, ok := in.Argument("setting-key").(string)
	if !ok {
		return 0, nil
	}

	values := console.StringsArgument(in, "setting-value")
	unset := console.BoolOption(in, "unset")

	// If the user enters in a config variable, parse it and save to file
	if len(values) != 0 && unset {
		return 0, configErr(ClassRuntime, "You can not combine a setting value with --unset")
	}

	// show the value if no value is provided
	if len(values) == 0 && !unset {
		return c.showValue(in, settingKey)
	}

	return c.setValue(in, settingKey, values, unset)
}

func (c *ConfigCommand) openEditor(in console.Input) (int, error) {
	editor, ok := util.GetEnv("EDITOR")
	if !ok || editor == "" {
		editor = ""
		if util.IsWindows() {
			editor = "notepad"
		} else {
			for _, candidate := range []string{"editor", "vim", "vi", "nano", "pico", "ed"} {
				outp, _ := exec.Command("/bin/sh", "-c", "which "+candidate).Output() //nolint:gosec // exec('which '.$candidate), a fixed list
				lines := strings.Split(strings.TrimRight(string(outp), " \t\n\r\x00\x0B"), "\n")
				if last := lines[len(lines)-1]; last != "" && last != "0" {
					editor = candidate

					break
				}
			}
		}
	} else {
		var err error
		if editor, err = php.Escapeshellarg(editor); err != nil {
			return 0, err
		}
	}

	file := c.ConfigFile.Path()
	if console.BoolOption(in, "auth") {
		file = c.AuthFile.Path()
	}
	quoted, err := php.Escapeshellarg(file)
	if err != nil {
		return 0, err
	}
	cmd := exec.Command("/bin/sh", "-c", editor+" "+quoted+" > `tty`") //nolint:gosec // system(), as Composer runs $EDITOR
	if util.IsWindows() {
		cmd = exec.Command("cmd", "/c", editor+" "+quoted) //nolint:gosec // system(), as Composer runs $EDITOR
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = cmd.Run()

	return 0, nil
}

func (c *ConfigCommand) showValue(in console.Input, settingKey string) (int, error) {
	propertiesDefaults := php.ArrayOf(
		"type", "library",
		"description", "",
		"homepage", "",
		"minimum-stability", "stable",
		"prefer-stable", false,
		"keywords", php.NewArray(),
		"license", php.NewArray(),
		"suggest", php.NewArray(),
		"extra", php.NewArray(),
	)
	rawData, err := c.ConfigFile.Read()
	if err != nil {
		return 0, err
	}
	data, err := c.Config.All(0)
	if err != nil {
		return 0, err
	}
	source, err := c.Config.SourceOfValue(settingKey)
	if err != nil {
		return 0, err
	}

	var value any
	m, err := reposPattern.Match(settingKey)
	if err != nil {
		return 0, err
	}
	switch {
	case m != nil:
		repoName, has := m.Group(1)
		repos, _ := data.GetArray("repositories")
		if !has {
			if repos != nil {
				value = repos
			} else {
				value = php.NewArray()
			}
		} else {
			v, ok := issetDim(repos, repoName)
			if repos == nil || !ok {
				return 0, configErr(ClassInvalidArgument, "There is no "+repoName+" repository defined")
			}
			value = v
		}
	case strings.Index(settingKey, ".") > 0:
		bits := strings.Split(settingKey, ".")
		var cur any
		if bits[0] == "extra" || bits[0] == "suggest" {
			cur = rawData
		} else {
			cur, _ = data.Get("config")
		}
		match := false
		key, hasKey := "", false
		for _, bit := range bits {
			if hasKey {
				key = key + "." + bit
			} else {
				key = bit
			}
			hasKey = true
			match = false
			if v, ok := issetDim(cur, key); ok {
				match = true
				cur = v
				hasKey = false
			}
		}

		if !match {
			return 0, configErr(ClassRuntime, settingKey+" is not defined.")
		}

		value = cur
	default:
		cfgData, _ := data.Get("config")
		rawArr, _ := rawData.(*php.Array)
		if _, ok := issetDim(cfgData, settingKey); ok {
			flags := config.RelativePaths
			if console.BoolOption(in, "absolute") {
				flags = 0
			}
			if value, err = c.Config.Get(settingKey, flags); err != nil {
				return 0, err
			}
			// ensure we get {} output for properties which are objects
			if a, ok := value.(*php.Array); ok && a.Len() == 0 && schemaConfigIsObject(settingKey) {
				value = php.NewObject()
			}
		} else if v, ok := issetDim(rawArr, settingKey); ok && slices.Contains(configurablePackageProperties, settingKey) {
			value = v
			source = c.ConfigFile.Path()
		} else if v, ok := issetDim(propertiesDefaults, settingKey); ok {
			value = v
			source = "defaults"
		} else {
			return 0, configErr(ClassRuntime, settingKey+" is not defined")
		}
	}

	var text string
	switch value.(type) {
	case *php.Array, *php.Object, bool:
		if text, err = json.Encode(value, php.JSONUnescapedSlashes|php.JSONUnescapedUnicode, json.IndentDefault); err != nil {
			return 0, err
		}
	default:
		text = php.ToString(value)
	}

	sourceOfConfigValue := ""
	if console.BoolOption(in, "source") {
		sourceOfConfigValue = " (" + source + ")"
	}

	c.IO().Write(text+sourceOfConfigValue, true, io.Quiet)

	return 0, nil
}

// schemaConfigIsObject reports whether composer-schema.json types
// config.<key> as an object.
func schemaConfigIsObject(key string) bool {
	schema, err := json.ParseJSON(res.ComposerSchema(), "")
	if err != nil {
		return false
	}
	cur := schema
	for _, k := range []string{"properties", "config", "properties", key, "type"} {
		v, ok := issetDim(cur, k)
		if !ok {
			return false
		}
		cur = v
	}
	switch t := cur.(type) {
	case string:
		return t == "object"
	case *php.Array:
		for _, v := range t.All() {
			if v == "object" {
				return true
			}
		}
	}

	return false
}

// parseJSON is JsonFile::parseJson($value).
func parseJSON(value string) (any, error) { return json.ParseJSON(value, "") }

func (c *ConfigCommand) setValue(in console.Input, settingKey string, values []string, unset bool) (int, error) {
	src := c.ConfigSource
	first := ""
	if len(values) > 0 {
		first = values[0]
	}

	// unsetting any audit/policy.* key resolves to the same removeConfigSetting call
	// (JsonConfigSource handles the cascade-cleanup of empty ancestors for policy.*),
	// so handle them all in one place rather than repeating the check in each branch below.
	if unset && (settingKey == "audit" || settingKey == "policy" || strings.HasPrefix(settingKey, "policy.")) {
		return 0, src.RemoveConfigSetting(settingKey)
	}

	// handle policy.*.ignore / policy.advisories.ignore-id with --json + --merge support (mirrors audit.ignore)
	policyJSONMergeKeys := []string{"policy.advisories.ignore-id"}
	for _, listName := range policy.BuiltinListNames {
		policyJSONMergeKeys = append(policyJSONMergeKeys, "policy."+listName+".ignore")
	}
	nonCustomPolicyKeys := append(slices.Clone(policy.BuiltinListNames[:]), policy.NonListKeys[:]...)
	quoted := make([]string, len(nonCustomPolicyKeys))
	for i, name := range nonCustomPolicyKeys {
		quoted[i] = php.PregQuote(name, "/")
	}
	customIgnore, err := php.Compile(`/^policy\.(?!(?:` + strings.Join(quoted, "|") + `)$)([^.]+)\.ignore$/`)
	if err != nil {
		return 0, err
	}
	customMatch, err := customIgnore.Match(settingKey)
	if err != nil {
		return 0, err
	}
	if slices.Contains(policyJSONMergeKeys, settingKey) || customMatch != nil {
		if customMatch != nil {
			if reservedError := policy.FutureReservedListNameError(customMatch.Get(1)); reservedError != "" {
				return 0, configErr(ClassRuntime, "Invalid dependency policy name: "+reservedError)
			}
		}

		value, err := c.jsonListValue(in, settingKey, values)
		if err != nil {
			return 0, err
		}

		if console.BoolOption(in, "merge") {
			currentConfig, err := readArray(c.ConfigFile)
			if err != nil {
				return 0, err
			}
			currentValue, _ := currentConfig.Get("config")
			for bit := range strings.SplitSeq(settingKey, ".") {
				a, ok := currentValue.(*php.Array)
				if !ok {
					currentValue = nil

					break
				}
				v, ok := issetDim(a, bit)
				if !ok {
					currentValue = nil

					break
				}
				currentValue = v
			}

			if value, err = mergeJSONValue(currentValue, value, settingKey); err != nil {
				return 0, err
			}
		}

		return 0, src.AddConfigSetting(settingKey, value)
	}

	// handle policy.ignore-unreachable array form. Accepts:
	//   --json '["update","install"]'           (canonical JSON)
	//   policy.ignore-unreachable update install (positional enum values)
	// The boolean form (true/false) falls through to $uniqueConfigValues.
	if settingKey == "policy.ignore-unreachable" {
		scopesMessage := "valid values for " + settingKey + " include: " + strings.Join(policy.IgnoreUnreachableScopes[:], ", ")
		if console.BoolOption(in, "json") {
			value, err := parseJSON(first)
			if err != nil {
				return 0, err
			}
			a, ok := value.(*php.Array)
			if !ok {
				return 0, configErr(ClassRuntime, "Expected a boolean or array for "+settingKey)
			}
			for _, v := range a.All() {
				if s, ok := v.(string); !ok || !slices.Contains(policy.IgnoreUnreachableScopes[:], s) {
					return 0, configErr(ClassRuntime, scopesMessage)
				}
			}

			return 0, src.AddConfigSetting(settingKey, value)
		}

		// Positional enum values: accept e.g. `composer config policy.ignore-unreachable update install`.
		// Triggers only when the first value is one of the allowed scope strings, so `true`/`false` still
		// fall through to the boolean validator below.
		if len(values) > 0 && slices.Contains(policy.IgnoreUnreachableScopes[:], values[0]) {
			for _, v := range values {
				if !slices.Contains(policy.IgnoreUnreachableScopes[:], v) {
					return 0, configErr(ClassRuntime, scopesMessage)
				}
			}

			return 0, src.AddConfigSetting(settingKey, stringList(values))
		}
	}

	// handle policy.<list> = true|false (enable/disable an entire list) for built-in and custom lists;
	// policy.ignore-unreachable is excluded because it already has its own scalar/array handling via $uniqueConfigValues
	if m, err := policyListPattern.Match(settingKey); err != nil {
		return 0, err
	} else if m != nil && !slices.Contains(policy.NonListKeys[:], m.Get(1)) {
		if reservedError := policy.FutureReservedListNameError(m.Get(1)); reservedError != "" {
			return 0, configErr(ClassRuntime, "Invalid dependency policy name: "+reservedError)
		}
		if ok, _ := booleanValidator(first); !ok {
			return 0, configErr(ClassRuntime, `"`+first+`" is an invalid value for `+settingKey+", expected a boolean")
		}
		v, _ := booleanNormalizer(first)

		return 0, src.AddConfigSetting(settingKey, v)
	}

	// handle custom policy lists: policy.<name>.block / policy.<name>.audit
	if m, err := policyCustomPattern.Match(settingKey); err != nil {
		return 0, err
	} else if m != nil && !slices.Contains(nonCustomPolicyKeys, m.Get(1)) {
		if reservedError := policy.FutureReservedListNameError(m.Get(1)); reservedError != "" {
			return 0, configErr(ClassRuntime, "Invalid dependency policy name: "+reservedError)
		}
		if m.Get(2) == "block" {
			if ok, _ := booleanValidator(first); !ok {
				return 0, configErr(ClassRuntime, `"`+first+`" is an invalid value for `+settingKey+", expected a boolean")
			}
			v, _ := booleanNormalizer(first)
			if err := src.AddConfigSetting(settingKey, v); err != nil {
				return 0, err
			}
		} else {
			if ok, _ := auditPair.validate(first); !ok {
				return 0, configErr(ClassRuntime, `"`+first+`" is an invalid value for `+settingKey+", must be one of: ignore, report, fail")
			}
			if err := src.AddConfigSetting(settingKey, first); err != nil {
				return 0, err
			}
		}

		return 0, nil
	}

	// policy.<list>.sources is managed via the dedicated `composer policy` command rather than
	// `composer config`, since sources have structured shape (type/url) and validation rules.
	if m, err := policySourcesPattern.Match(settingKey); err != nil {
		return 0, err
	} else if m != nil {
		return 0, configErr(ClassRuntime, "Setting dependency policy sources is not supported by `composer config`. Use `composer policy add-source "+m.Get(1)+" url <https-url>` instead.")
	}

	_, isUnique := uniqueConfigValues[settingKey]
	_, isMulti := multiConfigValues[settingKey]
	if unset && (isUnique || isMulti) {
		if settingKey == "disable-tls" {
			v, err := c.Config.Get("disable-tls", 0)
			if err != nil {
				return 0, err
			}
			if php.ToBool(v) {
				c.IO().WriteError("<info>You are now running Composer with SSL/TLS protection enabled.</info>", true, io.Normal)
			}
		}

		return 0, src.RemoveConfigSetting(settingKey)
	}
	if isUnique {
		return 0, c.handleSingleValue(settingKey, uniqueConfigValues[settingKey], values, src.AddConfigSetting)
	}
	if isMulti {
		return 0, c.handleMultiValue(settingKey, multiConfigValues[settingKey], values, src.AddConfigSetting)
	}
	// handle preferred-install per-package config
	if m, err := preferredInstallPattern.Match(settingKey); err != nil {
		return 0, err
	} else if m != nil {
		if unset {
			return 0, src.RemoveConfigSetting(settingKey)
		}

		if ok, _ := uniqueConfigValues["preferred-install"].validate(first); !ok {
			return 0, configErr(ClassRuntime, "Invalid value for "+settingKey+". Should be one of: auto, source, or dist")
		}

		return 0, src.AddConfigSetting(settingKey, first)
	}

	// handle allow-plugins config setting elements true or false to add/remove
	if m, err := allowPluginsPattern.Match(settingKey); err != nil {
		return 0, err
	} else if m != nil {
		if unset {
			return 0, src.RemoveConfigSetting(settingKey)
		}

		if ok, _ := booleanValidator(first); !ok {
			return 0, configErr(ClassRuntime, `"`+first+`" is an invalid value`)
		}

		normalizedValue, _ := booleanNormalizer(first)

		return 0, src.AddConfigSetting(settingKey, normalizedValue)
	}

	// handle properties
	_, isUniqueProp := uniqueProps[settingKey]
	_, isMultiProp := multiProps[settingKey]
	if console.BoolOption(in, "global") && (isUniqueProp || isMultiProp || strings.HasPrefix(settingKey, "extra.")) {
		return 0, configErr(ClassInvalidArgument, "The "+settingKey+" property can not be set in the global config.json file. Use `composer global config` to apply changes to the global composer.json")
	}
	if unset && (isUniqueProp || isMultiProp) {
		return 0, src.RemoveProperty(settingKey)
	}
	if isUniqueProp {
		return 0, c.handleSingleValue(settingKey, uniqueProps[settingKey], values, src.AddProperty)
	}
	if isMultiProp {
		return 0, c.handleMultiValue(settingKey, multiProps[settingKey], values, src.AddProperty)
	}

	// handle repositories
	if m, err := reposKeyPattern.MatchStrictGroups(settingKey); err != nil {
		return 0, err
	} else if m != nil {
		name := m.Get(1)
		appendRepo := console.BoolOption(in, "append")
		if unset {
			return 0, src.RemoveRepository(name)
		}

		if len(values) == 2 {
			return 0, src.AddRepository(name, php.ArrayOf("type", values[0], "url", values[1]), appendRepo)
		}

		if len(values) == 1 {
			value := php.Strtolower(values[0])
			if ok, _ := booleanValidator(value); ok {
				if v, _ := booleanNormalizer(value); v == false {
					return 0, src.AddRepository(name, false, appendRepo)
				}
			} else {
				parsed, err := parseJSON(values[0])
				if err != nil {
					return 0, err
				}

				return 0, src.AddRepository(name, parsed, appendRepo)
			}
		}

		return 0, configErr(ClassRuntime, "You must pass the type and a url. Example: php composer.phar config repositories.foo vcs https://bar.com")
	}

	// handle extra
	if m, err := extraPattern.Match(settingKey); err != nil {
		return 0, err
	} else if m != nil {
		if unset {
			return 0, src.RemoveProperty(settingKey)
		}

		var value any = first
		if console.BoolOption(in, "json") {
			if value, err = parseJSON(first); err != nil {
				return 0, err
			}
			if console.BoolOption(in, "merge") {
				currentValue, err := c.ConfigFile.Read()
				if err != nil {
					return 0, err
				}
				for bit := range strings.SplitSeq(settingKey, ".") {
					currentValue, _ = issetDim(currentValue, bit)
				}
				cur, ok1 := currentValue.(*php.Array)
				val, ok2 := value.(*php.Array)
				if ok1 && ok2 {
					if cur.IsList() && val.IsList() {
						value = arrayMerge(cur, val)
					} else {
						value = arrayUnion(val, cur)
					}
				}
			}
		}

		return 0, src.AddProperty(settingKey, value)
	}

	// handle suggest
	if m, err := suggestPattern.Match(settingKey); err != nil {
		return 0, err
	} else if m != nil {
		if unset {
			return 0, src.RemoveProperty(settingKey)
		}

		return 0, src.AddProperty(settingKey, strings.Join(values, " "))
	}

	// handle unsetting extra/suggest
	if (settingKey == "suggest" || settingKey == "extra") && unset {
		return 0, src.RemoveProperty(settingKey)
	}

	// handle platform
	if m, err := platformPattern.Match(settingKey); err != nil {
		return 0, err
	} else if m != nil {
		if unset {
			return 0, src.RemoveConfigSetting(settingKey)
		}

		var v any = first
		if first == "false" {
			v = false
		}

		return 0, src.AddConfigSetting(settingKey, v)
	}

	// handle unsetting platform
	if settingKey == "platform" && unset {
		return 0, src.RemoveConfigSetting(settingKey)
	}

	// handle audit.ignore and audit.ignore-abandoned with --merge support
	if settingKey == "audit.ignore" || settingKey == "audit.ignore-abandoned" {
		if unset {
			return 0, src.RemoveConfigSetting(settingKey)
		}

		value, err := c.jsonListValue(in, settingKey, values)
		if err != nil {
			return 0, err
		}

		if console.BoolOption(in, "merge") {
			currentConfig, err := readArray(c.ConfigFile)
			if err != nil {
				return 0, err
			}
			var currentValue any = currentConfig
			for _, bit := range []string{"config", "audit", strings.ReplaceAll(settingKey, "audit.", "")} {
				currentValue, _ = issetDim(currentValue, bit)
			}

			if value, err = mergeJSONValue(currentValue, value, settingKey); err != nil {
				return 0, err
			}
		}

		return 0, src.AddConfigSetting(settingKey, value)
	}

	// handle auth
	if m, err := authPattern.Match(settingKey); err != nil {
		return 0, err
	} else if m != nil {
		return 0, c.setAuth(m.Get(1), m.Get(1)+"."+m.Get(2), values, unset)
	}

	// handle script
	if m, err := scriptsPattern.Match(settingKey); err != nil {
		return 0, err
	} else if m != nil {
		if unset {
			return 0, src.RemoveProperty(settingKey)
		}

		var v any = first
		if len(values) > 1 {
			v = stringList(values)
		}

		return 0, src.AddProperty(settingKey, v)
	}

	// handle unsetting other top level properties
	if unset {
		return 0, src.RemoveProperty(settingKey)
	}

	return 0, configErr(ClassInvalidArgument, "Setting "+settingKey+" does not exist or is not supported by this command")
}

// jsonListValue is `$value = $values;` with --json decoding values[0]
// into an array.
func (*ConfigCommand) jsonListValue(in console.Input, settingKey string, values []string) (any, error) {
	if !console.BoolOption(in, "json") {
		return stringList(values), nil
	}
	first := ""
	if len(values) > 0 {
		first = values[0]
	}
	value, err := parseJSON(first)
	if err != nil {
		// two lines above the throw
		return nil, err
	}
	if _, ok := value.(*php.Array); !ok {
		return nil, configErr(ClassRuntime, "Expected an array or object for "+settingKey)
	}

	return value, nil
}

// mergeJSONValue is the --merge of the audit/policy ignore lists.
func mergeJSONValue(currentValue, value any, settingKey string) (any, error) {
	cur, ok1 := currentValue.(*php.Array)
	val, ok2 := value.(*php.Array)
	if !ok1 || !ok2 {
		return value, nil
	}
	switch {
	case cur.IsList() && val.IsList():
		return arrayMerge(cur, val), nil
	case !cur.IsList() && !val.IsList():
		return arrayUnion(val, cur), nil
	}

	return nil, configErr(ClassRuntime, "Cannot merge array and object for "+settingKey)
}

func (c *ConfigCommand) setAuth(kind, key string, values []string, unset bool) error {
	if unset {
		if err := c.AuthConfigSource.RemoveConfigSetting(key); err != nil {
			return err
		}

		return c.ConfigSource.RemoveConfigSetting(key)
	}

	var value any
	switch {
	case kind == "bitbucket-oauth":
		if len(values) != 2 {
			return configErr(ClassRuntime, "Expected two arguments (consumer-key, consumer-secret), got "+strconv.Itoa(len(values)))
		}
		value = php.ArrayOf("consumer-key", values[0], "consumer-secret", values[1])
	case kind == "gitlab-token" && len(values) == 2:
		value = php.ArrayOf("username", values[0], "token", values[1])
	case kind == "github-oauth" || kind == "gitlab-oauth" || kind == "gitlab-token" || kind == "bearer":
		if len(values) != 1 {
			return configErr(ClassRuntime, "Too many arguments, expected only one token")
		}
		value = values[0]
	case kind == "http-basic":
		if len(values) != 2 {
			return configErr(ClassRuntime, "Expected two arguments (username, password), got "+strconv.Itoa(len(values)))
		}
		value = php.ArrayOf("username", values[0], "password", values[1])
	case kind == "custom-headers":
		if len(values) == 0 {
			return configErr(ClassRuntime, "Expected at least one argument (header), got none")
		}

		// Validate headers format
		for _, header := range values {
			// Check if the header is in correct "Name: Value" format
			ok, err := headerPattern.IsMatch(header)
			if err != nil {
				return err
			}
			if !ok {
				return configErr(ClassRuntime, `Header "`+header+`" is not in "Header-Name: Header-Value" format`)
			}
		}
		value = stringList(values)
	case kind == "forgejo-token":
		if len(values) != 2 {
			return configErr(ClassRuntime, "Expected two arguments (username, access token), got "+strconv.Itoa(len(values)))
		}
		value = php.ArrayOf("username", values[0], "token", values[1])
	default:
		return nil
	}

	if err := c.ConfigSource.RemoveConfigSetting(key); err != nil {
		return err
	}

	return c.AuthConfigSource.AddConfigSetting(key, value)
}

// handleSingleValue ports handleSingleValue.
func (c *ConfigCommand) handleSingleValue(key string, callbacks configValidator, values []string, method func(string, any) error) error {
	if len(values) != 1 {
		return configErr(ClassRuntime, "You can only pass one value. Example: php composer.phar config process-timeout 300")
	}

	ok, err := callbacks.validate(values[0])
	if err != nil {
		return err
	}
	if !ok {
		return configErr(ClassRuntime, `"`+values[0]+`" is an invalid value`)
	}

	normalizedValue, err := callbacks.normalize(values[0])
	if err != nil {
		return err
	}

	if key == "disable-tls" {
		current, err := c.Config.Get("disable-tls", 0)
		if err != nil {
			return err
		}
		if !php.ToBool(normalizedValue) && php.ToBool(current) {
			c.IO().WriteError("<info>You are now running Composer with SSL/TLS protection enabled.</info>", true, io.Normal)
		} else if php.ToBool(normalizedValue) && !php.ToBool(current) {
			c.IO().WriteError("<warning>You are now running Composer with SSL/TLS protection disabled.</warning>", true, io.Normal)
		}
	}

	return method(key, normalizedValue)
}

// handleMultiValue ports handleMultiValue.
func (*ConfigCommand) handleMultiValue(key string, callbacks multiValidator, values []string, method func(string, any) error) error {
	if validation := callbacks.validate(values); validation != "" {
		encoded, _ := php.JSONEncode(stringList(values), 0)

		return configErr(ClassRuntime, encoded+" is an invalid value ("+validation+")")
	}

	return method(key, callbacks.normalize(values))
}

// listConfiguration ports listConfiguration; k "" with hasK false is null.
func (c *ConfigCommand) listConfiguration(contents *php.Array, rawContents any, k string, hasK, showSource bool) error {
	out := c.IO()
	for key, value := range contents.All() {
		keyStr := key.String()
		if !hasK && keyStr != "config" && keyStr != "repositories" {
			continue
		}

		var rawVal any
		if rawArr, ok := rawContents.(*php.Array); ok {
			rawVal, _ = rawArr.GetKey(key)
		}

		if a, ok := value.(*php.Array); ok {
			firstKey, _, nonEmpty := a.First()
			numericFirst := nonEmpty && firstKey.IsInt()
			if !numericFirst || (keyStr == "repositories" && !hasK) {
				suffix, _, err := configPrefixPattern.Replace(keyStr+".", "", -1)
				if err != nil {
					return err
				}
				if err := c.listConfiguration(a, rawVal, k+suffix, true, showSource); err != nil {
					return err
				}

				continue
			}
		}

		var text string
		isString := false
		switch v := value.(type) {
		case *php.Array:
			parts := make([]string, 0, v.Len())
			for _, item := range v.All() {
				if _, ok := item.(*php.Array); ok {
					enc, _ := php.JSONEncode(item, 0)
					parts = append(parts, enc)
				} else {
					parts = append(parts, php.ToString(item))
				}
			}
			text = "[" + strings.Join(parts, ", ") + "]"
			isString = true
		case bool:
			text = php.VarExport(v)
			isString = true
		case string:
			text = v
			isString = true
		default:
			text = php.ToString(v)
		}

		source := ""
		if showSource {
			s, err := c.Config.SourceOfValue(k + keyStr)
			if err != nil {
				return err
			}
			source = " (" + s + ")"
		}

		var link string
		if hasK && strings.HasPrefix(k, "repositories") {
			link = "https://getcomposer.org/doc/05-repositories.md"
		} else {
			id := k
			if k == "" {
				id = keyStr
			}
			var err error
			if id, _, err = dotSuffixPattern.Replace(id, "", -1); err != nil {
				return err
			}
			if id, _, err = nonAlnumPattern.Replace(php.Strtolower(php.Trim(id)), "-", -1); err != nil {
				return err
			}
			if id, _, err = dashesPattern.Replace(id, "-", -1); err != nil {
				return err
			}
			link = "https://getcomposer.org/doc/06-config.md#" + id
		}
		if raw, ok := rawVal.(string); ok && (!isString || raw != text) {
			out.Write("[<fg=yellow;href="+link+">"+k+keyStr+"</>] <info>"+raw+" ("+text+")</info>"+source, true, io.Quiet)
		} else {
			out.Write("[<fg=yellow;href="+link+">"+k+keyStr+"</>] <info>"+text+"</info>"+source, true, io.Quiet)
		}
	}
	return nil
}

// suggestSettingKeys ports suggestSettingKeys.
func (c *ConfigCommand) suggestSettingKeys() console.SuggestFunc {
	return func(input *console.CompletionInput, _ *console.CompletionSuggestions) []console.Suggestion {
		if console.BoolOption(input, "list") || console.BoolOption(input, "editor") || console.BoolOption(input, "auth") {
			return nil
		}

		// initialize configuration
		cfg := must(c.factory().CreateConfig(io.NewNullIO(), ""))

		// load configuration
		configFile := must(json.NewFile(must(c.ComposerConfigFile(input, cfg)), nil, nil))
		if configFile.Exists() {
			must(0, cfg.Merge(must(readArray(configFile)), configFile.Path()))
		}

		// load auth-configuration
		authConfigFile := must(json.NewFile(must(c.AuthConfigFile(input, cfg)), nil, nil))
		if authConfigFile.Exists() {
			must(0, cfg.Merge(php.ArrayOf("config", must(readArray(authConfigFile))), authConfigFile.Path()))
		}

		// collect all configuration setting-keys
		rawConfig := cfg.Raw()
		rawCfg, _ := rawConfig.GetArray("config")
		rawRepos, _ := rawConfig.GetArray("repositories")
		keys := append(flattenSettingKeys(rawCfg, ""), flattenSettingKeys(rawRepos, "repositories.")...)

		// if unsetting …
		if console.BoolOption(input, "unset") {
			// … keep only the currently customized setting-keys …
			sources := []string{configFile.Path(), authConfigFile.Path()}
			keys = slices.DeleteFunc(keys, func(key string) bool {
				return !slices.Contains(sources, must(cfg.SourceOfValue(key)))
			})

			// … else if showing or setting a value …
		} else {
			// … add all configurable package-properties, no matter if it exist
			keys = append(keys, configurablePackageProperties...)
		}

		// add all existing configurable package-properties
		if configFile.Exists() {
			properties := php.NewArray()
			for k, v := range must(readArray(configFile)).All() {
				if slices.Contains(configurablePackageProperties, k.String()) {
					properties.SetKey(k, v)
				}
			}

			keys = append(keys, flattenSettingKeys(properties, "")...)
		}

		// filter settings-keys by completion value
		if completionValue := input.CompletionValue(); completionValue != "" {
			keys = slices.DeleteFunc(keys, func(key string) bool { return !strings.HasPrefix(key, completionValue) })
		}

		php.SortSlice(keys, func(a, b string) int { return php.Compare(a, b) })

		return suggestStrings(slices.Compact(keys))
	}
}

// flattenSettingKeys ports flattenSettingKeys.
func flattenSettingKeys(cfg *php.Array, prefix string) []string {
	keys := []string{}
	if cfg == nil {
		return keys
	}
	for key, value := range cfg.All() {
		keys = append(keys, prefix+key.String())
		// array-lists must not be added to completion
		// sub-keys of repository-keys must not be added to completion
		if a, ok := value.(*php.Array); ok && !a.IsList() && prefix != "repositories." {
			keys = append(keys, flattenSettingKeys(a, prefix+key.String()+".")...)
		}
	}

	return keys
}

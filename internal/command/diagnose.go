// Ports src/Composer/Command/DiagnoseCommand.php.

package command

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/repository/composerrepo"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/ui"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
	"github.com/stubbedev/maestro/internal/util/vcs"
)

func init() {
	registerCommand(OrderDiagnose, func() console.Commander { return NewDiagnoseCommand() })
}

// DiagnoseCommand is Composer\Command\DiagnoseCommand.
//
// The PHP checks answer for the PHP Composer would run on (the process
// runtime's ComposerView). Divergences, all documented here:
//   - "Checking pubkeys" and "Checking Composer version" only run in
//     Composer's phar build (strpos(__FILE__, 'phar:') === 0), the build
//     self-update can update. maestro updates itself from its own GitHub
//     releases (PORTING.md deviation 4), so it runs the version check in a
//     release build that has a self-update (releaseBuild) instead, and
//     skips the pubkeys check everywhere, as Composer run from source does.
//     checkPubKeys reports whether keys.tags.pub and keys.dev.pub, which
//     self-update needs to verify a phar's signature, are in COMPOSER_HOME;
//     maestro verifies a download against the checksums.txt of its
//     release, which self-update fetches with it, so there is no local key
//     material that could be missing and the line could only say OK.
//   - "Checking Composer version" compares maestro's version with its
//     latest release on the update channel, found as self-update finds it
//     (releaseSource), and warns when self-update would install another
//     version (checkVersion).
//   - "Checking Composer and its dependencies for vulnerabilities" audits
//     Composer's own installed.json; maestro bundles no PHP dependencies,
//     so it audits composer/composer at Composer::getVersion() only.
//   - ioncube_loader_version() is derived from the probed
//     ioncube_loader_iversion().
//   - Without a php on PATH (maestro runs without one; PHP code needs it)
//     "Checking PHP" fails in place of the lines describing the PHP and
//     of "Checking platform settings", and allow_url_fopen, which only
//     gates PHP's own streams, skips no network check.
type DiagnoseCommand struct {
	*BaseCommand

	// APIBase, Executable and CurrentVersion are self-update's test
	// hooks, for the version check.
	APIBase        string
	Executable     func() (string, error)
	CurrentVersion string

	httpDownloader *http.HttpDownloader
	process        *util.ProcessExecutor
	exitCode       int
	view           *platform.Snapshot
	noPHP          bool
	// decorated is whether stdout is decorated: then the report is a
	// check list (ui.CheckList), else Composer's lines.
	decorated bool
	// current is the label of the check running.
	current string
}

// diagnoseList is the look of diagnose's decorated report.
var diagnoseList = ui.CheckList{LabelWidth: 34}

// plainText is Composer's markup as the text it shows, for the check
// list, which styles it itself.
func plainText(markup string) string {
	return console.NewOutputFormatter(false, console.ThemeStyles()...).Format(markup)
}

// diagnoseMaestroVersion is the running maestro's version, "dev" for a
// build without one, so a report says which tool it diagnosed.
func diagnoseMaestroVersion(rt *composer.Runtime) string {
	if v := rt.ClientVersion(); v != "" {
		return v
	}

	return "dev"
}

// writeLines writes lines of the check list, unformatted.
func (c *DiagnoseCommand) writeLines(lines ...string) {
	for _, l := range lines {
		c.IO().WriteRaw(l, true, io.Normal)
	}
}

// fact writes what diagnose found out: "label: value" (value is markup),
// a line of the check list when decorated.
func (c *DiagnoseCommand) fact(label, value string) {
	if c.decorated {
		c.writeLines(diagnoseList.Fact(label, plainText(value)))

		return
	}
	c.IO().Write(label+": "+value, true, io.Normal)
}

// checking starts the check labelled label: undecorated, Composer's
// "Checking label: ", which its result completes; decorated, its line is
// written once it has a result.
func (c *DiagnoseCommand) checking(label string) {
	c.current = label
	if !c.decorated {
		c.IO().Write("Checking "+label+": ", false, io.Normal)
	}
}

// writeResult writes the result of the current check: Composer's status
// word (none for a passed check that says what it found) and messages
// (markup), or the check list's lines.
func (c *DiagnoseCommand) writeResult(status ui.CheckStatus, messages []string) {
	if c.decorated {
		plain := make([]string, len(messages))
		for i, m := range messages {
			plain[i] = php.Trim(plainText(m))
		}
		c.writeLines(diagnoseList.Check(status, c.current, plain)...)

		return
	}
	cio := c.IO()
	switch {
	case status == ui.CheckFailed:
		cio.Write("<error>FAIL</error>", true, io.Normal)
	case status == ui.CheckWarning:
		cio.Write("<warning>WARNING</warning>", true, io.Normal)
	case len(messages) == 0:
		cio.Write("<info>OK</info>", true, io.Normal)
	}
	for _, message := range messages {
		cio.Write(php.Trim(message), true, io.Normal)
	}
}

// NewDiagnoseCommand ports new DiagnoseCommand().
func NewDiagnoseCommand() *DiagnoseCommand {
	c := &DiagnoseCommand{BaseCommand: NewBaseCommand(""), APIBase: githubAPI, Executable: currentExecutable}
	c.SetImpl(c)
	c.SetName("diagnose").
		SetDescription("Diagnoses the system to identify common errors").
		SetHelp(`The <info>diagnose</info> command checks common errors to help debugging problems.

The process exit code will be 1 in case of warnings and 2 for errors.

Read more at https://getcomposer.org/doc/03-cli.md#diagnose`)

	return c
}

// PHPClass implements php.Classer.
func (*DiagnoseCommand) PHPClass() string { return `Composer\Command\DiagnoseCommand` }

func (c *DiagnoseCommand) runtime() *composer.Runtime {
	if app := c.application(); app != nil {
		return app.Runtime()
	}

	return c.factory().Runtime
}

// extensionLoaded is extension_loaded($name) of the PHP Composer runs on.
func (c *DiagnoseCommand) extensionLoaded(name string) bool {
	if c.view == nil {
		return false
	}
	for _, e := range c.view.Extensions {
		if php.Strcasecmp(e.Name, name) == 0 {
			return true
		}
	}

	return false
}

// functionExists is function_exists($name).
func (c *DiagnoseCommand) functionExists(name string) bool {
	if c.view == nil {
		return false
	}

	return platform.NewRuntime(c.view).HasFunction(name)
}

// constant is constant($name); ok is false when it is not defined.
func (c *DiagnoseCommand) constant(name string) (any, bool) {
	if c.view == nil {
		return nil, false
	}

	return c.view.Constant(name)
}

// iniGet is ini_get($name) ("" for false).
func (c *DiagnoseCommand) iniGet(name string) string {
	if c.view == nil {
		return ""
	}
	v, _ := c.view.IniGet(name)

	return v
}

// filterBool is filter_var($v, FILTER_VALIDATE_BOOLEAN).
func filterBool(v string) bool {
	switch php.Strtolower(php.Trim(v)) {
	case "1", "true", "on", "yes":
		return true
	}

	return false
}

// Execute implements console.Executor.
func (c *DiagnoseCommand) Execute(in console.Input, out console.Output) (int, error) {
	composerInst, err := c.TryComposer(nil, nil)
	if err != nil {
		return 0, err
	}
	cio := c.IO()
	c.decorated = cio.IsDecorated()
	rt := c.runtime()
	view, _, verr := rt.ComposerView()
	switch {
	case verr == nil:
		c.view = view
	case errors.Is(verr, platform.ErrPHPNotFound):
		c.noPHP = true
	}

	var cfg *config.Config
	if composerInst != nil {
		cfg = composerInst.Config()

		commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "diagnose", in, out, nil, nil)
		if _, err := composerInst.EventDispatcher().Dispatch(commandEvent.Name(), commandEvent); err != nil {
			return 0, err
		}
		if loop := composerInst.Loop(); loop != nil && loop.ProcessExecutor() != nil {
			c.process = loop.ProcessExecutor()
		} else {
			c.process = util.NewProcessExecutor(cio)
		}
	} else {
		if cfg, err = c.factory().CreateConfig(io.NewNullIO(), ""); err != nil {
			return 0, err
		}

		c.process = util.NewProcessExecutor(cio)
	}

	if err := cfg.Merge(php.ArrayOf("config", php.ArrayOf("secure-http", false)), config.SourceCommand); err != nil {
		return 0, err
	}
	if err := cfg.ProhibitURLByConfig("http://repo.packagist.org", io.NewNullIO(), nil); err != nil {
		return 0, err
	}

	if c.httpDownloader, err = c.factory().CreateHttpDownloader(cio, cfg, nil); err != nil {
		return 0, err
	}

	// if (strpos(__FILE__, 'phar:') === 0): a release build of maestro;
	// the pubkeys check is skipped (see the type's comment).
	if current, ok := c.releaseBuild(); ok {
		c.checking("Composer version")
		res, err := c.checkVersion(cfg, current)
		if err != nil {
			return 0, err
		}
		c.outputResult(res)
	}

	c.fact("Composer version", "<comment>"+composer.GetVersion()+"</comment>")
	c.fact("Maestro version", "<comment>"+diagnoseMaestroVersion(rt)+"</comment>")

	c.checking("Composer and its dependencies for vulnerabilities")
	c.outputResult(c.checkComposerAudit(cfg))

	if c.noPHP {
		c.checking("PHP")
		c.outputResult("<error>No php binary was found in PATH. maestro runs PHP code (platform detection, plugins, scripts) with the php first on PATH: install PHP or put it on PATH.</error>")
	} else if err := c.writePHP(cfg, rt); err != nil {
		return 0, err
	}

	finder := util.NewExecutableFinder()
	_, hasSystemUnzip := finder.Find("unzip")
	bin7zip := ""
	hasSystem7zip := false
	if _, hasSystem7zip = finder.Find("7z", `C:\Program Files\7-Zip`); hasSystem7zip {
		bin7zip = "7z"
	} else if !util.IsWindows() {
		if _, hasSystem7zip = finder.Find("7zz"); hasSystem7zip {
			bin7zip = "7zz"
		} else if _, hasSystem7zip = finder.Find("7za"); hasSystem7zip {
			bin7zip = "7za"
		}
	}

	zip := ""
	if c.extensionLoaded("zip") {
		zip += "<comment>extension present</comment>"
	} else {
		zip += "<comment>extension not loaded</comment>"
	}
	if hasSystemUnzip {
		zip += ", <comment>unzip present</comment>"
	} else {
		zip += ", <comment>unzip not available</comment>"
	}
	if hasSystem7zip {
		zip += ", <comment>7-Zip present (" + bin7zip + ")</comment>"
	} else {
		zip += ", <comment>7-Zip not available</comment>"
	}
	if (hasSystem7zip || hasSystemUnzip) && !c.functionExists("proc_open") {
		zip += ", <warning>proc_open is disabled or not present, unzip/7-z will not be usable</warning>"
	}
	c.fact("zip", zip)

	if composerInst != nil {
		var plugins []string
		if r, ok := composerInst.PluginManager().(interface{ RegisteredPlugins() []string }); ok {
			plugins = r.RegisteredPlugins()
		}
		c.fact("Active plugins", strings.Join(plugins, ", "))

		c.checking("composer.json")
		res, err := c.checkComposerSchema()
		if err != nil {
			return 0, err
		}
		c.outputResult(res)

		l := composerInst.Locker()
		locked, err := l.IsLocked()
		if err != nil {
			return 0, err
		}
		if locked {
			c.checking("composer.lock")
			res, err := c.checkComposerLockSchema(l)
			if err != nil {
				return 0, err
			}
			c.outputResult(res)
		}
	}

	if !c.noPHP {
		c.checking("platform settings")
		c.outputResult(c.checkPlatform())
	}

	c.checking("git settings")
	gitResult, err := c.checkGit()
	if err != nil {
		return 0, err
	}
	c.outputResult(gitResult)

	c.checking("http connectivity to packagist")
	c.outputResult(c.checkHTTP("http", cfg))

	c.checking("https connectivity to packagist")
	c.outputResult(c.checkHTTP("https", cfg))

	if repos := cfg.Repositories(); repos != nil {
		for _, r := range repos.All() {
			repo, ok := r.(*php.Array)
			if !ok {
				continue
			}
			typ, _ := repo.Get("type")
			urlVal, hasURL := repo.Get("url")
			if typ != "composer" || !hasURL || urlVal == nil {
				continue
			}
			repoURL := php.ToString(urlVal)
			if _, err := composerrepo.New(repo, cio, cfg, c.httpDownloader, nil); err != nil {
				return 0, err
			}
			u := diagnosePackagesJSONURL(repoURL)
			if !strings.HasPrefix(u, "http") {
				continue
			}
			if strings.HasPrefix(u, "https://repo.packagist.org") {
				continue
			}
			c.checking("connectivity to " + repoURL)
			c.outputResult(c.checkComposerRepo(u, cfg))
		}
	}

	if err := c.checkProxies(cfg); err != nil {
		return 0, err
	}

	oauthVal, err := cfg.Get("github-oauth", 0)
	if err != nil {
		return 0, err
	}
	oauth, _ := oauthVal.(*php.Array)
	if oauth != nil && oauth.Len() > 0 {
		for domain, token := range oauth.All() {
			c.checking(domain.String() + " oauth access")
			c.outputResult(c.checkGithubOauth(domain.String(), php.ToString(token)))
		}
	} else {
		c.checking("github.com rate limit")
		rate, err := c.githubRateLimit("github.com")
		switch {
		case err != nil:
			if te, ok := errors.AsType[*util.TransportError](err); ok && te.Code == 401 {
				c.outputResult(`<comment>The oauth token for github.com seems invalid, run "composer config --global --unset github-oauth.github.com" to remove it</comment>`)
			} else {
				c.outputResult(err)
			}
		default:
			if a, ok := rate.(*php.Array); !ok {
				c.outputResult(rate)
			} else if remaining, _ := a.Get("remaining"); php.Compare(int64(10), remaining) > 0 {
				limit, _ := a.Get("limit")
				// a warning that leaves the exit code alone
				c.writeResult(ui.CheckWarning, []string{"<comment>GitHub has a rate limit on their API. " +
					"You currently have <options=bold>" + phpSprintfU(php.ToInt(remaining)) + "</options=bold> " +
					"out of <options=bold>" + phpSprintfU(php.ToInt(limit)) + "</options=bold> requests left." + php.EOL +
					"See https://developer.github.com/v3/#rate-limiting and also" + php.EOL +
					"    https://getcomposer.org/doc/articles/troubleshooting.md#api-rate-limit-and-oauth-tokens</comment>"})
			} else {
				c.outputResult(true)
			}
		}
	}

	c.checking("disk free space")
	disk, err := c.checkDiskSpace(cfg)
	if err != nil {
		return 0, err
	}
	c.outputResult(disk)

	return c.exitCode, nil
}

// writePHP writes the lines describing the PHP Composer runs on: its
// version, binary, OpenSSL and curl.
func (c *DiagnoseCommand) writePHP(cfg *config.Config, rt *composer.Runtime) error {
	platformOverrides, err := cfg.Get("platform", 0)
	if err != nil {
		return err
	}
	overrides, _ := platformOverrides.(*php.Array)
	opts, err := rt.PlatformOptions(c.process)
	if err != nil {
		return err
	}
	platformRepo, err := repository.NewPlatformRepository(nil, overrides, opts)
	if err != nil {
		return err
	}
	phpPkg, err := platformRepo.FindPackage("php", nil)
	if err != nil {
		return err
	}
	phpVersion := ""
	if phpPkg != nil {
		phpVersion = phpPkg.PrettyVersion()
		if cp, ok := phpPkg.(pkg.CompletePackageInterface); ok && strings.Contains(cp.Description().S, "overridden") {
			phpVersion += " - " + cp.Description().S
		}
	}

	c.fact("PHP version", "<comment>"+phpVersion+"</comment>")

	if v, ok := c.constant("PHP_BINARY"); ok {
		c.fact("PHP binary path", "<comment>"+php.ToString(v)+"</comment>")
	}

	openssl := "<error>missing</error>"
	if v, ok := c.constant("OPENSSL_VERSION_TEXT"); ok {
		openssl = "<comment>" + php.ToString(v) + "</comment>"
	}
	c.fact("OpenSSL version", openssl)
	c.fact("curl version", c.curlVersion())

	return nil
}

// releaseBuild is the running maestro's version when it is a release
// build that self-update can update. Its version must parse, as
// checkVersion skips the comparison for an unreplaced
// '@package_version@'.
func (c *DiagnoseCommand) releaseBuild() (string, bool) {
	current, _, ok := runningMaestro(c.application(), c.CurrentVersion, c.Executable)
	if !ok {
		return "", false
	}
	if _, err := pkg.NewVersionParser().Normalize(current); err != nil {
		return "", false
	}

	return current, true
}

// checkVersion ports checkVersion against maestro's releases: Versions
// ::getLatest() is the latest release of the channel self-update reads,
// and the result warns when self-update would install another version
// (Composer compares with !==; maestro's stable channel, like its
// self-update, does not offer an older release than the running one).
func (c *DiagnoseCommand) checkVersion(cfg *config.Config, current string) (any, error) {
	if result := c.checkConnectivityAndComposerNetworkHTTPEnablement(); result != true {
		return result, nil
	}

	home, err := cfg.Get("home", 0)
	if err != nil {
		return nil, err
	}
	channel := readChannel(php.ToString(home))
	latest, err := releaseSource(c.APIBase).latest(c.httpDownloader, channel)
	if err != nil {
		return err, nil
	}

	if latest.version != current && (channel != "stable" || semver.VersionCompare(latest.version, current) > 0) {
		return "<comment>You are not running the latest " + channel + " version, run `composer self-update` to update (" + current + " => " + latest.version + ")</comment>", nil
	}

	return true, nil
}

// diagnosePackagesJSONURL ports ComposerRepository::getPackagesJsonUrl.
func diagnosePackagesJSONURL(u string) string {
	if path := util.URLPath(php.Strtr(u, `\`, "/")); strings.Contains(path, ".json") {
		return u
	}

	return u + "/packages.json"
}

// checkProxies is the proxy block of execute().
func (c *DiagnoseCommand) checkProxies(cfg *config.Config) error {
	proxyManager := http.GetProxyManager()
	disableTLS, err := cfg.Get("disable-tls", 0)
	if err != nil {
		return err
	}
	protos := []string{"http", "https"}
	if disableTLS == true {
		protos = []string{"http"}
	}
	for _, proto := range protos {
		proxy, err := proxyManager.ProxyForRequest(proto + "://repo.packagist.org")
		if err != nil {
			if _, ok := errors.AsType[*util.TransportError](err); !ok {
				return err
			}
			c.checking("HTTP proxy")
			if status := c.checkConnectivityAndComposerNetworkHTTPEnablement(); status != true {
				c.outputResult(status)
			} else {
				c.outputResult(err)
			}

			return nil
		}
		if proxy.Status() != "" {
			typ := "HTTP"
			if proxy.IsSecure() {
				typ = "HTTPS"
			}
			c.checking(typ + " proxy with " + proto)
			c.outputResult(c.checkHTTPProxy(proxy, proto))
		}
	}

	return nil
}

func (c *DiagnoseCommand) checkComposerSchema() (any, error) {
	validator := NewConfigValidator(c.IO())
	file, err := composerFile()
	if err != nil {
		return nil, err
	}
	errs, _, warnings, err := validator.Validate(file, loader.CheckAll, ConfigValidatorCheckVersion)
	if err != nil {
		return nil, err
	}

	if len(errs) > 0 || len(warnings) > 0 {
		var output strings.Builder
		for _, m := range []struct {
			style string
			msgs  []string
		}{{"error", errs}, {"warning", warnings}} {
			for _, msg := range m.msgs {
				output.WriteString("<" + m.style + ">" + msg + "</" + m.style + ">" + php.EOL)
			}
		}

		return strings.TrimRight(output.String(), php.TrimChars), nil
	}

	return true, nil
}

func (c *DiagnoseCommand) checkComposerLockSchema(l *locker.Locker) (any, error) {
	jf, ok := l.JSONFile().(*json.File)
	if !ok {
		return true, nil
	}

	if err := jf.ValidateSchema(json.LockSchema, ""); err != nil {
		ve, ok := errors.AsType[*json.ValidationError](err)
		if !ok {
			return nil, err
		}
		var output strings.Builder
		for _, e := range ve.Errors {
			output.WriteString("<error>" + e + "</error>" + php.EOL)
		}

		return php.Trim(output.String()), nil
	}

	return true, nil
}

func (c *DiagnoseCommand) checkGit() (any, error) {
	if !c.functionExists("proc_open") {
		return "<comment>proc_open is not available, git cannot be used</comment>", nil
	}

	var output string
	_, _ = c.process.Execute(util.Cmd("git", "config", "color.ui"), &output, "")
	if php.Strtolower(php.Trim(output)) == "always" {
		return `<comment>Your git color.ui setting is set to always, this is known to create issues. Use "git config --global color.ui true" to set it correctly.</comment>`, nil
	}

	gitVersion, ok, err := vcs.GetVersion(c.process)
	if err != nil {
		return nil, err
	}
	if !ok {
		return "<comment>No git process found</>", nil
	}

	if semver.VersionCompare("2.24.0", gitVersion) > 0 {
		return "<warning>Your git version (" + gitVersion + ") is too old and possibly will cause issues. Please upgrade to git 2.24 or above</>", nil
	}

	return "<info>OK</> <comment>git version " + gitVersion + "</>", nil
}

const tlsWarning = "<warning>Composer is configured to disable SSL/TLS protection. This will leave remote HTTPS requests vulnerable to Man-In-The-Middle attacks.</warning>"

// transportResult is the catch (TransportException) block of checkHttp
// and checkComposerRepo; other errors are returned as the result (PHP
// lets them escape, Application renders them; here they become a FAIL).
func transportResult(err error) []string {
	var result []string
	result = append(result, http.GetExceptionHints(err)...)
	class, _ := phperr.ClassOf(err)

	return append(result, "<error>["+class+"] "+err.Error()+"</error>")
}

func (c *DiagnoseCommand) checkHTTP(proto string, cfg *config.Config) any {
	return c.checkURL(proto+"://repo.packagist.org/packages.json", proto == "https", cfg)
}

func (c *DiagnoseCommand) checkComposerRepo(u string, cfg *config.Config) any {
	return c.checkURL(u, strings.HasPrefix(u, "https://"), cfg)
}

func (c *DiagnoseCommand) checkURL(u string, secure bool, cfg *config.Config) any {
	if result := c.checkConnectivityAndComposerNetworkHTTPEnablement(); result != true {
		return result
	}

	var result []string
	warnTLS := false
	if secure {
		if v, _ := cfg.Get("disable-tls", 0); v == true {
			warnTLS = true
		}
	}

	if _, err := c.httpDownloader.Get(u, nil); err != nil {
		result = append(result, transportResult(err)...)
	}

	if warnTLS {
		result = append(result, tlsWarning)
	}

	if len(result) > 0 {
		return result
	}

	return true
}

func (c *DiagnoseCommand) checkHTTPProxy(proxy *http.RequestProxy, protocol string) any {
	if result := c.checkConnectivityAndComposerNetworkHTTPEnablement(); result != true {
		return result
	}

	proxyStatus := proxy.Status()

	if proxy.IsExcludedByNoProxy() {
		return "<info>SKIP</> <comment>Because repo.packagist.org is " + proxyStatus + "</>"
	}

	resp, err := c.httpDownloader.Get(protocol+"://repo.packagist.org/packages.json", nil)
	if err != nil {
		return err
	}
	data, err := resp.DecodeJSON()
	if err != nil {
		return err
	}
	if a, ok := data.(*php.Array); ok {
		if includes, ok := a.GetArray("provider-includes"); ok {
			key, first, _ := includes.First()
			hash := ""
			if fa, ok := first.(*php.Array); ok {
				h, _ := fa.Get("sha256")
				hash = php.ToString(h)
			}
			path := strings.ReplaceAll(key.String(), "%hash%", hash)
			provider, err := c.httpDownloader.Get(protocol+"://repo.packagist.org/"+path, nil)
			if err != nil {
				return err
			}

			sum := sha256.Sum256([]byte(provider.Body()))
			if hex.EncodeToString(sum[:]) != hash {
				return "<warning>It seems that your proxy (" + proxyStatus + ") is modifying " + protocol + " traffic on the fly</>"
			}
		}
	}

	return "<info>OK</> <comment>" + proxyStatus + "</>"
}

func (c *DiagnoseCommand) checkGithubOauth(domain, token string) any {
	if result := c.checkConnectivityAndComposerNetworkHTTPEnablement(); result != true {
		return result
	}

	c.IO().SetAuthentication(domain, token, new("x-oauth-basic"))
	u := "https://" + domain + "/api/v3/"
	if domain == "github.com" {
		u = "https://api." + domain + "/"
	}

	response, err := c.httpDownloader.Get(u, php.ArrayOf("retry-auth-failure", false))
	if err != nil {
		if te, ok := errors.AsType[*util.TransportError](err); ok && te.Code == 401 {
			return "<comment>The oauth token for " + domain + ` seems invalid, run "composer config --global --unset github-oauth.` + domain + `" to remove it</comment>`
		}

		return err
	}

	expiration, ok, err := response.HeaderChecked("github-authentication-token-expiration")
	if err != nil {
		return err
	}
	if !ok {
		return "<info>OK</> <comment>does not expire</>"
	}

	return "<info>OK</> <comment>expires on " + expiration + "</>"
}

// githubRateLimit ports getGithubRateLimit: the "core" rate limit array,
// or the connectivity skip message.
func (c *DiagnoseCommand) githubRateLimit(domain string) (any, error) {
	if result := c.checkConnectivityAndComposerNetworkHTTPEnablement(); result != true {
		return result, nil
	}

	u := "https://" + domain + "/api/rate_limit"
	if domain == "github.com" {
		u = "https://api." + domain + "/rate_limit"
	}
	resp, err := c.httpDownloader.Get(u, php.ArrayOf("retry-auth-failure", false))
	if err != nil {
		return nil, err
	}
	data, err := resp.DecodeJSON()
	if err != nil {
		return nil, err
	}
	a, _ := data.(*php.Array)
	if a == nil {
		return nil, nil
	}
	resources, _ := a.GetArray("resources")
	if resources == nil {
		return nil, nil
	}
	core, _ := resources.Get("core")

	return core, nil
}

func (c *DiagnoseCommand) checkDiskSpace(cfg *config.Config) (any, error) {
	const minSpaceFree = 1024 * 1024
	for _, key := range []string{"home", "vendor-dir"} {
		v, err := cfg.Get(key, 0)
		if err != nil {
			return nil, err
		}
		dir := php.ToString(v)
		if df, ok := diskFreeSpace(dir); ok && df < minSpaceFree {
			return "<error>The disk hosting " + dir + " is full</error>", nil
		}
	}

	return true, nil
}

func (c *DiagnoseCommand) checkComposerAudit(cfg *config.Config) any {
	if result := c.checkConnectivityAndComposerNetworkHTTPEnablement(); result != true {
		return result
	}

	version := composer.GetVersion()
	var packages []pkg.PackageInterface
	if version != "@package_version@" {
		normalized, err := pkg.NewVersionParser().Normalize(version)
		if err != nil {
			return "<highlight>Failed performing audit: " + err.Error() + "</>"
		}
		packages = append(packages, pkg.NewRootPackage("composer/composer", normalized, version))
	}

	result, auditOutput, err := c.runComposerAudit(cfg, packages)
	if err != nil {
		return "<highlight>Failed performing audit: " + err.Error() + "</>"
	}

	if result > 0 {
		return "<highlight>Audit found some issues:</>" + php.EOL + auditOutput
	}

	return true
}

func (c *DiagnoseCommand) runComposerAudit(cfg *config.Config, packages []pkg.PackageInterface) (int, string, error) {
	repoSet, err := repository.NewRepositorySet("stable", nil, nil, nil, nil, nil)
	if err != nil {
		return 0, "", err
	}
	repo, err := composerrepo.New(php.ArrayOf("type", "composer", "url", "https://packagist.org"), io.NewNullIO(), cfg, c.httpDownloader, nil)
	if err != nil {
		return 0, "", err
	}
	if err := repoSet.AddRepository(repo); err != nil {
		return 0, "", err
	}
	policyConfig, err := c.CreatePolicyConfig(cfg, nil)
	if err != nil {
		return 0, "", err
	}
	// $policyConfig->withAudit(ListPolicyConfig::AUDIT_IGNORE)
	withAudit := *policyConfig
	withAudit.Abandoned = policyConfig.Abandoned.WithAudit(policy.AuditIgnore)

	bio, err := io.NewBufferIO("", 0, nil)
	if err != nil {
		return 0, "", err
	}
	result, err := advisory.Auditor{}.Audit(bio, repoSet, &withAudit, packages, advisory.FormatTable, false, nil)
	if err != nil {
		return 0, "", err
	}

	return result, bio.Output(), nil
}

func (c *DiagnoseCommand) curlVersion() string {
	if !c.extensionLoaded("curl") {
		return "<error>missing, using php streams fallback, which reduces performance</error>"
	}
	if !c.functionExists("curl_multi_exec") || !c.functionExists("curl_multi_init") {
		return "<error>disabled via disable_functions, using php streams fallback, which reduces performance</error>"
	}

	v, err := platform.NewRuntime(c.view).Invoke(platform.Func("curl_version"))
	version, _ := v.(*php.Array)
	if err != nil || version == nil {
		version = php.NewArray()
	}
	str := func(key string) string {
		if s, ok := version.Get(key); ok && s != nil && php.ToString(s) != "" {
			return php.ToString(s)
		}

		return "missing"
	}
	featuresVal, hasFeatures := version.Get("features")
	hasFeatures = hasFeatures && featuresVal != nil
	features := php.ToInt(featuresVal)
	constInt := func(name string) (int64, bool) {
		cv, ok := c.constant(name)

		return php.ToInt(cv), ok
	}
	zstd, zstdOK := constInt("CURL_VERSION_ZSTD")
	hasZstd := hasFeatures && zstdOK && features&zstd != 0
	httpVersions := "1.0, 1.1"
	if http2, ok := constInt("CURL_VERSION_HTTP2"); hasFeatures && ok {
		if _, ok2 := c.constant("CURL_HTTP_VERSION_2_0"); ok2 && http2&features != 0 {
			httpVersions += ", 2"
		}
	}
	if http3, ok := constInt("CURL_VERSION_HTTP3"); hasFeatures && ok && features&http3 != 0 {
		httpVersions += ", 3"
	}
	zstdStr := "missing"
	if hasZstd {
		zstdStr = "supported"
	}
	ver, _ := version.Get("version")

	return "<comment>" + php.ToString(ver) + "</comment> " +
		"libz <comment>" + str("libz_version") + "</comment> " +
		"brotli <comment>" + str("brotli_version") + "</comment> " +
		"zstd <comment>" + zstdStr + "</comment> " +
		"ssl <comment>" + str("ssl_version") + "</comment> " +
		"HTTP <comment>" + httpVersions + "</comment>"
}

// outputResult ports outputResult: result is true, a string, a []string
// or an error (an \Exception).
func (c *DiagnoseCommand) outputResult(result any) {
	if result == true {
		c.writeResult(ui.CheckOK, nil)

		return
	}

	hadError := false
	hadWarning := false
	if err, ok := result.(error); ok {
		class, _ := phperr.ClassOf(err)
		result = "<error>[" + class + "] " + err.Error() + "</error>"
	}

	var messages []string
	switch r := result.(type) {
	case string:
		if php.Truthy(r) {
			messages = []string{r}
		}
	case []string:
		messages = r
	}

	if len(messages) == 0 {
		// falsey results should be considered as an error, even if there is nothing to output
		hadError = true
	} else {
		for _, message := range messages {
			if strings.Contains(message, "<error>") {
				hadError = true
			} else if strings.Contains(message, "<warning>") {
				hadWarning = true
			}
		}
	}

	status := ui.CheckOK
	if hadError {
		status = ui.CheckFailed
		c.exitCode = max(c.exitCode, 2)
	} else if hadWarning {
		status = ui.CheckWarning
		c.exitCode = max(c.exitCode, 1)
	}
	c.writeResult(status, messages)
}

// checkPlatform ports checkPlatform (code taken from
// getcomposer.org/installer).
func (c *DiagnoseCommand) checkPlatform() any {
	var output strings.Builder
	out := func(msg, style string) {
		output.WriteString("<" + style + ">" + msg + "</" + style + ">" + php.EOL)
	}

	type issue struct {
		name    string
		current string
	}
	var errs, warnings []issue
	displayIniMessage := false

	iniFiles := func() []string { return nil }
	if c.view != nil {
		iniFiles = c.view.IniFiles
	}
	iniMessage := php.EOL + php.EOL + util.IniGetMessage(iniFiles)
	iniMessage += php.EOL + "If you can not modify the ini file, you can also run `php -d option=value` to modify ini values on the fly. You can use -d multiple times."

	if !c.functionExists("json_decode") {
		errs = append(errs, issue{name: "json"})
	}
	if !c.extensionLoaded("Phar") {
		errs = append(errs, issue{name: "phar"})
	}
	if !c.extensionLoaded("filter") {
		errs = append(errs, issue{name: "filter"})
	}
	if !c.extensionLoaded("hash") {
		errs = append(errs, issue{name: "hash"})
	}
	if !c.extensionLoaded("iconv") && !c.extensionLoaded("mbstring") {
		errs = append(errs, issue{name: "iconv_mbstring"})
	}
	if !filterBool(c.iniGet("allow_url_fopen")) {
		errs = append(errs, issue{name: "allow_url_fopen"})
	}
	if c.extensionLoaded("ionCube Loader") && c.view != nil {
		if v, err := platform.NewRuntime(c.view).Invoke(platform.Func("ioncube_loader_iversion")); err == nil {
			if iv := php.ToInt(v); iv < 40009 {
				errs = append(errs, issue{name: "ioncube", current: strconv.FormatInt(iv/10000, 10) + "." + strconv.FormatInt(iv/100%100, 10) + "." + strconv.FormatInt(iv%100, 10)})
			}
		}
	}
	if c.view != nil && c.view.VersionID < 70205 {
		errs = append(errs, issue{name: "php", current: c.view.Version})
	}
	if !c.extensionLoaded("openssl") {
		errs = append(errs, issue{name: "openssl"})
	}
	if c.extensionLoaded("openssl") {
		if v, ok := c.constant("OPENSSL_VERSION_NUMBER"); ok && php.ToInt(v) < 0x1000100f {
			warnings = append(warnings, issue{name: "openssl_version"})
		}
	}
	if _, hhvm := c.constant("HHVM_VERSION"); !hhvm && !c.extensionLoaded("apcu") && filterBool(c.iniGet("apc.enable_cli")) {
		warnings = append(warnings, issue{name: "apc_cli"})
	}
	if !c.extensionLoaded("zlib") {
		warnings = append(warnings, issue{name: "zlib"})
	}

	// ob_start(); phpinfo(INFO_GENERAL); matched by the platform probe
	// (Snapshot.ConfigureCommand).
	if c.view != nil {
		if configure, ok := c.view.ConfigureCommand(); ok {
			if strings.Contains(configure, "--enable-sigchild") {
				warnings = append(warnings, issue{name: "sigchild"})
			}

			if strings.Contains(configure, "--with-curlwrappers") {
				warnings = append(warnings, issue{name: "curlwrappers"})
			}
		}
	}

	if filterBool(c.iniGet("xdebug.profiler_enabled")) {
		warnings = append(warnings, issue{name: "xdebug_profile"})
	} else if c.view != nil && c.view.Xdebug.Active {
		warnings = append(warnings, issue{name: "xdebug_loaded"})
	}

	if _, win := c.constant("PHP_WINDOWS_VERSION_BUILD"); win && c.view != nil &&
		(semver.VersionCompare(c.view.Version, "7.2.23") < 0 ||
			(semver.VersionCompare(c.view.Version, "7.3.0") >= 0 && semver.VersionCompare(c.view.Version, "7.3.10") < 0)) {
		warnings = append(warnings, issue{name: "onedrive", current: c.view.Version})
	}

	if c.extensionLoaded("uopz") && !filterBool(c.iniGet("uopz.disable")) && !filterBool(c.iniGet("uopz.exit")) {
		warnings = append(warnings, issue{name: "uopz"})
	}

	if len(errs) > 0 {
		for _, e := range errs {
			var text string
			switch e.name {
			case "json":
				text = php.EOL + "The json extension is missing." + php.EOL + "Install it or recompile php without --disable-json"
			case "phar":
				text = php.EOL + "The phar extension is missing." + php.EOL + "Install it or recompile php without --disable-phar"
			case "filter":
				text = php.EOL + "The filter extension is missing." + php.EOL + "Install it or recompile php without --disable-filter"
			case "hash":
				text = php.EOL + "The hash extension is missing." + php.EOL + "Install it or recompile php without --disable-hash"
			case "iconv_mbstring":
				text = php.EOL + "The iconv OR mbstring extension is required and both are missing." + php.EOL + "Install either of them or recompile php without --disable-iconv"
			case "php":
				text = php.EOL + "Your PHP (" + e.current + ") is too old, you must upgrade to PHP 7.2.5 or higher."
			case "allow_url_fopen":
				text = php.EOL + "The allow_url_fopen setting is incorrect." + php.EOL + "Add the following to the end of your `php.ini`:" + php.EOL + "    allow_url_fopen = On"
				displayIniMessage = true
			case "ioncube":
				text = php.EOL + "Your ionCube Loader extension (" + e.current + ") is incompatible with Phar files." + php.EOL + "Upgrade to ionCube 4.0.9 or higher or remove this line (path may be different) from your `php.ini` to disable it:" + php.EOL + "    zend_extension = /usr/lib/php5/20090626+lfs/ioncube_loader_lin_5.3.so"
				displayIniMessage = true
			case "openssl":
				text = php.EOL + "The openssl extension is missing, which means that secure HTTPS transfers are impossible." + php.EOL + "If possible you should enable it or recompile php with --with-openssl"
			}
			out(text, "error")
		}

		output.WriteString(php.EOL)
	}

	for _, w := range warnings {
		var text string
		switch w.name {
		case "apc_cli":
			text = "The apc.enable_cli setting is incorrect." + php.EOL + "Add the following to the end of your `php.ini`:" + php.EOL + "  apc.enable_cli = Off"
			displayIniMessage = true
		case "zlib":
			text = "The zlib extension is not loaded, this can slow down Composer a lot." + php.EOL + "If possible, enable it or recompile php with --with-zlib" + php.EOL
			displayIniMessage = true
		case "sigchild":
			text = "PHP was compiled with --enable-sigchild which can cause issues on some platforms." + php.EOL + "Recompile it without this flag if possible, see also:" + php.EOL + "  https://bugs.php.net/bug.php?id=22999"
		case "curlwrappers":
			text = "PHP was compiled with --with-curlwrappers which will cause issues with HTTP authentication and GitHub." + php.EOL + " Recompile it without this flag if possible"
		case "openssl_version":
			// Attempt to parse version number out, fallback to whole string value.
			v, _ := c.constant("OPENSSL_VERSION_TEXT")
			full := php.ToString(v)
			opensslVersion := ""
			if i := strings.IndexByte(full, ' '); i >= 0 {
				rest := php.Trim(full[i:])
				if before, _, found := strings.Cut(rest, " "); found {
					opensslVersion = before
				}
			}
			if !php.Truthy(opensslVersion) {
				opensslVersion = full
			}
			text = "The OpenSSL library (" + opensslVersion + ") used by PHP does not support TLSv1.2 or TLSv1.1." + php.EOL + "If possible you should upgrade OpenSSL to version 1.0.1 or above."
		case "xdebug_loaded":
			text = "The xdebug extension is loaded, this can slow down Composer a little." + php.EOL + " Disabling it when using Composer is recommended."
		case "xdebug_profile":
			text = "The xdebug.profiler_enabled setting is enabled, this can slow down Composer a lot." + php.EOL + "Add the following to the end of your `php.ini` to disable it:" + php.EOL + "  xdebug.profiler_enabled = 0"
			displayIniMessage = true
		case "onedrive":
			text = "The Windows OneDrive folder is not supported on PHP versions below 7.2.23 and 7.3.10." + php.EOL + "Upgrade your PHP (" + w.current + ") to use this location with Composer." + php.EOL
		case "uopz":
			text = "The uopz extension ignores exit calls and may not work with all Composer commands." + php.EOL + "Disabling it when using Composer is recommended."
		}
		out(text, "comment")
	}

	if displayIniMessage {
		out(iniMessage, "comment")
	}

	if v, _ := util.GetEnv("COMPOSER_IPRESOLVE"); v == "4" || v == "6" {
		warnings = append(warnings, issue{name: "ipresolve"})
		out("The COMPOSER_IPRESOLVE env var is set to "+v+" which may result in network failures below.", "comment")
	}

	if len(warnings) == 0 && len(errs) == 0 {
		return true
	}

	return output.String()
}

// checkConnectivity ports checkConnectivity: true, or the SKIP message.
func (c *DiagnoseCommand) checkConnectivity() any {
	if v := c.iniGet("allow_url_fopen"); !c.noPHP && !php.Truthy(v) {
		return "<info>SKIP</> <comment>Because allow_url_fopen is missing.</>"
	}

	return true
}

func (c *DiagnoseCommand) checkConnectivityAndComposerNetworkHTTPEnablement() any {
	if result := c.checkConnectivity(); result != true {
		return result
	}

	if result := checkComposerNetworkHTTPEnablement(); result != true {
		return result
	}

	return true
}

func checkComposerNetworkHTTPEnablement() any {
	if util.EnvTruthy("COMPOSER_DISABLE_NETWORK") {
		return "<info>SKIP</> <comment>Network is disabled by COMPOSER_DISABLE_NETWORK.</>"
	}

	return true
}

// phpSprintfU is sprintf('%u', $v): a negative value prints as its
// unsigned 64-bit two's complement.
func phpSprintfU(v int64) string {
	return strconv.FormatUint(uint64(v), 10) //nolint:gosec // %u reinterprets the bits, as PHP does
}

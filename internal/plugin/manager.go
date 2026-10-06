// Ports src/Composer/Plugin/PluginManager.php (docs/PLUGINS.md §5.4, D8):
// the policy of plugin loading. Which packages load and in which order,
// allow-plugins with its prompt, the plugin API check and the autoload plan
// are here; the mechanism (class loading, instantiation, activate(),
// addSubscriber()) runs in PHP (`plugin.load`, Maestro\Shim\Plugins).

package plugin

import (
	"errors"
	"slices"
	"sync"

	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// pluginManagerFile is the PHP file Composer's PluginManager exceptions are
// thrown in.
const pluginManagerFile = "PluginManager.php"

// Manager is Composer\Plugin\PluginManager. It implements
// composer.PluginManager (and so installer.PluginManager). As a Go object
// crossing to PHP it is the mirror of the shim's PluginManager, whose
// composer, io, globalComposer, disablePlugins and runningInGlobalDir
// properties it fills; the plugin instances live in PHP.
type Manager struct {
	r              *Runtime
	composer       *composer.Composer
	io             io.IO
	globalComposer *composer.PartialComposer
	versionParser  semver.VersionParser
	disablePlugins composer.DisablePlugins

	// registeredPlugins are the objects each plugin package registered, by
	// package name in registration order.
	registeredPlugins      []registration
	allowPluginRules       *allowRules
	allowGlobalPluginRules *allowRules
	runningInGlobalDir     bool
	autoloadedPackages     map[string]bool

	// pluginAPIVersion is getPluginApiVersion() (tests override it, as
	// PluginInstallerTest mocks the method).
	pluginAPIVersion func() string

	mu  sync.Mutex
	rev uint64
}

// registration is registeredPlugins[$name]: the PHP objects a package
// registered (plugins, or legacy installers).
type registration struct {
	name    string
	objects []*rpc.PHPObject
}

// allowRules is a parsed allow-plugins setting: regex => allow, in order
// (PHP's array semantics: a later duplicate pattern keeps the first one's
// position).
type allowRules struct {
	patterns []string
	allow    map[string]bool
}

func (a *allowRules) set(pattern string, allow bool) {
	if _, ok := a.allow[pattern]; !ok {
		a.patterns = append(a.patterns, pattern)
	}
	a.allow[pattern] = allow
}

// NewManager is new PluginManager($io, $composer, $globalComposer,
// $disablePlugins), for the plugin runtime r.
func NewManager(r *Runtime, out io.IO, c *composer.Composer, globalComposer *composer.PartialComposer, disablePlugins composer.DisablePlugins) (*Manager, error) {
	m := &Manager{
		r:                  r,
		composer:           c,
		io:                 out,
		globalComposer:     globalComposer,
		disablePlugins:     disablePlugins,
		autoloadedPackages: map[string]bool{},
		pluginAPIVersion:   func() string { return repository.PluginAPIVersion },
	}

	allow, err := c.Config().Get("allow-plugins", 0)
	if err != nil {
		return nil, err
	}
	if m.allowPluginRules, err = parseAllowedPlugins(allow, c); err != nil {
		return nil, err
	}

	var globalAllow any = false
	if globalComposer != nil {
		if globalAllow, err = globalComposer.Config().Get("allow-plugins", 0); err != nil {
			return nil, err
		}
	}
	if m.allowGlobalPluginRules, err = parseAllowedPlugins(globalAllow, nil); err != nil {
		return nil, err
	}

	return m, nil
}

// SetPluginAPIVersion replaces getPluginApiVersion() (PluginInstallerTest
// mocks it).
func (m *Manager) SetPluginAPIVersion(version string) {
	m.pluginAPIVersion = func() string { return version }
}

// parseAllowedPlugins ports parseAllowedPlugins: nil is null (the BC mode
// of lock files from before Composer 2.2).
func parseAllowedPlugins(allowPluginsConfig any, c *composer.Composer) (*allowRules, error) {
	if a, ok := allowPluginsConfig.(*php.Array); ok && a.Len() == 0 && c != nil && c.Locker() != nil {
		locked, err := c.Locker().IsLocked()
		if err != nil {
			return nil, err
		}
		if locked {
			api, err := c.Locker().PluginAPI()
			if err != nil {
				return nil, err
			}
			if semver.VersionCompare(api, "2.2.0") < 0 {
				return nil, nil
			}
		}
	}

	rules := &allowRules{allow: map[string]bool{}}
	switch v := allowPluginsConfig.(type) {
	case bool:
		rules.set("{}", v)

		return rules, nil
	case *php.Array:
		for pattern, allow := range v.All() {
			rules.set(pkg.PackageNameToRegexp(pattern.String(), "{^%s$}i"), allow == true)
		}
	}

	return rules, nil
}

// PHPOpaque implements php.Opaque.
func (*Manager) PHPOpaque() {}

// PHPClass implements rpc.Object.
func (*Manager) PHPClass() string { return `Composer\Plugin\PluginManager` }

// MirrorBase implements rpc.Mirror.
func (*Manager) MirrorBase() string { return `Composer\Plugin\PluginManager` }

// Rev implements rpc.Mirror.
func (m *Manager) Rev() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.rev
}

func (m *Manager) bump() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.rev++
}

// MirrorSnapshot implements rpc.Mirror.
func (m *Manager) MirrorSnapshot() (*php.Array, error) {
	var disable any = false
	switch m.disablePlugins {
	case composer.PluginsDisabled:
		disable = true
	case composer.PluginsDisabledLocal:
		disable = "local"
	case composer.PluginsDisabledGlobal:
		disable = "global"
	}
	var global any
	if m.globalComposer != nil {
		global = m.r.value(m.globalComposer)
	}

	return php.ArrayOf(
		"composer", m.r.value(m.composer),
		"io", m.r.value(m.io),
		"globalComposer", global,
		"disablePlugins", disable,
		"runningInGlobalDir", m.runningInGlobalDir,
	), nil
}

// ApplyMirror implements rpc.Mirror: the shim changes the manager through
// its methods only.
func (*Manager) ApplyMirror(*php.Array) error {
	return &rpc.ProtocolError{Message: "plugin manager fields are not synced from PHP"}
}

// SetRunningInGlobalDir ports setRunningInGlobalDir.
func (m *Manager) SetRunningInGlobalDir(runningInGlobalDir bool) {
	m.runningInGlobalDir = runningInGlobalDir
	m.bump()
}

// LoadInstalledPlugins ports loadInstalledPlugins: the plugins of the
// local repository, then the global ones.
func (m *Manager) LoadInstalledPlugins() error {
	defer m.frame("loadInstalledPlugins")()

	if !m.ArePluginsDisabled("local") {
		repo := m.composer.RepositoryManager().LocalRepository()
		done := phperr.Enter(pluginManagerClass+"->loadRepository", pluginManagerFile, 106)
		if err := m.loadRepository(repo, false, m.composer.Package()); done(err) != nil {
			return err
		}
	}

	if m.globalComposer != nil && !m.ArePluginsDisabled("global") {
		done := phperr.Enter(pluginManagerClass+"->loadRepository", pluginManagerFile, 110)
		if err := m.loadRepository(m.globalComposer.RepositoryManager().LocalRepository(), true, nil); done(err) != nil {
			return err
		}
	}

	return nil
}

// DeactivateInstalledPlugins ports deactivateInstalledPlugins.
func (m *Manager) DeactivateInstalledPlugins() error {
	if !m.ArePluginsDisabled("local") {
		if err := m.deactivateRepository(m.composer.RepositoryManager().LocalRepository()); err != nil {
			return err
		}
	}

	if m.globalComposer != nil && !m.ArePluginsDisabled("global") {
		if err := m.deactivateRepository(m.globalComposer.RepositoryManager().LocalRepository()); err != nil {
			return err
		}
	}

	return nil
}

// RegisteredPlugins ports getRegisteredPlugins: the names of the active
// plugin packages.
func (m *Manager) RegisteredPlugins() []string {
	names := make([]string, len(m.registeredPlugins))
	for i, reg := range m.registeredPlugins {
		names[i] = reg.name
	}

	return names
}

// GlobalComposer ports getGlobalComposer.
func (m *Manager) GlobalComposer() *composer.PartialComposer { return m.globalComposer }

func (m *Manager) registered(name string) (int, bool) {
	for i, reg := range m.registeredPlugins {
		if reg.name == name {
			return i, true
		}
	}

	return -1, false
}

// globalNote is the "(installed globally) " of the messages.
func (m *Manager) globalNote(isGlobalPlugin bool) string {
	if isGlobalPlugin || m.runningInGlobalDir {
		return "(installed globally) "
	}

	return ""
}

// RegisterPackage ports registerPackage: activates a plugin package (or
// registers a legacy composer-installer).
func (m *Manager) RegisterPackage(p pkg.PackageInterface, failOnMissingClasses, isGlobalPlugin bool) error {
	if failOnMissingClasses && !isGlobalPlugin {
		// PluginInstaller's registerPackage($package, true)
		defer m.frame("registerPackage", p, true)()
	} else {
		defer m.frame("registerPackage", p, failOnMissingClasses, isGlobalPlugin)()
	}

	scope := "local"
	if isGlobalPlugin {
		scope = "global"
	}
	if m.ArePluginsDisabled(scope) {
		m.io.WriteError(`<warning>The "`+p.Name()+`" plugin was not loaded as plugins are disabled.</warning>`, true, io.Normal)

		return nil
	}

	if p.Type() == "composer-plugin" {
		var requiresComposer semver.ConstraintInterface
		for link := range p.Requires().Values() {
			if link.Target() == "composer-plugin-api" {
				requiresComposer = link.Constraint()

				break
			}
		}

		if requiresComposer == nil {
			return &util.RuntimeError{Message: "Plugin " + p.Name() + " is missing a require statement for a version of the composer-plugin-api package.", Site: phperr.At(pluginManagerFile, 187)}
		}

		currentPluginAPIVersion := m.pluginAPIVersion()
		normalized, err := m.versionParser.Normalize(currentPluginAPIVersion)
		if err != nil {
			return err
		}
		currentPluginAPIConstraint, err := semver.NewConstraint("==", normalized)
		if err != nil {
			return err
		}

		if requiresComposer.PrettyString() == m.pluginAPIVersion() {
			m.io.WriteError(`<warning>The "`+p.Name()+`" plugin requires composer-plugin-api `+m.pluginAPIVersion()+`, this *WILL* break in the future and it should be fixed ASAP (require ^`+m.pluginAPIVersion()+` instead for example).</warning>`, true, io.Normal)
		} else if !requiresComposer.Matches(currentPluginAPIConstraint) {
			m.io.WriteError(`<warning>The "`+p.Name()+`" plugin `+m.globalNote(isGlobalPlugin)+`was skipped because it requires a Plugin API version ("`+requiresComposer.PrettyString()+`") that does not match your Composer installation ("`+currentPluginAPIVersion+`"). You may need to run composer update with the "--no-plugins" option.</warning>`, true, io.Normal)

			return nil
		}

		if p.Name() == "symfony/flex" {
			numeric, err := flexVersionPattern.IsMatch(p.Version())
			if err != nil {
				return err
			}
			if numeric && semver.VersionCompare(p.Version(), "1.9.8") < 0 {
				m.io.WriteError(`<warning>The "`+p.Name()+`" plugin `+m.globalNote(isGlobalPlugin)+`was skipped because it is not compatible with Composer 2+. Make sure to update it to version 1.9.8 or greater.</warning>`, true, io.Normal)

				return nil
			}
		}
	}

	allowed, err := m.IsPluginAllowed(p.Name(), isGlobalPlugin, pluginOptional(p))
	if err != nil {
		return err
	}
	if !allowed {
		m.io.WriteError(`Skipped loading "`+p.Name()+`" `+m.globalNote(isGlobalPlugin)+`as it is not in config.allow-plugins`, true, io.Debug)

		return nil
	}

	oldInstallerPlugin := p.Type() == "composer-installer"

	if _, ok := m.registered(p.Name()); ok {
		return nil
	}

	extra := p.Extra()
	classValue, _ := extra.Get("class")
	if !php.ToBool(classValue) {
		return &util.UnexpectedValueError{Message: "Error while installing " + p.PrettyName() + ", composer-plugin packages should have a class defined in their extra key to be usable.", Site: phperr.At(pluginManagerFile, 222)}
	}
	var classes []string
	if list, ok := classValue.(*php.Array); ok {
		for _, v := range list.Values() {
			classes = append(classes, php.ToString(v))
		}
	} else {
		classes = []string{php.ToString(classValue)}
	}

	loader, files, err := m.autoloadPlan(p)
	if err != nil {
		return err
	}

	if err := m.r.startFor(`plugin "`+p.Name()+`"`, m.io); err != nil {
		return err
	}

	// the shim's addPlugin() (or, for a legacy composer-installer,
	// InstallationManager::addInstaller()) is the call in progress while
	// plugin code runs (phperr.Live); pluginLoadCall locates errors
	pluginCall := phperr.Frame{Function: pluginManagerClass + "->addPlugin", File: pluginManagerFile, Line: 323}
	if oldInstallerPlugin {
		pluginCall = phperr.Frame{Function: `Composer\Installer\InstallationManager->addInstaller`, File: pluginManagerFile, Line: 315}
	}
	inPlugin := phperr.Within(pluginCall)
	res, err := m.r.Call("plugin.load", m.r.framed(php.ArrayOf(
		"pm", m,
		"package", m.r.packageObject(p),
		"classes", php.StringList(classes),
		"loader", loader,
		"files", files,
		"isGlobal", isGlobalPlugin,
		"legacyInstaller", oldInstallerPlugin,
		"failOnMissing", failOnMissingClasses,
		"runningInGlobalDir", m.runningInGlobalDir,
	)))
	inPlugin()
	reg := registration{name: p.Name()}
	if a, ok := res.(*php.Array); ok {
		if list, ok := a.GetArray("registered"); ok {
			for _, v := range list.Values() {
				if o, ok := v.(*rpc.PHPObject); ok {
					reg.objects = append(reg.objects, o)
				}
			}
		}
	}
	// What registered before an exception stays registered, as in
	// Composer, where registeredPlugins[] is appended one by one.
	if len(reg.objects) > 0 || err == nil {
		m.registeredPlugins = append(m.registeredPlugins, reg)
	}

	return pluginLoadCall(err, oldInstallerPlugin)
}

// frame pushes the frame of Composer's PluginManager method on the stack
// plugin code sees (docs/PLUGINS.md §5.12): Composer's stack holds
// loadInstalledPlugins(), loadRepository() and registerPackage() while a
// plugin is activated. It returns the function popping it.
func (m *Manager) frame(method string, args ...any) func() {
	crt := m.r.frameRuntime()
	if crt == nil {
		return func() {}
	}
	crt.PushFrame(pluginManagerClass+"->"+method, m, args...)

	return crt.PopFrame
}

// pluginManagerClass names PluginManager in the frames of its calls.
const pluginManagerClass = `Composer\Plugin\PluginManager`

// pluginLoadCall records the call of registerPackage() that a plugin's
// exception left through (docs/PLUGINS.md §5.12): `new $class()` (a
// constructor), addPlugin() (activate() and the subscription below it, in
// the shim's PHP), or, for a legacy composer-installer, `new $class(...)`
// and InstallationManager::addInstaller().
func pluginLoadCall(err error, oldInstallerPlugin bool) error {
	pe, ok := errors.AsType[*rpc.PHPException](err)
	if !ok {
		return err
	}
	open, ok := pe.OpenFrame()
	if !ok {
		return err
	}
	switch {
	case open.Function == "__construct" && oldInstallerPlugin:
		return phperr.Locate(err, pluginManagerFile, 314)
	case open.Function == "__construct":
		return phperr.Locate(err, pluginManagerFile, 322)
	case oldInstallerPlugin:
		return phperr.Call(err, `Composer\Installer\InstallationManager->addInstaller`, pluginManagerFile, 315)
	}

	return phperr.Call(err, pluginManagerClass+"->addPlugin", pluginManagerFile, 323)
}

var flexVersionPattern = php.MustCompile(`{^[0-9.]+$}`)

// pluginOptional is `true === ($package->getExtra()['plugin-optional'] ?? false)`.
func pluginOptional(p pkg.PackageInterface) bool {
	v, _ := p.Extra().Get("plugin-optional")

	return v == true
}

// autoloadPlan is registerPackage's class loader for a plugin package: the
// root package (without its `files`), the plugin package and its
// installed dependencies, as Composer builds it with
// parseAutoloads/createLoader. loader holds what the PHP ClassLoader is
// built from; files are the `files` to require.
func (m *Manager) autoloadPlan(p pkg.PackageInterface) (loader, files *php.Array, err error) {
	localRepo := m.composer.RepositoryManager().LocalRepository()
	var globalRepo repository.InstalledRepositoryInterface
	if m.globalComposer != nil {
		globalRepo = m.globalComposer.RepositoryManager().LocalRepository()
	}

	rootPackage, ok := pkg.Clone(m.composer.Package()).(pkg.RootPackageInterface)
	if !ok {
		return nil, nil, errors.New("plugin: the root package's clone is not a root package")
	}

	// clear files autoload rules from the root package as the root dependencies are not
	// necessarily all present yet when booting this runtime autoloader
	rootAutoload := rootPackage.Autoload().Clone()
	rootAutoload.Set("files", php.NewArray())
	rootPackage.SetAutoload(rootAutoload)
	rootDevAutoload := rootPackage.DevAutoload().Clone()
	rootDevAutoload.Set("files", php.NewArray())
	rootPackage.SetDevAutoload(rootDevAutoload)

	rootPackageRepo, err := repository.NewRootPackageRepository(rootPackage)
	if err != nil {
		return nil, nil, err
	}
	repos := []repository.RepositoryInterface{localRepo, rootPackageRepo}
	installedRepo, err := repository.NewInstalledRepository(repos)
	if err != nil {
		return nil, nil, err
	}
	if globalRepo != nil {
		if err := installedRepo.AddRepository(globalRepo); err != nil {
			return nil, nil, err
		}
	}

	collected := &collectedPackages{index: map[string]int{}}
	collected.add(p)
	if err := m.collectDependencies(installedRepo, collected, p); err != nil {
		return nil, nil, err
	}

	generator := m.composer.AutoloadGenerator()
	var autoloads []autoload.PackageMapEntry
	if m.shouldAutoloadPackage(rootPackage, "") {
		autoloads = append(autoloads, autoload.PackageMapEntry{Package: rootPackage, InstallPath: "", Installed: true})
	}
	for _, autoloadPackage := range collected.packages {
		if autoloadPackage == pkg.PackageInterface(rootPackage) {
			continue
		}

		global := false
		if globalRepo != nil {
			if global, err = globalRepo.HasPackage(autoloadPackage); err != nil {
				return nil, nil, err
			}
		}
		installPath, ok, err := m.installPath(autoloadPackage, global)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}

		// call shouldAutoloadPackage first here to ensure the plugin package gets marked in the autoloadedPackages array
		if m.shouldAutoloadPackage(autoloadPackage, installPath) || autoloadPackage == p {
			autoloads = append(autoloads, autoload.PackageMapEntry{Package: autoloadPackage, InstallPath: installPath, Installed: true})
		}
	}

	if len(autoloads) == 0 {
		return nil, nil, &util.LogicError{Message: "At least the plugin package should always be autoloaded for the code below to work", Site: phperr.At(pluginManagerFile, 271)}
	}

	parsed, err := generator.ParseAutoloads(autoloads, rootPackage, autoload.NoDevFilter)
	if err != nil {
		return nil, nil, err
	}
	vendorDir, err := m.composer.Config().Get("vendor-dir", 0)
	if err != nil {
		return nil, nil, err
	}
	classLoader, err := generator.CreateLoader(parsed, php.ToString(vendorDir))
	if err != nil {
		return nil, nil, err
	}

	loader = php.ArrayOf(
		"vendorDir", php.ToString(vendorDir),
		"psr0", orEmptyArray(parsed.PSR0),
		"psr4", orEmptyArray(parsed.PSR4),
		"classmap", orEmptyArray(classLoader.ClassMap),
	)

	return loader, orEmptyArray(parsed.Files), nil
}

func orEmptyArray(a *php.Array) *php.Array {
	if a == nil {
		return php.NewArray()
	}

	return a
}

// collectedPackages is collectDependencies' map of package names to
// packages, in insertion order.
type collectedPackages struct {
	packages []pkg.PackageInterface
	index    map[string]int
}

func (c *collectedPackages) add(p pkg.PackageInterface) {
	c.index[p.Name()] = len(c.packages)
	c.packages = append(c.packages, p)
}

// collectDependencies ports collectDependencies: the installed
// dependencies of a package, recursively.
func (m *Manager) collectDependencies(installedRepo *repository.InstalledRepository, collected *collectedPackages, p pkg.PackageInterface) error {
	for requireLink := range p.Requires().Values() {
		found, err := installedRepo.FindPackagesWithReplacersAndProviders(requireLink.Target(), nil)
		if err != nil {
			return err
		}
		for _, requiredPackage := range found {
			if _, ok := collected.index[requiredPackage.Name()]; !ok {
				collected.add(requiredPackage)
				if err := m.collectDependencies(installedRepo, collected, requiredPackage); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// shouldAutoloadPackage ports shouldAutoloadPackage.
func (m *Manager) shouldAutoloadPackage(p pkg.PackageInterface, path string) bool {
	key := p.Name() + ":" + p.Version() + ":" + path
	if m.autoloadedPackages[key] {
		return false
	}
	m.autoloadedPackages[key] = true

	return true
}

// installPath ports getInstallPath: ok is false for null.
func (m *Manager) installPath(p pkg.PackageInterface, global bool) (string, bool, error) {
	if !global {
		return m.composer.InstallationManager().InstallPath(p)
	}

	return m.globalComposer.InstallationManager().InstallPath(p)
}

// DeactivatePackage ports deactivatePackage.
func (m *Manager) DeactivatePackage(p pkg.PackageInterface) error {
	return m.unregister(p, "plugin.deactivate")
}

// UninstallPackage ports uninstallPackage.
func (m *Manager) UninstallPackage(p pkg.PackageInterface) error {
	return m.unregister(p, "plugin.uninstall")
}

// unregister runs deactivatePackage/uninstallPackage: the PHP side
// (method) removes the plugins; installers leave the installation
// manager.
func (m *Manager) unregister(p pkg.PackageInterface, method string) error {
	i, ok := m.registered(p.Name())
	if !ok {
		return nil
	}
	reg := m.registeredPlugins[i]

	if len(reg.objects) > 0 {
		objects := php.NewArrayCap(len(reg.objects))
		for _, o := range reg.objects {
			objects.Append(o)
		}
		if _, err := m.r.Call(method, php.ArrayOf("pm", m, "objects", objects)); err != nil {
			return err
		}
	}

	if i, ok := m.registered(p.Name()); ok {
		m.registeredPlugins = append(m.registeredPlugins[:i:i], m.registeredPlugins[i+1:]...)
	}

	return nil
}

// loadRepository ports loadRepository: the plugin packages of repo, in
// dependency order, composer/installers and install-path plugins first.
func (m *Manager) loadRepository(repo repository.RepositoryInterface, isGlobalRepo bool, rootPackage pkg.RootPackageInterface) error {
	if isGlobalRepo {
		defer m.frame("loadRepository", repo, true)() // loadRepository($repo, true)
	} else {
		defer m.frame("loadRepository", repo, false, rootPackage)()
	}

	packages, err := repo.Packages()
	if err != nil {
		return err
	}

	weights := map[string]int{}
	for _, p := range packages {
		if p.Type() == "composer-plugin" {
			v, _ := p.Extra().Get("plugin-modifies-install-path")
			if p.Name() == "composer/installers" || v == true {
				weights[p.Name()] = -10000
			}
		}
	}

	sortedPackages := pkg.SortPackages(packages, weights)
	var requiredPackages []pkg.PackageInterface
	if !isGlobalRepo {
		requiredPackages = repository.FilterRequiredPackages(packages, rootPackage, true)
	}

	for _, p := range sortedPackages {
		if _, ok := pkg.AsCompletePackage(p); !ok {
			continue
		}

		if p.Type() != "composer-plugin" && p.Type() != "composer-installer" {
			continue
		}

		if !isGlobalRepo && !containsPackage(requiredPackages, p) {
			allowed, err := m.isPluginAllowed(p.Name(), false, true, false)
			if err != nil {
				return err
			}
			if !allowed {
				m.io.WriteError(`<warning>The "`+p.Name()+`" plugin was not loaded as it is not listed in allow-plugins and is not required by the root package anymore.</warning>`, true, io.Normal)

				continue
			}
		}

		line := 533 // composer-plugin
		if p.Type() == "composer-installer" {
			line = 536
		}
		done := phperr.Enter(pluginManagerClass+"->registerPackage", pluginManagerFile, line)
		if err := m.RegisterPackage(p, false, isGlobalRepo); done(err) != nil {
			return err
		}
	}

	return nil
}

func containsPackage(list []pkg.PackageInterface, p pkg.PackageInterface) bool {
	return slices.Contains(list, p)
}

// deactivateRepository ports deactivateRepository: in reverse dependency
// order.
func (m *Manager) deactivateRepository(repo repository.RepositoryInterface) error {
	packages, err := repo.Packages()
	if err != nil {
		return err
	}
	for _, p := range slices.Backward(pkg.SortPackages(packages, nil)) {
		if _, ok := pkg.AsCompletePackage(p); !ok {
			continue
		}
		if p.Type() == "composer-plugin" || p.Type() == "composer-installer" {
			if err := m.DeactivatePackage(p); err != nil {
				return err
			}
		}
	}

	return nil
}

// ArePluginsDisabled ports arePluginsDisabled($type): "local" or "global".
func (m *Manager) ArePluginsDisabled(typ string) bool {
	return m.disablePlugins == composer.PluginsDisabled ||
		(typ == "local" && m.disablePlugins == composer.PluginsDisabledLocal) ||
		(typ == "global" && m.disablePlugins == composer.PluginsDisabledGlobal)
}

// DisablePlugins ports disablePlugins.
func (m *Manager) DisablePlugins() {
	m.disablePlugins = composer.PluginsDisabled
	m.bump()
}

// IsPluginAllowed ports isPluginAllowed($package, $isGlobalPlugin,
// $optional), prompting when the IO is interactive.
func (m *Manager) IsPluginAllowed(packageName string, isGlobalPlugin, optional bool) (bool, error) {
	return m.isPluginAllowed(packageName, isGlobalPlugin, optional, true)
}

// isPluginAllowed ports isPluginAllowed with its $prompt.
func (m *Manager) isPluginAllowed(packageName string, isGlobalPlugin, optional, prompt bool) (bool, error) {
	rules := &m.allowPluginRules
	if isGlobalPlugin {
		rules = &m.allowGlobalPluginRules
	}

	// This is a BC mode for lock files created pre-Composer-2.2 where the expectation of
	// an allow-plugins config being present cannot be made.
	if *rules == nil {
		if !m.io.IsInteractive() {
			return false, &util.RuntimeError{
				Message: "Your composer.lock was generated before the allow-plugins security feature was introduced and your composer.json does not define allow-plugins. " +
					`Run "composer update --lock" locally and commit the updated composer.lock, then add an explicit allow-plugins section to composer.json. ` +
					"See https://getcomposer.org/allow-plugins",
				Site: phperr.At(pluginManagerFile, 746),
			}
		}

		// keep going and prompt the user
		*rules = &allowRules{allow: map[string]bool{}}
	}

	for _, pattern := range (*rules).patterns {
		re, err := php.Compile(pattern)
		if err != nil {
			return false, err
		}
		ok, err := re.IsMatch(packageName)
		if err != nil {
			return false, err
		}
		if ok {
			return (*rules).allow[pattern], nil
		}
	}

	if packageName == "composer/package-versions-deprecated" {
		return false, nil
	}

	globally := ""
	if isGlobalPlugin || m.runningInGlobalDir {
		globally = " (installed globally)"
	}

	if m.io.IsInteractive() && prompt {
		c := m.composer.Partial()
		if isGlobalPlugin && m.globalComposer != nil {
			c = m.globalComposer
		}

		m.io.WriteError("<warning>"+packageName+globally+" contains a Composer plugin which is currently not in your allow-plugins config. See https://getcomposer.org/allow-plugins</warning>", true, io.Normal)
		attempts := 0
		for {
			// do not allow more than 5 prints of the help message, at some point assume the
			// input is not interactive and bail defaulting to a disabled plugin
			def := "?"
			if attempts > 5 {
				m.io.WriteError("Too many failed prompts, aborting.", true, io.Normal)

				break
			}

			answer, err := m.io.Ask(`Do you trust "<fg=green;options=bold>`+packageName+`</>" to execute code and wish to enable it now? (writes "allow-plugins" to composer.json) [<comment>y,n,d,?</comment>] `, def)
			if err != nil {
				// an IO written in PHP left its code through the call
				return false, phperr.Locate(err, pluginManagerFile, 781)
			}
			switch a, _ := answer.(string); a {
			case "y", "n", "d":
				allow := a == "y"

				// persist answer in current rules to avoid prompting again if the package gets reloaded
				(*rules).set(pkg.PackageNameToRegexp(packageName, "{^%s$}i"), allow)

				// persist answer in composer.json if it wasn't simply discarded
				if a == "y" || a == "n" {
					if err := persistAllowPlugin(c, packageName, allow); err != nil {
						return false, err
					}
				}

				return allow, nil
			default:
				attempts++
				m.io.WriteErrorMessages([]string{
					"y - add package to allow-plugins in composer.json and let it run immediately",
					"n - add package (as disallowed) to allow-plugins in composer.json to suppress further prompts",
					"d - discard this, do not change composer.json and do not allow the plugin to run",
					"? - print help",
				}, true, io.Normal)
			}
		}
	} else if optional {
		return false, nil
	}

	global := ""
	if isGlobalPlugin || m.runningInGlobalDir {
		global = "global "
	}

	return false, &PluginBlockedError{Message: packageName + globally + " contains a Composer plugin which is blocked by your allow-plugins config. You may add it to the list if you consider it safe." + php.EOL +
		`You can run "composer ` + global + `config --no-plugins allow-plugins.` + packageName + ` [true|false]" to enable it (true) or disable it explicitly and suppress this exception (false)` + php.EOL +
		"See https://getcomposer.org/allow-plugins"}
}

// persistAllowPlugin writes an allow-plugins answer to composer.json and
// the configuration, as isPluginAllowed does.
func persistAllowPlugin(c *composer.PartialComposer, packageName string, allow bool) error {
	cfg := c.Config()
	v, err := cfg.Get("allow-plugins", 0)
	if err != nil {
		return err
	}
	allowPlugins, ok := v.(*php.Array)
	if !ok {
		return nil
	}
	allowPlugins = allowPlugins.Clone()
	allowPlugins.Set(packageName, allow)
	sortPackages, err := cfg.Get("sort-packages", 0)
	if err != nil {
		return err
	}
	if php.ToBool(sortPackages) {
		php.Ksort(allowPlugins, php.SortRegular)
	}
	if err := cfg.ConfigSource().AddConfigSetting("allow-plugins", allowPlugins); err != nil {
		return err
	}

	return cfg.Merge(php.ArrayOf("config", php.ArrayOf("allow-plugins", allowPlugins)), "unknown")
}

// PluginBlockedError is Composer\Plugin\PluginBlockedException (an
// UnexpectedValueException).
type PluginBlockedError struct {
	Message string
}

func (e *PluginBlockedError) Error() string { return e.Message }

// ThrowableClass implements console.Throwable.
func (*PluginBlockedError) ThrowableClass() string { return `Composer\Plugin\PluginBlockedException` }

// ThrowableFile implements console.Throwable.
func (*PluginBlockedError) ThrowableFile() string { return phperr.AbsPath(pluginManagerFile) }

// ThrowableLine implements console.Throwable.
func (*PluginBlockedError) ThrowableLine() int { return 821 }

// ThrowableCode implements console.Throwable.
func (*PluginBlockedError) ThrowableCode() int { return 0 }

// ThrowablePrevious implements console.Throwable.
func (*PluginBlockedError) ThrowablePrevious() error { return nil }

// Unwrap makes errors.As see the UnexpectedValueException it extends.
func (e *PluginBlockedError) Unwrap() error { return &util.UnexpectedValueError{Message: e.Message} }

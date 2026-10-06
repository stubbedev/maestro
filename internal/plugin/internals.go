// Internals emulation (docs/PLUGINS.md §5.12; phase 6): the protected and
// private properties of Composer's services that plugins read by array
// cast, Closure::bind or reflection, and the running Composer\Installer
// as a PHP object.
//
// The parity list: Installer's properties (php-http/discovery and
// symfony/flex read `platformRequirementFilter` from the Installer on the
// stack, clone it and call __construct() on the clone), EventDispatcher's
// `runScripts` (both read it by array cast) and Config's private
// `baseDir` (typo3, pyrech, by reflection). ConsoleIO's `input`, `output`
// and `helperSet` are the IO mirror's (svc_io.go), ArgvInput's `tokens`
// the input mirror's (svc_console.go) and Transaction's
// `presentPackages`/`resultPackageMap` the transaction's
// (svc_resolverevents.go).

package plugin

import (
	"fmt"
	"strconv"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/filter"
	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
)

// serviceBase is the mirror base of the services whose PHP objects hold
// properties (Maestro\Shim\Adapter\ServiceAdapter): their class is built
// without its constructor, as a plain service's, and the properties are
// written into it, in their declaring class's scope.
const serviceBase = `Maestro\Shim\Service`

// serviceMirror is a Go-owned service whose PHP object also carries
// Composer's protected or private properties (§5.12's parity list),
// kept current by the sync engine. Its methods are maestro's, as a
// service's; PHP setters that only record a property (Composer\Installer's)
// send it back as a dirty field.
type serviceMirror struct {
	service

	// props returns the properties PHP's object holds.
	props func() (*php.Array, error)
	// state is a cheap fingerprint of props: the revision moves when it
	// changes.
	state func() string
	// apply applies properties PHP's setters changed; nil when PHP
	// changes none.
	apply func(fields *php.Array) error

	last string
	rev  uint64
}

// MirrorBase implements rpc.Mirror.
func (*serviceMirror) MirrorBase() string { return serviceBase }

// Rev implements rpc.Mirror.
func (m *serviceMirror) Rev() uint64 {
	if st := m.state(); st != m.last {
		m.last = st
		m.rev++
	}

	return m.rev
}

// MirrorSnapshot implements rpc.Mirror.
func (m *serviceMirror) MirrorSnapshot() (*php.Array, error) { return m.props() }

// ApplyMirror implements rpc.Mirror.
func (m *serviceMirror) ApplyMirror(fields *php.Array) error {
	if m.apply == nil {
		return &rpc.ProtocolError{Message: m.class + " properties are not synced from PHP"}
	}

	return m.apply(fields)
}

// newServiceMirror returns the service mirror of v.
func (r *Runtime) newServiceMirror(v any, class string, props func() (*php.Array, error), state func() string, apply func(*php.Array) error) rpc.Object {
	m := &serviceMirror{props: props, state: state, apply: apply}
	m.v, m.class = v, class
	m.last = state()

	return m
}

// eventDispatcherObject is an EventDispatcher with its protected
// `runScripts`.
func (r *Runtime) eventDispatcherObject(ed *eventdispatcher.EventDispatcher) any {
	return r.bridge.object(ed, func() rpc.Object {
		return r.newServiceMirror(ed, classEventDispatcher,
			func() (*php.Array, error) { return php.ArrayOf("runScripts", ed.RunScripts()), nil },
			func() string { return strconv.FormatBool(ed.RunScripts()) },
			nil)
	})
}

// configObject is a Config with its private `baseDir`.
func (r *Runtime) configObject(cfg *config.Config) any {
	return r.bridge.object(cfg, func() rpc.Object {
		baseDir := func() any {
			if d := cfg.BaseDir(); d != "" {
				return d
			}

			return nil
		}

		return r.newServiceMirror(cfg, classConfig,
			func() (*php.Array, error) { return php.ArrayOf("baseDir", baseDir()), nil },
			cfg.BaseDir,
			nil)
	})
}

// installerObject is maestro's running Installer as PHP sees it: a
// Composer\Installer whose properties are the Installer's settings and
// collaborators. Its setters record a property, as Composer's do, and
// maestro's Installer takes it (flex's
// setSuggestedPackagesReporter(new SuggestedPackagesReporter(new NullIO))
// silences the run's suggestions). A clone of it is PHP's own Installer
// object, which run() runs as a new maestro Installer (installer.run).
func (r *Runtime) installerObject(inst *composer.Installer) any {
	if inst == nil {
		return nil
	}

	return r.bridge.object(inst, func() rpc.Object {
		return r.newServiceMirror(inst, classInstaller,
			func() (*php.Array, error) { return r.installerProps(inst.State()) },
			func() string { return fmt.Sprintf("%v", inst.State()) },
			func(fields *php.Array) error { return r.applyInstallerFields(inst, fields) })
	})
}

// classInstaller is Composer\Installer.
const classInstaller = `Composer\Installer`

// installerProps are the properties of Composer\Installer for an
// installer's state.
func (r *Runtime) installerProps(s composer.InstallerState) (*php.Array, error) {
	var apcuPrefix any
	if s.ApcuAutoloaderPrefix != nil {
		apcuPrefix = *s.ApcuAutoloaderPrefix
	}
	var allowed, allowList any
	if s.AllowedTypes != nil {
		allowed = php.StringList(s.AllowedTypes)
	}
	if s.UpdateAllowList != nil {
		allowList = php.StringList(s.UpdateAllowList)
	}
	constraints := php.NewArray()
	if s.TemporaryConstraints != nil {
		for name, c := range s.TemporaryConstraints.All() {
			constraints.Set(name, constraintValue{c})
		}
	}
	var ed any
	if d, ok := s.EventDispatcher.(*eventdispatcher.EventDispatcher); ok && d != nil {
		ed = r.value(d)
	}
	var cfg any
	if c, ok := s.Config.(*config.Config); ok && c != nil {
		cfg = r.value(c)
	}
	var im any
	if s.InstallationManager != nil {
		im = r.value(s.InstallationManager)
	}
	var fixedRoot any
	if s.FixedRootPackage != nil {
		fixedRoot = r.value(s.FixedRootPackage)
	}
	var additional any
	if s.AdditionalFixedRepository != nil {
		additional = r.value(s.AdditionalFixedRepository)
	}
	var lockTx any
	if s.LockTransaction != nil {
		lockTx = r.value(s.LockTransaction)
	}
	var auditConfig, policyConfig any
	if s.AuditConfig != nil {
		auditConfig = r.auditConfigObject(s.AuditConfig)
	}
	if s.PolicyConfig != nil {
		policyConfig = r.serviceObject(s.PolicyConfig, classPolicyConfig)
	}
	var reporter any
	if s.SuggestedPackagesReporter != nil {
		reporter = r.serviceObject(s.SuggestedPackagesReporter, `Composer\Installer\SuggestedPackagesReporter`)
	}

	return php.ArrayOf(
		"io", r.value(s.IO),
		"config", cfg,
		"package", r.value(s.Package),
		"fixedRootPackage", fixedRoot,
		"downloadManager", r.value(s.DownloadManager),
		"repositoryManager", r.value(s.RepositoryManager),
		"locker", r.value(s.Locker),
		"installationManager", im,
		"eventDispatcher", ed,
		"autoloadGenerator", r.value(s.AutoloadGenerator),
		"preferSource", s.PreferSource,
		"preferDist", s.PreferDist,
		"optimizeAutoloader", s.OptimizeAutoloader,
		"classMapAuthoritative", s.ClassMapAuthoritative,
		"apcuAutoloader", s.ApcuAutoloader,
		"apcuAutoloaderPrefix", apcuPrefix,
		"devMode", s.DevMode,
		"dryRun", s.DryRun,
		"downloadOnly", s.DownloadOnly,
		"verbose", s.Verbose,
		"update", s.Update,
		"install", s.Install,
		"dumpAutoloader", s.DumpAutoloader,
		"runScripts", s.RunScripts,
		"preferStable", s.PreferStable,
		"preferLowest", s.PreferLowest,
		"minimalUpdate", s.MinimalUpdate,
		"writeLock", s.WriteLock,
		"executeOperations", s.ExecuteOperations,
		"audit", s.Audit,
		"errorOnAudit", s.ErrorOnAudit,
		"auditFormat", s.AuditFormat,
		"ignoredTypes", php.StringList(s.IgnoredTypes),
		"allowedTypes", allowed,
		"updateMirrors", s.UpdateMirrors,
		"updateAllowList", allowList,
		"updateAllowTransitiveDependencies", int64(s.UpdateAllowTransitiveDependencies),
		"suggestedPackagesReporter", reporter,
		"platformRequirementFilter", filterValue(s.PlatformRequirementFilter),
		"additionalFixedRepository", additional,
		"temporaryConstraints", constraints,
		"strictPsrAutoloader", s.StrictPsrAutoloader,
		"auditConfig", auditConfig,
		"policyConfig", policyConfig,
		"lockTransaction", lockTx,
	), nil
}

// filterValue describes a platform requirement filter as the shim builds
// it again (PlatformRequirementFilterFactory::fromBoolOrList): true
// (ignore all), false (ignore nothing) or the requirements of an ignore
// list, under the "\0filter" key the ServiceAdapter knows.
func filterValue(f version.PlatformRequirementFilter) any {
	var d any = false
	switch f := f.(type) {
	case version.IgnoreAllPlatformRequirementFilter:
		d = true
	case *filter.IgnoreList:
		d = php.StringList(f.ReqList())
	}

	return php.ArrayOf("\x00filter", d)
}

// applyInstallerFields applies what PHP's setters changed on the running
// Installer's object, the settings before the collaborators.
func (r *Runtime) applyInstallerFields(inst *composer.Installer, fields *php.Array) error {
	a := args{method: "Composer\\Installer"}
	get := func(name string) (any, bool) {
		v, ok := fields.Get(name)

		return unwrap(v), ok
	}
	boolean := func(name string, set func(bool) *composer.Installer) {
		if v, ok := get(name); ok {
			set(php.ToBool(v))
		}
	}
	boolean("preferSource", inst.SetPreferSource)
	boolean("preferDist", inst.SetPreferDist)
	boolean("optimizeAutoloader", inst.SetOptimizeAutoloader)
	boolean("classMapAuthoritative", inst.SetClassMapAuthoritative)
	boolean("strictPsrAutoloader", inst.SetStrictPsrAutoloader)
	boolean("devMode", inst.SetDevMode)
	boolean("dryRun", inst.SetDryRun)
	boolean("downloadOnly", inst.SetDownloadOnly)
	boolean("verbose", inst.SetVerbose)
	boolean("update", inst.SetUpdate)
	boolean("install", inst.SetInstall)
	boolean("dumpAutoloader", inst.SetDumpAutoloader)
	boolean("runScripts", inst.SetRunScripts)
	boolean("preferStable", inst.SetPreferStable)
	boolean("preferLowest", inst.SetPreferLowest)
	boolean("minimalUpdate", inst.SetMinimalUpdate)
	boolean("writeLock", inst.SetWriteLock)
	boolean("executeOperations", inst.SetExecuteOperations)
	boolean("audit", inst.SetAudit)
	boolean("errorOnAudit", inst.SetErrorOnAudit)
	boolean("updateMirrors", inst.SetUpdateMirrors)
	if v, ok := get("auditFormat"); ok {
		inst.SetAuditFormat(php.ToString(v))
	}
	_, hasApcu := get("apcuAutoloader")
	_, hasPrefix := get("apcuAutoloaderPrefix")
	if hasApcu || hasPrefix {
		st := inst.State()
		apcu := st.ApcuAutoloader
		if v, ok := get("apcuAutoloader"); ok {
			apcu = php.ToBool(v)
		}
		prefix := st.ApcuAutoloaderPrefix
		if v, ok := get("apcuAutoloaderPrefix"); ok {
			prefix = nil
			if v != nil {
				p := php.ToString(v)
				prefix = &p
			}
		}
		inst.SetApcuAutoloader(apcu, prefix)
	}
	if v, ok := get("ignoredTypes"); ok {
		inst.SetIgnoredTypes(stringList(v))
	}
	if v, ok := get("allowedTypes"); ok {
		if v == nil {
			inst.SetAllowedTypes(nil)
		} else {
			types := stringList(v)
			if types == nil {
				types = []string{}
			}
			inst.SetAllowedTypes(types)
		}
	}
	if v, ok := get("updateAllowList"); ok {
		inst.SetUpdateAllowList(stringList(v))
	}
	if v, ok := get("updateAllowTransitiveDependencies"); ok {
		if _, err := inst.SetUpdateAllowTransitiveDependencies(php.ToNativeInt(v)); err != nil {
			return err
		}
	}
	if v, ok := get("temporaryConstraints"); ok {
		constraints := &repository.ConstraintMap{}
		if list, ok := v.(*php.Array); ok {
			for name, c := range list.All() {
				cv, ok := c.(constraintValue)
				if !ok {
					return a.errorf("a temporary constraint is a %T, not a constraint", c)
				}
				constraints.Set(name.String(), cv.c)
			}
		}
		inst.SetTemporaryConstraints(constraints)
	}
	if v, ok := get("config"); ok {
		cfg, ok := v.(*config.Config)
		if !ok {
			return a.errorf("the installer's config is a %T, not a Config maestro knows", v)
		}
		inst.SetConfig(cfg)
	}
	if v, ok := get("suggestedPackagesReporter"); ok {
		rep, ok := v.(*installer.SuggestedPackagesReporter)
		if !ok {
			return unsupportedf("maestro does not support a %T as the running Installer's SuggestedPackagesReporter yet", v)
		}
		inst.SetSuggestedPackagesReporter(rep)
	}
	if v, ok := get("platformRequirementFilter"); ok {
		f, err := r.filterFromPHP(v)
		if err != nil {
			return err
		}
		inst.SetPlatformRequirementFilter(f)
	}
	if v, ok := get("additionalFixedRepository"); ok && v != nil {
		repo, ok := v.(repository.RepositoryInterface)
		if !ok {
			return unsupportedf("maestro does not support Composer\\Installer::setAdditionalFixedRepository() with a repository created in PHP yet")
		}
		inst.SetAdditionalFixedRepository(repo)
	}
	if v, ok := get("auditConfig"); ok && v != nil {
		c, ok := v.(*advisory.AuditConfig)
		if !ok {
			return unsupportedf("maestro does not support an AuditConfig created in PHP yet")
		}
		inst.SetAuditConfig(*c)
	}
	if v, ok := get("policyConfig"); ok && v != nil {
		c, ok := v.(*policy.PolicyConfig)
		if !ok {
			return unsupportedf("maestro does not support a PolicyConfig created in PHP yet")
		}
		inst.SetPolicyConfig(c)
	}

	return nil
}

// filterFromPHP is the platform requirement filter a PHP value stands
// for: true, false or a list (Composer\Installer::run's description), the
// filter object's description the ServiceAdapter sends, or a filter class
// of a plugin's own (phpFilter).
func (r *Runtime) filterFromPHP(v any) (version.PlatformRequirementFilter, error) {
	if a, ok := v.(*php.Array); ok {
		if d, ok := a.Get("\x00filter"); ok {
			v = d
		}
	}
	switch t := v.(type) {
	case bool, *php.Array:
		return filter.FromBoolOrList(v)
	case *rpc.PHPObject:
		return r.phpFilter(t), nil
	}

	return nil, &typeError{msg: fmt.Sprintf("a platform requirement filter is a %T", v)}
}

// transactionObject is a transaction with Composer's protected
// $presentPackages and $resultPackageMap (symfony/flex reads the
// PRE_OPERATIONS_EXEC transaction's result map through Closure::bind and
// builds a Transaction of its own from it). The packages PHP does not know
// yet cross with their core fields (the lazy tier of docs/PLUGINS.md
// §5.3); $operations stay null until getOperations() asks maestro.
func (r *Runtime) transactionObject(v any, t *resolver.Transaction, class string) any {
	lock, _ := v.(*resolver.LockTransaction)

	return r.bridge.object(v, func() rpc.Object {
		return r.newServiceMirror(v, class,
			func() (*php.Array, error) {
				byName := php.NewArray()
				for _, e := range t.ResultPackagesByName() {
					byName.Set(e.Name, r.keyedPackages(e.Packages))
				}
				props := php.ArrayOf(
					"presentPackages", r.lazyPackageList(t.PresentPackages()),
					"resultPackageMap", php.ArrayOf("\x00idmap", r.lazyPackageList(t.ResultPackageMap())),
					"resultPackagesByName", byName,
				)
				if lock != nil {
					// LockTransaction's own: $presentMap by spl_object_id,
					// $unlockableMap by package id, $resultPackages.
					unlockable := php.NewArray()
					for _, p := range lock.UnlockableMap() {
						unlockable.Set(int64(p.ID()), r.lazyPackage(p))
					}
					all, nonDev, dev := lock.ResultPackages()
					devList := php.NewArray()
					for i, p := range dev {
						if p != nil {
							devList.Set(int64(i), r.lazyPackage(p))
						}
					}
					props.Set("presentMap", php.ArrayOf("\x00idmap", r.lazyPackageList(lock.PresentMap())))
					props.Set("unlockableMap", unlockable)
					props.Set("resultPackages", php.ArrayOf(
						"all", r.lazyPackageList(all),
						"non-dev", r.lazyPackageList(nonDev),
						"dev", devList,
					))
				}

				return props, nil
			},
			func() string {
				if lock != nil {
					return strconv.Itoa(lock.Revision())
				}

				return ""
			},
			nil)
	})
}

// keyedPackages is a PHP array of packages with their keys (the lazy
// tier, as lazyPackageList).
func (r *Runtime) keyedPackages(packages []resolver.KeyedPackage) *php.Array {
	out := php.NewArrayCap(len(packages))
	for _, kp := range packages {
		out.Set(int64(kp.Key), r.lazyPackage(kp.Package))
	}

	return out
}

// registerInternals registers the handlers of the internals emulation.
func (r *Runtime) registerInternals() {
	// `clone $object` of one of maestro's objects (Remote::self): the
	// clone becomes a copy of the original's Go object.
	r.Handle("object.clone", func(v any) (any, error) {
		a := argsOf("object.clone", v)
		o, ok := a.at(0).(*rpc.PHPObject)
		if !ok {
			return nil, a.errorf("param 0 is not an object created in PHP (a %T)", a.at(0))
		}
		var clone rpc.Object
		switch src := unwrap(a.at(1)).(type) {
		case *config.Config:
			c := src.Clone()
			clone, _ = r.configObject(c).(rpc.Object)
		default:
			return nil, unsupportedf("maestro does not support cloning a %s in plugins yet", o.Class)
		}
		conn, err := r.started()
		if err != nil {
			return nil, err
		}

		return nil, conn.Adopt(o, clone)
	})
}

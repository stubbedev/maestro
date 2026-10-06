// Composer\Installer and its SuggestedPackagesReporter (docs/PLUGINS.md
// §4.12, §5.11; phase 5): a plugin's `Installer::create($io,
// $composer)->...->run()` runs maestro's Installer with the services and
// settings the PHP object recorded (`installer.run`), re-entrantly, from
// inside whatever maestro was doing (merge-plugin re-runs the update from
// POST_UPDATE_CMD).

package plugin

import (
	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/filter"
	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// installerDownloadManager is a DownloadManager as the Installer's.
type installerDownloadManager struct{ m *downloader.DownloadManager }

func (a installerDownloadManager) SetPreferSource(preferSource bool) {
	a.m.SetPreferSource(preferSource)
}

func (a installerDownloadManager) SetPreferDist(preferDist bool) { a.m.SetPreferDist(preferDist) }

// Manager returns the DownloadManager (composer.Installer.State's).
func (a installerDownloadManager) Manager() *downloader.DownloadManager { return a.m }

// composerOf returns the Composer instance PHP got whose repository
// manager is rm (the process runtime and executor of the services a PHP
// Installer was built from); nil when there is none.
func (r *Runtime) composerOf(rm *repository.RepositoryManager) *composer.Composer {
	r.bridge.mu.Lock()
	defer r.bridge.mu.Unlock()

	for key := range r.bridge.objects {
		if c, ok := key.(*composer.Composer); ok && c.RepositoryManager() == rm {
			return c
		}
	}

	return nil
}

// settings are the properties of a PHP Installer.
type settings struct {
	a args
	s *php.Array
}

func (s settings) get(name string) any {
	v, _ := s.s.Get(name)

	return unwrap(v)
}

func (s settings) boolean(name string) bool { return php.ToBool(s.get(name)) }

func (s settings) strings(name string) []string {
	list, ok := s.get(name).(*php.Array)
	if !ok {
		return nil
	}
	out := make([]string, 0, list.Len())
	for _, v := range list.Values() {
		out = append(out, php.ToString(v))
	}

	return out
}

func settingAs[T any](s settings, name string) (T, error) {
	var zero T
	v := s.get(name)
	if v == nil {
		return zero, nil
	}
	t, ok := v.(T)
	if !ok {
		return zero, s.a.errorf("the installer's %s is a %T, not a %T", name, v, zero)
	}

	return t, nil
}

// newInstallerFromSettings builds maestro's Installer of a PHP Installer's
// properties, as its constructor and setters would.
func (r *Runtime) newInstallerFromSettings(a args) (*composer.Installer, error) {
	s := settings{a: a, s: a.arrayOrEmpty(0)}

	out, ok, err := ioParam(args{method: a.method, list: []any{mustGet(s.s, "io")}}, 0)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, a.errorf("an installer without an IO")
	}
	cfg, err := settingAs[*config.Config](s, "config")
	if err != nil {
		return nil, err
	}
	var root pkg.RootPackageInterface
	if m, ok := mustGet(s.s, "package").(*packageMirror); ok {
		root, _ = m.p.(pkg.RootPackageInterface)
	}
	if root == nil {
		return nil, a.errorf("an installer without a root package maestro knows")
	}
	rm, err := settingAs[*repository.RepositoryManager](s, "repositoryManager")
	if err != nil {
		return nil, err
	}
	lk, err := settingAs[*locker.Locker](s, "locker")
	if err != nil {
		return nil, err
	}
	im, err := settingAs[composer.InstallationManager](s, "installationManager")
	if err != nil {
		return nil, err
	}
	dm, err := settingAs[*downloader.DownloadManager](s, "downloadManager")
	if err != nil {
		return nil, err
	}
	ed, err := settingAs[*eventdispatcher.EventDispatcher](s, "eventDispatcher")
	if err != nil {
		return nil, err
	}
	gen, err := settingAs[*autoload.Generator](s, "autoloadGenerator")
	if err != nil {
		return nil, err
	}
	reporter, err := settingAs[*installer.SuggestedPackagesReporter](s, "suggestedPackagesReporter")
	if err != nil {
		return nil, err
	}

	deps := composer.InstallerDeps{
		IO:                        out,
		Config:                    cfg,
		Package:                   root,
		RepositoryManager:         rm,
		Locker:                    lk,
		InstallationManager:       im,
		SuggestedPackagesReporter: reporter,
	}
	if c := r.composerOf(rm); c != nil {
		deps.Runtime = c.Runtime()
		deps.Process = c.ProcessExecutor()
	} else if r.composerFactory != nil {
		deps.Runtime = r.composerFactory.Runtime
		deps.Process = util.NewProcessExecutor(out)
	}
	if dm != nil {
		deps.DownloadManager = installerDownloadManager{dm}
	}
	if ed != nil {
		deps.EventDispatcher = ed
	}
	if gen != nil {
		deps.AutoloadGenerator = composer.GeneratorAdapter{Generator: gen}
	}

	inst, err := composer.NewInstaller(deps)
	if err != nil {
		return nil, err
	}

	return inst, r.applyInstallerSettings(inst, s)
}

func mustGet(a *php.Array, key string) any {
	v, _ := a.Get(key)

	return v
}

// applyInstallerSettings sets what a PHP Installer's setters recorded.
func (r *Runtime) applyInstallerSettings(inst *composer.Installer, s settings) error {
	inst.SetPreferSource(s.boolean("preferSource")).
		SetPreferDist(s.boolean("preferDist")).
		SetOptimizeAutoloader(s.boolean("optimizeAutoloader")).
		SetClassMapAuthoritative(s.boolean("classMapAuthoritative")).
		SetStrictPsrAutoloader(s.boolean("strictPsrAutoloader")).
		SetDevMode(s.boolean("devMode")).
		SetDryRun(s.boolean("dryRun")).
		SetDownloadOnly(s.boolean("downloadOnly")).
		SetVerbose(s.boolean("verbose")).
		SetUpdate(s.boolean("update")).
		SetInstall(s.boolean("install")).
		SetDumpAutoloader(s.boolean("dumpAutoloader")).
		SetRunScripts(s.boolean("runScripts")).
		SetPreferStable(s.boolean("preferStable")).
		SetPreferLowest(s.boolean("preferLowest")).
		SetMinimalUpdate(s.boolean("minimalUpdate")).
		SetWriteLock(s.boolean("writeLock")).
		SetExecuteOperations(s.boolean("executeOperations")).
		SetAudit(s.boolean("audit")).
		SetErrorOnAudit(s.boolean("errorOnAudit")).
		SetAuditFormat(php.ToString(s.get("auditFormat"))).
		SetUpdateMirrors(s.boolean("updateMirrors")).
		SetUpdateAllowList(s.strings("updateAllowList")).
		SetIgnoredTypes(s.strings("ignoredTypes"))

	var prefix *string
	if v := s.get("apcuAutoloaderPrefix"); v != nil {
		p := php.ToString(v)
		prefix = &p
	}
	inst.SetApcuAutoloader(s.boolean("apcuAutoloader"), prefix)

	if v := s.get("allowedTypes"); v != nil {
		types := s.strings("allowedTypes")
		if types == nil {
			types = []string{}
		}
		inst.SetAllowedTypes(types)
	}
	if v := s.get("updateAllowTransitiveDependencies"); v != nil {
		if _, err := inst.SetUpdateAllowTransitiveDependencies(int(php.ToInt(v))); err != nil {
			return err
		}
	}
	if v := s.get("config"); v != nil {
		if cfg, ok := v.(*config.Config); ok {
			inst.SetConfig(cfg)
		}
	}

	switch f := s.get("platformRequirementFilter").(type) {
	case bool, *php.Array:
		pf, err := filter.FromBoolOrList(f)
		if err != nil {
			return err
		}
		inst.SetPlatformRequirementFilter(pf)
	case nil:
	case *rpc.PHPObject:
		return unsupportedf("maestro does not support running an Installer with a %s yet", f.Class)
	}

	if v := s.get("additionalFixedRepository"); v != nil {
		repo, ok := v.(repository.RepositoryInterface)
		if !ok {
			return unsupportedf("maestro does not support Composer\\Installer::setAdditionalFixedRepository() with a repository created in PHP yet")
		}
		inst.SetAdditionalFixedRepository(repo)
	}
	if list, ok := s.get("temporaryConstraints").(*php.Array); ok && list.Len() > 0 {
		constraints := &repository.ConstraintMap{}
		for name, v := range list.All() {
			c, ok := v.(constraintValue)
			if !ok {
				return s.a.errorf("a temporary constraint is a %T, not a constraint", v)
			}
			constraints.Set(name.String(), c.c)
		}
		inst.SetTemporaryConstraints(constraints)
	}
	for _, name := range []string{"auditConfig", "policyConfig"} {
		switch v := s.get(name).(type) {
		case *rpc.PHPObject:
			return unsupportedf("maestro does not support running an Installer with a %s yet", v.Class)
		case *advisory.AuditConfig:
			// The AuditConfig of an Installer maestro ran (a clone of the
			// one on the stack keeps it).
			inst.SetAuditConfig(*v)
		case *policy.PolicyConfig:
			inst.SetPolicyConfig(v)
		}
	}

	return nil
}

func (r *Runtime) registerRunInstaller() {
	r.Handle("installer.run", func(v any) (any, error) {
		a := argsOf("installer.run", v)
		inst, err := r.newInstallerFromSettings(a)
		if err != nil {
			return nil, err
		}
		// PHP's Installer::run() is on PHP's stack already: the frame the
		// run pushes stands for it and is left out.
		r.mu.Lock()
		if r.phpRunInstallers == nil {
			r.phpRunInstallers = map[*composer.Installer]bool{}
		}
		r.phpRunInstallers[inst] = true
		r.mu.Unlock()
		code, err := inst.Run()
		if err != nil {
			return nil, err
		}
		var tx any
		if lt := inst.LockTransaction(); lt != nil {
			tx = r.value(lt)
		}

		return php.ArrayOf("code", int64(code), "lockTransaction", tx), nil
	})

	r.Handle("suggested.new", func(v any) (any, error) {
		a := argsOf("suggested.new", v)
		out, _, err := ioParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nil, r.adopt(a, installer.NewSuggestedPackagesReporter(out))
	})
	method := func(name string, fn func(rep *installer.SuggestedPackagesReporter, a args) (any, error)) {
		r.Handle("suggested."+name, func(v any) (any, error) {
			a := argsOf("suggested."+name, v)
			rep, err := receiver[*installer.SuggestedPackagesReporter](a)
			if err != nil {
				return nil, err
			}

			return fn(rep, a)
		})
	}
	method("getPackages", func(rep *installer.SuggestedPackagesReporter, _ args) (any, error) {
		out := php.NewArray()
		for _, s := range rep.Packages() {
			out.Append(php.ArrayOf("source", s.Source, "target", s.Target, "reason", s.Reason))
		}

		return out, nil
	})
	method("addPackage", func(rep *installer.SuggestedPackagesReporter, a args) (any, error) {
		rep.AddPackage(a.str(1), a.str(2), a.str(3))

		return nil, nil
	})
	method("addSuggestionsFromPackage", func(rep *installer.SuggestedPackagesReporter, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		rep.AddSuggestionsFromPackage(p)

		return nil, nil
	})
	outputArgs := func(a args, i int) (*repository.InstalledRepository, pkg.PackageInterface, error) {
		var installed *repository.InstalledRepository
		if a.has(i) {
			repo, ok := goRepository(a.at(i))
			if !ok {
				return nil, nil, unsupportedf("maestro does not support reporting suggestions against an InstalledRepository created in PHP yet")
			}
			if installed, ok = repo.(*repository.InstalledRepository); !ok {
				return nil, nil, a.errorf("param %d is not an InstalledRepository", i)
			}
		}
		var only pkg.PackageInterface
		if a.has(i + 1) {
			p, err := packageParam(a, i+1)
			if err != nil {
				return nil, nil, err
			}
			only = p
		}

		return installed, only, nil
	}
	method("output", func(rep *installer.SuggestedPackagesReporter, a args) (any, error) {
		installed, only, err := outputArgs(a, 2)
		if err != nil {
			return nil, err
		}

		return nil, rep.Output(a.integer(1), installed, only)
	})
	method("outputMinimalistic", func(rep *installer.SuggestedPackagesReporter, a args) (any, error) {
		installed, only, err := outputArgs(a, 1)
		if err != nil {
			return nil, err
		}

		return nil, rep.OutputMinimalistic(installed, only)
	})
}

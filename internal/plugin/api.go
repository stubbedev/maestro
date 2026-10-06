// The Composer API PHP calls (docs/PLUGINS.md §6.6), registered on every
// Runtime.

package plugin

// registerAPI registers the handlers of the PHP → Go methods, the value
// tags and the mirror factories of the Composer API.
func (r *Runtime) registerAPI() {
	r.RegisterTag(valueTag, decodeValue)
	r.Handle("dispatch.before", r.dispatchBefore)

	r.registerPackages()
	r.registerRepositories()
	r.registerComposer()
	r.registerIO()
	r.registerEvents()
	r.registerPluginManager()
	r.registerUtil()
	r.registerInstallers()
	r.registerInstallationManagerInstallers()
	r.registerPromises()
	r.registerConsole()
	r.Handle("repo.getPlatformPhpVersion", func(any) (any, error) { return r.platformPHPVersion(), nil })
}

// platformPHPVersion is PlatformRepository::getPlatformPhpVersion(): the
// last config.platform php version seen; null for none.
func (r *Runtime) platformPHPVersion() any {
	if r.opts.PlatformPHPVersion == nil {
		return nil
	}
	if v := r.opts.PlatformPHPVersion(); v != "" {
		return v
	}

	return nil
}

// Repositories and downloaders written in PHP (docs/PLUGINS.md §4.6, §4.8;
// phase 6): a repository object created in PHP handed to maestro
// (RepositoryManager::addRepository(), prependRepository(),
// RepositorySet::addRepository()) or created from a class a plugin
// registered (RepositoryManager::setRepositoryClass()), and a downloader
// handed to DownloadManager::setDownloader(), are maestro's through
// proxies whose methods call the PHP object (`object.call`, `object.new`).

package plugin

import (
	"sync"

	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// phpObjects are the proxies of PHP objects maestro uses, one per object.
type phpObjects struct {
	mu          sync.Mutex
	repos       map[*rpc.PHPObject]*proxyRepository
	downloaders map[*rpc.PHPObject]*proxyDownloader
	ios         map[*rpc.PHPObject]*phpIO
	// subclassDownloaders are the downloaders of FileDownloader
	// subclasses written in PHP, by maestro's downloader they extend.
	subclassDownloaders map[downloader.Downloader]*subclassDownloader
}

// callObject calls a method of a PHP object maestro uses (`object.call`).
func (r *Runtime) callObject(obj *rpc.PHPObject, method string, params ...any) (any, error) {
	list := php.NewArrayCap(len(params))
	for _, p := range params {
		list.Append(p)
	}

	return r.Call("object.call", php.ArrayOf("object", obj, "method", method, "args", list))
}

// proxyRepository is a repository written in PHP as maestro uses it.
type proxyRepository struct {
	r   *Runtime
	obj *rpc.PHPObject
}

var _ repository.RepositoryInterface = (*proxyRepository)(nil)

// phpRepository returns the proxy of a PHP repository object.
func (r *Runtime) phpRepository(obj *rpc.PHPObject) *proxyRepository {
	r.phpObjs.mu.Lock()
	defer r.phpObjs.mu.Unlock()

	if r.phpObjs.repos == nil {
		r.phpObjs.repos = map[*rpc.PHPObject]*proxyRepository{}
	}
	p, ok := r.phpObjs.repos[obj]
	if !ok {
		p = &proxyRepository{r: r, obj: obj}
		r.phpObjs.repos[obj] = p
	}

	return p
}

// repositoryParam returns param i, a repository of maestro's or one
// written in PHP.
func (r *Runtime) repositoryParam(a args, i int) (repository.RepositoryInterface, error) {
	if o, ok := a.at(i).(*rpc.PHPObject); ok {
		return r.phpRepository(o), nil
	}

	return goRepositoryParam(a, i)
}

func (p *proxyRepository) packages(v any) ([]pkg.PackageInterface, error) {
	list, _ := v.(*php.Array)
	if list == nil {
		return nil, nil
	}
	out := make([]pkg.PackageInterface, 0, list.Len())
	for _, item := range list.Values() {
		m, ok := item.(*packageMirror)
		if !ok {
			return nil, &rpc.ProtocolError{Message: p.obj.Class + " returned a " + php.GetType(item) + ", not a package"}
		}
		out = append(out, m.p)
	}

	return out, nil
}

func constraintParamValue(c semver.ConstraintInterface) any {
	if c == nil {
		return nil
	}

	return constraintValue{c}
}

// RepoName implements repository.RepositoryInterface.
func (p *proxyRepository) RepoName() string {
	v, err := p.r.callObject(p.obj, "getRepoName")
	if err != nil {
		return p.obj.Class
	}

	return php.ToString(v)
}

// Class implements repository.RepositoryInterface.
func (p *proxyRepository) Class() string { return p.obj.Class }

// HasPackage implements repository.RepositoryInterface.
func (p *proxyRepository) HasPackage(pk pkg.PackageInterface) (bool, error) {
	v, err := p.r.callObject(p.obj, "hasPackage", p.r.value(pk))

	return php.ToBool(v), err
}

// FindPackage implements repository.RepositoryInterface.
func (p *proxyRepository) FindPackage(name string, c semver.ConstraintInterface) (pkg.PackageInterface, error) {
	var constraint any = "*"
	if c != nil {
		constraint = constraintValue{c}
	}
	v, err := p.r.callObject(p.obj, "findPackage", name, constraint)
	if err != nil || v == nil {
		return nil, err
	}
	m, ok := v.(*packageMirror)
	if !ok {
		return nil, &rpc.ProtocolError{Message: p.obj.Class + "::findPackage() returned no package"}
	}

	return m.p, nil
}

// FindPackages implements repository.RepositoryInterface.
func (p *proxyRepository) FindPackages(name string, c semver.ConstraintInterface) ([]pkg.PackageInterface, error) {
	v, err := p.r.callObject(p.obj, "findPackages", name, constraintParamValue(c))
	if err != nil {
		return nil, err
	}

	return p.packages(v)
}

// Packages implements repository.RepositoryInterface.
func (p *proxyRepository) Packages() ([]pkg.PackageInterface, error) {
	v, err := p.r.callObject(p.obj, "getPackages")
	if err != nil {
		return nil, err
	}

	return p.packages(v)
}

// LoadPackages implements repository.RepositoryInterface.
func (p *proxyRepository) LoadPackages(packageNameMap *repository.ConstraintMap, acceptableStabilities, stabilityFlags *php.Array, alreadyLoaded repository.AlreadyLoaded) (repository.LoadResult, error) {
	names := php.NewArray()
	if packageNameMap != nil {
		for name, c := range packageNameMap.All() {
			names.Set(name, constraintParamValue(c))
		}
	}
	loaded := php.NewArray()
	for name, versions := range alreadyLoaded {
		v := php.NewArray()
		for version, pk := range versions {
			v.Set(version, p.r.value(pk))
		}
		loaded.Set(name, v)
	}
	if acceptableStabilities == nil {
		acceptableStabilities = php.NewArray()
	}
	if stabilityFlags == nil {
		stabilityFlags = php.NewArray()
	}
	v, err := p.r.callObject(p.obj, "loadPackages", names, acceptableStabilities, stabilityFlags, loaded)
	if err != nil {
		return repository.LoadResult{}, err
	}
	res, _ := v.(*php.Array)
	if res == nil {
		return repository.LoadResult{}, &rpc.ProtocolError{Message: p.obj.Class + "::loadPackages() returned no array"}
	}
	var out repository.LoadResult
	found, _ := res.Get("namesFound")
	out.NamesFound = stringList(found)
	packages, _ := res.Get("packages")
	out.Packages, err = p.packages(packages)

	return out, err
}

// Search implements repository.RepositoryInterface.
func (p *proxyRepository) Search(query string, mode int, typ string) ([]repository.SearchResult, error) {
	var t any
	if typ != "" {
		t = typ
	}
	v, err := p.r.callObject(p.obj, "search", query, int64(mode), t)
	if err != nil {
		return nil, err
	}
	list, _ := v.(*php.Array)
	if list == nil {
		return nil, nil
	}
	out := make([]repository.SearchResult, 0, list.Len())
	for _, item := range list.Values() {
		a, ok := item.(*php.Array)
		if !ok {
			continue
		}
		res := repository.SearchResult{Raw: a}
		res.Name, _ = a.GetString("name")
		if d, ok := a.Get("description"); ok && d != nil {
			res.Description = pkg.Str(php.ToString(d))
		}
		if ab, ok := a.Get("abandoned"); ok {
			res.Abandoned = ab
		}
		if u, ok := a.Get("url"); ok && u != nil {
			res.URL = pkg.Str(php.ToString(u))
		}
		out = append(out, res)
	}

	return out, nil
}

// Providers implements repository.RepositoryInterface.
func (p *proxyRepository) Providers(packageName string) ([]repository.ProviderInfo, error) {
	v, err := p.r.callObject(p.obj, "getProviders", packageName)
	if err != nil {
		return nil, err
	}
	list, _ := v.(*php.Array)
	if list == nil {
		return nil, nil
	}
	out := make([]repository.ProviderInfo, 0, list.Len())
	for _, item := range list.Values() {
		a, ok := item.(*php.Array)
		if !ok {
			continue
		}
		var info repository.ProviderInfo
		info.Name, _ = a.GetString("name")
		info.Type, _ = a.GetString("type")
		if d, ok := a.Get("description"); ok && d != nil {
			info.Description = pkg.Str(php.ToString(d))
		}
		out = append(out, info)
	}

	return out, nil
}

// Count implements repository.RepositoryInterface.
func (p *proxyRepository) Count() (int, error) {
	v, err := p.r.callObject(p.obj, "count")

	return php.ToNativeInt(v), err
}

// phpRepositoryConstructor is the constructor of a repository type a
// plugin registered with setRepositoryClass(): `new $class($repoConfig,
// $io, $config, $httpDownloader, $eventDispatcher)` in PHP.
func (r *Runtime) phpRepositoryConstructor(class string) repository.Constructor {
	return func(cfg *php.Array, deps repository.Deps) (repository.RepositoryInterface, error) {
		var ed any
		if d, ok := deps.EventDispatcher.(interface{ RunScripts() bool }); ok && d != nil {
			ed = r.value(d)
		}
		v, err := r.Call("object.new", php.ArrayOf("class", class, "args", php.ListOf(cfg, r.value(deps.IO), r.value(deps.Config), r.value(deps.HTTPDownloader), ed)))
		if err != nil {
			return nil, err
		}
		o, ok := v.(*rpc.PHPObject)
		if !ok {
			return nil, &rpc.ProtocolError{Message: "new " + class + " gave no object"}
		}

		return r.phpRepository(o), nil
	}
}

// proxyDownloader is a downloader written in PHP as maestro's
// DownloadManager uses it.
type proxyDownloader struct {
	r   *Runtime
	obj *rpc.PHPObject
}

var _ downloader.Downloader = (*proxyDownloader)(nil)

func (r *Runtime) phpDownloader(obj *rpc.PHPObject) *proxyDownloader {
	r.phpObjs.mu.Lock()
	defer r.phpObjs.mu.Unlock()

	if r.phpObjs.downloaders == nil {
		r.phpObjs.downloaders = map[*rpc.PHPObject]*proxyDownloader{}
	}
	d, ok := r.phpObjs.downloaders[obj]
	if !ok {
		d = &proxyDownloader{r: r, obj: obj}
		r.phpObjs.downloaders[obj] = d
	}

	return d
}

// Class gives the PHP class (downloader.Classer).
func (d *proxyDownloader) Class() string { return d.obj.Class }

// promise calls a method returning ?PromiseInterface: PHP hands its
// promise over as Promises::watch does (`object.promise`).
func (d *proxyDownloader) promise(method string, params ...any) (*downloader.Promise, error) {
	list := php.NewArrayCap(len(params))
	for _, p := range params {
		list.Append(p)
	}
	v, err := d.r.Call("object.promise", php.ArrayOf("object", d.obj, "method", method, "args", list))
	if err != nil {
		return nil, err
	}
	if v == nil {
		return util.Resolved(""), nil
	}
	p, err := d.r.promiseFromPHP(v, nil)
	if err != nil {
		return nil, err
	}

	return util.Then(p, func(struct{}) (string, error) { return "", nil }), nil
}

func (d *proxyDownloader) pkgValue(p pkg.PackageInterface) any {
	if p == nil {
		return nil
	}

	return d.r.value(p)
}

// InstallationSource implements downloader.Downloader.
func (d *proxyDownloader) InstallationSource() string {
	v, _ := d.r.callObject(d.obj, "getInstallationSource")

	return php.ToString(v)
}

// Download implements downloader.Downloader.
func (d *proxyDownloader) Download(p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*downloader.Promise, error) {
	return d.promise("download", d.pkgValue(p), path, d.pkgValue(prev))
}

// Prepare implements downloader.Downloader.
func (d *proxyDownloader) Prepare(typ string, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*downloader.Promise, error) {
	return d.promise("prepare", typ, d.pkgValue(p), path, d.pkgValue(prev))
}

// Install implements downloader.Downloader.
func (d *proxyDownloader) Install(p pkg.PackageInterface, path string) (*downloader.Promise, error) {
	return d.promise("install", d.pkgValue(p), path)
}

// Update implements downloader.Downloader.
func (d *proxyDownloader) Update(initial, target pkg.PackageInterface, path string) (*downloader.Promise, error) {
	return d.promise("update", d.pkgValue(initial), d.pkgValue(target), path)
}

// Remove implements downloader.Downloader.
func (d *proxyDownloader) Remove(p pkg.PackageInterface, path string) (*downloader.Promise, error) {
	return d.promise("remove", d.pkgValue(p), path)
}

// Cleanup implements downloader.Downloader.
func (d *proxyDownloader) Cleanup(typ string, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*downloader.Promise, error) {
	return d.promise("cleanup", typ, d.pkgValue(p), path, d.pkgValue(prev))
}

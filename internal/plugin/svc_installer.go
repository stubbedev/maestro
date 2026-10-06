// Custom installers (docs/PLUGINS.md §4.7, §5.6, D9): the Go peers of the
// shim's installer base classes, the proxies maestro's
// InstallationManager holds for PHP installers, and the `installer.*` and
// `im.*` methods PHP calls. php/src/Maestro/Shim/Installers.php is the PHP
// half.
//
// A PHP installer object (a plugin's subclass of LibraryInstaller, say)
// has a peer: the Go installer of its shim base class, built when the PHP
// constructor runs. The proxy (phpInstaller) is what the installation
// manager holds. Each of its methods calls PHP when the object's class
// overrides it (its override set) and the peer otherwise; the peer's own
// calls to its overridable methods (Virtuals) come back through the proxy.
// A method PHP inherits runs the peer's implementation (`installer.base`),
// so the calls go PHP → Go (base) → PHP (override) → ..., which is PHP's
// virtual dispatch.

package plugin

import (
	"sync"

	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// The PHP classes of the installers.
const (
	classLibraryInstaller     = `Composer\Installer\LibraryInstaller`
	classPluginInstaller      = `Composer\Installer\PluginInstaller`
	classMetapackageInstaller = `Composer\Installer\MetapackageInstaller`
	classNoopInstaller        = `Composer\Installer\NoopInstaller`
	classProjectInstaller     = `Composer\Installer\ProjectInstaller`
)

// The kinds of installer objects: their nearest shim base class, or
// interface for a direct InstallerInterface implementation.
const (
	kindLibrary     = "library"
	kindPlugin      = "plugin"
	kindMetapackage = "metapackage"
	kindNoop        = "noop"
	kindProject     = "project"
	kindInterface   = "interface"
)

// installerTable holds the PHP installer objects and BinaryInstallers
// maestro knows, by handle.
type installerTable struct {
	mu         sync.Mutex
	installers map[rpc.Handle]*phpInstaller
	binaries   map[rpc.Handle]*phpBinary
}

func newInstallerTable() *installerTable {
	return &installerTable{installers: map[rpc.Handle]*phpInstaller{}, binaries: map[rpc.Handle]*phpBinary{}}
}

// installerDesc is what PHP reports of an installer object
// (Installers::describe).
type installerDesc struct {
	kind           string
	overrides      map[string]bool
	binaryPresence bool
}

func descParam(a args, i int) installerDesc {
	d := installerDesc{kind: kindInterface, overrides: map[string]bool{}}
	arr := a.array(i)
	if arr == nil {
		return d
	}
	if kind, ok := arr.GetString("kind"); ok {
		d.kind = kind
	}
	if list, ok := arr.GetArray("overrides"); ok {
		for _, m := range list.Values() {
			d.overrides[php.ToString(m)] = true
		}
	}
	if bp, ok := arr.Get("binaryPresence"); ok {
		d.binaryPresence = php.ToBool(bp)
	}

	return d
}

// phpInstaller is the proxy of a PHP installer object: the
// installer.Installer maestro calls.
type phpInstaller struct {
	r    *Runtime
	obj  *rpc.PHPObject
	desc installerDesc
	// peer is the Go installer of the object's shim base class (nil for an
	// interface implementation); lib is its LibraryInstaller part, if any.
	peer installer.Installer
	lib  *installer.LibraryInstaller
	// sched is the event loop the object's composer runs on (nil: none).
	sched *util.Scheduler
	// facing is what the installation manager holds: the proxy with the
	// interfaces the PHP object implements (BinaryPresenceInterface,
	// `instanceof PluginInstaller`). It is the same value every time, as
	// removeInstaller finds installers by identity.
	facing installer.Installer
}

// phpBPInstaller is a phpInstaller whose object is a
// BinaryPresenceInterface.
type phpBPInstaller struct{ *phpInstaller }

// EnsureBinariesPresence implements installer.BinaryPresence.
func (p *phpBPInstaller) EnsureBinariesPresence(pk pkg.PackageInterface) error {
	return p.ensureBinariesPresence(pk)
}

// phpPluginInstaller is a phpInstaller whose object is a PluginInstaller.
type phpPluginInstaller struct{ *phpBPInstaller }

// DisablePlugins is disablePlugins() (InstallationManager::disablePlugins
// calls it on every PluginInstaller).
func (p *phpPluginInstaller) DisablePlugins() error {
	if p.overridden("disablePlugins") {
		_, err := p.call("disablePlugins")

		return err
	}
	pi, ok := p.peer.(*installer.PluginInstaller)
	if !ok {
		return p.unconstructed()
	}

	return pi.DisablePlugins()
}

var (
	_ installer.Installer      = (*phpInstaller)(nil)
	_ installer.BinaryPresence = (*phpBPInstaller)(nil)
	_ installer.Virtuals       = installerVirtuals{}
)

// phpInstallerOf returns the proxy of a PHP installer object, creating it
// with desc the first time.
func (r *Runtime) phpInstallerOf(obj *rpc.PHPObject, desc installerDesc) *phpInstaller {
	r.installers.mu.Lock()
	defer r.installers.mu.Unlock()

	if pi, ok := r.installers.installers[obj.H]; ok {
		return pi
	}

	pi := &phpInstaller{r: r, obj: obj, desc: desc}
	switch {
	case desc.kind == kindPlugin:
		pi.facing = &phpPluginInstaller{&phpBPInstaller{pi}}
	case desc.binaryPresence || desc.kind == kindLibrary:
		pi.facing = &phpBPInstaller{pi}
	default:
		pi.facing = pi
	}
	r.installers.installers[obj.H] = pi

	return pi
}

// knownPHPInstaller returns the proxy of a PHP installer object maestro
// has seen.
func (r *Runtime) knownPHPInstaller(obj *rpc.PHPObject) (*phpInstaller, bool) {
	r.installers.mu.Lock()
	defer r.installers.mu.Unlock()

	pi, ok := r.installers.installers[obj.H]

	return pi, ok
}

// setPeer makes peer the Go installer of the object; the peer's calls to
// its overridable methods come back through the proxy.
func (pi *phpInstaller) setPeer(peer installer.Installer) {
	pi.peer = peer
	switch t := peer.(type) {
	case *installer.LibraryInstaller:
		pi.lib = t
	case *installer.PluginInstaller:
		pi.lib = t.LibraryInstaller
	}
	if pi.lib != nil {
		pi.lib.SetVirtuals(installerVirtuals{pi})
	}
}

// base returns the peer, creating a NoopInstaller's (whose class has no
// constructor) when first needed.
func (pi *phpInstaller) base() (installer.Installer, error) {
	if pi.peer == nil && pi.desc.kind == kindNoop {
		pi.setPeer(installer.NewNoopInstaller())
	}
	if pi.peer == nil {
		return nil, pi.unconstructed()
	}

	return pi.peer, nil
}

// unconstructed is the error of an inherited method called on an object
// whose base class constructor never ran.
func (pi *phpInstaller) unconstructed() error {
	return &util.LogicError{Message: "maestro: the installer " + pi.obj.Class + " has no maestro peer: its constructor did not call the constructor of its Composer base class"}
}

// phpObject is the PHP installer object.
func (pi *phpInstaller) phpObject() *rpc.PHPObject { return pi.obj }

// overridden reports whether PHP implements method for this object.
func (pi *phpInstaller) overridden(method string) bool {
	return pi.desc.kind == kindInterface || pi.desc.overrides[method]
}

// call calls method on the PHP object (`installer.call`); args are PHP
// values. It returns PHP's result, {v} or {p}.
func (pi *phpInstaller) call(method string, params ...any) (*php.Array, error) {
	list := php.NewArrayCap(len(params))
	for _, p := range params {
		list.Append(p)
	}
	a := php.ArrayOf("h", pi.obj, "method", method, "args", list)
	if pi.lib != nil {
		a.Set("vendorDir", pi.lib.VendorDir())
	}
	res, err := pi.r.Call("installer.call", pi.r.framed(a))
	if err != nil {
		return nil, err
	}
	out, ok := res.(*php.Array)
	if !ok {
		return nil, &rpc.ProtocolError{Message: "invalid result of installer.call"}
	}

	return out, nil
}

// callValue calls method in PHP and returns its value.
func (pi *phpInstaller) callValue(method string, params ...any) (any, error) {
	res, err := pi.call(method, params...)
	if err != nil {
		return nil, err
	}
	v, _ := res.Get("v")

	return v, nil
}

// callPromise calls a method of PHP's returning ?PromiseInterface.
func (pi *phpInstaller) callPromise(method string, params ...any) (*installer.Promise, error) {
	res, err := pi.call(method, params...)
	if err != nil {
		return nil, err
	}
	if p, ok := res.Get("p"); ok && p != nil {
		return pi.r.promiseFromPHP(p, pi.sched)
	}

	return nil, nil
}

// Supports implements installer.Installer.
func (pi *phpInstaller) Supports(packageType string) (bool, error) {
	if pi.overridden("supports") {
		v, err := pi.callValue("supports", packageType)

		return php.ToBool(v), err
	}
	peer, err := pi.base()
	if err != nil {
		return false, err
	}

	return peer.Supports(packageType)
}

// IsInstalled implements installer.Installer.
func (pi *phpInstaller) IsInstalled(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (bool, error) {
	if pi.overridden("isInstalled") {
		v, err := pi.callValue("isInstalled", pi.r.value(repo), pi.r.value(p))

		return php.ToBool(v), err
	}
	peer, err := pi.base()
	if err != nil {
		return false, err
	}

	return peer.IsInstalled(repo, p)
}

// Download implements installer.Installer.
func (pi *phpInstaller) Download(p, prev pkg.PackageInterface) (*installer.Promise, error) {
	if pi.overridden("download") {
		return pi.callPromise("download", pi.r.value(p), pi.r.value(prev))
	}
	peer, err := pi.base()
	if err != nil {
		return nil, err
	}

	return peer.Download(p, prev)
}

// Prepare implements installer.Installer.
func (pi *phpInstaller) Prepare(typ string, p, prev pkg.PackageInterface) (*installer.Promise, error) {
	if pi.overridden("prepare") {
		return pi.callPromise("prepare", typ, pi.r.value(p), pi.r.value(prev))
	}
	peer, err := pi.base()
	if err != nil {
		return nil, err
	}

	return peer.Prepare(typ, p, prev)
}

// Install implements installer.Installer.
func (pi *phpInstaller) Install(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*installer.Promise, error) {
	if pi.overridden("install") {
		return pi.callPromise("install", pi.r.value(repo), pi.r.value(p))
	}
	peer, err := pi.base()
	if err != nil {
		return nil, err
	}

	return peer.Install(repo, p)
}

// Update implements installer.Installer.
func (pi *phpInstaller) Update(repo repository.InstalledRepositoryInterface, initial, target pkg.PackageInterface) (*installer.Promise, error) {
	if pi.overridden("update") {
		return pi.callPromise("update", pi.r.value(repo), pi.r.value(initial), pi.r.value(target))
	}
	peer, err := pi.base()
	if err != nil {
		return nil, err
	}

	return peer.Update(repo, initial, target)
}

// Uninstall implements installer.Installer.
func (pi *phpInstaller) Uninstall(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*installer.Promise, error) {
	if pi.overridden("uninstall") {
		return pi.callPromise("uninstall", pi.r.value(repo), pi.r.value(p))
	}
	peer, err := pi.base()
	if err != nil {
		return nil, err
	}

	return peer.Uninstall(repo, p)
}

// Cleanup implements installer.Installer.
func (pi *phpInstaller) Cleanup(typ string, p, prev pkg.PackageInterface) (*installer.Promise, error) {
	if pi.overridden("cleanup") {
		return pi.callPromise("cleanup", typ, pi.r.value(p), pi.r.value(prev))
	}
	peer, err := pi.base()
	if err != nil {
		return nil, err
	}

	return peer.Cleanup(typ, p, prev)
}

// InstallPath implements installer.Installer.
func (pi *phpInstaller) InstallPath(p pkg.PackageInterface) (string, bool, error) {
	if pi.overridden("getInstallPath") {
		v, err := pi.callValue("getInstallPath", pi.r.value(p))
		if err != nil || v == nil {
			return "", false, err
		}

		return php.ToString(v), true, nil
	}
	peer, err := pi.base()
	if err != nil {
		return "", false, err
	}

	return peer.InstallPath(p)
}

// ensureBinariesPresence is ensureBinariesPresence().
func (pi *phpInstaller) ensureBinariesPresence(p pkg.PackageInterface) error {
	if pi.overridden("ensureBinariesPresence") {
		_, err := pi.call("ensureBinariesPresence", pi.r.value(p))

		return err
	}
	if pi.lib == nil {
		return pi.unconstructed()
	}

	return pi.lib.EnsureBinariesPresence(p)
}

// installerVirtuals are the overridable methods of a PHP installer's
// LibraryInstaller peer: PHP's for those its class overrides, the peer's
// otherwise.
type installerVirtuals struct{ pi *phpInstaller }

func (v installerVirtuals) Supports(packageType string) (bool, error) {
	return v.pi.Supports(packageType)
}

func (v installerVirtuals) IsInstalled(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (bool, error) {
	return v.pi.IsInstalled(repo, p)
}

func (v installerVirtuals) InstallPath(p pkg.PackageInterface) (string, bool, error) {
	return v.pi.InstallPath(p)
}

func (v installerVirtuals) PackageBasePath(p pkg.PackageInterface) (string, error) {
	if v.pi.overridden("getPackageBasePath") {
		s, err := v.pi.callValue("getPackageBasePath", v.pi.r.value(p))

		return php.ToString(s), err
	}

	return v.pi.lib.PackageBasePath(p)
}

func (v installerVirtuals) InstallCode(p pkg.PackageInterface) (*installer.Promise, error) {
	if v.pi.overridden("installCode") {
		return v.pi.callPromise("installCode", v.pi.r.value(p))
	}

	return v.pi.lib.InstallCode(p)
}

func (v installerVirtuals) UpdateCode(initial, target pkg.PackageInterface) (*installer.Promise, error) {
	if v.pi.overridden("updateCode") {
		return v.pi.callPromise("updateCode", v.pi.r.value(initial), v.pi.r.value(target))
	}

	return v.pi.lib.UpdateCode(initial, target)
}

func (v installerVirtuals) RemoveCode(p pkg.PackageInterface) (*installer.Promise, error) {
	if v.pi.overridden("removeCode") {
		return v.pi.callPromise("removeCode", v.pi.r.value(p))
	}

	return v.pi.lib.RemoveCode(p)
}

func (v installerVirtuals) EnsureBinariesPresence(p pkg.PackageInterface) error {
	return v.pi.ensureBinariesPresence(p)
}

// phpBinary is a PHP BinaryInstaller object: its Go peer, and whether its
// class overrides the methods LibraryInstaller calls.
type phpBinary struct {
	r          *Runtime
	obj        *rpc.PHPObject
	peer       *installer.BinaryInstaller
	overridden bool
}

// InstallBinaries implements installer.Binaries for an overriding class.
func (b *phpBinary) InstallBinaries(p pkg.PackageInterface, installPath string, warnOnOverwrite bool) error {
	_, err := b.r.Call("installer.call", php.ArrayOf("h", b.obj, "method", "installBinaries", "args", php.ListOf(b.r.value(p), installPath, warnOnOverwrite)))

	return err
}

// RemoveBinaries implements installer.Binaries for an overriding class.
func (b *phpBinary) RemoveBinaries(p pkg.PackageInterface) error {
	_, err := b.r.Call("installer.call", php.ArrayOf("h", b.obj, "method", "removeBinaries", "args", php.ListOf(b.r.value(p))))

	return err
}

// binaries is what a LibraryInstaller peer is given as its
// BinaryInstaller.
func (b *phpBinary) binaries() installer.Binaries {
	if b.overridden {
		return b
	}

	return b.peer
}

// binariesParam returns param i, a PHP BinaryInstaller, as a peer's
// Binaries; nil for null.
func (r *Runtime) binariesParam(a args, i int) (installer.Binaries, error) {
	if !a.has(i) {
		return nil, nil
	}
	obj, ok := a.at(i).(*rpc.PHPObject)
	if !ok {
		return nil, a.errorf("param %d is a %T, not a BinaryInstaller created in PHP", i, a.at(i))
	}

	r.installers.mu.Lock()
	b, ok := r.installers.binaries[obj.H]
	r.installers.mu.Unlock()
	if !ok {
		return nil, a.errorf("param %d is a %s whose BinaryInstaller constructor did not run", i, obj.Class)
	}

	return b.binaries(), nil
}

// loopScheduler is the scheduler of a composer's event loop, nil for
// none.
func loopScheduler(c any) *util.Scheduler {
	lc, ok := c.(interface{ Loop() *http.Loop })
	if !ok {
		return nil
	}
	loop := lc.Loop()
	if loop == nil || loop.HttpDownloader() == nil {
		return nil
	}

	return loop.HttpDownloader().Scheduler()
}

// installerValue is an installer as PHP sees it: the PHP object of a PHP
// installer, the proxy of a maestro installer's class otherwise.
func (r *Runtime) installerValue(inst installer.Installer) any {
	if pb, ok := inst.(interface{ phpObject() *rpc.PHPObject }); ok {
		return pb.phpObject()
	}

	var class string
	switch inst.(type) {
	case *installer.LibraryInstaller:
		class = classLibraryInstaller
	case *installer.PluginInstaller:
		class = classPluginInstaller
	case *installer.MetapackageInstaller:
		class = classMetapackageInstaller
	case *installer.NoopInstaller:
		class = classNoopInstaller
	case *installer.ProjectInstaller:
		class = classProjectInstaller
	default:
		class = `Composer\Installer\InstallerInterface`
	}

	return r.serviceObject(inst, class)
}

// installerParam returns param i as the installer.Installer maestro holds
// for it: a PHP object's proxy (created with desc when new), or a
// maestro installer.
func (r *Runtime) installerParam(a args, i int, desc *installerDesc) (installer.Installer, bool, error) {
	switch v := a.at(i).(type) {
	case *rpc.PHPObject:
		if desc == nil {
			pi, ok := r.knownPHPInstaller(v)
			if !ok {
				return nil, false, nil
			}

			return pi.facing, true, nil
		}

		return r.phpInstallerOf(v, *desc).facing, true, nil
	case *service:
		if inst, ok := v.v.(installer.Installer); ok {
			return inst, true, nil
		}
	}

	return nil, false, a.errorf("param %d is a %T, not an installer", i, a.at(i))
}

// installerReceiver returns the Go installer an `installer.base` call runs
// on: a PHP object's peer, or a maestro installer.
func (r *Runtime) installerReceiver(a args) (installer.Installer, error) {
	switch v := a.at(0).(type) {
	case *rpc.PHPObject:
		pi, ok := r.knownPHPInstaller(v)
		if !ok {
			return nil, a.errorf("the %s has no maestro peer: its constructor did not call the constructor of its Composer base class", v.Class)
		}

		return pi.base()
	case *service:
		if inst, ok := v.v.(installer.Installer); ok {
			return inst, nil
		}
	}

	return nil, a.errorf("param 0 is a %T, not an installer", a.at(0))
}

// installerAt is the part of a Go installer implementing the shim class
// level (parent::method() calls the method of the class declaring it).
func installerAt(a args, inst installer.Installer, level string) (installer.Installer, error) {
	switch level {
	case classLibraryInstaller:
		switch t := inst.(type) {
		case *installer.LibraryInstaller:
			return t, nil
		case *installer.PluginInstaller:
			return t.LibraryInstaller, nil
		}
	case classPluginInstaller:
		if t, ok := inst.(*installer.PluginInstaller); ok {
			return t, nil
		}
	case classMetapackageInstaller:
		if t, ok := inst.(*installer.MetapackageInstaller); ok {
			return t, nil
		}
	case classNoopInstaller:
		if t, ok := inst.(*installer.NoopInstaller); ok {
			return t, nil
		}
	case classProjectInstaller:
		if t, ok := inst.(*installer.ProjectInstaller); ok {
			return t, nil
		}
	}

	return nil, a.errorf("a %T is not a %s", inst, level)
}

// libraryOf is the LibraryInstaller part of a Go installer, nil for none.
func libraryOf(inst installer.Installer) *installer.LibraryInstaller {
	switch t := inst.(type) {
	case *installer.LibraryInstaller:
		return t
	case *installer.PluginInstaller:
		return t.LibraryInstaller
	}

	return nil
}

// optionalPackage returns param i as a package, nil for null.
func optionalPackage(a args, i int) (pkg.PackageInterface, error) {
	if !a.has(i) {
		return nil, nil
	}

	return packageParam(a, i)
}

func (r *Runtime) registerInstallers() {
	r.Handle("installer.new", r.installerNew)
	r.Handle("installer.base", r.installerBase)
	r.Handle("installer.newBinary", r.installerNewBinary)
	r.Handle("installer.binary", r.installerBinary)
	r.Handle("installer.determineBinaryCaller", func(v any) (any, error) {
		a := argsOf("installer.determineBinaryCaller", v)

		return util.DetermineBinaryCaller(a.str(0))
	})
}

// installerNew serves `installer.new`: the peer of a PHP installer object
// whose base class constructor runs.
func (r *Runtime) installerNew(v any) (any, error) {
	a := argsOf("installer.new", v)
	obj, ok := a.at(0).(*rpc.PHPObject)
	if !ok {
		return nil, a.errorf("param 0 is a %T, not an installer created in PHP", a.at(0))
	}
	desc := descParam(a, 1)
	p := args{method: "installer.new " + desc.kind, list: a.arrayOrEmpty(2).Values()}
	pi := r.phpInstallerOf(obj, desc)

	var (
		peer installer.Installer
		err  error
	)
	switch desc.kind {
	case kindLibrary, kindPlugin:
		var out io.IO
		out, _, err = ioParam(p, 0)
		if err != nil {
			return nil, err
		}
		c, ok := serviceValue(p.at(1)).(installer.PartialComposer)
		if !ok {
			return nil, p.errorf("param 1 is a %T, not a Composer instance maestro knows", p.at(1))
		}
		var bins installer.Binaries
		bins, err = r.binariesParam(p, 3)
		if err != nil {
			return nil, err
		}
		pi.sched = loopScheduler(c)
		if desc.kind == kindPlugin {
			peer, err = installer.NewPluginInstaller(out, c, nil, bins)
		} else {
			peer, err = installer.NewLibraryInstaller(out, c, nullString(p, 2), nil, bins)
		}
	case kindMetapackage:
		var out io.IO
		out, _, err = ioParam(p, 0)
		if err != nil {
			return nil, err
		}
		peer = installer.NewMetapackageInstaller(out)
	case kindNoop:
		peer = installer.NewNoopInstaller()
	case kindProject:
		dm, ok := serviceValue(p.at(1)).(*downloader.DownloadManager)
		if !ok {
			return nil, p.errorf("param 1 is a %T, not a DownloadManager maestro knows", p.at(1))
		}
		peer = installer.NewProjectInstaller(p.str(0), dm, util.NewFilesystem(nil))
	default:
		return nil, p.errorf("unknown installer kind %q", desc.kind)
	}
	if err != nil {
		return nil, err
	}
	pi.setPeer(peer)

	return nil, nil
}

// installerBase serves `installer.base`: the base class implementation of
// a method (level is the shim class declaring it) on an installer's peer,
// as parent::method() runs it. Promises go to PHP as {id, s}; the
// LibraryInstaller's vendor dir (initializeVendorDir() resolves it) comes
// back with every result.
func (r *Runtime) installerBase(v any) (any, error) {
	a := argsOf("installer.base", v)
	inst, err := r.installerReceiver(a)
	if err != nil {
		return nil, err
	}
	method := a.str(2)
	target, err := installerAt(a, inst, a.str(1))
	if err != nil {
		return nil, err
	}
	p := args{method: "installer.base " + method, list: a.arrayOrEmpty(3).Values()}

	value, promise, isPromise, err := r.runBase(target, method, p)
	if err != nil {
		return nil, err
	}

	out := php.ArrayOf("v", value)
	if isPromise {
		out.Set("p", r.promiseToPHP(promise))
	}
	if lib := libraryOf(inst); lib != nil {
		out.Set("vd", lib.VendorDir())
	}

	return out, nil
}

// runBase runs method on target with PHP's params.
func (r *Runtime) runBase(target installer.Installer, method string, p args) (value any, promise *installer.Promise, isPromise bool, err error) {
	lib, _ := target.(*installer.LibraryInstaller)
	needLib := func() error {
		if lib == nil {
			return p.errorf("a %T has no %s()", target, method)
		}

		return nil
	}

	switch method {
	case "supports":
		value, err = target.Supports(p.str(0))
	case "isInstalled":
		repo, perr := param[repository.InstalledRepositoryInterface](p, 0)
		if perr != nil {
			return nil, nil, false, perr
		}
		pk, perr := packageParam(p, 1)
		if perr != nil {
			return nil, nil, false, perr
		}
		value, err = target.IsInstalled(repo, pk)
	case "download", "prepare", "cleanup":
		off := 0
		typ := ""
		if method != "download" {
			off = 1
			typ = p.str(0)
		}
		pk, perr := packageParam(p, off)
		if perr != nil {
			return nil, nil, false, perr
		}
		prev, perr := optionalPackage(p, off+1)
		if perr != nil {
			return nil, nil, false, perr
		}
		isPromise = true
		switch method {
		case "download":
			promise, err = target.Download(pk, prev)
		case "prepare":
			promise, err = target.Prepare(typ, pk, prev)
		default:
			promise, err = target.Cleanup(typ, pk, prev)
		}
	case "install", "uninstall":
		repo, perr := param[repository.InstalledRepositoryInterface](p, 0)
		if perr != nil {
			return nil, nil, false, perr
		}
		pk, perr := packageParam(p, 1)
		if perr != nil {
			return nil, nil, false, perr
		}
		isPromise = true
		if method == "install" {
			promise, err = target.Install(repo, pk)
		} else {
			promise, err = target.Uninstall(repo, pk)
		}
	case "update":
		repo, perr := param[repository.InstalledRepositoryInterface](p, 0)
		if perr != nil {
			return nil, nil, false, perr
		}
		initial, perr := packageParam(p, 1)
		if perr != nil {
			return nil, nil, false, perr
		}
		tgt, perr := packageParam(p, 2)
		if perr != nil {
			return nil, nil, false, perr
		}
		isPromise = true
		promise, err = target.Update(repo, initial, tgt)
	case "getInstallPath":
		pk, perr := packageParam(p, 0)
		if perr != nil {
			return nil, nil, false, perr
		}
		path, ok, perr := target.InstallPath(pk)
		if perr != nil || !ok {
			return nil, nil, false, perr
		}
		value = path
	case "ensureBinariesPresence", "getPackageBasePath", "installCode", "removeCode":
		if err := needLib(); err != nil {
			return nil, nil, false, err
		}
		pk, perr := packageParam(p, 0)
		if perr != nil {
			return nil, nil, false, perr
		}
		switch method {
		case "ensureBinariesPresence":
			err = lib.EnsureBinariesPresence(pk)
		case "getPackageBasePath":
			value, err = lib.PackageBasePath(pk)
		case "installCode":
			isPromise = true
			promise, err = lib.InstallCode(pk)
		default:
			isPromise = true
			promise, err = lib.RemoveCode(pk)
		}
	case "updateCode":
		if err := needLib(); err != nil {
			return nil, nil, false, err
		}
		initial, perr := packageParam(p, 0)
		if perr != nil {
			return nil, nil, false, perr
		}
		tgt, perr := packageParam(p, 1)
		if perr != nil {
			return nil, nil, false, perr
		}
		isPromise = true
		promise, err = lib.UpdateCode(initial, tgt)
	case "initializeVendorDir":
		if err := needLib(); err != nil {
			return nil, nil, false, err
		}
		err = lib.InitializeVendorDir()
	default:
		return nil, nil, false, &rpc.UnsupportedError{Method: "installer.base " + method}
	}

	return value, promise, isPromise, err
}

// installerNewBinary serves `installer.newBinary`: the peer of a PHP
// BinaryInstaller object whose constructor runs.
func (r *Runtime) installerNewBinary(v any) (any, error) {
	a := argsOf("installer.newBinary", v)
	obj, ok := a.at(0).(*rpc.PHPObject)
	if !ok {
		return nil, a.errorf("param 0 is a %T, not a BinaryInstaller created in PHP", a.at(0))
	}
	p := args{method: "installer.newBinary", list: a.arrayOrEmpty(2).Values()}
	out, _, err := ioParam(p, 0)
	if err != nil {
		return nil, err
	}
	peer := installer.NewBinaryInstaller(out, p.str(1), p.str(2), nil, nullString(p, 3))

	r.installers.mu.Lock()
	r.installers.binaries[obj.H] = &phpBinary{r: r, obj: obj, peer: peer, overridden: a.boolean(1)}
	r.installers.mu.Unlock()

	return nil, nil
}

// installerBinary serves `installer.binary`: BinaryInstaller's
// installBinaries() and removeBinaries() on the object's peer.
func (r *Runtime) installerBinary(v any) (any, error) {
	a := argsOf("installer.binary", v)
	obj, ok := a.at(0).(*rpc.PHPObject)
	if !ok {
		return nil, a.errorf("param 0 is a %T, not a BinaryInstaller created in PHP", a.at(0))
	}
	r.installers.mu.Lock()
	b, ok := r.installers.binaries[obj.H]
	r.installers.mu.Unlock()
	if !ok {
		return nil, a.errorf("the %s has no maestro peer: its constructor did not call BinaryInstaller's", obj.Class)
	}

	method := a.str(1)
	p := args{method: "installer.binary " + method, list: a.arrayOrEmpty(2).Values()}
	pk, err := packageParam(p, 0)
	if err != nil {
		return nil, err
	}
	switch method {
	case "installBinaries":
		warn := true
		if p.has(2) {
			warn = p.boolean(2)
		}

		return nil, b.peer.InstallBinaries(pk, p.str(1), warn)
	case "removeBinaries":
		return nil, b.peer.RemoveBinaries(pk)
	}

	return nil, &rpc.UnsupportedError{Method: "installer.binary " + method}
}

// installerList is the part of maestro's InstallationManager holding the
// installers.
type installerList interface {
	AddInstaller(installer.Installer)
	RemoveInstaller(installer.Installer)
	Installer(typ string) (installer.Installer, error)
}

// operationParam returns param i as one of maestro's operations.
func operationParam[T operation.Operation](a args, i int) (T, error) {
	var zero T
	m, ok := a.at(i).(*operationMirror)
	if !ok {
		return zero, a.errorf("param %d is a %T, not an operation maestro knows", i, a.at(i))
	}
	op, ok := m.op.(T)
	if !ok {
		return zero, a.errorf("param %d is a %T, not a %T", i, m.op, zero)
	}

	return op, nil
}

func (r *Runtime) registerInstallationManagerInstallers() {
	list := func(name string, fn func(im installerList, a args) (any, error)) {
		r.Handle("im."+name, func(v any) (any, error) {
			a := argsOf("im."+name, v)
			im, err := receiver[installerList](a)
			if err != nil {
				return nil, err
			}

			return fn(im, a)
		})
	}
	manager := func(name string, fn func(im *installer.Manager, a args) (any, error)) {
		r.Handle("im."+name, func(v any) (any, error) {
			a := argsOf("im."+name, v)
			im, err := receiver[*installer.Manager](a)
			if err != nil {
				return nil, err
			}

			return fn(im, a)
		})
	}

	list("addInstaller", func(im installerList, a args) (any, error) {
		desc := descParam(a, 2)
		inst, _, err := r.installerParam(a, 1, &desc)
		if err != nil {
			return nil, err
		}
		im.AddInstaller(inst)

		return nil, nil
	})
	list("removeInstaller", func(im installerList, a args) (any, error) {
		inst, ok, err := r.installerParam(a, 1, nil)
		if err != nil || !ok {
			return nil, err
		}
		im.RemoveInstaller(inst)

		return nil, nil
	})
	list("getInstaller", func(im installerList, a args) (any, error) {
		inst, err := im.Installer(a.str(1))
		if err != nil {
			return nil, err
		}

		return r.installerValue(inst), nil
	})
	manager("download", func(im *installer.Manager, a args) (any, error) {
		pk, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		p, err := im.Download(pk)
		if err != nil {
			return nil, err
		}

		return r.promiseToPHP(p), nil
	})
	manager("install", func(im *installer.Manager, a args) (any, error) {
		repo, err := param[repository.InstalledRepositoryInterface](a, 1)
		if err != nil {
			return nil, err
		}
		op, err := operationParam[*operation.InstallOperation](a, 2)
		if err != nil {
			return nil, err
		}
		p, err := im.Install(repo, op)
		if err != nil {
			return nil, err
		}

		return r.promiseToPHP(p), nil
	})
	manager("update", func(im *installer.Manager, a args) (any, error) {
		repo, err := param[repository.InstalledRepositoryInterface](a, 1)
		if err != nil {
			return nil, err
		}
		op, err := operationParam[*operation.UpdateOperation](a, 2)
		if err != nil {
			return nil, err
		}
		p, err := im.Update(repo, op)
		if err != nil {
			return nil, err
		}

		return r.promiseToPHP(p), nil
	})
	manager("uninstall", func(im *installer.Manager, a args) (any, error) {
		repo, err := param[repository.InstalledRepositoryInterface](a, 1)
		if err != nil {
			return nil, err
		}
		op, err := operationParam[*operation.UninstallOperation](a, 2)
		if err != nil {
			return nil, err
		}
		p, err := im.Uninstall(repo, op)
		if err != nil {
			return nil, err
		}

		return r.promiseToPHP(p), nil
	})
	manager("markAliasInstalled", func(im *installer.Manager, a args) (any, error) {
		repo, err := param[repository.InstalledRepositoryInterface](a, 1)
		if err != nil {
			return nil, err
		}
		op, err := operationParam[*operation.MarkAliasInstalledOperation](a, 2)
		if err != nil {
			return nil, err
		}

		return nil, im.MarkAliasInstalled(repo, op)
	})
	manager("markAliasUninstalled", func(im *installer.Manager, a args) (any, error) {
		repo, err := param[repository.InstalledRepositoryInterface](a, 1)
		if err != nil {
			return nil, err
		}
		op, err := operationParam[*operation.MarkAliasUninstalledOperation](a, 2)
		if err != nil {
			return nil, err
		}

		return nil, im.MarkAliasUninstalled(repo, op)
	})
	manager("execute", func(im *installer.Manager, a args) (any, error) {
		repo, err := param[repository.InstalledRepositoryInterface](a, 1)
		if err != nil {
			return nil, err
		}
		var ops []operation.Operation
		for i, v := range a.arrayOrEmpty(2).Values() {
			m, ok := v.(*operationMirror)
			if !ok {
				return nil, a.errorf("operation %d is a %T, not an operation maestro knows", i, v)
			}
			ops = append(ops, m.op)
		}

		return nil, im.Execute(repo, ops, a.boolean(3), a.boolean(4), a.boolean(5))
	})
	manager("reset", func(im *installer.Manager, _ args) (any, error) {
		im.Reset()

		return nil, nil
	})
}

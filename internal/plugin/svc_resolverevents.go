// The events of docs/PLUGINS.md's phase 5 (§4.4, §5.8): PRE_POOL_CREATE
// with its Request, PRE_FILE_DOWNLOAD and POST_FILE_DOWNLOAD. Their
// setters are maestro's (`event.*`), so the pool is built from, and the
// download made with, what PHP set.

package plugin

import (
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/repository/composerrepo"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util/http"
)

const classRequest = `Composer\DependencyResolver\Request`

// responseObject is an HTTP response as PHP gets it: a
// Composer\Util\Http\Response built from its data (Maestro\Shim\Values).
type responseObject struct{ res *http.Response }

// PHPOpaque implements php.Opaque.
func (responseObject) PHPOpaque() {}

// EncodeRPC implements rpc.ValueEncoder.
func (v responseObject) EncodeRPC(e *rpc.Encoder) (*php.Array, error) {
	t := rpc.Tag(valueTag, "response", "code", int64(v.res.StatusCode()))
	for _, f := range []struct {
		key string
		v   any
	}{{"url", v.res.URL()}, {"headers", php.StringList(v.res.Headers())}, {"body", v.res.Body()}} {
		enc, err := e.Value(f.v)
		if err != nil {
			return nil, err
		}
		t.Set(f.key, enc)
	}

	return t, nil
}

// downloadContext is a download event's context as PHP holds it: the
// package, or the metadata's ['repository' => ..., 'response' => ...].
func (r *Runtime) downloadContext(ctx any) any {
	switch c := ctx.(type) {
	case nil:
		return nil
	case pkg.PackageInterface:
		return r.packageObject(c)
	case *composerrepo.MetadataContext:
		out := php.NewArray()
		if c.Response != nil {
			out.Set("response", responseObject{c.Response})
		}
		out.Set("repository", r.repositoryObject(c.Repository))

		return out
	case *php.Array:
		return c
	}

	return nil
}

// phase5EventFields adds the fields of the phase 5 events to an event's
// snapshot.
func (r *Runtime) phase5EventFields(s *php.Array, e eventdispatcher.Event) {
	switch e := e.(type) {
	case *eventdispatcher.PrePoolCreateEvent:
		repos := php.NewArrayCap(len(e.Repositories()))
		for _, repo := range e.Repositories() {
			repos.Append(r.repositoryObject(repo))
		}
		s.Set("repositories", repos)
		var request any
		if req, ok := e.Request().(*resolver.Request); ok && req != nil {
			request = r.serviceObject(req, classRequest)
		}
		s.Set("request", request)
		s.Set("acceptableStabilities", orEmptyArray(e.AcceptableStabilities()))
		s.Set("stabilityFlags", orEmptyArray(e.StabilityFlags()))
		s.Set("rootAliases", orEmptyArray(e.RootAliases()))
		s.Set("rootReferences", orEmptyArray(e.RootReferences()))
		s.Set("packages", r.lazyPackageList(e.Packages()))
		s.Set("unacceptableFixedPackages", r.lazyPackageList(e.UnacceptableFixedPackages()))
	case *eventdispatcher.PreFileDownloadEvent:
		var downloader any
		if h, ok := e.HttpDownloader().(*http.HttpDownloader); ok {
			downloader = r.value(h)
		}
		s.Set("httpDownloader", downloader)
		s.Set("processedUrl", e.ProcessedURL())
		s.Set("customCacheKey", nullable(e.CustomCacheKey()))
		s.Set("type", e.Type())
		s.Set("context", r.downloadContext(e.Context()))
		s.Set("transportOptions", orEmptyArray(e.TransportOptions()))
	case *eventdispatcher.PostFileDownloadEvent:
		s.Set("fileName", nullable(e.FileName()))
		s.Set("checksum", nullable(e.Checksum()))
		s.Set("url", e.URL())
		s.Set("context", r.downloadContext(e.Context()))
		s.Set("type", e.Type())
	}
}

func (r *Runtime) registerResolverEvents() {
	eventMethod := func(name string, fn func(e eventdispatcher.Event, a args) error) {
		r.Handle("event."+name, func(v any) (any, error) {
			a := argsOf("event."+name, v)
			e, err := eventParam(a, 0)
			if err != nil {
				return nil, err
			}

			return nil, fn(e, a)
		})
	}
	poolEvent := func(e eventdispatcher.Event, a args) (*eventdispatcher.PrePoolCreateEvent, error) {
		p, ok := e.(*eventdispatcher.PrePoolCreateEvent)
		if !ok {
			return nil, a.errorf("%s is not a PrePoolCreateEvent", e.Class())
		}

		return p, nil
	}
	downloadEvent := func(e eventdispatcher.Event, a args) (*eventdispatcher.PreFileDownloadEvent, error) {
		p, ok := e.(*eventdispatcher.PreFileDownloadEvent)
		if !ok {
			return nil, a.errorf("%s is not a PreFileDownloadEvent", e.Class())
		}

		return p, nil
	}

	eventMethod("setPackages", func(e eventdispatcher.Event, a args) error {
		p, err := poolEvent(e, a)
		if err != nil {
			return err
		}
		packages, err := packagesParam(a, 1)
		if err != nil {
			return err
		}
		p.SetPackages(packages)

		return nil
	})
	eventMethod("setUnacceptableFixedPackages", func(e eventdispatcher.Event, a args) error {
		p, err := poolEvent(e, a)
		if err != nil {
			return err
		}
		packages, err := packagesParam(a, 1)
		if err != nil {
			return err
		}
		p.SetUnacceptableFixedPackages(packages)

		return nil
	})
	eventMethod("setProcessedUrl", func(e eventdispatcher.Event, a args) error {
		d, err := downloadEvent(e, a)
		if err != nil {
			return err
		}
		d.SetProcessedURL(a.str(1))

		return nil
	})
	eventMethod("setCustomCacheKey", func(e eventdispatcher.Event, a args) error {
		d, err := downloadEvent(e, a)
		if err != nil {
			return err
		}
		d.SetCustomCacheKey(nullString(a, 1))

		return nil
	})
	eventMethod("setTransportOptions", func(e eventdispatcher.Event, a args) error {
		d, err := downloadEvent(e, a)
		if err != nil {
			return err
		}
		d.SetTransportOptions(a.arrayOrEmpty(1))

		return nil
	})

	r.registerRequest()
	r.registerTransactions()
}

// registerRequest registers the `request.*` methods: the Request of
// PRE_POOL_CREATE, a proxy of maestro's.
func (r *Runtime) registerRequest() {
	method := func(name string, fn func(req *resolver.Request, a args) (any, error)) {
		r.Handle("request."+name, func(v any) (any, error) {
			a := argsOf("request."+name, v)
			req, err := receiver[*resolver.Request](a)
			if err != nil {
				return nil, err
			}

			return fn(req, a)
		})
	}
	packageMethod := func(name string, fn func(req *resolver.Request, p pkg.PackageInterface) any) {
		method(name, func(req *resolver.Request, a args) (any, error) {
			p, err := packageParam(a, 1)
			if err != nil {
				return nil, err
			}

			return fn(req, p), nil
		})
	}

	method("getRequires", func(req *resolver.Request, _ args) (any, error) {
		out := php.NewArray()
		for name, c := range req.Requires().All() {
			if c == nil {
				out.Set(name, nil)

				continue
			}
			out.Set(name, constraintValue{c})
		}

		return out, nil
	})
	method("getFixedPackages", func(req *resolver.Request, _ args) (any, error) {
		return r.packageList(req.FixedPackages()), nil
	})
	method("getLockedPackages", func(req *resolver.Request, _ args) (any, error) {
		return r.packageList(req.LockedPackages()), nil
	})
	method("getFixedOrLockedPackages", func(req *resolver.Request, _ args) (any, error) {
		return r.packageList(req.FixedOrLockedPackages()), nil
	})
	method("getFixedPackagesMap", func(req *resolver.Request, _ args) (any, error) {
		out := php.NewArray()
		for id, p := range req.FixedPackagesMap() {
			out.Set(int64(id), r.packageObject(p))
		}

		return out, nil
	})
	method("getPresentMap", func(req *resolver.Request, a args) (any, error) {
		present, err := req.PresentMap()
		if err != nil {
			return nil, err
		}
		if !a.boolean(1) {
			return r.packageList(present), nil
		}
		out := php.NewArray()
		for _, p := range present {
			out.Set(int64(p.ID()), r.packageObject(p))
		}

		return out, nil
	})
	method("getLockedRepository", func(req *resolver.Request, _ args) (any, error) {
		if repo := req.LockedRepository(); repo != nil {
			return r.repositoryObject(repo), nil
		}

		return nil, nil
	})
	method("getRestrictedPackages", func(req *resolver.Request, _ args) (any, error) {
		if names := req.RestrictedPackages(); names != nil {
			return php.StringList(names), nil
		}

		return nil, nil
	})
	method("getUpdateAllowList", func(req *resolver.Request, _ args) (any, error) {
		return php.StringList(req.UpdateAllowList()), nil
	})
	method("getUpdateAllowTransitiveDependencies", func(req *resolver.Request, _ args) (any, error) {
		return req.UpdateAllowTransitiveDependencies(), nil
	})
	method("getUpdateAllowTransitiveRootDependencies", func(req *resolver.Request, _ args) (any, error) {
		return req.UpdateAllowTransitiveRootDependencies(), nil
	})
	packageMethod("isFixedPackage", func(req *resolver.Request, p pkg.PackageInterface) any { return req.IsFixedPackage(p) })
	packageMethod("isLockedPackage", func(req *resolver.Request, p pkg.PackageInterface) any { return req.IsLockedPackage(p) })
	packageMethod("fixPackage", func(req *resolver.Request, p pkg.PackageInterface) any { req.FixPackage(p); return nil })
	packageMethod("lockPackage", func(req *resolver.Request, p pkg.PackageInterface) any { req.LockPackage(p); return nil })
	packageMethod("fixLockedPackage", func(req *resolver.Request, p pkg.PackageInterface) any { req.FixLockedPackage(p); return nil })
	packageMethod("unlockPackage", func(req *resolver.Request, p pkg.PackageInterface) any { req.UnlockPackage(p); return nil })
	method("requireName", func(req *resolver.Request, a args) (any, error) {
		var c semver.ConstraintInterface
		if a.has(2) {
			cv, ok := a.at(2).(constraintValue)
			if !ok {
				return nil, a.errorf("param 2 is not a constraint")
			}
			c = cv.c
		}

		return nil, req.RequireName(a.str(1), c)
	})
	method("restrictPackages", func(req *resolver.Request, a args) (any, error) {
		var names []string
		for _, n := range a.arrayOrEmpty(1).Values() {
			names = append(names, php.ToString(n))
		}
		req.RestrictPackages(names)

		return nil, nil
	})
	method("setUpdateAllowList", func(req *resolver.Request, a args) (any, error) {
		var names []string
		for _, n := range a.arrayOrEmpty(1).Values() {
			names = append(names, php.ToString(n))
		}
		req.SetUpdateAllowList(names, int(php.ToInt(a.at(2))))

		return nil, nil
	})
}

// registerTransactions registers the `transaction.*` methods: the
// transactions of maestro's installer events (PRE_OPERATIONS_EXEC).
func (r *Runtime) registerTransactions() {
	// new Transaction($presentPackages, $resultPackages): maestro computes
	// the operations.
	r.Handle("transaction.new", func(v any) (any, error) {
		a := argsOf("transaction.new", v)
		present, err := packagesParam(a, 1)
		if err != nil {
			return nil, err
		}
		result, err := packagesParam(a, 2)
		if err != nil {
			return nil, err
		}
		t := resolver.NewTransaction(present, result)
		o, ok := a.at(0).(*rpc.PHPObject)
		if !ok {
			return nil, a.errorf("param 0 is not an object being constructed in PHP (a %T)", a.at(0))
		}
		conn, err := r.started()
		if err != nil {
			return nil, err
		}
		obj, _ := r.transactionObject(t, t, o.Class).(rpc.Object)
		if err := conn.Adopt(o, obj); err != nil {
			return nil, err
		}
		ops := t.Operations()
		out := php.NewArrayCap(len(ops))
		for _, op := range ops {
			out.Append(r.value(op))
		}

		return out, nil
	})
	r.Handle("transaction.getOperations", func(v any) (any, error) {
		a := argsOf("transaction.getOperations", v)
		var ops []operation.Operation
		switch t := serviceValue(a.at(0)).(type) {
		case *resolver.Transaction:
			ops = t.Operations()
		case *resolver.LockTransaction:
			ops = t.Operations()
		default:
			return nil, a.errorf("param 0 is not a transaction maestro knows")
		}
		out := php.NewArrayCap(len(ops))
		for _, op := range ops {
			out.Append(r.value(op))
		}

		return out, nil
	})
}

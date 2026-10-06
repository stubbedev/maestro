// HTTP, the event loop, downloads and the cache (docs/PLUGINS.md §4.8,
// phase 5): maestro's HttpDownloader (`http.*`), RemoteFilesystem
// (`rfs.*`), Loop (`loop.*`), DownloadManager (`dm.*`) and Cache
// (`cache.*`). Objects PHP constructs become maestro's (Runtime.adopt);
// promises settle in PHP when maestro's do, which happens while maestro's
// event loop runs on the goroutine holding the PHP baton (a Loop::wait()
// from PHP drives it).

package plugin

import (
	"errors"
	"slices"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// responseValue is a response as PHP rebuilds its Response: url, code,
// headers and body (null for a copy to a file).
func responseValue(res *http.Response, copied bool) any {
	if res == nil {
		return nil
	}
	var body any
	if !copied {
		body = res.Body()
	}

	return php.ArrayOf("url", res.URL(), "code", int64(res.StatusCode()), "headers", php.StringList(res.Headers()), "body", body)
}

// httpRuntime is the http.Runtime of downloaders PHP creates.
func (r *Runtime) httpRuntime() http.Runtime {
	if f := r.composerFactory; f != nil && f.Runtime != nil {
		return f.Runtime
	}

	return &http.StaticRuntime{}
}

// downloaderObject is a downloader as PHP gets it: a proxy of its class.
func (r *Runtime) downloaderObject(d downloader.Downloader) any {
	if d == nil {
		return nil
	}
	if p, ok := d.(*proxyDownloader); ok {
		return p.obj
	}
	if s, ok := d.(*subclassDownloader); ok {
		// A subclass written in PHP: its object (adopted for the
		// downloader it extends).
		d = s.Downloader
	}
	class := `Composer\Downloader\DownloaderInterface`
	if c, ok := d.(downloader.Classer); ok {
		class = c.Class()
	}

	return r.serviceObject(d, class)
}

func (r *Runtime) registerHTTP() {
	configParam := func(a args, i int) (*config.Config, error) { return param[*config.Config](a, i) }

	r.Handle("http.new", func(v any) (any, error) {
		a := argsOf("http.new", v)
		out, _, err := r.ioParam(a, 1)
		if err != nil {
			return nil, err
		}
		cfg, err := configParam(a, 2)
		if err != nil {
			return nil, err
		}
		h, err := http.NewHttpDownloader(out, cfg.ForHTTP(), a.arrayOrEmpty(3), a.boolean(4), r.httpRuntime())
		if err != nil {
			return nil, err
		}

		return nil, r.adopt(a, h)
	})
	httpMethod := func(name string, fn func(h *http.HttpDownloader, a args) (any, error)) {
		r.Handle("http."+name, func(v any) (any, error) {
			a := argsOf("http."+name, v)
			h, err := receiver[*http.HttpDownloader](a)
			if err != nil {
				return nil, err
			}

			return fn(h, a)
		})
	}
	httpMethod("get", func(h *http.HttpDownloader, a args) (any, error) {
		res, err := h.Get(a.str(1), a.arrayOrEmpty(2))
		if err != nil {
			return nil, err
		}

		return responseValue(res, false), nil
	})
	httpMethod("copy", func(h *http.HttpDownloader, a args) (any, error) {
		res, err := h.Copy(a.str(1), a.str(2), a.arrayOrEmpty(3))
		if err != nil {
			return nil, err
		}

		return responseValue(res, true), nil
	})
	httpMethod("add", func(h *http.HttpDownloader, a args) (any, error) {
		p, err := h.Add(a.str(1), a.arrayOrEmpty(2))
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, p, func(res *http.Response) any { return responseValue(res, false) }), nil
	})
	httpMethod("addCopy", func(h *http.HttpDownloader, a args) (any, error) {
		p, err := h.AddCopy(a.str(1), a.str(2), a.arrayOrEmpty(3))
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, p, func(res *http.Response) any { return responseValue(res, true) }), nil
	})
	httpMethod("getOptions", func(h *http.HttpDownloader, _ args) (any, error) { return h.Options(), nil })
	httpMethod("setOptions", func(h *http.HttpDownloader, a args) (any, error) {
		h.SetOptions(a.arrayOrEmpty(1))

		return nil, nil
	})
	httpMethod("wait", func(h *http.HttpDownloader, _ args) (any, error) {
		h.Wait()

		return nil, nil
	})
	httpMethod("enableAsync", func(h *http.HttpDownloader, _ args) (any, error) {
		h.EnableAsync()

		return nil, nil
	})
	httpMethod("countActiveJobs", func(h *http.HttpDownloader, _ args) (any, error) {
		return int64(h.CountActiveJobs()), nil
	})
	r.Handle("http.outputWarnings", func(v any) (any, error) {
		a := argsOf("http.outputWarnings", v)
		out, _, err := r.ioParam(a, 0)
		if err != nil {
			return nil, err
		}

		return http.OutputWarnings(out, a.str(1), a.at(2))
	})
	r.Handle("http.getExceptionHints", func(v any) (any, error) {
		a := argsOf("http.getExceptionHints", v)
		e, ok := a.at(0).(error)
		if !ok {
			return nil, nil
		}
		hints := http.GetExceptionHints(e)
		if hints == nil {
			return nil, nil
		}

		return php.StringList(hints), nil
	})

	r.Handle("rfs.new", func(v any) (any, error) {
		a := argsOf("rfs.new", v)
		out, _, err := r.ioParam(a, 1)
		if err != nil {
			return nil, err
		}
		cfg, err := configParam(a, 2)
		if err != nil {
			return nil, err
		}
		fs, err := http.NewRemoteFilesystem(out, cfg.ForHTTP(), a.arrayOrEmpty(3), a.boolean(4), nil)
		if err != nil {
			return nil, err
		}

		return nil, r.adopt(a, fs)
	})
	rfsMethod := func(name string, fn func(fs *http.RemoteFilesystem, a args) (any, error)) {
		r.Handle("rfs."+name, func(v any) (any, error) {
			a := argsOf("rfs."+name, v)
			fs, err := receiver[*http.RemoteFilesystem](a)
			if err != nil {
				return nil, err
			}

			return fn(fs, a)
		})
	}
	rfsMethod("copy", func(fs *http.RemoteFilesystem, a args) (any, error) {
		return fs.Copy(a.str(1), a.str(2), a.str(3), !a.has(4) || a.boolean(4), a.arrayOrEmpty(5))
	})
	rfsMethod("getContents", func(fs *http.RemoteFilesystem, a args) (any, error) {
		return fs.GetContents(a.str(1), a.str(2), !a.has(3) || a.boolean(3), a.arrayOrEmpty(4))
	})
	rfsMethod("getOptions", func(fs *http.RemoteFilesystem, _ args) (any, error) { return fs.Options(), nil })
	// The protected methods a subclass calls (parent::get(), ...).
	rfsMethod("get", func(fs *http.RemoteFilesystem, a args) (any, error) {
		var fileName *string
		if name, ok := a.nullableString(4); ok {
			fileName = &name
		}

		return fs.Get(a.str(1), a.str(2), a.arrayOrEmpty(3), fileName, !a.has(5) || a.boolean(5))
	})
	rfsMethod("getOptionsForUrl", func(fs *http.RemoteFilesystem, a args) (any, error) {
		return fs.OptionsForURL(a.str(1), a.arrayOrEmpty(2)), nil
	})
	rfsMethod("callbackGet", func(fs *http.RemoteFilesystem, a args) (any, error) {
		return nil, fs.CallbackGet(a.integer(1), a.integer(2), a.str(3), a.integer(4), int64(a.integer(5)), int64(a.integer(6)))
	})
	rfsMethod("promptAuthAndRetry", func(fs *http.RemoteFilesystem, a args) (any, error) {
		return nil, fs.PromptAuthAndRetry(a.integer(1), a.str(2), stringList(a.at(3)))
	})
	// Url::sanitize() for getRemoteContents()'s MaxFileSizeExceededException.
	r.Handle("rfs.sanitizeUrl", func(v any) (any, error) {
		return util.SanitizeURLChecked(argsOf("rfs.sanitizeUrl", v).str(0))
	})
	rfsMethod("setOptions", func(fs *http.RemoteFilesystem, a args) (any, error) {
		fs.SetOptions(a.arrayOrEmpty(1))

		return nil, nil
	})
	rfsMethod("isTlsDisabled", func(fs *http.RemoteFilesystem, _ args) (any, error) { return fs.IsTLSDisabled(), nil })
	rfsMethod("getLastHeaders", func(fs *http.RemoteFilesystem, _ args) (any, error) {
		return php.StringList(fs.LastHeaders()), nil
	})

	r.Handle("loop.new", func(v any) (any, error) {
		a := argsOf("loop.new", v)
		h, err := param[*http.HttpDownloader](a, 1)
		if err != nil {
			return nil, err
		}

		return nil, r.adopt(a, http.NewLoop(h, nil))
	})
	loopMethod := func(name string, fn func(l *http.Loop, a args) (any, error)) {
		r.Handle("loop."+name, func(v any) (any, error) {
			a := argsOf("loop."+name, v)
			l, err := receiver[*http.Loop](a)
			if err != nil {
				return nil, err
			}

			return fn(l, a)
		})
	}
	loopMethod("getHttpDownloader", func(l *http.Loop, _ args) (any, error) { return r.value(l.HttpDownloader()), nil })
	loopMethod("hasProcessExecutor", func(l *http.Loop, _ args) (any, error) { return l.ProcessExecutor() != nil, nil })
	loopMethod("countJobs", func(l *http.Loop, _ args) (any, error) {
		n, err := l.CountActiveJobs()

		return int64(n), err
	})
	// wait($promises): PHP checks its promises itself (React's all()), as
	// Composer's Loop does; maestro runs its loop until no job is left,
	// settling the promises of its work, PHP's included.
	loopMethod("wait", func(l *http.Loop, _ args) (any, error) {
		return nil, l.Wait(nil, nil)
	})
	loopMethod("abortJobs", func(l *http.Loop, _ args) (any, error) {
		l.AbortJobs()

		return nil, nil
	})
	// The ProcessExecutor PHP has for the loop (getProcessExecutor(), or
	// the one given to new Loop()): its asynchronous processes run in PHP,
	// as Composer runs them, and maestro's loop drives and counts them
	// whenever it waits, as Composer's loop does the processes of its one
	// executor.
	loopMethod("phpProcessExecutor", func(l *http.Loop, a args) (any, error) {
		obj, ok := a.at(1).(*rpc.PHPObject)
		if !ok {
			return nil, a.errorf("param 1 is a %T, not a PHP object", a.at(1))
		}
		r.phpObjs.mu.Lock()
		defer r.phpObjs.mu.Unlock()
		if r.phpObjs.executors == nil {
			r.phpObjs.executors = map[*rpc.PHPObject]*phpProcessJobs{}
		}
		src, ok := r.phpObjs.executors[obj]
		if !ok {
			src = &phpProcessJobs{r: r, obj: obj}
			r.phpObjs.executors[obj] = src
		}
		if !slices.Contains(src.loops, l) {
			src.loops = append(src.loops, l)
			l.AddJobSource(src)
		}

		return nil, nil
	})
	// executeAsync() queued a job on a PHP executor: the loops it belongs
	// to poll it until it has none left.
	r.Handle("proc.asyncStarted", func(v any) (any, error) {
		obj, _ := argsOf("proc.asyncStarted", v).at(0).(*rpc.PHPObject)
		r.phpObjs.mu.Lock()
		defer r.phpObjs.mu.Unlock()
		if src, ok := r.phpObjs.executors[obj]; ok {
			src.active = true
		}

		return nil, nil
	})

	r.registerDownloadManager()
	r.registerCache()
}

func (r *Runtime) registerDownloadManager() {
	dmMethod := func(name string, fn func(dm *downloader.DownloadManager, a args) (any, error)) {
		r.Handle("dm."+name, func(v any) (any, error) {
			a := argsOf("dm."+name, v)
			dm, err := receiver[*downloader.DownloadManager](a)
			if err != nil {
				return nil, err
			}

			return fn(dm, a)
		})
	}
	optionalPackage := func(a args, i int) (pkg.PackageInterface, error) {
		if !a.has(i) {
			return nil, nil
		}

		return packageParam(a, i)
	}
	pathValue := func(path string) any { return path }

	dmMethod("setPreferSource", func(dm *downloader.DownloadManager, a args) (any, error) {
		dm.SetPreferSource(a.boolean(1))

		return nil, nil
	})
	dmMethod("resolvePackageInstallPreference", func(dm *downloader.DownloadManager, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}

		return dm.ResolvePackageInstallPreference(p)
	})
	dmMethod("setPreferDist", func(dm *downloader.DownloadManager, a args) (any, error) {
		dm.SetPreferDist(a.boolean(1))

		return nil, nil
	})
	dmMethod("setPreferences", func(dm *downloader.DownloadManager, a args) (any, error) {
		_, err := dm.SetPreferences(a.arrayOrEmpty(1))

		return nil, err
	})
	dmMethod("setSourceFallback", func(dm *downloader.DownloadManager, a args) (any, error) {
		dm.SetSourceFallback(a.boolean(1))

		return nil, nil
	})
	dmMethod("setDownloader", func(dm *downloader.DownloadManager, a args) (any, error) {
		var d downloader.Downloader
		if o, ok := a.at(2).(*rpc.PHPObject); ok {
			// A downloader written in PHP.
			d = r.phpDownloader(o)
		} else {
			var err error
			if d, err = param[downloader.Downloader](a, 2); err != nil {
				return nil, err
			}
			// A FileDownloader subclass written in PHP: its overrides.
			d = r.managedDownloader(d)
		}
		dm.SetDownloader(a.str(1), d)

		return nil, nil
	})
	dmMethod("getDownloader", func(dm *downloader.DownloadManager, a args) (any, error) {
		d, err := dm.Downloader(a.str(1))
		if err != nil {
			return nil, err
		}

		return r.downloaderObject(d), nil
	})
	dmMethod("getDownloaderForPackage", func(dm *downloader.DownloadManager, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		d, err := dm.DownloaderForPackage(p)
		if err != nil {
			return nil, err
		}

		return r.downloaderObject(d), nil
	})
	dmMethod("getDownloaderType", func(dm *downloader.DownloadManager, a args) (any, error) {
		d, err := param[downloader.Downloader](a, 1)
		if err != nil {
			return nil, err
		}
		typ, ok := dm.DownloaderType(r.managedDownloader(d))
		if !ok {
			return nil, &util.InvalidArgumentError{Message: "Unknown downloader"}
		}

		return typ, nil
	})
	dmMethod("download", func(dm *downloader.DownloadManager, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		prev, err := optionalPackage(a, 3)
		if err != nil {
			return nil, err
		}
		promise, err := dm.Download(p, a.str(2), prev)
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, promise, pathValue), nil
	})
	dmMethod("prepare", func(dm *downloader.DownloadManager, a args) (any, error) {
		p, err := packageParam(a, 2)
		if err != nil {
			return nil, err
		}
		prev, err := optionalPackage(a, 4)
		if err != nil {
			return nil, err
		}
		promise, err := dm.Prepare(a.str(1), p, a.str(3), prev)
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, promise, nil), nil
	})
	dmMethod("install", func(dm *downloader.DownloadManager, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		promise, err := dm.Install(p, a.str(2))
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, promise, nil), nil
	})
	dmMethod("update", func(dm *downloader.DownloadManager, a args) (any, error) {
		initial, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		target, err := packageParam(a, 2)
		if err != nil {
			return nil, err
		}
		promise, err := dm.Update(initial, target, a.str(3))
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, promise, nil), nil
	})
	dmMethod("remove", func(dm *downloader.DownloadManager, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		promise, err := dm.Remove(p, a.str(2))
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, promise, nil), nil
	})
	dmMethod("cleanup", func(dm *downloader.DownloadManager, a args) (any, error) {
		p, err := packageParam(a, 2)
		if err != nil {
			return nil, err
		}
		prev, err := optionalPackage(a, 4)
		if err != nil {
			return nil, err
		}
		promise, err := dm.Cleanup(a.str(1), p, a.str(3), prev)
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, promise, nil), nil
	})
}

func (r *Runtime) registerCache() {
	r.Handle("cache.new", func(v any) (any, error) {
		a := argsOf("cache.new", v)
		out, _, err := r.ioParam(a, 1)
		if err != nil {
			return nil, err
		}
		c, err := cache.New(out, a.str(2), a.str(3), util.NewFilesystem(nil), a.boolean(4))
		if err != nil {
			return nil, err
		}

		return nil, r.adopt(a, c)
	})
	r.Handle("cache.isUsable", func(v any) (any, error) {
		return cache.IsUsable(argsOf("cache.isUsable", v).str(0)), nil
	})
	method := func(name string, fn func(c *cache.Cache, a args) (any, error)) {
		r.Handle("cache."+name, func(v any) (any, error) {
			a := argsOf("cache."+name, v)
			c, err := receiver[*cache.Cache](a)
			if err != nil {
				return nil, err
			}

			return fn(c, a)
		})
	}
	// orFalse is a string or PHP's false.
	orFalse := func(s string, ok bool, err error) (any, error) {
		if err != nil || !ok {
			return false, err
		}

		return s, nil
	}

	method("setReadOnly", func(c *cache.Cache, a args) (any, error) {
		c.SetReadOnly(a.boolean(1))

		return nil, nil
	})
	method("isReadOnly", func(c *cache.Cache, _ args) (any, error) { return c.IsReadOnly(), nil })
	method("isEnabled", func(c *cache.Cache, _ args) (any, error) { return c.IsEnabled(), nil })
	method("getRoot", func(c *cache.Cache, _ args) (any, error) { return c.Root(), nil })
	method("read", func(c *cache.Cache, a args) (any, error) { return orFalse(c.Read(a.str(1))) })
	method("write", func(c *cache.Cache, a args) (any, error) { return c.Write(a.str(1), a.str(2)) })
	method("copyFrom", func(c *cache.Cache, a args) (any, error) { return c.CopyFrom(a.str(1), a.str(2)) })
	method("copyTo", func(c *cache.Cache, a args) (any, error) { return c.CopyTo(a.str(1), a.str(2)) })
	method("gcIsNecessary", func(c *cache.Cache, _ args) (any, error) { return c.GcIsNecessary(), nil })
	method("remove", func(c *cache.Cache, a args) (any, error) { return c.Remove(a.str(1)) })
	method("clear", func(c *cache.Cache, _ args) (any, error) { return c.Clear() })
	method("getAge", func(c *cache.Cache, a args) (any, error) {
		age, ok := c.Age(a.str(1))
		if !ok {
			return false, nil
		}

		return age, nil
	})
	method("gc", func(c *cache.Cache, a args) (any, error) { return c.Gc(a.integer(1), int64(a.integer(2))) })
	method("gcVcsCache", func(c *cache.Cache, a args) (any, error) { return c.GcVcsCache(a.integer(1)) })
	method("sha1", func(c *cache.Cache, a args) (any, error) { return orFalse(c.Sha1(a.str(1))) })
	method("sha256", func(c *cache.Cache, a args) (any, error) { return orFalse(c.Sha256(a.str(1))) })
}

// phpProcessJobs is a ProcessExecutor of PHP code as a job source of
// maestro's loops (http.JobSource): its countActiveJobs(), which settles
// the finished processes (their then() callbacks run in PHP), starts
// queued ones and checks their timeouts. It is polled only while it may
// have jobs: from the executeAsync() that queues one until a poll finds
// none.
type phpProcessJobs struct {
	r     *Runtime
	obj   *rpc.PHPObject
	loops []*http.Loop

	active bool // guarded by r.phpObjs.mu
}

// CountActiveJobs implements http.JobSource.
func (p *phpProcessJobs) CountActiveJobs() (int, error) {
	p.r.phpObjs.mu.Lock()
	active := p.active
	p.r.phpObjs.mu.Unlock()
	if !active {
		return 0, nil
	}

	v, err := p.r.callObject(p.obj, "countActiveJobs")
	if errors.Is(err, rpc.ErrBaton) {
		// a wait off the PHP baton: PHP's processes progress when PHP
		// next waits for them
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := php.ToNativeInt(v)
	if n == 0 {
		p.r.phpObjs.mu.Lock()
		p.active = false
		p.r.phpObjs.mu.Unlock()
	}

	return n, nil
}

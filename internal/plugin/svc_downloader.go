// Downloaders in PHP (docs/PLUGINS.md §4.8; phase 6): maestro's
// downloaders (DownloadManager::getDownloader(), getDownloaderForPackage())
// serve their methods (`downloader.*`: download, install, getLocalChanges,
// ...), and `new FileDownloader(...)` (or one of its subclasses without a
// constructor of its own: ZipDownloader, PathDownloader, ...) creates
// maestro's downloader of that class (vaimo/composer-patches builds one).
// A downloader written in PHP can be handed to the DownloadManager
// (setDownloader()): maestro calls its methods in PHP (proxyDownloader).

package plugin

import (
	"os"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util/http"
)

// downloaderConstructors are the downloader classes `new` creates in
// maestro.
var downloaderConstructors = map[string]func(downloader.Deps) (downloader.Downloader, error){
	`Composer\Downloader\FileDownloader`: func(d downloader.Deps) (downloader.Downloader, error) { return downloader.NewFileDownloader(d) },
	`Composer\Downloader\PathDownloader`: func(d downloader.Deps) (downloader.Downloader, error) { return downloader.NewPathDownloader(d) },
	`Composer\Downloader\ZipDownloader`:  func(d downloader.Deps) (downloader.Downloader, error) { return downloader.NewZipDownloader(d) },
	`Composer\Downloader\RarDownloader`:  func(d downloader.Deps) (downloader.Downloader, error) { return downloader.NewRarDownloader(d) },
	`Composer\Downloader\TarDownloader`:  func(d downloader.Deps) (downloader.Downloader, error) { return downloader.NewTarDownloader(d) },
	`Composer\Downloader\GzipDownloader`: func(d downloader.Deps) (downloader.Downloader, error) { return downloader.NewGzipDownloader(d) },
	`Composer\Downloader\XzDownloader`:   func(d downloader.Deps) (downloader.Downloader, error) { return downloader.NewXzDownloader(d) },
	`Composer\Downloader\PharDownloader`: func(d downloader.Deps) (downloader.Downloader, error) { return downloader.NewPharDownloader(d) },
}

// newDownloader serves `downloader.new`: new FileDownloader($io, $config,
// $httpDownloader, $eventDispatcher, $cache, $filesystem, $process).
func (r *Runtime) newDownloader(a args) (any, error) {
	o, ok := a.at(0).(*rpc.PHPObject)
	if !ok {
		return nil, a.errorf("param 0 is not an object being constructed in PHP (a %T)", a.at(0))
	}
	ctor, ok := downloaderConstructors[o.Class]
	var overrides []string
	if !ok {
		// A subclass written in PHP: maestro's downloader of the
		// Composer class it extends, with its overrides.
		var err error
		if ctor, overrides, err = r.subclassDownloaderBase(o); err != nil {
			return nil, err
		}
	}
	out, _, err := r.ioParam(a, 1)
	if err != nil {
		return nil, err
	}
	cfg, err := param[*config.Config](a, 2)
	if err != nil {
		return nil, err
	}
	hd, err := param[*http.HttpDownloader](a, 3)
	if err != nil {
		return nil, err
	}
	// $process ?? new ProcessExecutor($io), and $filesystem ?? new
	// Filesystem($this->process): the ones PHP gives are honoured (a
	// loop's executor runs remove()'s asynchronous rm)
	process, err := r.processParam(a, 7, out)
	if err != nil {
		return nil, err
	}
	fs, err := r.filesystemOf(a.at(6), process)
	if err != nil {
		return nil, err
	}
	deps := downloader.Deps{
		IO:             out,
		Config:         cfg.ForHTTP(),
		HTTPDownloader: hd,
		Filesystem:     fs,
		Process:        process,
		Metadata:       downloader.NewMetadata(),
	}
	if crt := r.frameRuntime(); crt != nil {
		deps.IniFiles = crt.Environment().IniFiles
		deps.ExtensionLoaded = crt.Environment().ExtensionLoaded
	}
	if a.has(4) {
		ed, err := param[*eventdispatcher.EventDispatcher](a, 4)
		if err != nil {
			return nil, err
		}
		deps.EventDispatcher = ed
	}
	if a.has(5) {
		c, err := param[*cache.Cache](a, 5)
		if err != nil {
			return nil, err
		}
		deps.Cache = c
		method, err := store.ParseMethod(os.Getenv(store.MethodEnv))
		if err != nil {
			return nil, err
		}
		st, err := store.Open(cache.Store(), &store.Options{Method: method})
		if err != nil {
			return nil, err
		}
		deps.Store = st
	}
	d, err := ctor(deps)
	if err != nil {
		return nil, err
	}
	if err := r.adopt(a, d); err != nil {
		return nil, err
	}
	r.adoptSubclassDownloader(o, d, overrides)

	return nil, nil
}

// subclassDownloaderBase asks PHP for the classes a downloader written in
// PHP extends and the methods it overrides (`downloader.describe`): the
// constructor of the nearest Composer class maestro has, and the
// overrides.
func (r *Runtime) subclassDownloaderBase(o *rpc.PHPObject) (func(downloader.Deps) (downloader.Downloader, error), []string, error) {
	v, err := r.Call("downloader.describe", php.ArrayOf("object", o))
	if err != nil {
		return nil, nil, err
	}
	d, _ := v.(*php.Array)
	if d == nil {
		return nil, nil, &rpc.ProtocolError{Message: "no description of " + o.Class}
	}
	parents, _ := d.Get("parents")
	for _, class := range stringList(parents) {
		if ctor, ok := downloaderConstructors[class]; ok {
			overrides, _ := d.Get("overrides")

			return ctor, stringList(overrides), nil
		}
	}

	return nil, nil, unsupportedf("maestro does not support creating a %s in plugins yet", o.Class)
}

func (r *Runtime) registerDownloaders() {
	r.Handle("downloader.new", func(v any) (any, error) { return r.newDownloader(argsOf("downloader.new", v)) })

	method := func(name string, fn func(d downloader.Downloader, a args) (any, error)) {
		r.Handle("downloader."+name, func(v any) (any, error) {
			a := argsOf("downloader."+name, v)
			d, err := receiver[downloader.Downloader](a)
			if err != nil {
				return nil, err
			}

			return fn(d, a)
		})
	}
	optionalPackage := func(a args, i int) (pkg.PackageInterface, error) {
		if !a.has(i) {
			return nil, nil
		}

		return packageParam(a, i)
	}
	nullString := func(s pkg.NullString, err error) (any, error) {
		if err != nil || !s.Valid {
			return nil, err
		}

		return s.S, nil
	}
	pathValue := func(path string) any { return path }

	method("getInstallationSource", func(d downloader.Downloader, _ args) (any, error) { return d.InstallationSource(), nil })
	method("download", func(d downloader.Downloader, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		prev, err := optionalPackage(a, 3)
		if err != nil {
			return nil, err
		}
		promise, err := ownDownload(d, p, a.str(2), prev, !a.has(4) || a.boolean(4))
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, promise, pathValue), nil
	})
	method("prepare", func(d downloader.Downloader, a args) (any, error) {
		p, err := packageParam(a, 2)
		if err != nil {
			return nil, err
		}
		prev, err := optionalPackage(a, 4)
		if err != nil {
			return nil, err
		}
		promise, err := d.Prepare(a.str(1), p, a.str(3), prev)
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, promise, nil), nil
	})
	method("install", func(d downloader.Downloader, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		promise, err := ownInstall(d, p, a.str(2), !a.has(3) || a.boolean(3))
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, promise, nil), nil
	})
	method("update", func(d downloader.Downloader, a args) (any, error) {
		initial, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		target, err := packageParam(a, 2)
		if err != nil {
			return nil, err
		}
		promise, err := d.Update(initial, target, a.str(3))
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, promise, nil), nil
	})
	method("remove", func(d downloader.Downloader, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		promise, err := ownRemove(d, p, a.str(2), !a.has(3) || a.boolean(3))
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, promise, nil), nil
	})
	method("cleanup", func(d downloader.Downloader, a args) (any, error) {
		p, err := packageParam(a, 2)
		if err != nil {
			return nil, err
		}
		prev, err := optionalPackage(a, 4)
		if err != nil {
			return nil, err
		}
		promise, err := d.Cleanup(a.str(1), p, a.str(3), prev)
		if err != nil {
			return nil, err
		}

		return promiseValueToPHP(r, promise, nil), nil
	})
	method("getLocalChanges", func(d downloader.Downloader, a args) (any, error) {
		cr, ok := d.(downloader.ChangeReporter)
		if !ok {
			return nil, unsupportedf("maestro's %T reports no local changes", d)
		}
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nullString(cr.LocalChanges(p, a.str(2)))
	})
	method("getUnpushedChanges", func(d downloader.Downloader, a args) (any, error) {
		dv, ok := d.(downloader.DvcsDownloader)
		if !ok {
			return nil, unsupportedf("maestro's %T reports no unpushed changes", d)
		}
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nullString(dv.UnpushedChanges(p, a.str(2)))
	})
	method("getVcsReference", func(d downloader.Downloader, a args) (any, error) {
		vc, ok := d.(downloader.VcsCapableDownloader)
		if !ok {
			return nil, unsupportedf("maestro's %T has no VCS reference", d)
		}
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nullString(vc.VcsReference(p, a.str(2)))
	})
}

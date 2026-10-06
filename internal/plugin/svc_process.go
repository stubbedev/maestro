// ProcessExecutors and Filesystems PHP code gives maestro's services
// (docs/PLUGINS.md §4.8).

package plugin

import (
	"slices"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// processExecutorOf is maestro's ProcessExecutor standing for v, a
// ProcessExecutor PHP code gives one of maestro's services (`new
// FileDownloader(..., $process)`, `new RepositoryManager(..., $process)`,
// $factory->createDownloadManager(..., $process), ...), which Composer's
// service then runs its processes on; nil for null (the service creates
// its own, as Composer's `$process ?? new ProcessExecutor($io)` does).
//
// The processes of PHP code itself run in PHP (Symfony Process, as in
// Composer), so maestro's service gets an executor of its own that
// behaves as the given one: a loop's executor (getProcessExecutor(), or
// the one given to new Loop()) is that loop's, whose wait() drives its
// asynchronous jobs, as Composer's FileDownloader::remove() needs
// (Filesystem::removeDirectoryAsync()); any other one writes to the same
// IO, and runs asynchronous jobs when enableAsync() was called on it. The
// same object always stands for the same executor.
func (r *Runtime) processExecutorOf(v any) (*util.ProcessExecutor, error) {
	switch pv := v.(type) {
	case nil:
		return nil, nil
	case *util.ProcessExecutor:
		return pv, nil
	case *rpc.PHPObject:
		return r.phpProcessExecutor(pv)
	}

	return nil, &rpc.ProtocolError{Message: "a ProcessExecutor maestro does not know"}
}

func (r *Runtime) phpProcessExecutor(obj *rpc.PHPObject) (*util.ProcessExecutor, error) {
	r.phpObjs.mu.Lock()
	pe, ok := r.phpObjs.processExecutors[obj]
	var loops []*http.Loop
	if src, isLoops := r.phpObjs.executors[obj]; isLoops {
		loops = slices.Clone(src.loops)
	}
	r.phpObjs.mu.Unlock()
	if ok {
		return pe, nil
	}

	if len(loops) > 0 && loops[0].ProcessExecutor() != nil {
		// maestro's loop: its executor runs Composer's processes
		pe = loops[0].ProcessExecutor()
	} else {
		d, err := r.Call("proc.describe", php.ArrayOf("object", obj))
		if err != nil {
			return nil, err
		}
		desc, _ := d.(*php.Array)
		if desc == nil {
			return nil, &rpc.ProtocolError{Message: "no description of " + obj.Class}
		}
		var pio util.IO
		ov, _ := desc.Get("io")
		out, ok, err := r.ioParam(argsOf("proc.describe", php.ListOf(ov)), 0)
		if err != nil {
			return nil, err
		}
		if ok {
			pio = out
		}
		pe = util.NewProcessExecutor(pio)
		if async, _ := desc.Get("async"); len(loops) > 0 {
			// a loop created in PHP: its wait() drives the jobs
			pe.SetScheduler(loops[0].HttpDownloader().Scheduler())
			pe.EnableAsync()
		} else if async == true {
			pe.EnableAsync()
		}
	}

	r.phpObjs.mu.Lock()
	defer r.phpObjs.mu.Unlock()
	if r.phpObjs.processExecutors == nil {
		r.phpObjs.processExecutors = map[*rpc.PHPObject]*util.ProcessExecutor{}
	}
	r.phpObjs.processExecutors[obj] = pe

	return pe, nil
}

// filesystemOf is maestro's Filesystem standing for v, a Filesystem PHP
// code gives one of maestro's services: one on the ProcessExecutor it was
// given (processExecutorOf), or, for null, Composer's `$filesystem ?? new
// Filesystem($process)`.
func (r *Runtime) filesystemOf(v any, process *util.ProcessExecutor) (*util.Filesystem, error) {
	obj, ok := v.(*rpc.PHPObject)
	if !ok {
		return util.NewFilesystem(process), nil
	}
	d, err := r.Call("fs.describe", php.ArrayOf("object", obj))
	if err != nil {
		return nil, err
	}
	var executor any
	if desc, ok := d.(*php.Array); ok {
		executor, _ = desc.Get("executor")
	}
	pe, err := r.processExecutorOf(executor)
	if err != nil {
		return nil, err
	}

	// a Filesystem given none creates `new ProcessExecutor()` when it
	// needs one, as util's does
	return util.NewFilesystem(pe), nil
}

// processParam is processExecutorOf of param i, or a new ProcessExecutor
// on out for null.
func (r *Runtime) processParam(a args, i int, out util.IO) (*util.ProcessExecutor, error) {
	pe, err := r.processExecutorOf(a.at(i))
	if err != nil || pe != nil {
		return pe, err
	}

	return util.NewProcessExecutor(out), nil
}

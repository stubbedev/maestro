// How maestro's objects cross to PHP (docs/PLUGINS.md §5.3): services as
// proxies of their Composer class, data as mirrors. Each Go object has one
// PHP object, so identity holds.

package plugin

import (
	"sync"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/archiver"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util/http"
)

// The PHP classes of maestro's services.
const (
	classComposer            = `Composer\Composer`
	classPartialComposer     = `Composer\PartialComposer`
	classConfig              = `Composer\Config`
	classEventDispatcher     = `Composer\EventDispatcher\EventDispatcher`
	classRepositoryManager   = `Composer\Repository\RepositoryManager`
	classInstallationManager = `Composer\Installer\InstallationManager`
	classLocker              = `Composer\Package\Locker`
	classAutoloadGenerator   = `Composer\Autoload\AutoloadGenerator`
	classDownloadManager     = `Composer\Downloader\DownloadManager`
	classLoop                = `Composer\Util\Loop`
	classArchiveManager      = `Composer\Package\Archiver\ArchiveManager`
	classJsonFile            = `Composer\Json\JsonFile`
	classHttpDownloader      = `Composer\Util\HttpDownloader`
)

// serviceObject returns the proxy of the Go service v of PHP class class
// (nil for nil).
func (r *Runtime) serviceObject(v any, class string) any {
	if v == nil || !hashable(v) {
		return nil
	}

	return r.bridge.object(v, func() rpc.Object { return &service{v: v, class: class} })
}

// value returns the PHP form of a Go object of the Composer API, or v
// itself for plain values.
func (r *Runtime) value(v any) any {
	switch v := v.(type) {
	case nil:
		return nil
	case *composer.Composer:
		if v == nil {
			return nil
		}

		return r.serviceObject(v, classComposer)
	case *composer.PartialComposer:
		if v == nil {
			return nil
		}

		return r.serviceObject(v, classPartialComposer)
	case *config.Config:
		if v == nil {
			return nil
		}

		return r.configObject(v)
	case *eventdispatcher.EventDispatcher:
		if v == nil {
			return nil
		}

		return r.eventDispatcherObject(v)
	case *composer.Installer:
		return r.installerObject(v)
	case *repository.RepositoryManager:
		if v == nil {
			return nil
		}

		return r.serviceObject(v, classRepositoryManager)
	case *locker.Locker:
		if v == nil {
			return nil
		}

		return r.serviceObject(v, classLocker)
	case *autoload.Generator:
		if v == nil {
			return nil
		}

		return r.serviceObject(v, classAutoloadGenerator)
	case *downloader.DownloadManager:
		if v == nil {
			return nil
		}

		return r.serviceObject(v, classDownloadManager)
	case *http.Loop:
		if v == nil {
			return nil
		}

		return r.serviceObject(v, classLoop)
	case *http.HttpDownloader:
		if v == nil {
			return nil
		}

		return r.serviceObject(v, classHttpDownloader)
	case *archiver.ArchiveManager:
		if v == nil {
			return nil
		}

		return r.serviceObject(v, classArchiveManager)
	case *Manager:
		if v == nil {
			return nil
		}

		return v
	case pkg.PackageInterface:
		return r.packageObject(v)
	case composer.InstallationManager:
		return r.serviceObject(v, classInstallationManager)
	case io.IO:
		return r.ioObject(v)
	case *advisory.AuditConfig:
		return r.auditConfigObject(v)
	case eventdispatcher.Event:
		return r.eventObject(v)
	case operation.Operation:
		return r.operationObject(v)
	case pkg.Repository:
		return r.repositoryObject(v)
	case *resolver.LockTransaction:
		if v == nil {
			return nil
		}

		return r.transactionObject(v, &v.Transaction, `Composer\DependencyResolver\LockTransaction`)
	case *resolver.Transaction:
		if v == nil {
			return nil
		}

		return r.transactionObject(v, v, `Composer\DependencyResolver\Transaction`)
	}

	return v
}

// ioMirror is maestro's IO as PHP mirrors it: a ConsoleIO, BufferIO or
// NullIO whose flags are fields (Maestro\Shim\Adapter\IOAdapter).
type ioMirror struct {
	io io.IO
	r  *Runtime

	mu   sync.Mutex
	last ioState
	rev  uint64
}

type ioState struct {
	interactive, decorated, verbose, veryVerbose, debug bool
}

func (m *ioMirror) state() ioState {
	return ioState{
		interactive: m.io.IsInteractive(),
		decorated:   m.io.IsDecorated(),
		verbose:     m.io.IsVerbose(),
		veryVerbose: m.io.IsVeryVerbose(),
		debug:       m.io.IsDebug(),
	}
}

// PHPOpaque implements php.Opaque.
func (*ioMirror) PHPOpaque() {}

// PHPClass implements rpc.Object.
func (m *ioMirror) PHPClass() string {
	switch m.io.(type) {
	case *io.NullIO:
		return `Composer\IO\NullIO`
	case *io.BufferIO:
		return `Composer\IO\BufferIO`
	}

	return `Composer\IO\ConsoleIO`
}

// MirrorBase implements rpc.Mirror.
func (*ioMirror) MirrorBase() string { return `Composer\IO\BaseIO` }

// Rev implements rpc.Mirror: it moves when one of the flags changed since
// it was last asked.
func (m *ioMirror) Rev() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	if st := m.state(); st != m.last {
		m.last = st
		m.rev++
	}

	return m.rev
}

// MirrorSnapshot implements rpc.Mirror. A ConsoleIO also carries its
// protected input and output (docs/PLUGINS.md §5.9, §5.12: the run's
// input and output mirrors; bamarni/composer-bin-plugin reads them).
func (m *ioMirror) MirrorSnapshot() (*php.Array, error) {
	st := m.state()

	s := php.ArrayOf(
		"interactive", st.interactive,
		"decorated", st.decorated,
		"verbose", st.verbose,
		"veryVerbose", st.veryVerbose,
		"debug", st.debug,
	)
	if c, ok := m.io.(*io.ConsoleIO); ok && m.r != nil {
		if in := c.ConsoleInput(); in != nil {
			s.Set("input", m.r.inputObject(in))
		}
		if out := c.ConsoleOutput(); out != nil {
			s.Set("output", m.r.outputObject(out))
		}
	}

	return s, nil
}

// ApplyMirror implements rpc.Mirror: PHP changes the IO through its
// methods only.
func (*ioMirror) ApplyMirror(*php.Array) error {
	return &rpc.ProtocolError{Message: "IO fields are not synced from PHP"}
}

// ioObject returns the object an IO crosses to PHP as.
func (r *Runtime) ioObject(v io.IO) any {
	if v == nil {
		return nil
	}
	if !hashable(v) {
		return nil
	}
	m := r.bridge.object(v, func() rpc.Object {
		m := &ioMirror{io: v, r: r}
		m.last = m.state()

		return m
	})

	return m
}

// ioParam returns param i as an IO: maestro's own, or maestro's null IO
// for a PHP NullIO; ok is false for null.
func ioParam(a args, i int) (io.IO, bool, error) {
	switch v := a.at(i).(type) {
	case nil:
		return nil, false, nil
	case *ioMirror:
		return v.io, true, nil
	case *rpc.PHPObject:
		if v.Class == `Composer\IO\NullIO` {
			return io.NewNullIO(), true, nil
		}

		return nil, false, a.errorf("maestro does not support passing a %s created in PHP to maestro yet", v.Class)
	}

	return nil, false, a.errorf("param %d is a %T, not an IO", i, a.at(i))
}

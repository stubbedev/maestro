// The part of Composer\Util\ProcessExecutor PathRepository and the Locker
// use.

package repository

import (
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/vcs"
)

// Process is the part of Composer\Util\ProcessExecutor the repositories
// and the Locker use: what the Git utilities run commands with, plus
// executeAsync for the version guesser. *util.ProcessExecutor and
// *processmock.Mock implement it. When it also has SetMaxJobs,
// ResetMaxJobs and Wait (as *util.ProcessExecutor does), the guesser uses
// them.
type Process interface {
	vcs.Process
	ExecuteAsync(command util.Command, cwd string) (*util.Promise[*util.Process], error)
}

// guesserProcess adapts a Process to the version guesser's
// version.ProcessExecutor.
type guesserProcess struct{ p Process }

var _ version.ProcessExecutor = guesserProcess{}

// NewGuesserProcess adapts p to the ProcessExecutor of
// version.NewVersionGuesser.
func NewGuesserProcess(p Process) version.ProcessExecutor {
	if e, ok := p.(*util.ProcessExecutor); ok {
		return version.NewProcessExecutor(e)
	}

	return guesserProcess{p}
}

func (g guesserProcess) Execute(command []string, output *string, cwd string) (int, error) {
	return g.p.Execute(util.Cmd(command...), output, cwd)
}

func (g guesserProcess) ExecuteAsync(command []string, cwd string) (*util.Promise[version.ProcessResult], error) {
	promise, err := g.p.ExecuteAsync(util.Cmd(command...), cwd)
	if err != nil {
		return nil, err
	}

	return util.Then(promise, func(proc *util.Process) (version.ProcessResult, error) { return proc, nil }), nil
}

func (g guesserProcess) GetErrorOutput() string { return g.p.GetErrorOutput() }

func (g guesserProcess) SplitLines(output string) []string { return util.SplitLines(output) }

func (g guesserProcess) SetMaxJobs(maxJobs int) {
	if p, ok := g.p.(interface{ SetMaxJobs(int) }); ok {
		p.SetMaxJobs(maxJobs)
	}
}

func (g guesserProcess) ResetMaxJobs() {
	if p, ok := g.p.(interface{ ResetMaxJobs() }); ok {
		p.ResetMaxJobs()
	}
}

func (g guesserProcess) Wait() {
	if p, ok := g.p.(interface{ Wait() }); ok {
		p.Wait()
	}
}

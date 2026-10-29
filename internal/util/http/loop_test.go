package http

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
)

// countdownJobs is a job source with n jobs, one finishing per poll.
type countdownJobs struct {
	n, polls int
	err      error
}

func (c *countdownJobs) CountActiveJobs() (int, error) {
	c.polls++
	if c.n > 0 {
		c.n--
	}
	if c.n == 0 && c.err != nil {
		return 0, c.err
	}

	return c.n, nil
}

// progressJobs is a job source whose counts are given, one per poll.
type progressJobs struct{ counts []int }

func (p *progressJobs) CountActiveJobs() (int, error) {
	n := p.counts[0]
	if len(p.counts) > 1 {
		p.counts = p.counts[1:]
	}

	return n, nil
}

func TestLoop_ReportsProgressAndFinalCompletion(t *testing.T) {
	h, _ := newTestDownloader(t, nil, "")
	loop := NewLoop(h, nil)
	loop.AddJobSource(&progressJobs{counts: []int{3, 2, 0}})

	out := console.NewBufferedOutput(console.VerbosityNormal, false, nil)
	progress := console.NewProgressBar(out, 0, console.DefaultMinSecondsBetweenRedraws)

	var percentages []int

	err := loop.WaitProgress(nil, progress, func(percent int) { percentages = append(percentages, percent) })
	if err != nil {
		t.Fatal(err)
	}

	if len(percentages) != 2 || percentages[0] != 33 || percentages[len(percentages)-1] != 100 {
		t.Fatalf("percentages %v", percentages)
	}
}

func TestLoop_DoesNotReportProgressWithoutProgressBar(t *testing.T) {
	h, _ := newTestDownloader(t, nil, "")
	loop := NewLoop(h, nil)
	loop.AddJobSource(&progressJobs{counts: []int{0}})

	err := loop.WaitProgress(nil, nil, func(int) { t.Fatal("Progress is disabled") })
	if err != nil {
		t.Fatal(err)
	}
}

// Wait drives the job sources until they have no job left, as Composer's
// wait() polls its executor's processes, and counts them; what a poll
// throws ends it.
func TestLoop_JobSources(t *testing.T) {
	h, _ := newTestDownloader(t, nil, "")
	loop := NewLoop(h, nil)

	jobs := &countdownJobs{n: 4}
	loop.AddJobSource(jobs)
	if err := loop.Wait(nil, nil); err != nil {
		t.Fatal(err)
	}
	if jobs.n != 0 || jobs.polls != 4 {
		t.Errorf("after Wait: %d jobs left, %d polls", jobs.n, jobs.polls)
	}

	timedOut := errors.New("timed out")
	loop.AddJobSource(&countdownJobs{n: 2, err: timedOut})
	if err := loop.Wait(nil, nil); !errors.Is(err, timedOut) {
		t.Errorf("Wait = %v, want the source's error", err)
	}
}

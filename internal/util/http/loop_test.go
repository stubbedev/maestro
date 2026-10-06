package http

import (
	"errors"
	"testing"
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

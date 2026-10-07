// Ports nothing: how the optimizer and the pool filters split their work
// over several goroutines (deliberate deviation 3, speed).

package resolver

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// minParallelPackages is the number of packages (or groups of them) from
// which the optimizer and the security advisory filter split their work
// over several goroutines (a variable for the tests).
var minParallelPackages = 512

// parallelChunk is the number of items a goroutine of parallelRanges takes
// at a time (a variable for the tests).
var parallelChunk = 64

// parallelRanges runs work over the items [0, n): on the calling goroutine
// when n is below minParallelPackages, else on up to GOMAXPROCS goroutines
// that each take the next parallelChunk items when done with theirs.
// newWorker gives each goroutine its work function, called with
// consecutive ranges [from, to).
//
// Items differ widely in cost and a pool keeps a name's versions together
// (a version replacing a hundred names, as symfony/symfony's do, costs
// a hundred times what a version replacing none does), so ranges fixed
// in advance would leave most goroutines waiting for one.
func parallelRanges(n int, newWorker func() func(from, to int)) {
	chunk := parallelChunk
	workers := min(runtime.GOMAXPROCS(0), (n+chunk-1)/chunk)
	if n < minParallelPackages || workers < 2 {
		if n > 0 {
			newWorker()(0, n)
		}

		return
	}

	var next atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		work := newWorker()
		wg.Go(func() {
			for {
				to := int(next.Add(int64(chunk)))
				from := to - chunk
				if from >= n {
					return
				}
				work(from, min(to, n))
			}
		})
	}
	wg.Wait()
}

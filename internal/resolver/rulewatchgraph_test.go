// Ports tests/Composer/Test/DependencyResolver/RuleWatchChainTest.php.

package resolver

import (
	"slices"
	"strconv"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
)

const watchedLiteral int32 = 1

type watchChainFixture struct {
	graph     *RuleWatchGraph
	decisions *Decisions
	nodes     []*RuleWatchNode
}

func newWatchChainFixture(size int, removed func(int) bool) *watchChainFixture {
	packages := make([]pkg.PackageInterface, 2*size+2)
	for i := range packages {
		packages[i] = pkg.NewPackage("a/p"+strconv.Itoa(i), "1.0.0.0", "1.0.0")
	}
	f := &watchChainFixture{graph: NewRuleWatchGraph(len(packages)), decisions: NewDecisions(NewPool(packages, nil, nil))}
	const satisfied int32 = 2
	if err := f.decisions.Decide(satisfied, 1, nil); err != nil {
		panic(err)
	}
	for i := range size {
		literals := []int32{watchedLiteral, satisfied}
		if removed(i) {
			literals = []int32{watchedLiteral, int32(2*i + 3), int32(2*i + 4)}
		}
		f.nodes = append(f.nodes, NewRuleWatchNode(NewGenericRule(literals, RuleLearned, 0)))
	}
	for _, node := range slices.Backward(f.nodes) {
		f.graph.Insert(node)
	}

	return f
}

func (f *watchChainFixture) chain() []*RuleWatchNode {
	chain := slices.Clone(f.graph.chains[f.graph.slot(watchedLiteral)])
	slices.Reverse(chain)

	return chain
}

func (f *watchChainFixture) propagate(t *testing.T) {
	t.Helper()
	conflict, err := f.graph.PropagateLiteral(-watchedLiteral, 1, f.decisions)
	if err != nil || conflict != nil {
		t.Fatalf("PropagateLiteral: conflict %v, error %v", conflict, err)
	}
}

func TestRuleWatchChain_RemovePreservesIteratorPosition(t *testing.T) {
	for name, c := range map[string]struct {
		size, offset int
		remaining    []int
	}{
		"only element": {1, 0, nil},
		"head":         {4, 0, []int{1, 2, 3}},
		"middle":       {4, 1, []int{0, 2, 3}},
		"before tail":  {4, 2, []int{0, 1, 3}},
		"tail":         {4, 3, []int{0, 1, 2}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newWatchChainFixture(c.size, func(i int) bool { return i == c.offset })

			f.propagate(t)

			want := []*RuleWatchNode{}
			for _, i := range c.remaining {
				want = append(want, f.nodes[i])
			}
			if got := f.chain(); !slices.Equal(got, want) {
				t.Errorf("chain %v, want %v", got, want)
			}
			moved := f.nodes[c.offset]
			if moved.Watch1 != int32(2*c.offset+4) && moved.Watch2 != int32(2*c.offset+4) {
				t.Errorf("removed node watches %d and %d", moved.Watch1, moved.Watch2)
			}
		})
	}
}

func TestRuleWatchChain_RemoveConsecutiveNodesAfterRetainedPrefix(t *testing.T) {
	f := newWatchChainFixture(5, func(i int) bool { return i >= 1 })

	f.propagate(t)

	if got := f.chain(); !slices.Equal(got, []*RuleWatchNode{f.nodes[0]}) {
		t.Errorf("chain %v, want only the first node", got)
	}
}

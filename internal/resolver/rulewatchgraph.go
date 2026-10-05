// Ports src/Composer/DependencyResolver/RuleWatchGraph.php,
// RuleWatchChain.php and RuleWatchNode.php.

package resolver

// RuleWatchNode ports Composer\DependencyResolver\RuleWatchNode: a rule
// and the two literals watched in it.
type RuleWatchNode struct {
	rule           *Rule
	Watch1, Watch2 int32
}

// NewRuleWatchNode is new RuleWatchNode($rule): it watches the first two
// literals.
func NewRuleWatchNode(rule *Rule) *RuleWatchNode {
	n := &RuleWatchNode{rule: rule}
	if len(rule.literals) > 0 {
		n.Watch1 = rule.literals[0]
	}
	if len(rule.literals) > 1 {
		n.Watch2 = rule.literals[1]
	}

	return n
}

// Watch2OnHighest ports watch2OnHighest: watch2 moves to the literal
// decided on the highest level.
func (n *RuleWatchNode) Watch2OnHighest(decisions *Decisions) {
	literals := n.rule.literals

	// if there are only 2 elements, both are being watched anyway
	if len(literals) < 3 || n.rule.kind == kindMultiConflict {
		return
	}

	watchLevel := 0
	for _, literal := range literals {
		if level := decisions.DecisionLevel(literal); level > watchLevel {
			n.Watch2 = literal
			watchLevel = level
		}
	}
}

// Rule ports getRule.
func (n *RuleWatchNode) Rule() *Rule { return n.rule }

// OtherWatch ports getOtherWatch.
func (n *RuleWatchNode) OtherWatch(literal int32) int32 {
	if n.Watch1 == literal {
		return n.Watch2
	}

	return n.Watch1
}

// MoveWatch ports moveWatch.
func (n *RuleWatchNode) MoveWatch(from, to int32) {
	if n.Watch1 == from {
		n.Watch1 = to
	} else {
		n.Watch2 = to
	}
}

// RuleWatchGraph ports Composer\DependencyResolver\RuleWatchGraph: for
// each literal, the nodes watching it.
//
// A chain (RuleWatchChain, an SplDoublyLinkedList the nodes are unshifted
// onto) is stored reversed: the most recently inserted node is last.
type RuleWatchGraph struct {
	chains [][]*RuleWatchNode
}

// NewRuleWatchGraph returns a graph for literals of the packages 1..n.
func NewRuleWatchGraph(n int) *RuleWatchGraph {
	return &RuleWatchGraph{chains: make([][]*RuleWatchNode, 2*(n+1))}
}

// slot maps a literal to its chain.
func (g *RuleWatchGraph) slot(literal int32) int {
	if literal < 0 {
		return int(-literal)*2 + 1
	}

	return int(literal) * 2
}

func (g *RuleWatchGraph) unshift(literal int32, node *RuleWatchNode) {
	s := g.slot(literal)
	if s >= len(g.chains) {
		g.chains = append(g.chains, make([][]*RuleWatchNode, s+1-len(g.chains))...)
	}
	g.chains[s] = append(g.chains[s], node)
}

// Insert ports insert: assertions are not watched, a MultiConflictRule
// watches all its literals.
func (g *RuleWatchGraph) Insert(node *RuleWatchNode) {
	if node.rule.IsAssertion() {
		return
	}

	if node.rule.kind != kindMultiConflict {
		g.unshift(node.Watch1, node)
		g.unshift(node.Watch2, node)
	} else {
		for _, literal := range node.rule.literals {
			g.unshift(literal, node)
		}
	}
}

// PropagateLiteral ports propagateLiteral: it decides what the decision
// of decidedLiteral implies through the rules watching its negation, and
// returns the first rule that conflicts, or nil.
func (g *RuleWatchGraph) PropagateLiteral(decidedLiteral int32, level int, decisions *Decisions) (*Rule, error) {
	// we invert the decided literal here, example:
	// A was decided => (-A|B) now requires B to be true, so we look for
	// rules which are fulfilled by -A, rather than A.
	literal := -decidedLiteral

	s := g.slot(literal)
	if s >= len(g.chains) || len(g.chains[s]) == 0 {
		return nil, nil
	}

	// Walk the chain from its head (the end of the slice). Nodes that move
	// their watch to another literal are dropped by compacting the kept
	// ones towards the end: w is the next free position from the end.
	chain := g.chains[s]
	w := len(chain) - 1
	r := len(chain) - 1
	var conflict *Rule
	var err error
	for ; r >= 0; r-- {
		node := chain[r]
		moved := false
		conflict, moved, err = g.propagateNode(node, literal, level, decisions)
		if err != nil || conflict != nil {
			break
		}
		if !moved {
			chain[w] = node
			w--
		}
	}

	// keep the unvisited nodes (r and below) and the kept ones (above w)
	if w != r {
		kept := len(chain) - 1 - w
		copy(chain[r+1:], chain[w+1:])
		clear(chain[r+1+kept:])
		chain = chain[:r+1+kept]
	}
	g.chains[s] = chain

	return conflict, err
}

// propagateNode handles one node of propagateLiteral's loop; moved reports
// that the node watches another literal now.
func (g *RuleWatchGraph) propagateNode(node *RuleWatchNode, literal int32, level int, decisions *Decisions) (conflict *Rule, moved bool, err error) {
	rule := node.rule
	if rule.kind == kindMultiConflict {
		for _, otherLiteral := range rule.literals {
			if literal != otherLiteral && !decisions.Satisfy(otherLiteral) {
				if decisions.Conflict(otherLiteral) {
					return rule, false, nil
				}
				if err := decisions.Decide(otherLiteral, level, rule); err != nil {
					return nil, false, err
				}
			}
		}

		return nil, false, nil
	}

	otherWatch := node.OtherWatch(literal)
	if rule.disabled || decisions.Satisfy(otherWatch) {
		return nil, false, nil
	}

	for _, ruleLiteral := range rule.literals {
		if ruleLiteral != literal && ruleLiteral != otherWatch && !decisions.Conflict(ruleLiteral) {
			g.moveWatch(literal, ruleLiteral, node)

			return nil, true, nil
		}
	}

	if decisions.Conflict(otherWatch) {
		return rule, false, nil
	}

	return nil, false, decisions.Decide(otherWatch, level, rule)
}

// moveWatch ports moveWatch, without the removal from the source chain,
// which propagateLiteral does.
func (g *RuleWatchGraph) moveWatch(fromLiteral, toLiteral int32, node *RuleWatchNode) {
	node.MoveWatch(fromLiteral, toLiteral)
	g.unshift(toLiteral, node)
}

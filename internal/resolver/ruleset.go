// Ports src/Composer/DependencyResolver/RuleSet.php and RuleSetIterator.php.

package resolver

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/phperr"
)

// The rule types, highest priority (lowest number) first: RuleSet::TYPE_*.
const (
	TypePackage = 0
	TypeRequest = 1
	TypeLearned = 4
)

// ruleTypes are RuleSet::TYPES in order.
var ruleTypes = [...]struct {
	typ  int
	name string
}{{TypePackage, "PACKAGE"}, {TypeRequest, "REQUEST"}, {TypeLearned, "LEARNED"}}

// typeSlot returns the index of typ in ruleTypes, or -1.
func typeSlot(typ int) int {
	for i, t := range ruleTypes {
		if t.typ == typ {
			return i
		}
	}

	return -1
}

// RuleSet ports Composer\DependencyResolver\RuleSet: the rules by type,
// without duplicates, and by id.
type RuleSet struct {
	// RuleByID is $ruleById: the rules in the order they were added.
	RuleByID []*Rule
	rules    [len(ruleTypes)][]*Rule
	byHash   map[uint64]*Rule
}

// NewRuleSet is new RuleSet().
func NewRuleSet() *RuleSet { return &RuleSet{byHash: make(map[uint64]*Rule)} }

// Add ports add: a rule equal to one already in the set is dropped. An
// unknown type is an *OutOfBoundsError.
func (s *RuleSet) Add(rule *Rule, typ int) error {
	slot := typeSlot(typ)
	if slot < 0 {
		return &OutOfBoundsError{Site: phperr.At("RuleSet.php", 65), Message: "Unknown rule type: " + strconv.Itoa(typ)}
	}

	hash := rule.hash()
	// Do not add if rule already exists
	head := s.byHash[hash]
	for potentialDuplicate := head; potentialDuplicate != nil; potentialDuplicate = potentialDuplicate.next {
		if rule.Equals(potentialDuplicate) {
			return nil
		}
	}

	s.rules[slot] = append(s.rules[slot], rule)
	s.RuleByID = append(s.RuleByID, rule)
	rule.SetType(typ)
	rule.next = head
	s.byHash[hash] = rule

	return nil
}

// Count ports count().
func (s *RuleSet) Count() int { return len(s.RuleByID) }

// RuleByIDAt ports ruleById.
func (s *RuleSet) RuleByIDAt(id int) *Rule { return s.RuleByID[id] }

// Rules ports getRules: type => rules, for every type.
func (s *RuleSet) Rules() map[int][]*Rule {
	rules := make(map[int][]*Rule, len(ruleTypes))
	for i, t := range ruleTypes {
		rules[t.typ] = s.rules[i]
	}

	return rules
}

// Iterator ports getIterator.
func (s *RuleSet) Iterator() *RuleSetIterator { return NewRuleSetIterator(s.Rules()) }

// IteratorFor ports getIteratorFor.
func (s *RuleSet) IteratorFor(types ...int) *RuleSetIterator {
	rules := make(map[int][]*Rule, len(types))
	for _, typ := range types {
		if slot := typeSlot(typ); slot >= 0 {
			rules[typ] = s.rules[slot]
		} else {
			rules[typ] = nil
		}
	}

	return NewRuleSetIterator(rules)
}

// IteratorWithout ports getIteratorWithout.
func (s *RuleSet) IteratorWithout(types ...int) *RuleSetIterator {
	rules := s.Rules()
	for _, typ := range types {
		delete(rules, typ)
	}

	return NewRuleSetIterator(rules)
}

// Types ports getTypes.
func (s *RuleSet) Types() []int {
	types := make([]int, len(ruleTypes))
	for i, t := range ruleTypes {
		types[i] = t.typ
	}

	return types
}

// PrettyString ports getPrettyString; with a nil ctx the rules are
// printed with String.
func (s *RuleSet) PrettyString(ctx *PrettyContext) (string, error) {
	var b strings.Builder
	b.WriteByte('\n')
	for i, t := range ruleTypes {
		b.WriteString(t.name + strings.Repeat(" ", max(0, 8-len(t.name))) + ": ")
		for _, rule := range s.rules[i] {
			if ctx != nil {
				pretty, err := rule.PrettyString(ctx)
				if err != nil {
					return "", err
				}
				b.WriteString(pretty)
			} else {
				b.WriteString(rule.String())
			}
			b.WriteByte('\n')
		}
		b.WriteString("\n\n")
	}

	return b.String(), nil
}

// String ports __toString.
func (s *RuleSet) String() string {
	str, _ := s.PrettyString(nil)

	return str
}

// RuleSetIterator ports Composer\DependencyResolver\RuleSetIterator: it
// walks rules by ascending type.
type RuleSetIterator struct {
	rules             map[int][]*Rule
	types             []int
	currentOffset     int
	currentType       int
	currentTypeOffset int
}

// NewRuleSetIterator is new RuleSetIterator($rules): type => rules.
func NewRuleSetIterator(rules map[int][]*Rule) *RuleSetIterator {
	it := &RuleSetIterator{rules: rules}
	for typ := range rules {
		it.types = append(it.types, typ)
	}
	slices.Sort(it.types)
	it.Rewind()

	return it
}

// Current ports current.
func (it *RuleSetIterator) Current() *Rule { return it.rules[it.currentType][it.currentOffset] }

// Key ports key: the current rule's type.
func (it *RuleSetIterator) Key() int { return it.currentType }

// Next ports next.
func (it *RuleSetIterator) Next() {
	it.currentOffset++

	rules, ok := it.rules[it.currentType]
	if !ok {
		return
	}

	if it.currentOffset >= len(rules) {
		it.currentOffset = 0
		it.advanceType()
	}
}

// advanceType moves to the next type with rules, or past the end.
func (it *RuleSetIterator) advanceType() {
	for {
		it.currentTypeOffset++
		if it.currentTypeOffset >= len(it.types) {
			it.currentType = -1

			return
		}
		it.currentType = it.types[it.currentTypeOffset]
		if len(it.rules[it.currentType]) != 0 {
			return
		}
	}
}

// Rewind ports rewind.
func (it *RuleSetIterator) Rewind() {
	it.currentOffset = 0
	it.currentTypeOffset = -1
	it.currentType = -1
	it.advanceType()
}

// Valid ports valid.
func (it *RuleSetIterator) Valid() bool {
	rules, ok := it.rules[it.currentType]

	return ok && it.currentOffset < len(rules)
}

// Count counts the remaining rules, as count(iterator) would.
func (it *RuleSetIterator) Count() int {
	n := 0
	for _, rules := range it.rules {
		n += len(rules)
	}

	return n
}

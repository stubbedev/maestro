// Ports src/Composer/DependencyResolver/Decisions.php.

package resolver

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/util"
)

// Decision is an entry of the decision queue: a literal and the rule
// that caused it.
type Decision struct {
	Literal int32
	Reason  *Rule
}

// Decisions ports Composer\DependencyResolver\Decisions: the decided
// literals in order, and per package the decision level (positive:
// install, negative: don't install, 0: undecided).
type Decisions struct {
	pool          *Pool
	decisionMap   []int32
	decisionQueue []Decision
}

// NewDecisions is new Decisions($pool).
func NewDecisions(pool *Pool) *Decisions {
	return &Decisions{pool: pool, decisionMap: make([]int32, pool.Count()+1)}
}

// level returns $decisionMap[$packageId] ?? 0.
func (d *Decisions) level(packageID int32) int32 {
	if int(packageID) < len(d.decisionMap) {
		return d.decisionMap[packageID]
	}

	return 0
}

// Decide ports decide.
func (d *Decisions) Decide(literal int32, level int, why *Rule) error {
	if err := d.addDecision(literal, level); err != nil {
		return err
	}
	d.decisionQueue = append(d.decisionQueue, Decision{Literal: literal, Reason: why})

	return nil
}

// Satisfy ports satisfy: whether the literal is decided true.
func (d *Decisions) Satisfy(literal int32) bool {
	if literal > 0 {
		return d.level(literal) > 0
	}

	return d.level(-literal) < 0
}

// Conflict ports conflict: whether the literal is decided false.
func (d *Decisions) Conflict(literal int32) bool {
	if literal > 0 {
		return d.level(literal) < 0
	}

	return d.level(-literal) > 0
}

// Decided ports decided.
func (d *Decisions) Decided(literalOrPackageID int32) bool {
	return d.level(abs32(literalOrPackageID)) != 0
}

// Undecided ports undecided.
func (d *Decisions) Undecided(literalOrPackageID int32) bool {
	return d.level(abs32(literalOrPackageID)) == 0
}

// DecidedInstall ports decidedInstall.
func (d *Decisions) DecidedInstall(literalOrPackageID int32) bool {
	return d.level(abs32(literalOrPackageID)) > 0
}

// DecisionLevel ports decisionLevel.
func (d *Decisions) DecisionLevel(literalOrPackageID int32) int {
	return int(abs32(d.level(abs32(literalOrPackageID))))
}

// DecisionRule ports decisionRule: the rule of the first decision on the
// package.
func (d *Decisions) DecisionRule(literalOrPackageID int32) (*Rule, error) {
	packageID := abs32(literalOrPackageID)
	for _, decision := range d.decisionQueue {
		if packageID == abs32(decision.Literal) {
			return decision.Reason, nil
		}
	}

	return nil, &util.LogicError{Message: "Did not find a decision rule using " + strconv.Itoa(int(literalOrPackageID))}
}

// AtOffset ports atOffset.
func (d *Decisions) AtOffset(queueOffset int) Decision { return d.decisionQueue[queueOffset] }

// ValidOffset ports validOffset.
func (d *Decisions) ValidOffset(queueOffset int) bool {
	return queueOffset >= 0 && queueOffset < len(d.decisionQueue)
}

// LastReason ports lastReason.
func (d *Decisions) LastReason() *Rule { return d.decisionQueue[len(d.decisionQueue)-1].Reason }

// LastLiteral ports lastLiteral.
func (d *Decisions) LastLiteral() int32 { return d.decisionQueue[len(d.decisionQueue)-1].Literal }

// Reset ports reset.
func (d *Decisions) Reset() {
	for _, decision := range d.decisionQueue {
		d.decisionMap[abs32(decision.Literal)] = 0
	}
	d.decisionQueue = d.decisionQueue[:0]
}

// ResetToOffset ports resetToOffset: the decisions after offset are
// undone.
func (d *Decisions) ResetToOffset(offset int) {
	for len(d.decisionQueue) > offset+1 {
		decision := d.decisionQueue[len(d.decisionQueue)-1]
		d.decisionQueue = d.decisionQueue[:len(d.decisionQueue)-1]
		d.decisionMap[abs32(decision.Literal)] = 0
	}
}

// RevertLast ports revertLast.
func (d *Decisions) RevertLast() {
	d.decisionMap[abs32(d.LastLiteral())] = 0
	d.decisionQueue = d.decisionQueue[:len(d.decisionQueue)-1]
}

// Count ports count().
func (d *Decisions) Count() int { return len(d.decisionQueue) }

// IsEmpty ports isEmpty.
func (d *Decisions) IsEmpty() bool { return len(d.decisionQueue) == 0 }

// Reversed iterates the decisions from the last to the first, as foreach
// over Decisions does.
func (d *Decisions) Reversed(yield func(int, Decision) bool) {
	for i, decision := range slices.Backward(d.decisionQueue) {
		if !yield(i, decision) {
			return
		}
	}
}

func (d *Decisions) addDecision(literal int32, level int) error {
	packageID := abs32(literal)

	if previousDecision := d.level(packageID); previousDecision != 0 {
		literalString := d.pool.LiteralToPrettyString(literal, nil)
		p := d.pool.LiteralToPackage(literal)

		return newSolverBugError("Trying to decide " + literalString + " on level " + strconv.Itoa(level) + ", even though " + p.String() + " was previously decided as " + strconv.Itoa(int(previousDecision)) + ".")
	}

	if literal > 0 {
		d.decisionMap[packageID] = literalOf(level)
	} else {
		d.decisionMap[packageID] = -literalOf(level)
	}

	return nil
}

// String ports toString(null): "[id:level,...]" by package id.
func (d *Decisions) String() string { return d.ToString(nil) }

// ToString ports toString($pool): with a pool, the packages instead of
// their ids.
func (d *Decisions) ToString(pool *Pool) string {
	var b strings.Builder
	b.WriteByte('[')
	for id, level := range d.decisionMap {
		if id == 0 || !d.touched(int32(id)) {
			continue
		}
		if pool != nil {
			b.WriteString(pool.LiteralToPackage(int32(id)).String())
		} else {
			b.WriteString(strconv.Itoa(id))
		}
		b.WriteString(":" + strconv.Itoa(int(level)) + ",")
	}
	b.WriteByte(']')

	return b.String()
}

// touched stands for isset($decisionMap[$id]). PHP also lists the
// packages whose decisions were undone (with level 0); this debug output
// leaves them out.
func (d *Decisions) touched(id int32) bool { return d.decisionMap[id] != 0 }

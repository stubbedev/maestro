// Ports src/Composer/DependencyResolver/Solver.php.

package resolver

import (
	"slices"
	"strconv"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/semver"
)

// branch is an entry of Solver::$branches: the alternatives not chosen
// at a decision level (0 marks one taken out by the minimization step).
type branch struct {
	literals []int32
	level    int
}

// Solver ports Composer\DependencyResolver\Solver.
type Solver struct {
	policy         Policy
	pool           *Pool
	rules          *RuleSet
	watchGraph     *RuleWatchGraph
	decisions      *Decisions
	fixedMap       map[int]bool
	propagateIndex int
	branches       []branch
	problems       []*Problem
	learnedPool    [][]*Rule
	learnedWhy     map[*Rule]int
	seen           literalSet
	io             io.IO

	// TestFlagLearnedPositiveLiteral is $testFlagLearnedPositiveLiteral:
	// set when a negative decision is learned as a positive literal.
	TestFlagLearnedPositiveLiteral bool
}

// NewSolver is new Solver($policy, $pool, $io).
func NewSolver(policy Policy, pool *Pool, out io.IO) *Solver {
	return &Solver{policy: policy, pool: pool, io: out}
}

// RuleSetSize ports getRuleSetSize.
func (s *Solver) RuleSetSize() int { return s.rules.Count() }

// Pool ports getPool.
func (s *Solver) Pool() *Pool { return s.pool }

// makeAssertionRuleDecisions ports makeAssertionRuleDecisions (aka
// solver_makeruledecisions).
func (s *Solver) makeAssertionRuleDecisions() error {
	decisionStart := s.decisions.Count() - 1

	rulesCount := s.rules.Count()
	for ruleIndex := 0; ruleIndex < rulesCount; ruleIndex++ {
		rule := s.rules.RuleByID[ruleIndex]

		if !rule.IsAssertion() || rule.IsDisabled() {
			continue
		}

		literal := rule.literals[0]

		if !s.decisions.Decided(literal) {
			if err := s.decisions.Decide(literal, 1, rule); err != nil {
				return err
			}

			continue
		}

		if s.decisions.Satisfy(literal) {
			continue
		}

		// found a conflict
		if rule.Type() == TypeLearned {
			if err := rule.Disable(); err != nil {
				return err
			}

			continue
		}

		conflict, err := s.decisions.DecisionRule(literal)
		if err != nil {
			return err
		}

		if conflict.Type() == TypePackage {
			problem := NewProblem()

			problem.AddRule(rule)
			problem.AddRule(conflict)
			if err := rule.Disable(); err != nil {
				return err
			}
			s.problems = append(s.problems, problem)

			continue
		}

		// conflict with another root require/fixed package
		problem := NewProblem()
		problem.AddRule(rule)
		problem.AddRule(conflict)

		// push all of our rules (can only be root require/fixed package rules)
		// asserting this literal on the problem stack
		for it := s.rules.IteratorFor(TypeRequest); it.Valid(); it.Next() {
			assertRule := it.Current()
			if assertRule.IsDisabled() || !assertRule.IsAssertion() {
				continue
			}

			if abs32(literal) != abs32(assertRule.literals[0]) {
				continue
			}

			problem.AddRule(assertRule)
			if err := assertRule.Disable(); err != nil {
				return err
			}
		}
		s.problems = append(s.problems, problem)

		s.decisions.ResetToOffset(decisionStart)
		ruleIndex = -1
	}

	return nil
}

func (s *Solver) setupFixedMap(request *Request) {
	s.fixedMap = map[int]bool{}
	for _, p := range request.FixedPackages() {
		s.fixedMap[p.ID()] = true
	}
}

// checkForRootRequireProblems ports checkForRootRequireProblems.
func (s *Solver) checkForRootRequireProblems(request *Request, filter version.PlatformRequirementFilter) {
	ignoreList, _ := filter.(version.IgnoreListPlatformRequirementFilter)
	for packageName, constraint := range request.Requires().All() {
		if filter.IsIgnored(packageName) {
			continue
		} else if ignoreList != nil {
			constraint = ignoreList.FilterConstraint(packageName, constraint)
		}

		if len(s.pool.WhatProvides(packageName, constraint)) == 0 {
			problem := NewProblem()
			problem.AddRule(NewGenericRule(nil, RuleRootRequire, &RootRequire{PackageName: packageName, Constraint: constraint}))
			s.problems = append(s.problems, problem)
		}
	}
}

// checkForFilterListRemovedLockedPackages ports
// checkForFilterListRemovedLockedPackages.
func (s *Solver) checkForFilterListRemovedLockedPackages(request *Request) {
	for _, p := range request.LockedPackages() {
		constraint := semver.NewConstraintOp(semver.OpEQ, p.Version())
		if !s.pool.IsFilterListRemovedPackageVersion(p.Name(), constraint) {
			continue
		}

		problem := NewProblem()
		problem.AddRule(NewGenericRule(nil, RuleLockedFilterListRemoved, p))
		s.problems = append(s.problems, problem)
	}
}

// Solve ports solve; filter nil ignores nothing. An unsolvable request is
// a *SolverProblemsError.
func (s *Solver) Solve(request *Request, filter version.PlatformRequirementFilter) (*LockTransaction, error) {
	if filter == nil {
		filter = ignoreNothing{}
	}

	s.setupFixedMap(request)

	s.io.WriteError("Generating rules", true, io.Debug)
	rules, err := NewRuleSetGenerator(s.policy, s.pool).RulesFor(request, filter)
	if err != nil {
		return nil, err
	}
	s.rules = rules

	s.checkForRootRequireProblems(request, filter)
	// Must run after RuleSetGenerator: the generator silently skips locked
	// packages whose pool entry was filter-list-removed, so this check is
	// what actually surfaces the resulting SolverProblem to the user.
	s.checkForFilterListRemovedLockedPackages(request)
	s.decisions = NewDecisions(s.pool)
	s.watchGraph = NewRuleWatchGraph(s.pool.Count())
	s.learnedWhy = map[*Rule]int{}

	for it := s.rules.Iterator(); it.Valid(); it.Next() {
		s.watchGraph.Insert(NewRuleWatchNode(it.Current()))
	}

	/* make decisions based on root require/fix assertions */
	if err := s.makeAssertionRuleDecisions(); err != nil {
		return nil, err
	}

	s.io.WriteError("Resolving dependencies through SAT", true, io.Debug)
	before := time.Now()
	if err := s.runSat(); err != nil {
		return nil, err
	}
	s.io.WriteError("", true, io.Debug)
	s.io.WriteError("Dependency resolution completed in "+strconv.FormatFloat(time.Since(before).Seconds(), 'f', 3, 64)+" seconds", true, io.Verbose)

	if len(s.problems) > 0 {
		return nil, NewSolverProblemsError(s.problems, s.learnedPool)
	}

	present, err := request.PresentMap()
	if err != nil {
		return nil, err
	}

	return NewLockTransaction(s.pool, present, request.FixedPackagesMap(), s.decisions)
}

// propagate ports propagate: the rule that conflicts, or nil.
func (s *Solver) propagate(level int) (*Rule, error) {
	for s.decisions.ValidOffset(s.propagateIndex) {
		decision := s.decisions.AtOffset(s.propagateIndex)

		conflict, err := s.watchGraph.PropagateLiteral(decision.Literal, level, s.decisions)
		if err != nil {
			return nil, err
		}

		s.propagateIndex++

		if conflict != nil {
			return conflict, nil
		}
	}

	return nil, nil
}

// revert ports revert: the decisions above level are undone.
func (s *Solver) revert(level int) {
	for !s.decisions.IsEmpty() {
		literal := s.decisions.LastLiteral()

		if s.decisions.Undecided(literal) {
			break
		}

		decisionLevel := s.decisions.DecisionLevel(literal)

		if decisionLevel <= level {
			break
		}

		s.decisions.RevertLast()
		s.propagateIndex = s.decisions.Count()
	}

	for len(s.branches) > 0 && s.branches[len(s.branches)-1].level >= level {
		s.branches = s.branches[:len(s.branches)-1]
	}
}

// setPropagateLearn ports setPropagateLearn: it decides literal on the
// next level and propagates, learning from conflicts; 0 means unsolvable.
func (s *Solver) setPropagateLearn(level int, literal int32, rule *Rule) (int, error) {
	level++

	if err := s.decisions.Decide(literal, level, rule); err != nil {
		return 0, err
	}

	for {
		var err error
		rule, err = s.propagate(level)
		if err != nil {
			return 0, err
		}

		if rule == nil {
			break
		}

		if level == 1 {
			if err := s.analyzeUnsolvable(rule); err != nil {
				return 0, err
			}

			return 0, nil
		}

		// conflict
		learnLiteral, newLevel, newRule, why, err := s.analyze(level, rule)
		if err != nil {
			return 0, err
		}

		if newLevel <= 0 || newLevel >= level {
			return 0, newSolverBugError("Trying to revert to invalid level " + strconv.Itoa(newLevel) + " from level " + strconv.Itoa(level) + ".")
		}

		level = newLevel

		s.revert(level)

		if err := s.rules.Add(newRule, TypeLearned); err != nil {
			return 0, err
		}

		s.learnedWhy[newRule] = why

		ruleNode := NewRuleWatchNode(newRule)
		ruleNode.Watch2OnHighest(s.decisions)
		s.watchGraph.Insert(ruleNode)

		if err := s.decisions.Decide(learnLiteral, level, newRule); err != nil {
			return 0, err
		}
	}

	return level, nil
}

// selectAndInstall ports selectAndInstall.
func (s *Solver) selectAndInstall(level int, decisionQueue []int32, rule *Rule) (int, error) {
	// choose best package to install from decisionQueue
	requiredPackage, _ := rule.RequiredPackage()
	literals := s.policy.SelectPreferredPackages(s.pool, decisionQueue, requiredPackage)

	selectedLiteral := literals[0]

	// if there are multiple candidates, then branch
	if len(literals) > 1 {
		s.branches = append(s.branches, branch{literals: append([]int32(nil), literals[1:]...), level: level})
	}

	return s.setPropagateLearn(level, selectedLiteral, rule)
}

// analyze ports analyze: the learned literal, the level to go back to, the
// learned rule and its index in the learned pool.
func (s *Solver) analyze(level int, rule *Rule) (learnedLiteral int32, ruleLevel int, newRule *Rule, why int, err error) {
	analyzedRule := rule
	ruleLevel = 1
	num := 0
	l1num := 0
	s.seen.reset(s.pool.Count())
	seen := &s.seen
	hasLearnedLiteral := false
	var otherLearnedLiterals []int32

	decisionID := s.decisions.Count()

	s.learnedPool = append(s.learnedPool, nil)
	current := len(s.learnedPool) - 1

	countLiteral := func(literal int32) {
		l := s.decisions.DecisionLevel(literal)
		switch {
		case l == 1:
			l1num++
		case level == l:
			num++
		default:
			// not level1 or conflict level, add to new rule
			otherLearnedLiterals = append(otherLearnedLiterals, literal)

			if l > ruleLevel {
				ruleLevel = l
			}
		}
	}

analysis:
	for {
		s.learnedPool[current] = append(s.learnedPool[current], rule)

		for _, literal := range rule.literals {
			// multiconflictrule is really a bunch of rules in one, so some may not have finished propagating yet
			if rule.kind == kindMultiConflict && !s.decisions.Decided(literal) {
				continue
			}

			// skip the one true literal
			if s.decisions.Satisfy(literal) {
				continue
			}

			if seen.has(literal) {
				continue
			}
			seen.set(literal)

			countLiteral(literal)
		}

		for l1retry := true; l1retry; {
			l1retry = false

			if num == 0 {
				l1num--
				if l1num == 0 {
					// all level 1 literals done
					break analysis
				}
			}

			var literal int32
			for {
				if decisionID <= 0 {
					return 0, 0, nil, 0, newSolverBugError("Reached invalid decision id " + strconv.Itoa(decisionID) + " while looking through " + rule.String() + " for a literal present in the analyzed rule " + analyzedRule.String() + ".")
				}

				decisionID--

				literal = s.decisions.AtOffset(decisionID).Literal

				if seen.has(literal) {
					break
				}
			}

			seen.unset(literal)

			if num != 0 {
				num--
				if num == 0 {
					if literal < 0 {
						s.TestFlagLearnedPositiveLiteral = true
					}
					learnedLiteral = -literal
					hasLearnedLiteral = true

					if l1num == 0 {
						break analysis
					}

					for _, otherLiteral := range otherLearnedLiterals {
						seen.unset(otherLiteral)
					}
					// only level 1 marks left
					l1num++
					l1retry = true

					continue
				}
			}

			rule = s.decisions.AtOffset(decisionID).Reason
			if rule.kind == kindMultiConflict {
				// there is only ever exactly one positive decision in a MultiConflictRule
				for _, ruleLiteral := range rule.literals {
					if !seen.has(ruleLiteral) && s.decisions.Satisfy(-ruleLiteral) {
						s.learnedPool[current] = append(s.learnedPool[current], rule)
						countLiteral(ruleLiteral)
						seen.set(ruleLiteral)

						break
					}
				}

				l1retry = true
			}
		}

		rule = s.decisions.AtOffset(decisionID).Reason
	}

	why = len(s.learnedPool) - 1

	if !hasLearnedLiteral {
		return 0, 0, nil, 0, newSolverBugError("Did not find a learnable literal in analyzed rule " + analyzedRule.String() + ".")
	}

	literals := make([]int32, 0, len(otherLearnedLiterals)+1)
	literals = append(literals, learnedLiteral)
	literals = append(literals, otherLearnedLiterals...)
	newRule = NewGenericRule(literals, RuleLearned, why)

	return learnedLiteral, ruleLevel, newRule, why, nil
}

// analyzeUnsolvableRule ports analyzeUnsolvableRule.
func (s *Solver) analyzeUnsolvableRule(problem *Problem, conflictRule *Rule, ruleSeen map[*Rule]bool) {
	ruleSeen[conflictRule] = true

	if conflictRule.Type() == TypeLearned {
		learnedWhy := s.learnedWhy[conflictRule]
		problemRules := s.learnedPool[learnedWhy]

		for _, problemRule := range problemRules {
			if !ruleSeen[problemRule] {
				s.analyzeUnsolvableRule(problem, problemRule, ruleSeen)
			}
		}

		return
	}

	if conflictRule.Type() == TypePackage {
		// package rules cannot be part of a problem
		return
	}

	problem.NextSection()
	problem.AddRule(conflictRule)
}

// analyzeUnsolvable ports analyzeUnsolvable.
func (s *Solver) analyzeUnsolvable(conflictRule *Rule) error {
	problem := NewProblem()
	problem.AddRule(conflictRule)

	ruleSeen := map[*Rule]bool{}

	s.analyzeUnsolvableRule(problem, conflictRule, ruleSeen)

	s.problems = append(s.problems, problem)

	seen := map[int32]bool{}
	for _, literal := range conflictRule.literals {
		// skip the one true literal
		if s.decisions.Satisfy(literal) {
			continue
		}
		seen[abs32(literal)] = true
	}

	for _, decision := range s.decisions.Reversed {
		// skip literals that are not in this rule
		if !seen[abs32(decision.Literal)] {
			continue
		}

		why := decision.Reason

		problem.AddRule(why)
		s.analyzeUnsolvableRule(problem, why, ruleSeen)

		for _, literal := range why.literals {
			// skip the one true literal
			if s.decisions.Satisfy(literal) {
				continue
			}
			seen[abs32(literal)] = true
		}
	}

	return nil
}

// runSat ports runSat, the main loop:
// 1) propagate new decisions (only needed once)
// 2) fulfill root requires/fixed packages
// 3) fulfill all unresolved rules
// 4) minimalize solution if we had choices
// if we encounter a problem, we rewind to a safe level and restart
// with step 1
func (s *Solver) runSat() error {
	s.propagateIndex = 0

	level := 1
	systemLevel := level + 1

	for {
		if level == 1 {
			conflictRule, err := s.propagate(level)
			if err != nil {
				return err
			}
			if conflictRule != nil {
				return s.analyzeUnsolvable(conflictRule)
			}
		}

		// handle root require/fixed package rules
		if level < systemLevel {
			restart, stop, newLevel, err := s.fulfillRequestRules(level)
			if err != nil || stop {
				return err
			}
			level = newLevel
			systemLevel = level + 1
			if restart {
				continue
			}
		}

		if level < systemLevel {
			systemLevel = level
		}

		var err error
		if level, err = s.fulfillAllRules(level); err != nil || level == 0 {
			return err
		}

		if level < systemLevel {
			continue
		}

		// minimization step
		if len(s.branches) > 0 {
			var lastLiteral int32
			lastLevel := 0
			lastBranchIndex := 0
			lastBranchOffset := 0

			for i, b := range slices.Backward(s.branches) {
				for offset, literal := range b.literals {
					if literal > 0 && s.decisions.DecisionLevel(literal) > b.level+1 {
						lastLiteral = literal
						lastBranchIndex = i
						lastBranchOffset = offset
						lastLevel = b.level
					}
				}
			}

			if lastLiteral != 0 {
				s.branches[lastBranchIndex].literals[lastBranchOffset] = 0

				level = lastLevel
				s.revert(level)

				why := s.decisions.LastReason()

				if level, err = s.setPropagateLearn(level, lastLiteral, why); err != nil || level == 0 {
					return err
				}

				continue
			}
		}

		return nil
	}
}

// fulfillRequestRules is runSat's step 2: it decides the root require and
// fixed package rules. restart reports that runSat must start over (PHP's
// continue after the iterator still had rules), stop that it is done
// (unsolvable).
func (s *Solver) fulfillRequestRules(level int) (restart, stop bool, newLevel int, err error) {
	iterator := s.rules.IteratorFor(TypeRequest)
	for ; iterator.Valid(); iterator.Next() {
		rule := iterator.Current()
		if !rule.IsEnabled() {
			continue
		}

		var decisionQueue []int32
		noneSatisfied := true

		for _, literal := range rule.literals {
			if s.decisions.Satisfy(literal) {
				noneSatisfied = false

				break
			}
			if literal > 0 && s.decisions.Undecided(literal) {
				decisionQueue = append(decisionQueue, literal)
			}
		}

		if noneSatisfied && len(decisionQueue) > 0 {
			// if any of the options in the decision queue are fixed, only use those
			var prunedQueue []int32
			for _, literal := range decisionQueue {
				if s.fixedMap[int(abs32(literal))] {
					prunedQueue = append(prunedQueue, literal)
				}
			}
			if len(prunedQueue) > 0 {
				decisionQueue = prunedQueue
			}
		}

		if noneSatisfied && len(decisionQueue) > 0 {
			oLevel := level
			if level, err = s.selectAndInstall(level, decisionQueue, rule); err != nil || level == 0 {
				return false, true, level, err
			}
			if level <= oLevel {
				break
			}
		}
	}

	// root requires/fixed packages left
	iterator.Next()

	return iterator.Valid(), false, level, nil
}

// fulfillAllRules is runSat's step 3: it decides the rules that are not
// fulfilled yet and have at least two literals left to choose from. It
// returns the new level, 0 when the request is unsolvable.
func (s *Solver) fulfillAllRules(level int) (int, error) {
	rulesCount := s.rules.Count()
	pass := 1

	s.io.WriteError("Looking at all rules.", true, io.Debug)
	var decisionQueue []int32
	for i, n := 0, 0; n < rulesCount; i, n = i+1, n+1 {
		if i == rulesCount {
			if pass == 1 {
				s.io.WriteError("Something's changed, looking at all rules again (pass #"+strconv.Itoa(pass)+")", false, io.Debug)
			} else {
				s.io.OverwriteError("Something's changed, looking at all rules again (pass #"+strconv.Itoa(pass)+")", false, -1, io.Debug)
			}

			i = 0
			pass++
		}

		rule := s.rules.RuleByID[i]

		if rule.IsDisabled() {
			continue
		}

		decisionQueue = decisionQueue[:0]

		// make sure that
		// * all negative literals are installed
		// * no positive literal is installed
		// i.e. the rule is not fulfilled and we
		// just need to decide on the positive literals
		fulfilled := false
		for _, literal := range rule.literals {
			if literal <= 0 {
				if !s.decisions.DecidedInstall(literal) {
					fulfilled = true

					break
				}
			} else {
				if s.decisions.DecidedInstall(literal) {
					fulfilled = true

					break
				}
				if s.decisions.Undecided(literal) {
					decisionQueue = append(decisionQueue, literal)
				}
			}
		}

		// need to have at least 2 item to pick from
		if fulfilled || len(decisionQueue) < 2 {
			continue
		}

		var err error
		if level, err = s.selectAndInstall(level, decisionQueue, rule); err != nil || level == 0 {
			return 0, err
		}

		// something changed, so look at all rules again
		rulesCount = s.rules.Count()
		n = -1
	}

	return level, nil
}

// literalSet is a set of package ids (the analysis' $seen) that is
// cleared in time proportional to its size.
type literalSet struct {
	marks   []bool
	touched []int32
}

func (l *literalSet) reset(n int) {
	for _, id := range l.touched {
		l.marks[id] = false
	}
	l.touched = l.touched[:0]
	if len(l.marks) < n+1 {
		l.marks = make([]bool, n+1)
	}
}

func (l *literalSet) has(literal int32) bool { return l.marks[abs32(literal)] }

func (l *literalSet) set(literal int32) {
	id := abs32(literal)
	if !l.marks[id] {
		l.marks[id] = true
		l.touched = append(l.touched, id)
	}
}

func (l *literalSet) unset(literal int32) { l.marks[abs32(literal)] = false }

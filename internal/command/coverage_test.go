// The coverage guard of docs/PORTING.md (Tests): every command the
// Application registers, hidden ones included, has at least one positive
// and one negative test, or is pending with the issue that adds them.

package command_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
)

// coverage is the table, one entry per command (aliases resolve to their
// command). Name the command by its constructor, Go tests by their
// function, e2e steps by scenario name and exact args (checked against
// cmd/maestro's scenarios). Errors-oracle scenarios
// (testdata/errors) are not listed: each counts for the command its args
// run, positive when Composer exited 0 and negative otherwise.
//
// A command missing a kind is Pending on its "Tests: <command>" issue;
// remove Pending once both kinds exist (the guard fails while it stays).
var coverage = []entry{
	cover(console.NewHelpCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestHelp_All)),
			Positive(Go(TestHelp_Quiet)),
			Positive(E2E("commands", "help")),
			Positive(E2E("commands", "help", "validate", "--format=json")),
			Positive(E2E("commands", "help", "u")),
			Positive(E2E("scripts", "help", "hi")),
		},
	}),
	cover(console.NewListCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestHelp_List)),
			Positive(Go(TestHelp_ListJSON)),
			Positive(E2E("commands", "list")),
			Positive(E2E("commands", "list", "--raw")),
			Positive(E2E("commands", "list", "--format=json", "--short")),
			Positive(E2E("scripts", "list", "--format=json")),
			Positive(E2E("plugin-stubs", "list", "stubs")),
		},
	}),
	cover(newCompleteCommand, Coverage{
		Tests:   []Proof{Positive(Go(TestCompletionFunctional_Complete))},
		Pending: 53,
	}),
	cover(console.NewDumpCompletionCommand, Coverage{
		Pending: 53,
	}),
	cover(command.NewAboutCommand, Coverage{
		Tests: []Proof{Positive(Go(TestAboutCommand_About)), Positive(E2E("commands", "about"))},
	}),
	cover(command.NewConfigCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestConfigCommand_ConfigUpdates)),
			Positive(Go(TestConfigCommand_ConfigReads)),
			Negative(Go(TestConfigCommand_ConfigThrowsForInvalidArgCombination)),
		},
	}),
	cover(command.NewDependsCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestBaseDependencyCommandTest_WhyCommandOutputs)),
			Negative(Go(TestBaseDependencyCommandTest_ExceptionWhenNoRequiredParameters)),
		},
	}),
	cover(command.NewProhibitsCommand, Coverage{
		Tests: []Proof{Positive(Go(TestBaseDependencyCommandTest_WhyNotCommandOutputs))},
	}),
	cover(command.NewInitCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestInitCommand_RunCommand)),
			Negative(Go(TestInitCommand_RunCommandInvalid)),
		},
	}),
	cover(command.NewInstallCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestInstallCommand_InstallFromEmptyVendor)),
			Positive(Go(TestInstallCommand_Options)),
			Positive(E2E("install-from-lock", "i", "--dry-run")),
			Positive(E2E("prefer-source", "install", "--prefer-install=source")),
			Negative(Go(TestInstallCommand_InstallCommandErrors)),
			Negative(Go(TestInstallCommand_Options)),
			Negative(E2E("autoload", "install", "-o", "--strict-psr-autoloader")),
			Negative(E2E("security", "install", "--audit")),
		},
	}),
	cover(command.NewCreateProjectCommand, Coverage{
		Tests: []Proof{Positive(E2E("create-project", "create-project", "psr/log", "psr-log", "1.1.4"))},
	}),
	cover(command.NewUpdateCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestUpdateCommand_Update)),
			Positive(Go(TestUpdateCommand_Options)),
			Positive(E2E("update", "u", "--dry-run")),
			Positive(E2E("update", "upgrade", "psr/log")),
			Positive(E2E("outdated", "update", "--bump-after-update=no-dev")),
			Negative(Go(TestUpdateCommand_InteractiveModeThrowsIfNoPackageToUpdate)),
			Negative(Go(TestUpdateCommand_Options)),
			Negative(Go(TestUpdateCommand_BumpAfterUpdateFailurePassesThrough)),
		},
	}),
	cover(command.NewSearchCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestSearchCommand_Search)),
			Negative(Go(TestSearchCommand_InvalidFormat)),
		},
	}),
	cover(command.NewValidateCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestValidateCommand_Validate)),
			Negative(Go(TestValidateCommand_ValidateOnFileIssues)),
		},
	}),
	cover(command.NewAuditCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestAuditCommand_AuditPackageWithNoSecurityVulnerabilities)),
			Positive(Go(TestAuditCommand_NoPackagesSkipsBeforeValidatingOptions)),
			Positive(Go(TestAuditCommand_Abandoned)),
			Positive(E2E("security", "audit", "--ignore-severity=medium", "--ignore-severity=high")),
			Positive(E2E("security", "audit", "--ignore-severity=medium", "--ignore-severity=high", "--format=json")),
			Negative(Go(TestAuditCommand_ErrorAuditingLockFileWhenItIsMissing)),
			Negative(Go(TestAuditCommand_Abandoned)),
			Negative(E2E("security", "audit", "--format=summary")),
			Negative(E2E("security", "audit", "--locked")),
		},
	}),
	cover(command.NewShowCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestShowCommand_Show)),
			Negative(Go(TestShowCommand_NotExistingPackage)),
		},
	}),
	cover(command.NewSuggestsCommand, Coverage{
		Tests:   []Proof{Positive(Go(TestSuggestsCommand_Suggest))},
		Pending: 76,
	}),
	cover(command.NewRequireCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestRequireCommand_Require)),
			Positive(Go(TestRequireCommand_Options)),
			Positive(E2E("require-remove", "r", "psr/http-message:^1.1", "--no-update")),
			Positive(E2E("require-new-project", "require", "psr/log:1.0.0", "--no-update")),
			Negative(Go(TestRequireCommand_RequireThrowsIfNoneMatches)),
			Negative(Go(TestRequireCommand_RequireWarnsIfResolvedToFeatureBranch)),
		},
	}),
	cover(command.NewDumpAutoloadCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestDumpAutoloadCommand_DumpAutoload)),
			Positive(Go(TestDumpAutoloadCommand_StrictAmbiguousWithoutAmbiguousClasses)),
			Positive(Go(TestDumpAutoloadCommand_ConfigFallbacks)),
			Positive(Go(TestDumpAutoloadCommand_DryRun)),
			Positive(Go(TestDumpAutoloadCommand_Alias)),
			Positive(E2E("autoload", "dumpautoload", "-o")),
			Positive(E2E("autoload", "dump-autoload", "-o", "--dry-run")),
			Positive(E2E("autoload", "dump-autoload", "--apcu-prefix=e2e")),
			Positive(E2E("autoload", "dump-autoload")),
			Positive(E2E("autoload", "dump-autoload", "--dev")),
			Positive(E2E("platform", "dump-autoload", "--ignore-platform-req=php")),
			Positive(E2E("platform", "dump-autoload", "--ignore-platform-reqs")),
			Negative(Go(TestDumpAutoloadCommand_DevAndNoDevCannotBeCombined)),
			Negative(Go(TestDumpAutoloadCommand_StrictAmbiguousDoesNotWorkWithoutOptimizedAutoloader)),
		},
	}),
	cover(command.NewStatusCommand, Coverage{
		Tests:   []Proof{Positive(Go(TestStatusCommand_NoLocalChanges))},
		Pending: 102,
	}),
	cover(command.NewArchiveCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestArchiveCommand_Streams)),
			Positive(E2E("archive", "archive")),
			Positive(E2E("archive", "archive", "--format=tar", "--file=filtered")),
			Positive(E2E("archive", "archive", "--format=tar", "--file=unfiltered", "--ignore-filters")),
			Positive(E2E("archive", "archive", "--format=tar.gz", "--file=gz")),
			Positive(E2E("archive", "archive", "psr/container", "^1.1", "--file=psr-container")),
			Negative(Go(TestArchiveCommand_Streams)),
			Negative(E2E("archive", "archive", "nope/nope-xyz")),
		},
	}),
	cover(command.NewDiagnoseCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestDiagnoseCommand_CmdSuccess)),
			Negative(Go(TestDiagnoseCommand_CmdFail)),
		},
	}),
	cover(command.NewRunScriptCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestRunScriptCommand_CanListScripts)),
			Negative(Go(TestRunScriptCommand_Errors)),
		},
	}),
	cover(command.NewLicensesCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestLicensesCommand_BasicRun)),
			Negative(Go(TestLicensesCommand_LockedWithoutLockFile)),
		},
	}),
	cover(command.NewGlobalCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestGlobalCommand_GlobalShow)),
			Positive(Go(TestGlobalCommand_Abbreviations)),
			Positive(E2E("global", "glob", "config", "home")),
			Positive(E2E("global", "global", "show", "--format=json")),
			Positive(E2E("global", "global", "exec", "pwd-tool")),
			Negative(Go(TestGlobalCommand_GlobalMissingCommandName)),
			Negative(Go(TestGlobalCommand_CannotCreateHome)),
			Negative(Go(TestGlobalCommand_CannotSwitchToHome)),
			Negative(E2E("global", "global", "require", "nothing/at-all")),
		},
	}),
	cover(command.NewClearCacheCommand, Coverage{
		Tests:   []Proof{Positive(Go(TestClearCacheCommand_Success))},
		Pending: 52,
	}),
	cover(command.NewRemoveCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestRemoveCommand_RemovePackageByName)),
			Negative(Go(TestRemoveCommand_ExceptionRunningWithNoRemovePackages)),
		},
	}),
	cover(command.NewHomeCommand, Coverage{
		Tests: []Proof{Positive(Go(TestHomeCommand_HomeCommandWithShowFlag))},
	}),
	cover(command.NewExecCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestExecCommand_List)),
			Positive(Go(TestExecCommand_Run)),
			Positive(Go(TestExecCommand_GlobalRunsInInitialDirectory)),
			Positive(E2E("path-repositories", "exec", "--list")),
			Positive(E2E("autoload", "exec", "c-run", "--", "--flag", "a b")),
			Positive(E2E("autoload", "exec", "-n")),
			Positive(E2E("autoload", "exec", "-l")),
			Positive(E2E("autoload", "exec")),
			Negative(Go(TestExecCommand_ListThrowsIfNoBinariesExist)),
			Negative(Go(TestExecCommand_Run)),
			Negative(E2E("autoload", "exec", "c-run")),
			Negative(E2E("autoload", "exec")),
		},
	}),
	cover(command.NewOutdatedCommand, Coverage{
		Tests: []Proof{Positive(E2E("commands", "outdated"))},
	}),
	cover(command.NewCheckPlatformReqsCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestCheckPlatformReqsCommand_PlatformReqsAreSatisfied)),
			Positive(Go(TestCheckPlatformReqsCommand_Rows)),
			Positive(E2E("platform", "check-platform-reqs", "--lock")),
			Positive(E2E("platform", "check-platform-reqs", "--lock", "--no-dev")),
			Negative(Go(TestCheckPlatformReqsCommand_ExceptionThrownIfNoLockfileFound)),
			Negative(Go(TestCheckPlatformReqsCommand_FailedPlatformRequirement)),
			Negative(Go(TestCheckPlatformReqsCommand_Rows)),
			Negative(E2E("platform", "check-platform-reqs", "--format=json")),
			Negative(E2E("platform", "check-platform-reqs", "--no-dev", "-f", "json")),
		},
	}),
	cover(command.NewFundCommand, Coverage{
		Tests:   []Proof{Positive(Go(TestFundCommand_FundCommand))},
		Pending: 49,
	}),
	cover(command.NewReinstallCommand, Coverage{
		Tests: []Proof{Positive(Go(TestReinstallCommand_ReinstallCommand))},
	}),
	cover(command.NewBumpCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestBumpCommand_Bump)),
			Positive(E2E("require-remove", "bump")),
			Negative(Go(TestBumpCommand_Bump)),
			Negative(Go(TestBumpCommand_BumpFailsOnNonExistingComposerFile)),
			Negative(Go(TestBumpCommand_BumpFailsOnNonExistingComposerEnvFile)),
			Negative(Go(TestBumpCommand_BumpFailsOnWriteErrorToComposerFile)),
			Negative(E2E("require-remove", "bump", "--dry-run")),
		},
	}),
	cover(command.NewRepositoryCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestRepositoryCommand_AddRepositoryWithTypeAndUrl)),
			Negative(Go(TestRepositoryCommand_InvalidArgCombinationThrows)),
		},
	}),
	cover(command.NewPolicyCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestPolicyCommand_AddSourceCreatesNewList)),
			Negative(Go(TestPolicyCommand_UnknownActionThrows)),
		},
	}),
	cover(command.NewSelfUpdateCommand, Coverage{
		Tests: []Proof{
			Positive(Go(TestSelfUpdateCommand_SuccessfulUpdateAndRollback)),
			Negative(Go(TestSelfUpdateCommand_UpdateWithInvalidOptionThrowsException)),
		},
	}),
}

// newCompleteCommand is console.NewCompleteCommand without its variadic
// outputs, so it fits cover.
func newCompleteCommand() *console.CompleteCommand { return console.NewCompleteCommand() }

// Coverage is a command's entry: its tests, and the issue adding the kind
// it lacks (0 when it has both).
type Coverage struct {
	Tests   []Proof
	Pending Issue
}

// Issue is a GitHub issue number of stubbedev/maestro.
type Issue int

// Kind says whether a test proves the command works or that it fails well.
type Kind int

const (
	// KindPositive: a representative input succeeds and the test asserts
	// the frozen surface.
	KindPositive Kind = iota + 1
	// KindNegative: bad input or an unmet precondition, and the test
	// asserts the exit code, the stream and the essential message.
	KindNegative
)

// Proof is one test of a command and its kind.
type Proof struct {
	Kind Kind
	Test Evidence
}

// Positive and Negative make a Proof of their kind.
func Positive(e Evidence) Proof { return Proof{KindPositive, e} }
func Negative(e Evidence) Proof { return Proof{KindNegative, e} }

// Evidence is a test that exercises a command: a Go test (Go), an e2e step
// (E2E) or an errors-oracle scenario (derived, see coverage).
type Evidence interface {
	String() string
}

// GoTest is a Go test function of this package.
type GoTest func(*testing.T)

// Go names a Go test by its function, so the compiler catches a rename.
func Go(f func(*testing.T)) GoTest { return f }

func (g GoTest) String() string {
	name := runtime.FuncForPC(reflect.ValueOf(g).Pointer()).Name()

	return name[strings.LastIndexByte(name, '.')+1:]
}

// E2EStep is a step of a cmd/maestro e2e scenario: the scenario's name and
// the step's args exactly.
type E2EStep struct {
	Scenario string
	Args     []string
}

// E2E names an e2e step.
func E2E(scenario string, args ...string) E2EStep { return E2EStep{scenario, args} }

func (e E2EStep) String() string {
	return "e2e " + e.Scenario + ": " + strings.Join(e.Args, " ")
}

// oracleScenario is a testdata/errors scenario.
type oracleScenario string

func (o oracleScenario) String() string { return "errors oracle " + string(o) }

// entry is a Coverage tied to the command its constructor builds.
type entry struct {
	command console.Commander
	Coverage
}

// cover ties c to the command ctor builds.
func cover[T console.Commander](ctor func() T, c Coverage) entry {
	return entry{ctor(), c}
}

func TestEveryCommandHasPositiveAndNegativeTests(t *testing.T) {
	// The goldens of `list` (TestHelp_List, TestHelp_ListJSON) list every
	// command of getDefaultCommands.
	if !command.AllCommandsRegistered() {
		t.Error("a command of getDefaultCommands is not registered")
	}

	app := commandtest.NewApplication()
	resolve := func(name string) (string, bool) {
		cmd, err := app.Find(name)
		if err != nil {
			return "", false
		}

		return cmd.Base().Name(), true
	}

	table := map[string]*entry{}
	for i := range coverage {
		e := &coverage[i]
		name := e.command.Base().Name()
		if _, ok := table[name]; ok {
			t.Errorf("%s: two entries in the coverage table", name)
		}
		table[name] = e
	}

	// Every registered name, aliases and hidden commands included, has an
	// entry through the command it runs.
	registered := map[string]bool{}
	for _, nc := range app.All("") {
		name := nc.Command.Base().Name()
		registered[name] = true
		if table[name] == nil {
			t.Errorf("%s (registered as %q): no entry in the coverage table (internal/command/coverage_test.go)", name, nc.Name)
		}
	}
	for name := range table {
		if !registered[name] {
			t.Errorf("%s: in the coverage table but not registered", name)
		}
	}

	proofs := map[string][]Proof{}
	for name, e := range table {
		proofs[name] = slices.Clone(e.Tests)
	}

	steps := e2eSteps(t)
	for name, e := range table {
		for _, p := range e.Tests {
			s, ok := p.Test.(E2EStep)
			if !ok {
				continue
			}
			if !slices.ContainsFunc(steps[s.Scenario], func(args []string) bool { return slices.Equal(args, s.Args) }) {
				t.Errorf("%s: %s: no such step in cmd/maestro's e2e scenarios", name, s)

				continue
			}
			if got, _ := resolve(firstArgument(s.Args)); got != name {
				t.Errorf("%s: %s runs %q, not %s", name, s, got, name)
			}
		}
	}

	for _, o := range oracleScenarios(t) {
		name, ok := resolve(o.command)
		if !ok {
			continue // an unknown command is the application's error, not a command's
		}
		proofs[name] = append(proofs[name], Proof{o.kind, o.scenario})
	}

	for name, e := range table {
		var positive, negative bool
		for _, p := range proofs[name] {
			positive = positive || p.Kind == KindPositive
			negative = negative || p.Kind == KindNegative
		}
		switch {
		case positive && negative && e.Pending != 0:
			t.Errorf("%s: has positive and negative tests now; remove Pending: %d from its entry", name, e.Pending)
		case (!positive || !negative) && e.Pending == 0:
			t.Errorf("%s: needs a positive and a negative test (has positive %v, negative %v); add them, or Pending: <its Tests issue>", name, positive, negative)
		}
	}
}

// firstArgument is the command name of args: the first that is not an
// option.
func firstArgument(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}

	return ""
}

type oracleRun struct {
	scenario oracleScenario
	command  string
	kind     Kind
}

var exitLine = regexp.MustCompile(`exit ([0-9]+)\n$`)

// oracleScenarios reads testdata/errors: each scenario's command (its args)
// and kind (Composer's exit code in default.txt).
func oracleScenarios(t *testing.T) []oracleRun {
	t.Helper()
	const dir = "testdata/errors"
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []oracleRun
	for _, d := range ents {
		args, err := os.ReadFile(filepath.Join(dir, d.Name(), "args"))
		if err != nil {
			continue // not a scenario (_server)
		}
		golden, err := os.ReadFile(filepath.Join(dir, d.Name(), "default.txt"))
		if err != nil {
			t.Fatal(err)
		}
		m := exitLine.FindSubmatch(golden)
		if m == nil {
			t.Fatalf("%s/default.txt: no exit line", d.Name())
		}
		kind := KindNegative
		if string(m[1]) == "0" {
			kind = KindPositive
		}
		out = append(out, oracleRun{oracleScenario(d.Name()), firstArgument(strings.Fields(string(args))), kind})
	}

	return out
}

// e2eSteps parses cmd/maestro's tests for the scenarios' literal steps:
// scenario name to the args of each step written as a string literal list.
func e2eSteps(t *testing.T) map[string][][]string {
	t.Helper()
	files, err := filepath.Glob("../../cmd/maestro/*_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no e2e sources: %v", err)
	}
	out := map[string][][]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			name, ok := stringField(lit, "name")
			if !ok {
				return true
			}
			steps, ok := field(lit, "steps").(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, s := range steps.Elts {
				step, ok := s.(*ast.CompositeLit)
				if !ok {
					continue
				}
				if args, ok := stringList(field(step, "args")); ok {
					out[name] = append(out[name], args)
				}
			}

			return true
		})
	}

	return out
}

func field(lit *ast.CompositeLit, key string) ast.Expr {
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok && id.Name == key {
				return kv.Value
			}
		}
	}

	return nil
}

func stringField(lit *ast.CompositeLit, key string) (string, bool) {
	b, ok := field(lit, key).(*ast.BasicLit)
	if !ok || b.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(b.Value)

	return s, err == nil
}

func stringList(e ast.Expr) ([]string, bool) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(lit.Elts))
	for _, el := range lit.Elts {
		b, ok := el.(*ast.BasicLit)
		if !ok || b.Kind != token.STRING {
			return nil, false
		}
		s, err := strconv.Unquote(b.Value)
		if err != nil {
			return nil, false
		}
		out = append(out, s)
	}

	return out, true
}

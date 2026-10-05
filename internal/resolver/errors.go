// Ports src/Composer/DependencyResolver/SolverBugException.php and
// SolverProblemsException.php.

package resolver

import (
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/repository"
)

// OutOfBoundsError is PHP's \OutOfBoundsException.
type OutOfBoundsError struct{ Message string }

func (e *OutOfBoundsError) Error() string { return e.Message }

// SolverBugError ports SolverBugException (a RuntimeException).
type SolverBugError struct{ Message string }

func newSolverBugError(message string) *SolverBugError {
	return &SolverBugError{Message: message + "\nThis exception was most likely caused by a bug in Composer.\n" +
		"Please report the command you ran, the exact error you received, and your composer.json on https://github.com/composer/composer/issues - thank you!\n"}
}

func (e *SolverBugError) Error() string { return e.Message }

// ErrorDependencyResolutionFailed is
// SolverProblemsException::ERROR_DEPENDENCY_RESOLUTION_FAILED, the
// exception's code.
const ErrorDependencyResolutionFailed = 2

// SolverProblemsError ports SolverProblemsException (a RuntimeException
// with code 2).
type SolverProblemsError struct {
	problems    []*Problem
	learnedPool [][]*Rule
}

// NewSolverProblemsError is new SolverProblemsException($problems, $learnedPool).
func NewSolverProblemsError(problems []*Problem, learnedPool [][]*Rule) *SolverProblemsError {
	return &SolverProblemsError{problems: problems, learnedPool: learnedPool}
}

func (e *SolverProblemsError) Error() string {
	return "Failed resolving dependencies with " + strconv.Itoa(len(e.problems)) + " problems, call getPrettyString to get formatted details"
}

// Code is the exception code, ErrorDependencyResolutionFailed.
func (e *SolverProblemsError) Code() int { return ErrorDependencyResolutionFailed }

// Problems ports getProblems.
func (e *SolverProblemsError) Problems() []*Problem { return e.problems }

// PrettyString ports getPrettyString. env answers what Composer asks the
// PHP running it (nil: nothing loaded).
func (e *SolverProblemsError) PrettyString(repositorySet *repository.RepositorySet, request *Request, pool *Pool, isVerbose, isDevExtraction bool, env Environment) (string, error) {
	installedMap, err := request.PresentIDMap()
	if err != nil {
		return "", err
	}
	ctx := &PrettyContext{RepositorySet: repositorySet, Request: request, Pool: pool, IsVerbose: isVerbose, InstalledMap: installedMap, LearnedPool: e.learnedPool, Env: env}

	missingExtensions := &repository.NameMap[bool]{}
	isCausedByLock := false
	problems := make([]string, 0, len(e.problems))
	for _, problem := range e.problems {
		pretty, err := problem.PrettyString(ctx)
		if err != nil {
			return "", err
		}
		problems = append(problems, pretty+"\n")
		for _, section := range problem.Reasons() {
			for _, rule := range section {
				if required, ok := rule.RequiredPackage(); ok && strings.HasPrefix(required, "ext-") {
					missingExtensions.Set(required, true)
				}
			}
		}
		if !isCausedByLock {
			if isCausedByLock, err = problem.IsCausedByLock(request, pool); err != nil {
				return "", err
			}
		}
	}

	var text strings.Builder
	text.WriteByte('\n')
	for i, problem := range uniqueStrings(problems) {
		text.WriteString("  Problem " + strconv.Itoa(i+1) + problem)
	}
	result := text.String()

	var hints []string
	if !isDevExtraction && (strings.Contains(result, "could not be found") || strings.Contains(result, "no matching package found")) {
		hints = append(hints, "Potential causes:\n - A typo in the package name\n - The package is not available in a stable-enough version according to your minimum-stability setting\n   see <https://getcomposer.org/doc/04-schema.md#minimum-stability> for more details.\n - It's a private package and you forgot to add a custom repository to find it\n\nRead <https://getcomposer.org/doc/articles/troubleshooting.md> for further common problems.")
	}

	if missingExtensions.Len() > 0 {
		hints = append(hints, createExtensionHint(missingExtensions.Keys(), env))
	}

	if isCausedByLock && !isDevExtraction && !request.UpdateAllowTransitiveRootDependencies() {
		hints = append(hints, "Use the option --with-all-dependencies (-W) to allow upgrades, downgrades and removals for packages currently locked to specific versions.")
	}

	if strings.Contains(result, "found composer-plugin-api[2.0.0] but it does not match") && strings.Contains(result, "- ocramius/package-versions") {
		hints = append(hints, "<warning>ocramius/package-versions only provides support for Composer 2 in 1.8+, which requires PHP 7.4.</warning>\nIf you can not upgrade PHP you can require <info>composer/package-versions-deprecated</info> to resolve this with PHP 7.0+.")
	}

	// Composer skips this hint when PHPUnit is loaded (its own test suite)
	if strings.Contains(result, "found composer-plugin-api[2.0.0] but it does not match") {
		hints = append(hints, "You are using Composer 2, which some of your plugins seem to be incompatible with. Make sure you update your plugins or report a plugin-issue to ask them to support Composer 2.")
	}

	if len(hints) > 0 {
		result += "\n" + strings.Join(hints, "\n\n")
	}

	return result, nil
}

// createExtensionHint ports SolverProblemsException::createExtensionHint.
func createExtensionHint(missingExtensions []string, env Environment) string {
	var paths []string
	if env != nil {
		paths = env.IniFiles()
	}
	if len(paths) == 0 {
		paths = []string{""}
	}
	if paths[0] == "" {
		if len(paths) == 1 {
			return ""
		}
		paths = paths[1:]
	}

	arguments := make([]string, 0, len(missingExtensions))
	for _, extension := range uniqueStrings(missingExtensions) {
		arguments = append(arguments, "--ignore-platform-req="+extension)
	}

	text := "To enable extensions, verify that they are enabled in your .ini files:\n    - "
	text += strings.Join(paths, "\n    - ")
	text += "\nYou can also run `php --ini` in a terminal to see which files are used by PHP in CLI mode."
	text += "\nAlternatively, you can run Composer with `" + strings.Join(arguments, " ") + "` to temporarily ignore these required extensions."

	return text
}

// IsCausedByLock ports Problem::isCausedByLock.
func (p *Problem) IsCausedByLock(request *Request, pool *Pool) (bool, error) {
	for _, section := range p.reasons {
		for _, rule := range section {
			caused, err := rule.IsCausedByLock(request, pool)
			if err != nil || caused {
				return caused, err
			}
		}
	}

	return false, nil
}

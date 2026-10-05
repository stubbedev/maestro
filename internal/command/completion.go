// Ports src/Composer/Command/CompletionTrait.php.

package command

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
)

// The suggestion callbacks run during completion (`composer _complete`).
// PHP closures throw; here a failure panics with a console.Throwable,
// which CompleteCommand recovers exactly as Symfony catches the exception.

func suggestStrings(values []string) []console.Suggestion {
	out := make([]console.Suggestion, len(values))
	for i, v := range values {
		out[i] = console.Suggestion{Value: v}
	}

	return out
}

// must panics with err as a Throwable (see above).
func must[T any](v T, err error) T {
	if err != nil {
		panic(asThrowable(err, -1))
	}

	return v
}

// SuggestPreferInstall ports suggestPreferInstall.
func SuggestPreferInstall() []string { return []string{"dist", "source", "auto"} }

// SuggestRootRequirement ports suggestRootRequirement: the names of the
// root requires and require-devs.
func (c *BaseCommand) SuggestRootRequirement() console.SuggestFunc {
	return func(*console.CompletionInput, *console.CompletionSuggestions) []console.Suggestion {
		composer := must(c.RequireComposer(nil, nil))
		root := composer.Package()

		var names []string
		for name := range root.Requires().All() {
			names = append(names, name)
		}
		for name := range root.DevRequires().All() {
			names = append(names, name)
		}

		return suggestStrings(names)
	}
}

// installedRepos is the repository list of suggestInstalledPackage(Types).
func installedRepos(composer *composer.Composer, includeRootPackage bool) []repository.RepositoryInterface {
	var repos []repository.RepositoryInterface
	if includeRootPackage {
		root, _ := pkg.Clone(composer.Package()).(pkg.RootPackageInterface)
		repos = append(repos, must(repository.NewRootPackageRepository(root)))
	}

	locker := composer.Locker()
	if must(locker.IsLocked()) {
		repos = append(repos, must(locker.LockedRepository(true)))
	} else {
		repos = append(repos, composer.RepositoryManager().LocalRepository())
	}

	return repos
}

// SuggestInstalledPackage ports suggestInstalledPackage: the names of the
// installed (or locked) packages.
func (c *BaseCommand) SuggestInstalledPackage(includeRootPackage, includePlatformPackages bool) console.SuggestFunc {
	return func(input *console.CompletionInput, _ *console.CompletionSuggestions) []console.Suggestion {
		composer := must(c.RequireComposer(nil, nil))
		repos := installedRepos(composer, includeRootPackage)

		var platformHint []string
		if includePlatformPackages {
			locker := composer.Locker()
			var overrides *php.Array
			if must(locker.IsLocked()) {
				overrides = must(locker.PlatformOverrides())
			} else {
				overrides, _ = must(composer.Config().Get("platform", 0)).(*php.Array)
			}
			platformRepo := must(repository.NewPlatformRepository(nil, overrides, must(composer.Runtime().PlatformOptions(composer.ProcessExecutor()))))
			if input.CompletionValue() == "" {
				// to reduce noise, when no text is yet entered we list only two entries for ext- and lib- prefixes
				hintsToFind := []struct {
					prefix string
					count  int
				}{{"ext-", 0}, {"lib-", 0}, {"php", 99}, {"composer", 99}}
				for _, p := range must(platformRepo.Packages()) {
					name := p.Name()
					for i := 0; i < len(hintsToFind); i++ {
						h := &hintsToFind[i]
						if !strings.HasPrefix(name, h.prefix) {
							continue
						}
						if h.count == 0 || h.count >= 99 {
							platformHint = append(platformHint, name)
							h.count++
						} else if h.count == 1 {
							hintsToFind = slices.Delete(hintsToFind, i, i+1)
							platformHint = append(platformHint, name[:max(len(name)-3, len(h.prefix)+1)]+"...")
						}

						break
					}
				}
			} else {
				repos = append(repos, platformRepo)
			}
		}

		installedRepo := must(repository.NewInstalledRepository(repos))
		var names []string
		for _, p := range must(installedRepo.Packages()) {
			names = append(names, p.Name())
		}

		return suggestStrings(append(names, platformHint...))
	}
}

// SuggestInstalledPackageTypes ports suggestInstalledPackageTypes.
func (c *BaseCommand) SuggestInstalledPackageTypes(includeRootPackage bool) console.SuggestFunc {
	return func(*console.CompletionInput, *console.CompletionSuggestions) []console.Suggestion {
		composer := must(c.RequireComposer(nil, nil))
		installedRepo := must(repository.NewInstalledRepository(installedRepos(composer, includeRootPackage)))

		var types []string
		seen := map[string]bool{}
		for _, p := range must(installedRepo.Packages()) {
			if t := p.Type(); !seen[t] {
				seen[t] = true
				types = append(types, t)
			}
		}

		return suggestStrings(types)
	}
}

// SuggestAvailablePackage ports suggestAvailablePackage($max): package
// (or vendor) names from all configured repositories.
func (c *BaseCommand) SuggestAvailablePackage(maxResults int) console.SuggestFunc {
	return func(input *console.CompletionInput, _ *console.CompletionSuggestions) []console.Suggestion {
		return suggestStrings(c.availablePackages(input, maxResults))
	}
}

func (c *BaseCommand) availablePackages(input *console.CompletionInput, maxResults int) []string {
	if maxResults < 1 {
		return nil
	}

	composer := must(c.RequireComposer(nil, nil))
	repos := must(repository.NewCompositeRepository(composer.RepositoryManager().Repositories()))

	value := input.CompletionValue()
	var results []repository.SearchResult
	showVendors := false
	if !strings.Contains(value, "/") {
		results = must(repos.Search("^"+php.PregQuote(value, ""), repository.SearchVendor, ""))
		showVendors = true
	}

	// if we get a single vendor, we expand it into its contents already
	if len(results) <= 1 {
		results = must(repos.Search("^"+php.PregQuote(value, ""), repository.SearchName, ""))
		showVendors = false
	}

	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.Name
	}

	if showVendors {
		for i, n := range names {
			names[i] = n + "/"
		}

		// sort shorter results first to avoid auto-expanding the completion to a longer string than needed
		slices.SortStableFunc(names, func(a, b string) int {
			if len(a) == len(b) {
				return strings.Compare(a, b)
			}

			return cmp.Compare(len(a), len(b))
		})

		var pinned []string

		// ensure if the input is an exact match that it is always in the result set
		completionInput := value + "/"
		if i := slices.Index(names, completionInput); i >= 0 {
			pinned = append(pinned, completionInput)
			names = slices.Delete(names, i, i+1)
		}

		return append(pinned, phpArraySlice(names, maxResults-len(pinned))...)
	}

	return phpArraySlice(names, maxResults)
}

// phpArraySlice is array_slice($a, 0, $length) (a negative length stops
// that many entries from the end).
func phpArraySlice(a []string, length int) []string {
	if length < 0 {
		length = max(0, len(a)+length)
	}

	return a[:min(length, len(a))]
}

var platformPrefix = php.MustCompile(`{^(ext|lib|php)(-|$)|^com}`)

// SuggestAvailablePackageInclPlatform ports
// suggestAvailablePackageInclPlatform.
func (c *BaseCommand) SuggestAvailablePackageInclPlatform() console.SuggestFunc {
	return func(input *console.CompletionInput, suggestions *console.CompletionSuggestions) []console.Suggestion {
		var matches []console.Suggestion
		if must(platformPrefix.IsMatch(input.CompletionValue())) {
			matches = c.SuggestPlatformPackage()(input, suggestions)
		}

		return append(matches, c.SuggestAvailablePackage(99-len(matches))(input, suggestions)...)
	}
}

// SuggestPlatformPackage ports suggestPlatformPackage.
func (c *BaseCommand) SuggestPlatformPackage() console.SuggestFunc {
	return func(input *console.CompletionInput, _ *console.CompletionSuggestions) []console.Suggestion {
		composer := must(c.RequireComposer(nil, nil))
		overrides, _ := must(composer.Config().Get("platform", 0)).(*php.Array)
		repos := must(repository.NewPlatformRepository(nil, overrides, must(composer.Runtime().PlatformOptions(composer.ProcessExecutor()))))

		pattern := must(php.Compile(pkg.PackageNameToRegexp(input.CompletionValue()+"*", "{^%s$}i")))

		var names []string
		for _, p := range must(repos.Packages()) {
			if must(pattern.IsMatch(p.Name())) {
				names = append(names, p.Name())
			}
		}

		return suggestStrings(names)
	}
}

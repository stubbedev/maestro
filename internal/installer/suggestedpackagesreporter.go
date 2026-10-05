// Ports src/Composer/Installer/SuggestedPackagesReporter.php.

package installer

import (
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
)

// The output modes of SuggestedPackagesReporter.Output.
const (
	ModeList         = 1
	ModeByPackage    = 2
	ModeBySuggestion = 4
)

// Suggestion is one suggested package: the source package suggesting
// target, and why.
type Suggestion struct {
	Source string
	Target string
	Reason string
}

// SuggestedPackagesReporter ports Composer\Installer\SuggestedPackagesReporter.
type SuggestedPackagesReporter struct {
	suggestedPackages []Suggestion
	io                mio.IO
}

// NewSuggestedPackagesReporter is new SuggestedPackagesReporter($io).
func NewSuggestedPackagesReporter(io mio.IO) *SuggestedPackagesReporter {
	return &SuggestedPackagesReporter{io: io}
}

// Packages is getPackages().
func (r *SuggestedPackagesReporter) Packages() []Suggestion {
	return r.suggestedPackages
}

// AddPackage is addPackage(): a suggested package to list after install.
func (r *SuggestedPackagesReporter) AddPackage(source, target, reason string) *SuggestedPackagesReporter {
	r.suggestedPackages = append(r.suggestedPackages, Suggestion{Source: source, Target: target, Reason: reason})

	return r
}

// AddSuggestionsFromPackage is addSuggestionsFromPackage().
func (r *SuggestedPackagesReporter) AddSuggestionsFromPackage(p pkg.PackageInterface) *SuggestedPackagesReporter {
	source := p.PrettyName()

	if suggests := p.Suggests(); suggests != nil {
		for target, reason := range suggests.All() {
			r.AddPackage(source, target.String(), php.ToString(reason))
		}
	}

	return r
}

// Output is output(): the suggestions in the given mode, leaving out those
// installedRepo (nil for null) provides and, with onlyDependentsOf, those
// not made by that package or its direct dependencies.
func (r *SuggestedPackagesReporter) Output(mode int, installedRepo *repository.InstalledRepository, onlyDependentsOf pkg.PackageInterface) error {
	suggestedPackages, err := r.filteredSuggestions(installedRepo, onlyDependentsOf)
	if err != nil {
		return err
	}

	suggesters := php.NewArray()
	suggested := php.NewArray()

	for _, s := range suggestedPackages {
		nested(suggesters, s.Source).Set(s.Target, s.Reason)
		nested(suggested, s.Target).Set(s.Source, s.Reason)
	}

	php.Ksort(suggesters, php.SortRegular)
	php.Ksort(suggested, php.SortRegular)

	// Simple mode
	if mode&ModeList != 0 {
		for _, name := range suggested.Keys() {
			r.io.Write("<info>"+name.String()+"</info>", true, mio.Normal)
		}

		return nil
	}

	// Grouped by package
	if mode&ModeByPackage != 0 {
		for suggester, suggestions := range suggesters.All() {
			r.io.Write("<comment>"+suggester.String()+"</comment> suggests:", true, mio.Normal)

			byTarget, _ := suggestions.(*php.Array)

			for suggestion, reason := range byTarget.All() {
				line, err := suggestionLine(suggestion.String(), php.ToString(reason))
				if err != nil {
					return err
				}

				r.io.Write(line, true, mio.Normal)
			}

			r.io.Write("", true, mio.Normal)
		}
	}

	// Grouped by suggestion
	if mode&ModeBySuggestion != 0 {
		// Improve readability in full mode
		if mode&ModeByPackage != 0 {
			r.io.Write(strings.Repeat("-", 78), true, mio.Normal)
		}

		for suggestion, suggestersOf := range suggested.All() {
			r.io.Write("<comment>"+suggestion.String()+"</comment> is suggested by:", true, mio.Normal)

			bySource, _ := suggestersOf.(*php.Array)

			for suggester, reason := range bySource.All() {
				line, err := suggestionLine(suggester.String(), php.ToString(reason))
				if err != nil {
					return err
				}

				r.io.Write(line, true, mio.Normal)
			}

			r.io.Write("", true, mio.Normal)
		}
	}

	if onlyDependentsOf != nil {
		allSuggestedPackages, err := r.filteredSuggestions(installedRepo, nil)
		if err != nil {
			return err
		}

		if diff := len(allSuggestedPackages) - len(suggestedPackages); diff != 0 {
			r.io.Write("<info>"+php.ToString(diff)+" additional suggestions</info> by transitive dependencies can be shown with <info>--all</info>", true, mio.Normal)
		}
	}

	return nil
}

// suggestionLine is sprintf(' - <info>%s</info>' . ($reason ? ': %s' :
// ”), $name, $this->escapeOutput($reason)).
func suggestionLine(name, reason string) (string, error) {
	escaped, err := escapeOutput(reason)
	if err != nil {
		return "", err
	}

	if !php.ToBool(reason) {
		return " - <info>" + name + "</info>", nil
	}

	return " - <info>" + name + "</info>: " + escaped, nil
}

// nested is `$a[$key]`, created as an empty array when missing.
func nested(a *php.Array, key string) *php.Array {
	if v, ok := a.GetArray(key); ok {
		return v
	}

	v := php.NewArray()
	a.Set(key, v)

	return v
}

// OutputMinimalistic is outputMinimalistic(): the number of new
// suggestions and a hint to use the suggest command.
func (r *SuggestedPackagesReporter) OutputMinimalistic(installedRepo *repository.InstalledRepository, onlyDependentsOf pkg.PackageInterface) error {
	suggestedPackages, err := r.filteredSuggestions(installedRepo, onlyDependentsOf)
	if err != nil {
		return err
	}

	if len(suggestedPackages) > 0 {
		r.io.WriteError("<info>"+php.ToString(len(suggestedPackages))+" package suggestions were added by new dependencies, use `composer suggest` to see details.</info>", true, mio.Normal)
	}

	return nil
}

// filteredSuggestions is getFilteredSuggestions().
func (r *SuggestedPackagesReporter) filteredSuggestions(installedRepo *repository.InstalledRepository, onlyDependentsOf pkg.PackageInterface) ([]Suggestion, error) {
	suggestedPackages := r.Packages()
	installedNames := map[string][]string{}

	if installedRepo != nil && len(suggestedPackages) > 0 {
		packages, err := installedRepo.Packages()
		if err != nil {
			return nil, err
		}

		for _, p := range packages {
			packageName := p.Name()
			for _, name := range p.Names(true) {
				installedNames[name] = append(installedNames[name], packageName)
			}
		}
	}

	var sourceFilter []string

	if onlyDependentsOf != nil {
		// array_merge() of the require and require-dev links: only
		// membership matters below, so the targets of both are enough
		for _, link := range onlyDependentsOf.Requires().All() {
			sourceFilter = append(sourceFilter, link.Target())
		}

		for _, link := range onlyDependentsOf.DevRequires().All() {
			sourceFilter = append(sourceFilter, link.Target())
		}

		sourceFilter = append(sourceFilter, onlyDependentsOf.Name())
	}

	var suggestions []Suggestion

	for _, suggestion := range suggestedPackages {
		// Only treat a suggestion as fulfilled when a package other than
		// the one making it provides the target, so a package that both
		// provides and suggests a name (e.g. an extension polyfill) does
		// not hide its own suggestion.
		providedByOthers := false

		for _, providerName := range installedNames[suggestion.Target] {
			if providerName != php.Strtolower(suggestion.Source) {
				providedByOthers = true

				break
			}
		}

		if providedByOthers || (len(sourceFilter) > 0 && !inArrayLoose(suggestion.Source, sourceFilter)) {
			continue
		}

		suggestions = append(suggestions, suggestion)
	}

	return suggestions, nil
}

// inArrayLoose is in_array($needle, $haystack) for strings.
func inArrayLoose(needle string, haystack []string) bool {
	for _, s := range haystack {
		if php.StringsLooseEqual(needle, s) {
			return true
		}
	}

	return false
}

// controlCharacters is removeControlCharacters()'s pattern.
var controlCharacters = php.MustCompile(`/[[:cntrl:]]/`)

// escapeOutput is escapeOutput(): control characters dropped (newlines
// become spaces) and the console formatting escaped.
func escapeOutput(s string) (string, error) {
	out, _, err := controlCharacters.Replace(strings.ReplaceAll(s, "\n", " "), "", -1)
	if err != nil {
		return "", err
	}

	return console.Escape(out), nil
}

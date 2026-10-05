// Ports src/Composer/DependencyResolver/Problem.php.

package resolver

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// Environment is what the problem messages read from the PHP process
// running Composer. A nil Environment is a PHP without extensions or ini
// files.
type Environment interface {
	// ExtensionLoaded ports extension_loaded($name).
	ExtensionLoaded(name string) bool
	// IniFiles ports IniHelper::getAll(): the php.ini, then the scanned
	// ini files ("" first when there is no php.ini).
	IniFiles() []string
}

// PrettyContext holds the arguments every getPrettyString of the
// DependencyResolver passes along.
type PrettyContext struct {
	RepositorySet *repository.RepositorySet
	Request       *Request
	Pool          *Pool
	IsVerbose     bool
	// InstalledMap holds the ids of the present packages
	// (Request.PresentIDMap).
	InstalledMap map[int]bool
	// LearnedPool is the solver's learned pool (unused by the messages,
	// as in Composer).
	LearnedPool [][]*Rule
	Env         Environment
}

func (ctx *PrettyContext) extensionLoaded(name string) bool {
	return ctx.Env != nil && ctx.Env.ExtensionLoaded(name)
}

// Problem ports Composer\DependencyResolver\Problem: the rules that make
// up one reason why the request cannot be solved, in sections.
type Problem struct {
	reasonSeen map[*Rule]bool
	reasons    [][]*Rule
	section    int
}

// NewProblem is new Problem().
func NewProblem() *Problem { return &Problem{reasonSeen: map[*Rule]bool{}} }

// AddRule ports addRule.
func (p *Problem) AddRule(rule *Rule) { p.addReason(rule) }

// Reasons ports getReasons: the rules by section. Sections without rules
// are empty.
func (p *Problem) Reasons() [][]*Rule { return p.reasons }

func (p *Problem) addReason(reason *Rule) {
	// TODO: if a rule is part of a problem description in two sections, isn't this going to remove a message
	// that is important to understand the issue?
	if p.reasonSeen[reason] {
		return
	}
	p.reasonSeen[reason] = true
	for len(p.reasons) <= p.section {
		p.reasons = append(p.reasons, nil)
	}
	p.reasons[p.section] = append(p.reasons[p.section], reason)
}

// NextSection ports nextSection.
func (p *Problem) NextSection() { p.section++ }

// PrettyString ports getPrettyString.
func (p *Problem) PrettyString(ctx *PrettyContext) (string, error) {
	// TODO doesn't this entirely defeat the purpose of the problem sections? what's the point of sections?
	var reasons []*Rule
	for _, section := range slices.Backward(p.reasons) {
		reasons = append(reasons, section...)
	}

	if len(reasons) == 1 {
		rule := reasons[0]
		switch rule.Reason() {
		case RuleRootRequire:
			rr := rule.RootRequire()
			if len(ctx.Pool.WhatProvides(rr.PackageName, rr.Constraint)) == 0 {
				reason, err := ctx.MissingPackageReason(rr.PackageName, rr.Constraint)
				if err != nil {
					return "", err
				}

				return "\n    " + reason[0] + reason[1], nil
			}
		case RuleLockedFilterListRemoved:
			// Solver::checkForFilterListRemovedLockedPackages emits these
			// for locked packages that the policy filter list dropped from
			// the pool (typically malware blocked at install time).
			reason, err := MissingLockedPackageReason(ctx.Pool, rule.Package())
			if err != nil {
				return "", err
			}

			return "\n    " + reason[0] + reason[1], nil
		}
	}

	type sortable struct {
		priority int
		key      string
	}
	keys := make(map[*Rule]sortable, len(reasons))
	for _, rule := range reasons {
		key, err := sortableString(ctx.Pool, rule)
		if err != nil {
			return "", err
		}
		keys[rule] = sortable{rulePriority(rule), key}
	}
	php.SortSlice(reasons, func(rule1, rule2 *Rule) int {
		k1, k2 := keys[rule1], keys[rule2]
		if k1.priority != k2.priority {
			return k2.priority - k1.priority
		}

		return php.Compare(k1.key, k2.key)
	})

	return FormatDeduplicatedRules(reasons, "    ", ctx)
}

// sortableString ports getSortableString.
func sortableString(pool *Pool, rule *Rule) (string, error) {
	switch rule.Reason() {
	case RuleRootRequire:
		return rule.RootRequire().PackageName, nil
	case RuleFixed, RuleLockedFilterListRemoved:
		return rule.Package().String(), nil
	case RulePackageConflict, RulePackageRequires:
		source, err := rule.SourcePackage(pool)
		if err != nil {
			return "", err
		}

		return source.String() + "//" + rule.Link().PrettyString(source), nil
	case RulePackageSameName:
		name, _ := rule.ReasonData().(string)

		return name, nil
	case RulePackageAlias, RulePackageInverseAlias:
		return rule.Package().String(), nil
	case RuleLearned:
		parts := make([]string, len(rule.Literals()))
		for i, l := range rule.Literals() {
			parts[i] = strconv.Itoa(int(l))
		}

		return strings.Join(parts, "-"), nil
	}

	return "", &util.LogicError{Site: phperr.At("Problem.php", 140), Message: "Unknown rule type: " + strconv.Itoa(rule.Reason())}
}

// rulePriority ports getRulePriority.
func rulePriority(rule *Rule) int {
	switch rule.Reason() {
	case RuleFixed, RuleLockedFilterListRemoved:
		return 3
	case RuleRootRequire:
		return 2
	case RulePackageConflict, RulePackageRequires:
		return 1
	}

	return 0
}

var (
	dedupMessageRegex  = php.MustCompile(`{^(?P<package>\S+) (?P<version>\S+) (?P<type>requires|conflicts)}`)
	dedupTemplateRegex = php.MustCompile(`{^\S+ \S+ }`)
	dedupGrammarRegex  = php.MustCompile(`{^(%s%s (?:require|conflict))s}`)
)

// FormatDeduplicatedRules ports Problem::formatDeduplicatedRules.
func FormatDeduplicatedRules(rules []*Rule, indent string, ctx *PrettyContext) (string, error) {
	var messages []string
	// template => package name => normalized version => pretty version
	templates := map[string]*repository.NameMap[*VersionMap]{}
	parser := pkg.NewVersionParser()

	for _, rule := range rules {
		message, err := rule.PrettyString(ctx)
		if err != nil {
			return "", err
		}
		deduplicatable := rule.Reason() == RulePackageRequires || rule.Reason() == RulePackageConflict
		var m *php.Match
		if deduplicatable {
			if m, err = dedupMessageRegex.MatchStrictGroups(message); err != nil {
				return "", err
			}
		}
		if m == nil {
			if message != "" {
				messages = append(messages, message)
			}

			continue
		}

		message = strings.ReplaceAll(message, "%", "%%")
		template, _, err := dedupTemplateRegex.Replace(message, "%s%s ", -1)
		if err != nil {
			return "", err
		}
		messages = append(messages, template)
		byPackage, ok := templates[template]
		if !ok {
			byPackage = &repository.NameMap[*VersionMap]{}
			templates[template] = byPackage
		}
		versions, ok := byPackage.Get(m.Get(1))
		if !ok {
			versions = &VersionMap{}
			byPackage.Set(m.Get(1), versions)
		}
		normalized, err := parser.Normalize(m.Get(2))
		if err != nil {
			return "", err
		}
		versions.Set(normalized, m.Get(2))
		sourcePackage, err := rule.SourcePackage(ctx.Pool)
		if err != nil {
			return "", err
		}
		for version, prettyVersion := range ctx.Pool.RemovedVersionsByPackage(sourcePackage).All() {
			versions.Set(version, prettyVersion)
		}
	}

	var result []string
	for _, message := range uniqueStrings(messages) {
		byPackage, ok := templates[message]
		if !ok {
			result = append(result, message)

			continue
		}
		for packageName, versionMap := range byPackage.All() {
			versions := sortVersionMap(versionMap)
			if !ctx.IsVerbose {
				versions = condenseVersionList(versions, 1, 16)
			}
			if len(versions) > 1 {
				// remove the s from requires/conflicts to correct grammar
				var err error
				if message, _, err = dedupGrammarRegex.Replace(message, "$1", -1); err != nil {
					return "", err
				}
				line, err := php.Sprintf(message, packageName, "["+joinPretty(versions)+"]")
				if err != nil {
					return "", err
				}
				result = append(result, line)
			} else {
				line, err := php.Sprintf(message, packageName, " "+versions[0].pretty)
				if err != nil {
					return "", err
				}
				result = append(result, line)
			}
		}
	}

	return "\n" + indent + "- " + strings.Join(result, "\n"+indent+"- "), nil
}

// uniqueStrings ports array_unique on a list of strings.
func uniqueStrings(list []string) []string {
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	return out
}

// versionEntry is one entry of a version => pretty version array.
type versionEntry struct{ version, pretty string }

// sortVersionMap ports uksort($versions, 'version_compare'), returning the
// pretty versions in order.
func sortVersionMap(m *VersionMap) []versionEntry {
	entries := make([]versionEntry, 0, m.Len())
	for v, p := range m.All() {
		entries = append(entries, versionEntry{v, p})
	}
	php.SortSlice(entries, func(a, b versionEntry) int { return semver.VersionCompare(a.version, b.version) })

	return entries
}

// MissingPackageReason ports Problem::getMissingPackageReason: the message
// prefix and the reason why no package satisfies name and constraint (nil:
// any version).
func (ctx *PrettyContext) MissingPackageReason(packageName string, constraint semver.ConstraintInterface) ([2]string, error) {
	pool := ctx.Pool
	repositorySet := ctx.RepositorySet
	if repository.IsPlatformPackage(packageName) {
		lower := php.Strtolower(packageName)
		// handle php/php-*/hhvm
		if strings.HasPrefix(lower, "php") || packageName == "hhvm" {
			version, err := platformPackageVersion(pool, packageName)
			if err != nil {
				return [2]string{}, err
			}
			msg := "- Root composer.json requires " + packageName + constraintToText(constraint) + " but "
			if packageName == "hhvm" && len(pool.WhatProvides(packageName, nil)) > 0 {
				return [2]string{msg, "your HHVM version does not satisfy that requirement."}, nil
			}
			if packageName == "hhvm" {
				return [2]string{msg, "HHVM was not detected on this machine, make sure it is in your PATH."}, nil
			}
			if !version.Valid {
				return [2]string{msg, `the ` + packageName + ` package is disabled by your platform config. Enable it again with "composer config platform.` + packageName + ` --unset".`}, nil
			}

			return [2]string{msg, "your " + packageName + " version (" + version.S + ") does not satisfy that requirement."}, nil
		}

		// handle php extensions
		if strings.HasPrefix(lower, "ext-") {
			if strings.Contains(packageName, " ") {
				return [2]string{"- ", "PHP extension " + packageName + " should be required as " + strings.ReplaceAll(packageName, " ", "-") + "."}, nil
			}

			ext := packageName[4:]
			msg := "- Root composer.json requires PHP extension " + packageName + constraintToText(constraint) + " but "

			version, err := platformPackageVersion(pool, packageName)
			if err != nil {
				return [2]string{}, err
			}
			if !version.Valid {
				providersStr, err := providersList(repositorySet, packageName, 5)
				if err != nil {
					return [2]string{}, err
				}
				if providersStr.Valid {
					providersStr.S = "\n\n      Alternatively you can require one of these packages that provide the extension (or parts of it):\n" +
						"      <warning>Keep in mind that the suggestions are automated and may not be valid or safe to use</warning>\n" + providersStr.S
				}

				if ctx.extensionLoaded(ext) {
					return [2]string{msg, `the ` + packageName + ` package is disabled by your platform config. Enable it again with "composer config platform.` + packageName + ` --unset".` + providersStr.S}, nil
				}

				return [2]string{msg, "it is missing from your system. Install or enable PHP's " + ext + " extension." + providersStr.S}, nil
			}

			return [2]string{msg, "it has the wrong version installed (" + version.S + ")."}, nil
		}

		// handle linked libs
		if strings.HasPrefix(lower, "lib-") {
			if lower == "lib-icu" {
				errorMsg := "it is missing from your system, make sure the intl extension is loaded."
				if ctx.extensionLoaded("intl") {
					errorMsg = "it has the wrong version installed, try upgrading the intl extension."
				}

				return [2]string{"- Root composer.json requires linked library " + packageName + constraintToText(constraint) + " but ", errorMsg}, nil
			}

			providersStr, err := providersList(repositorySet, packageName, 5)
			if err != nil {
				return [2]string{}, err
			}
			if providersStr.Valid {
				providersStr.S = "\n\n      Alternatively you can require one of these packages that provide the library (or parts of it):\n" +
					"      <warning>Keep in mind that the suggestions are automated and may not be valid or safe to use</warning>\n" + providersStr.S
			}

			return [2]string{"- Root composer.json requires linked library " + packageName + constraintToText(constraint) + " but ", "it has the wrong version installed or is missing from your system, make sure to load the extension providing it." + providersStr.S}, nil
		}
	}

	var lockedPackage pkg.PackageInterface
	for _, p := range ctx.Request.LockedPackages() {
		if p.Name() == packageName {
			lockedPackage = p
			if pool.IsUnacceptableFixedOrLockedPackage(p) {
				return [2]string{"- ", p.PrettyName() + " is fixed to " + p.PrettyVersion() + " (lock file version) by a partial update but that version is rejected by your minimum-stability. Make sure you list it as an argument for the update command."}, nil
			}

			break
		}
	}

	prefix := "- Root composer.json requires " + packageName + constraintToText(constraint) + ", "

	c, ok := constraint.(*semver.Constraint)
	isDevRef := false
	if ok && c.Op() == semver.OpEQ {
		var err error
		if isDevRef, err = devRefRegex.IsMatch(c.PrettyString()); err != nil {
			return [2]string{}, err
		}
	}

	if isDevRef {
		newConstraint, _, err := branchAliasSuffixRegex.Replace(c.PrettyString(), "", -1)
		if err != nil {
			return [2]string{}, err
		}
		multi, err := semver.NewMultiConstraint([]semver.ConstraintInterface{
			semver.NewConstraintOp(semver.OpEQ, newConstraint),
			semver.NewConstraintOp(semver.OpEQ, strings.ReplaceAll(newConstraint, "#", "+")),
		}, false)
		if err != nil {
			return [2]string{}, err
		}
		packages, err := repositorySet.FindPackages(packageName, multi, 0)
		if err != nil {
			return [2]string{}, err
		}
		if len(packages) > 0 {
			return [2]string{prefix, "found " + PackageList(packages, ctx.IsVerbose, pool, constraint, false) + `. The # character in branch names is replaced by a + character. Make sure to require it as "` + strings.ReplaceAll(c.PrettyString(), "#", "+") + `".`}, nil
		}
	}

	// first check if the actual requested package is found in normal conditions
	// if so it must mean it is rejected by another constraint than the one given here
	packages, err := repositorySet.FindPackages(packageName, constraint, 0)
	if err != nil {
		return [2]string{}, err
	}
	if len(packages) > 0 {
		return ctx.foundButNotLoaded(packageName, constraint, packages, lockedPackage, prefix)
	}

	// check if the package is found when bypassing stability checks
	packages, err = repositorySet.FindPackages(packageName, constraint, repository.AllowUnacceptableStabilities)
	if err != nil {
		return [2]string{}, err
	}
	if len(packages) > 0 {
		// we must first verify if a valid package would be found in a lower priority repository
		allReposPackages, err := repositorySet.FindPackages(packageName, constraint, repository.AllowShadowedRepositories)
		if err != nil {
			return [2]string{}, err
		}
		if len(allReposPackages) > 0 {
			return computeCheckForLowerPrioRepo(pool, ctx.IsVerbose, packageName, packages, allReposPackages, "minimum-stability", constraint), nil
		}

		return [2]string{prefix, "found " + PackageList(packages, ctx.IsVerbose, pool, constraint, false) + " but " + pick(hasMultipleNames(packages), "these do", "it does") + " not match your minimum-stability."}, nil
	}

	// check if the package is found when bypassing the constraint and stability checks
	packages, err = repositorySet.FindPackages(packageName, nil, repository.AllowUnacceptableStabilities)
	if err != nil {
		return [2]string{}, err
	}
	if len(packages) > 0 {
		// we must first verify if a valid package would be found in a lower priority repository
		allReposPackages, err := repositorySet.FindPackages(packageName, constraint, repository.AllowShadowedRepositories)
		if err != nil {
			return [2]string{}, err
		}
		if len(allReposPackages) > 0 {
			return computeCheckForLowerPrioRepo(pool, ctx.IsVerbose, packageName, packages, allReposPackages, "constraint", constraint), nil
		}

		suffix := ""
		if c, ok := constraint.(*semver.Constraint); ok && c.Version() == "dev-master" {
			for _, candidate := range packages {
				if v := candidate.Version(); v == "dev-default" || v == "dev-main" {
					suffix = " Perhaps dev-master was renamed to " + candidate.PrettyVersion() + "?"

					break
				}
			}
		}

		// check if the root package is a name match and hint the dependencies on root troubleshooting article
		if _, ok := packages[0].(pkg.RootPackageInterface); ok {
			suffix = " See https://getcomposer.org/dep-on-root for details and assistance."
		}

		return [2]string{prefix, "found " + PackageList(packages, ctx.IsVerbose, pool, constraint, false) + " but " + pick(hasMultipleNames(packages), "these do", "it does") + " not match the constraint." + suffix}, nil
	}

	if valid, err := validPackageNameRegex.IsMatch(packageName); err != nil {
		return [2]string{}, err
	} else if !valid {
		illegalChars, _, err := packageNameCharsRegex.Replace(packageName, "", -1)
		if err != nil {
			return [2]string{}, err
		}

		return [2]string{"- Root composer.json requires " + packageName + ", it ", `could not be found, it looks like its name is invalid, "` + illegalChars + `" is not allowed in package names.`}, nil
	}

	providersStr, err := providersList(repositorySet, packageName, 15)
	if err != nil {
		return [2]string{}, err
	}
	if providersStr.Valid {
		return [2]string{"- Root composer.json requires " + packageName + constraintToText(constraint) + ", it ", "could not be found in any version, but the following packages provide it:\n" + providersStr.S + "      Consider requiring one of these to satisfy the " + packageName + " requirement."}, nil
	}

	return [2]string{"- Root composer.json requires " + packageName + ", it ", "could not be found in any version, there may be a typo in the package name."}, nil
}

var (
	branchAliasSuffixRegex = php.MustCompile(`{ +as +([^,\s|]+)$}`)
	devRefRegex            = php.MustCompile(`{^dev-.*#.*}`)
	validPackageNameRegex  = php.MustCompile(`{^[A-Za-z0-9_./-]+$}`)
	packageNameCharsRegex  = php.MustCompile(`{[A-Za-z0-9_./-]+}`)
	exactVersionRegex      = php.MustCompile(`{^\d+(?:\.\d+)*$}`)
)

// foundButNotLoaded is the part of getMissingPackageReason for a package
// the repositories have in a matching version.
func (ctx *PrettyContext) foundButNotLoaded(packageName string, constraint semver.ConstraintInterface, packages []pkg.PackageInterface, lockedPackage pkg.PackageInterface, prefix string) ([2]string, error) {
	pool := ctx.Pool
	repositorySet := ctx.RepositorySet
	list := func() string { return PackageList(packages, ctx.IsVerbose, pool, constraint, false) }
	anyMatches := func(c semver.ConstraintInterface) bool {
		return slices.ContainsFunc(packages, func(p pkg.PackageInterface) bool { return matchesVersion(c, p.Version()) })
	}

	if rootReq, ok := repositorySet.RootRequires().Get(packageName); ok && !anyMatches(rootReq) {
		return [2]string{prefix, "found " + list() + " but " + pick(hasMultipleNames(packages), "these conflict", "it conflicts") + " with your root composer.json require (" + rootReq.PrettyString() + ")."}, nil
	}

	tempReqs := repositorySet.TemporaryConstraints()
	for _, name := range packages[0].Names(true) {
		if tempReq, ok := tempReqs.Get(name); ok && !anyMatches(tempReq) {
			return [2]string{"- Root composer.json requires " + name + constraintToText(constraint) + ", ", "found " + list() + " but " + pick(hasMultipleNames(packages), "these conflict", "it conflicts") + " with your temporary update constraint (" + name + ":" + tempReq.PrettyString() + ")."}, nil
		}
	}

	if lockedPackage != nil {
		fixedConstraint := semver.NewConstraintOp(semver.OpEQ, lockedPackage.Version())
		if !anyMatches(fixedConstraint) {
			return [2]string{prefix, "found " + list() + " but the package is fixed to " + lockedPackage.PrettyVersion() + " (lock file version) by a partial update and that version does not match. Make sure you list it as an argument for the update command."}, nil
		}
	}

	if pool.IsAbandonedRemovedPackageVersion(packageName, constraint) {
		return [2]string{prefix, "found " + list() + ` but these were not loaded, because they are abandoned and you configured "policy.abandoned.block" to true.`}, nil
	}

	if pool.IsSecurityRemovedPackageVersion(packageName, constraint) {
		return ctx.securityRemoved(packageName, constraint, packages, prefix)
	}

	if pool.IsFilterListRemovedPackageVersion(packageName, constraint) {
		filters := pool.FilterListEntryForPackageVersion(packageName, constraint)
		ignorePaths, offPaths := policyPaths(filters)

		return [2]string{prefix, "found " + list() + " but these were not loaded, because they were " + strings.Join(mapValues(filters), ", ") + ". To ignore filters for this package, add the package to the " + ignorePaths + " config. To turn the feature off entirely, you can set " + offPaths + " to false."}, nil
	}

	if !slices.ContainsFunc(packages, func(p pkg.PackageInterface) bool {
		_, locked := p.Repository().(*repository.LockArrayRepository)

		return !locked
	}) {
		return [2]string{prefix, "found " + list() + " in the lock file but not in remote repositories, make sure you avoid updating this package to keep the one from the lock file."}, nil
	}

	return [2]string{prefix, "found " + list() + " but these were not loaded, likely because " + pick(hasMultipleNames(packages), "they conflict", "it conflicts") + " with another require."}, nil
}

// securityRemoved is getMissingPackageReason's message for versions the
// security advisory filter removed.
func (ctx *PrettyContext) securityRemoved(packageName string, constraint semver.ConstraintInterface, packages []pkg.PackageInterface, prefix string) ([2]string, error) {
	advisoryLink := func(advisoryID string) string {
		if strings.HasPrefix(advisoryID, "PKSA-") {
			return "<href=" + console.Escape("https://packagist.org/security-advisories/"+advisoryID) + ">" + advisoryID + "</>"
		}

		return advisoryID
	}

	result, err := ctx.RepositorySet.GetMatchingSecurityAdvisories(packages, false, true)
	if err != nil {
		return [2]string{}, err
	}
	var advisoriesList, advisoryIDs []string
	if advisories, _ := result.Advisories.Get(packageName); len(advisories) > 0 {
		for _, advisory := range advisories {
			id := advisory.Partial().AdvisoryID
			advisoryIDs = append(advisoryIDs, id)
			if sa, ok := advisory.(*repository.SecurityAdvisory); ok && sa.Link.Valid && sa.Link.S != "" {
				advisoriesList = append(advisoriesList, "<href="+console.Escape(sa.Link.S)+">"+id+"</>")
			} else {
				advisoriesList = append(advisoriesList, advisoryLink(id))
			}
		}
	} else {
		advisoryIDs = ctx.Pool.SecurityAdvisoryIdentifiersForPackageVersion(packageName, constraint)
		for _, id := range advisoryIDs {
			advisoriesList = append(advisoriesList, advisoryLink(id))
		}
	}

	hasPackagistAdvisories := true
	for _, id := range advisoryIDs {
		if !strings.HasPrefix(id, "PKSA-") {
			hasPackagistAdvisories = false

			break
		}
	}
	advisoryDetailsHint := " Review the advisory details above for more information."
	if hasPackagistAdvisories {
		advisoryDetailsHint = " Go to https://packagist.org/security-advisories/ to find advisory details."
	}

	return [2]string{prefix, "found " + PackageList(packages, ctx.IsVerbose, ctx.Pool, constraint, false) + ` but these were not loaded, because they are affected by security advisories ("` + strings.Join(advisoriesList, `", "`) + `").` + advisoryDetailsHint + ` To ignore the advisories, add their IDs to the "policy.advisories.ignore-id" config or add the package to "policy.advisories.ignore". To turn the feature off entirely, you can set "policy.advisories.block" to false.`}, nil
}

// policyPaths returns the "policy.<list>.ignore" and "policy.<list>.block"
// config paths of the filter lists.
func policyPaths(filters *repository.NameMap[string]) (ignorePaths, offPaths string) {
	var ignore, off []string
	for listName := range filters.All() {
		ignore = append(ignore, `"policy.`+listName+`.ignore"`)
		off = append(off, `"policy.`+listName+`.block"`)
	}

	return strings.Join(ignore, " and "), strings.Join(off, " and ")
}

func mapValues(m *repository.NameMap[string]) []string {
	values := make([]string, 0, m.Len())
	for _, v := range m.All() {
		values = append(values, v)
	}

	return values
}

func pick(cond bool, a, b string) string {
	if cond {
		return a
	}

	return b
}

// MissingLockedPackageReason ports Problem::getMissingLockedPackageReason.
func MissingLockedPackageReason(pool *Pool, p pkg.PackageInterface) ([2]string, error) {
	packageName := p.Name()
	constraint := semver.NewConstraintOp(semver.OpEQ, p.Version())
	prefix := "- Package " + packageName + " " + p.PrettyVersion() + " (in the lock file) "
	if pool.IsFilterListRemovedPackageVersion(packageName, constraint) {
		filters := pool.FilterListEntryForPackageVersion(packageName, constraint)
		ignorePaths, offPaths := policyPaths(filters)

		return [2]string{prefix, "was not loaded, because it was " + strings.Join(mapValues(filters), ", ") + ". To ignore filters for this package, add the package to the " + ignorePaths + " config. To turn the feature off entirely, you can set " + offPaths + " to false."}, nil
	}

	return [2]string{}, &util.LogicError{Site: phperr.At("Problem.php", 536), Message: "Filter list removed locked package must have version removed from pool."}
}

// packageList is Rule::formatPackagesUnique for packages.
func (ctx *PrettyContext) packageList(packages []pkg.PackageInterface, constraint semver.ConstraintInterface, useRemovedVersionGroup bool) string {
	return PackageList(packages, ctx.IsVerbose, ctx.Pool, constraint, useRemovedVersionGroup)
}

// packageListOfLiterals is Rule::formatPackagesUnique for literals.
func (ctx *PrettyContext) packageListOfLiterals(literals []int32, constraint semver.ConstraintInterface, useRemovedVersionGroup bool) string {
	packages := make([]pkg.PackageInterface, len(literals))
	for i, l := range literals {
		packages[i] = ctx.Pool.LiteralToPackage(l)
	}

	return ctx.packageList(packages, constraint, useRemovedVersionGroup)
}

// PackageList ports Problem::getPackageList: "name[v1, v2], other[v3]";
// pool and constraint may be nil.
func PackageList(packages []pkg.PackageInterface, isVerbose bool, pool *Pool, constraint semver.ConstraintInterface, useRemovedVersionGroup bool) string {
	type prepared struct {
		name     string
		versions *VersionMap
	}
	byName := &repository.NameMap[*prepared]{}
	hasDefaultBranch := map[string]bool{}
	for _, p := range packages {
		entry, ok := byName.Get(p.Name())
		if !ok {
			entry = &prepared{versions: &VersionMap{}}
			byName.Set(p.Name(), entry)
		}
		entry.name = p.PrettyName()
		pretty := p.PrettyVersion()
		if a, ok := p.(pkg.Alias); ok {
			pretty += " (alias of " + a.AliasOf().PrettyVersion() + ")"
		}
		entry.versions.Set(p.Version(), pretty)
		if pool != nil && constraint != nil {
			for version, prettyVersion := range pool.RemovedVersions(p.Name(), constraint).All() {
				entry.versions.Set(version, prettyVersion)
			}
		}
		if pool != nil && useRemovedVersionGroup {
			for version, prettyVersion := range pool.RemovedVersionsByPackage(p).All() {
				entry.versions.Set(version, prettyVersion)
			}
		}
		if p.IsDefaultBranch() {
			hasDefaultBranch[p.Name()] = true
		}
	}

	preparedStrings := make([]string, 0, byName.Len())
	for name, entry := range byName.All() {
		// remove the implicit default branch alias to avoid cruft in the display
		if entry.versions.Has(pkg.DefaultBranchAlias) && hasDefaultBranch[name] {
			entry.versions.Delete(pkg.DefaultBranchAlias)
		}
		versions := sortVersionMap(entry.versions)
		if !isVerbose {
			versions = condenseVersionList(versions, 4, 16)
		}
		preparedStrings = append(preparedStrings, entry.name+"["+joinPretty(versions)+"]")
	}

	return strings.Join(preparedStrings, ", ")
}

func joinPretty(versions []versionEntry) string {
	var b strings.Builder
	for i, v := range versions {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(v.pretty)
	}

	return b.String()
}

// platformPackageVersion ports Problem::getPlatformPackageVersion (whose
// $version argument is always overwritten): null when nothing provides
// the platform package.
func platformPackageVersion(pool *Pool, packageName string) (pkg.NullString, error) {
	available := pool.WhatProvides(packageName, nil)
	if len(available) == 0 {
		return pkg.NullString{}, nil
	}

	var selected pkg.PackageInterface
	for _, p := range available {
		if _, ok := p.Repository().(*repository.PlatformRepository); ok {
			selected = p

			break
		}
	}
	if selected == nil {
		selected = available[0]
	}

	// must be a package providing/replacing and not a real platform package
	if selected.Name() != packageName {
		for _, links := range []pkg.Links{selected.Provides(), selected.Replaces()} {
			for link := range links.Values() {
				if link.Target() == packageName {
					prettyConstraint, err := link.PrettyConstraint()
					if err != nil {
						return pkg.NullString{}, err
					}
					description := link.Description()

					return pkg.Str(prettyConstraint + " " + description[:max(0, len(description)-1)] + "d by " + selected.PrettyString()), nil
				}
			}
		}
	}

	version := selected.PrettyVersion()
	if complete, ok := selected.(pkg.CompletePackageInterface); ok {
		if v, ok := selected.Extra().Get("config.platform"); ok && v == true {
			version += "; " + strings.ReplaceAll(complete.Description().S, "Package ", "")
		}
	}

	return pkg.Str(version), nil
}

// condenseVersionList ports Problem::condenseVersionList.
func condenseVersionList(versions []versionEntry, maxVersions, maxDev int) []versionEntry {
	if len(versions) <= maxVersions {
		return versions
	}

	byMajor := &repository.NameMap[[]versionEntry]{}
	for _, v := range versions {
		key := majorVersionKey(v.version)
		list, _ := byMajor.Get(key)
		byMajor.Set(key, append(list, v))
	}

	var filtered []versionEntry
	for majorVersion, versionsForMajor := range byMajor.All() {
		limit := maxVersions
		if majorVersion == "dev" {
			limit = maxDev
		}
		if len(versionsForMajor) > limit {
			// output only 1st and last versions
			filtered = append(filtered, versionsForMajor[0], versionEntry{pretty: "..."}, versionsForMajor[len(versionsForMajor)-1])
		} else {
			filtered = append(filtered, versionsForMajor...)
		}
	}

	return filtered
}

// majorVersionKey is condenseVersionList's grouping key: "dev" for
// branches, else Preg::replace('{^(\d+)\..*}', '$1', $version).
func majorVersionKey(version string) string {
	if len(version) >= 4 && strings.EqualFold(version[:4], "dev-") {
		return "dev"
	}
	i := 0
	for i < len(version) && version[i] >= '0' && version[i] <= '9' {
		i++
	}
	if i > 0 && i < len(version) && version[i] == '.' && !strings.Contains(version[i:], "\n") {
		return version[:i]
	}

	return version
}

// hasMultipleNames ports Problem::hasMultipleNames.
func hasMultipleNames(packages []pkg.PackageInterface) bool {
	name := ""
	for i, p := range packages {
		if i == 0 || name == p.Name() {
			name = p.Name()
		} else {
			return true
		}
	}

	return false
}

// computeCheckForLowerPrioRepo ports Problem::computeCheckForLowerPrioRepo.
func computeCheckForLowerPrioRepo(pool *Pool, isVerbose bool, packageName string, higherRepoPackages, allReposPackages []pkg.PackageInterface, reason string, constraint semver.ConstraintInterface) [2]string {
	var nextRepoPackages []pkg.PackageInterface
	var nextRepo pkg.Repository
	for _, p := range allReposPackages {
		if nextRepo == nil || nextRepo == p.Repository() {
			nextRepoPackages = append(nextRepoPackages, p)
			nextRepo = p.Repository()
		} else {
			break
		}
	}

	if len(higherRepoPackages) > 0 {
		if topPackage, ok := higherRepoPackages[0].(pkg.RootPackageInterface); ok {
			return [2]string{
				"- Root composer.json requires " + packageName + constraintToText(constraint) + ", it is ",
				"satisfiable by " + PackageList(nextRepoPackages, isVerbose, pool, constraint, false) + " from " + nextRepo.RepoName() + " but " + topPackage.PrettyName() + " " + topPackage.PrettyVersion() + " is the root package and cannot be modified. See https://getcomposer.org/dep-on-root for details and assistance.",
			}
		}
	}

	if _, ok := nextRepo.(*repository.LockArrayRepository); ok {
		singular := len(higherRepoPackages) == 1

		suggestion := "Make sure you either fix the " + reason + " or avoid updating this package to keep the one present in the lock file (" + PackageList(nextRepoPackages, isVerbose, pool, constraint, false) + ")."
		// symlinked path repos cannot be locked so do not suggest keeping it locked
		if first := nextRepoPackages[0]; first.DistType().Valid && first.DistType().S == "path" {
			if symlink, ok := first.TransportOptions().Get("symlink"); !ok || symlink != false {
				suggestion = "Make sure you fix the " + reason + " as packages installed from symlinked path repos are updated even in partial updates and the one from the lock file can thus not be used."
			}
		}

		return [2]string{
			"- Root composer.json requires " + packageName + constraintToText(constraint) + ", ",
			"found " + PackageList(higherRepoPackages, isVerbose, pool, constraint, false) + " but " + pick(singular, "it does", "these do") + " not match your " + reason + " and " + pick(singular, "is", "are") + " therefore not installable. " + suggestion,
		}
	}

	return [2]string{
		"- Root composer.json requires " + packageName + constraintToText(constraint) + ", it is ",
		"satisfiable by " + PackageList(nextRepoPackages, isVerbose, pool, constraint, false) + " from " + nextRepo.RepoName() + " but " + PackageList(higherRepoPackages, isVerbose, pool, constraint, false) + " from " + higherRepoPackages[0].Repository().RepoName() + " has higher repository priority. The packages from the higher priority repository do not match your " + reason + " and are therefore not installable. That repository is canonical so the lower priority repo's packages are not installable. See https://getcomposer.org/repoprio for details and assistance.",
	}
}

// constraintToText ports Problem::constraintToText.
func constraintToText(constraint semver.ConstraintInterface) string {
	if constraint == nil {
		return ""
	}
	c, ok := constraint.(*semver.Constraint)
	if !ok || c.Op() != semver.OpEQ || strings.HasPrefix(c.Version(), "dev-") {
		return " " + constraint.PrettyString()
	}

	pretty := c.PrettyString()
	// The pretty string of a parsed == constraint is a short version, far
	// below what could exhaust the backtrack limit: Preg cannot throw.
	if exact, _ := exactVersionRegex.IsMatch(pretty); !exact {
		return " " + pretty + " (exact version match)"
	}

	versions := []string{pretty}
	for i := 3 - strings.Count(pretty, "."); i > 0; i-- {
		versions = append(versions, versions[len(versions)-1]+".0")
	}
	if len(versions) > 1 {
		return " " + pretty + " (exact version match: " + strings.Join(versions[:len(versions)-1], ", ") + " or " + versions[len(versions)-1] + ")"
	}

	return " " + pretty + " (exact version match: " + versions[0] + ")"
}

// providersList ports Problem::getProvidersList; null when nothing
// provides the package.
func providersList(repositorySet *repository.RepositorySet, packageName string, maxProviders int) (pkg.NullString, error) {
	providers, err := repositorySet.Providers(packageName)
	if err != nil || len(providers) == 0 {
		return pkg.NullString{}, err
	}

	shown := providers
	if len(providers) > maxProviders+1 {
		shown = providers[:maxProviders]
	}
	var b strings.Builder
	for _, p := range shown {
		description := ""
		if p.Description.Valid && p.Description.S != "" {
			description = " " + php.SubstrLen(p.Description.S, 0, 100)
		}
		b.WriteString("      - " + p.Name + description + "\n")
	}
	if len(providers) > maxProviders+1 {
		b.WriteString("      ... and " + strconv.Itoa(len(providers)-maxProviders) + " more.\n")
	}

	return pkg.Str(b.String()), nil
}

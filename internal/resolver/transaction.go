// Ports src/Composer/DependencyResolver/Transaction.php,
// LockTransaction.php and LocalRepoTransaction.php.

package resolver

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
)

// Transaction ports Composer\DependencyResolver\Transaction: the
// operations turning the present packages into the result packages.
type Transaction struct {
	operations           []operation.Operation
	presentPackages      []pkg.PackageInterface
	resultPackageMap     []pkg.PackageInterface
	resultPackagesByName map[string][]pkg.PackageInterface
	// resultPackages are the result packages as given, which
	// ResultPackagesByName reads $resultPackagesByName's order from.
	resultPackages []pkg.PackageInterface
}

// KeyedPackage is an entry of a PHP array of packages: its key and the
// package.
type KeyedPackage struct {
	Key     int
	Package pkg.PackageInterface
}

// NamedPackages is an entry of $resultPackagesByName.
type NamedPackages struct {
	Name     string
	Packages []KeyedPackage
}

// ResultPackagesByName returns the protected $resultPackagesByName as
// setResultPackageMaps() builds it: the names in the order it meets them,
// each with its packages ordered by uasort(), which keeps their keys.
func (t *Transaction) ResultPackagesByName() []NamedPackages {
	var out []NamedPackages
	index := map[string]int{}
	for _, p := range t.resultPackages {
		for _, name := range p.Names(true) {
			i, ok := index[name]
			if !ok {
				i = len(out)
				index[name] = i
				out = append(out, NamedPackages{Name: name})
			}
			out[i].Packages = append(out[i].Packages, KeyedPackage{Key: len(out[i].Packages), Package: p})
		}
	}
	for _, e := range out {
		php.SortSlice(e.Packages, func(a, b KeyedPackage) int { return packageSort(a.Package, b.Package) })
	}

	return out
}

// CalculateOperations is the protected calculateOperations(): the
// operations, computed again (new operation objects).
func (t *Transaction) CalculateOperations() []operation.Operation { return t.calculateOperations() }

// NewTransaction is new Transaction($presentPackages, $resultPackages).
func NewTransaction(presentPackages, resultPackages []pkg.PackageInterface) *Transaction {
	t := &Transaction{}
	t.init(presentPackages, resultPackages)

	return t
}

func (t *Transaction) init(presentPackages, resultPackages []pkg.PackageInterface) {
	t.presentPackages = presentPackages
	t.setResultPackageMaps(resultPackages)
	t.operations = t.calculateOperations()
}

// Operations ports getOperations.
func (t *Transaction) Operations() []operation.Operation { return t.operations }

// PresentPackages returns the private $presentPackages (plugins read it
// through Closure::bind).
func (t *Transaction) PresentPackages() []pkg.PackageInterface { return t.presentPackages }

// ResultPackageMap returns the private $resultPackageMap: the result
// packages, sorted.
func (t *Transaction) ResultPackageMap() []pkg.PackageInterface { return t.resultPackageMap }

// packageSort is setResultPackageMaps' $packageSort.
func packageSort(a, b pkg.PackageInterface) int {
	// sort alias packages by the same name behind their non alias version
	if a.Name() == b.Name() {
		_, aAlias := a.(pkg.Alias)
		_, bAlias := b.(pkg.Alias)
		if aAlias != bAlias {
			if aAlias {
				return -1
			}

			return 1
		}

		// if names are the same, compare version, e.g. to sort aliases reliably, actual order does not matter
		return strings.Compare(b.Version(), a.Version())
	}

	return strings.Compare(b.Name(), a.Name())
}

func (t *Transaction) setResultPackageMaps(resultPackages []pkg.PackageInterface) {
	t.resultPackages = resultPackages
	var set pkgSet
	t.resultPackagesByName = map[string][]pkg.PackageInterface{}
	for _, p := range resultPackages {
		set.add(p)
		for _, name := range p.Names(true) {
			t.resultPackagesByName[name] = append(t.resultPackagesByName[name], p)
		}
	}
	t.resultPackageMap = set.appendTo(nil)

	php.SortSlice(t.resultPackageMap, packageSort)
	for _, packages := range t.resultPackagesByName {
		php.SortSlice(packages, packageSort)
	}
}

func (t *Transaction) calculateOperations() []operation.Operation {
	var operations []operation.Operation

	presentPackageMap := map[string]pkg.PackageInterface{}
	removeMap := &repository.NameMap[pkg.PackageInterface]{}
	presentAliasMap := map[string]bool{}
	removeAliasMap := &repository.NameMap[pkg.Alias]{}
	for _, p := range t.presentPackages {
		if alias, ok := p.(pkg.Alias); ok {
			key := p.Name() + "::" + p.Version()
			presentAliasMap[key] = true
			removeAliasMap.Set(key, alias)
		} else {
			presentPackageMap[p.Name()] = p
			removeMap.Set(p.Name(), p)
		}
	}

	stack := t.rootPackages()

	visited := map[pkg.PackageInterface]bool{}
	processed := map[pkg.PackageInterface]bool{}

	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if processed[p] {
			continue
		}

		if !visited[p] {
			visited[p] = true

			stack = append(stack, p)
			if alias, ok := p.(pkg.Alias); ok {
				stack = append(stack, alias.AliasOf())
			} else {
				for link := range p.Requires().Values() {
					stack = append(stack, t.providersInResult(link)...)
				}
			}

			continue
		}

		processed[p] = true

		if alias, ok := p.(pkg.Alias); ok {
			aliasKey := p.Name() + "::" + p.Version()
			if presentAliasMap[aliasKey] {
				removeAliasMap.Delete(aliasKey)
			} else {
				operations = append(operations, operation.NewMarkAliasInstalledOperation(alias))
			}

			continue
		}

		if source, ok := presentPackageMap[p.Name()]; ok {
			// do we need to update?
			// TODO different for lock?
			if needsUpdate(source, p) {
				operations = append(operations, operation.NewUpdateOperation(source, p))
			}
			removeMap.Delete(p.Name())
		} else {
			operations = append(operations, operation.NewInstallOperation(p))
			removeMap.Delete(p.Name())
		}
	}

	var uninstalls []operation.Operation
	for _, p := range removeMap.All() {
		uninstalls = append(uninstalls, operation.NewUninstallOperation(p))
	}
	slices.Reverse(uninstalls)
	operations = append(uninstalls, operations...)

	for _, p := range removeAliasMap.All() {
		operations = append(operations, operation.NewMarkAliasUninstalledOperation(p))
	}

	operations = movePluginsToFront(operations)
	// TODO fix this:
	// we have to do this again here even though the above stack code did it because moving plugins moves them before uninstalls
	operations = moveUninstallsToFront(operations)

	return operations
}

// needsUpdate is calculateOperations' "do we need to update?" test.
func needsUpdate(present, p pkg.PackageInterface) bool {
	if p.Version() != present.Version() ||
		!nullStringEqual(p.DistReference(), present.DistReference()) ||
		!nullStringEqual(p.SourceReference(), present.SourceReference()) {
		return true
	}

	complete, ok := p.(pkg.CompletePackageInterface)
	if !ok {
		return false
	}
	presentComplete, ok := present.(pkg.CompletePackageInterface)
	if !ok {
		return false
	}

	return complete.IsAbandoned() != presentComplete.IsAbandoned() ||
		!nullStringEqual(complete.ReplacementPackage(), presentComplete.ReplacementPackage())
}

func nullStringEqual(a, b pkg.NullString) bool {
	return a.Valid == b.Valid && (!a.Valid || a.S == b.S)
}

// rootPackages ports getRootPackages: the result packages no other result
// package requires, in result order.
func (t *Transaction) rootPackages() []pkg.PackageInterface {
	notRoot := map[pkg.PackageInterface]bool{}

	for _, p := range t.resultPackageMap {
		if notRoot[p] {
			continue
		}

		for link := range p.Requires().Values() {
			for _, require := range t.providersInResult(link) {
				if require != p {
					notRoot[require] = true
				}
			}
		}
	}

	roots := make([]pkg.PackageInterface, 0, len(t.resultPackageMap)-len(notRoot))
	for _, p := range t.resultPackageMap {
		if !notRoot[p] {
			roots = append(roots, p)
		}
	}

	return roots
}

// providersInResult ports getProvidersInResult.
func (t *Transaction) providersInResult(link *pkg.Link) []pkg.PackageInterface {
	return t.resultPackagesByName[link.Target()]
}

// operationPackage returns the package an install or update installs.
func operationPackage(op operation.Operation) (pkg.PackageInterface, bool) {
	switch op := op.(type) {
	case *operation.InstallOperation:
		return op.Package(), true
	case *operation.UpdateOperation:
		return op.TargetPackage(), true
	}

	return nil, false
}

// nonPlatformRequires returns the names of p's requirements that are not
// platform packages (array_keys of the requires, filtered).
func nonPlatformRequires(p pkg.PackageInterface) []string {
	links := p.Requires()
	var requires []string
	for i := range links.Len() {
		if req := links.Key(i); !repository.IsPlatformPackage(req) {
			requires = append(requires, req)
		}
	}

	return requires
}

func intersects(names, list []string) bool {
	for _, name := range names {
		if slices.Contains(list, name) {
			return true
		}
	}

	return false
}

// movePluginsToFront ports movePluginsToFront: plugins that modify
// downloads, then the other plugins, each with their dependencies, run
// first.
func movePluginsToFront(operations []operation.Operation) []operation.Operation {
	var dlModifyingPluginsNoDeps, dlModifyingPluginsWithDeps, pluginsNoDeps, pluginsWithDeps []operation.Operation
	var dlModifyingPluginRequires, pluginRequires []string
	removed := make([]bool, len(operations))

	for idx := len(operations) - 1; idx >= 0; idx-- {
		op := operations[idx]
		p, ok := operationPackage(op)
		if !ok {
			continue
		}

		modifiesDownloads, _ := p.Extra().Get("plugin-modifies-downloads")
		isDownloadsModifyingPlugin := p.Type() == pkg.PluginType && modifiesDownloads == true

		// is this a downloads modifying plugin or a dependency of one?
		if isDownloadsModifyingPlugin || intersects(p.Names(true), dlModifyingPluginRequires) {
			// get the package's requires, but filter out any platform requirements
			requires := nonPlatformRequires(p)

			// is this a plugin with no meaningful dependencies?
			if isDownloadsModifyingPlugin && len(requires) == 0 {
				// plugins with no dependencies go to the very front
				dlModifyingPluginsNoDeps = slices.Insert(dlModifyingPluginsNoDeps, 0, op)
			} else {
				// capture the requirements for this package so those packages will be moved up as well
				dlModifyingPluginRequires = append(dlModifyingPluginRequires, requires...)
				// move the operation to the front
				dlModifyingPluginsWithDeps = slices.Insert(dlModifyingPluginsWithDeps, 0, op)
			}

			removed[idx] = true

			continue
		}

		// is this package a plugin?
		isPlugin := pkg.IsPluginType(p.Type())

		// is this a plugin or a dependency of a plugin?
		if isPlugin || intersects(p.Names(true), pluginRequires) {
			// get the package's requires, but filter out any platform requirements
			requires := nonPlatformRequires(p)

			// is this a plugin with no meaningful dependencies?
			if isPlugin && len(requires) == 0 {
				// plugins with no dependencies go to the very front
				pluginsNoDeps = slices.Insert(pluginsNoDeps, 0, op)
			} else {
				// capture the requirements for this package so those packages will be moved up as well
				pluginRequires = append(pluginRequires, requires...)
				// move the operation to the front
				pluginsWithDeps = slices.Insert(pluginsWithDeps, 0, op)
			}

			removed[idx] = true
		}
	}

	result := make([]operation.Operation, 0, len(operations))
	result = append(result, dlModifyingPluginsNoDeps...)
	result = append(result, dlModifyingPluginsWithDeps...)
	result = append(result, pluginsNoDeps...)
	result = append(result, pluginsWithDeps...)
	for idx, op := range operations {
		if !removed[idx] {
			result = append(result, op)
		}
	}

	return result
}

// moveUninstallsToFront ports moveUninstallsToFront.
func moveUninstallsToFront(operations []operation.Operation) []operation.Operation {
	var uninstOps, others []operation.Operation
	for _, op := range operations {
		switch op.(type) {
		case *operation.UninstallOperation, *operation.MarkAliasUninstalledOperation:
			uninstOps = append(uninstOps, op)
		default:
			others = append(others, op)
		}
	}

	return append(uninstOps, others...)
}

// LockTransaction ports Composer\DependencyResolver\LockTransaction: the
// solver's result, from which the lock file is written.
type LockTransaction struct {
	Transaction
	presentMap    pkgSet
	unlockableMap map[int]pkg.PackageInterface
	// unlockable are the packages of $unlockableMap in its order.
	unlockable []pkg.PackageInterface
	all        []pkg.PackageInterface
	nonDev     []pkg.PackageInterface
	dev        []pkg.PackageInterface
	// revision counts the changes of $resultPackages.
	revision int
}

// NewLockTransaction is new LockTransaction($pool, $presentMap,
// $unlockableMap, $decisions): presentMap are the present packages,
// unlockable the fixed packages ($unlockableMap, keyed by their ids), in
// order.
func NewLockTransaction(pool *Pool, presentMap, unlockable []pkg.PackageInterface, decisions *Decisions) (*LockTransaction, error) {
	t := &LockTransaction{unlockableMap: make(map[int]pkg.PackageInterface, len(unlockable))}
	position := map[int]int{}
	for _, p := range unlockable {
		// $map[$id] = $package: a later one replaces the value in place.
		if i, ok := position[p.ID()]; ok {
			t.unlockable[i] = p
		} else {
			position[p.ID()] = len(t.unlockable)
			t.unlockable = append(t.unlockable, p)
		}
		t.unlockableMap[p.ID()] = p
	}
	for _, p := range presentMap {
		t.presentMap.add(p)
	}
	if err := t.SetResultPackages(pool, decisions); err != nil {
		return nil, err
	}
	t.init(t.presentMap.appendTo(nil), t.all)

	return t, nil
}

// PresentMap returns the protected $presentMap's packages, in order.
func (t *LockTransaction) PresentMap() []pkg.PackageInterface { return t.presentMap.appendTo(nil) }

// UnlockableMap returns the protected $unlockableMap's packages (keyed
// by their ids), in order.
func (t *LockTransaction) UnlockableMap() []pkg.PackageInterface { return t.unlockable }

// ResultPackages returns the protected $resultPackages: its 'all',
// 'non-dev' and 'dev' lists; dev has nil where setNonDevPackages() unset
// an entry (PHP's array keeps the other keys).
func (t *LockTransaction) ResultPackages() (all, nonDev, dev []pkg.PackageInterface) {
	return t.all, t.nonDev, t.dev
}

// Revision counts the changes of the result packages (for mirrors).
func (t *LockTransaction) Revision() int { return t.revision }

// SetResultPackages ports setResultPackages: the installed packages of
// the decisions, last decision first.
func (t *LockTransaction) SetResultPackages(pool *Pool, decisions *Decisions) error {
	t.revision++
	t.all, t.nonDev, t.dev = nil, nil, nil
	for _, decision := range decisions.Reversed {
		if decision.Literal <= 0 {
			continue
		}
		p := pool.LiteralToPackage(decision.Literal)
		if err := loader.ValidatePackage(p); err != nil {
			return err
		}
		t.all = append(t.all, p)
		if _, ok := t.unlockableMap[p.ID()]; !ok {
			t.nonDev = append(t.nonDev, p)
		}
	}

	return nil
}

// SetNonDevPackages ports setNonDevPackages: the packages of extraction
// (solved without dev requirements) are the non-dev ones, the rest dev.
func (t *LockTransaction) SetNonDevPackages(extractionResult *LockTransaction) error {
	packages, err := extractionResult.NewLockPackages(false, false)
	if err != nil {
		return err
	}

	t.revision++
	t.dev = t.nonDev
	t.nonDev = nil

	for _, p := range packages {
		for i, resultPackage := range t.dev {
			// TODO this comparison is probably insufficient, aliases, what about modified versions? I guess they aren't possible?
			if resultPackage != nil && p.Name() == resultPackage.Name() {
				t.nonDev = append(t.nonDev, resultPackage)
				t.dev[i] = nil
			}
		}
	}

	return nil
}

// NewLockPackages ports getNewLockPackages: the packages (no aliases) to
// write to the lock file. With updateMirrors, packages that are not
// present take the references of the present package of the same name
// and version.
func (t *LockTransaction) NewLockPackages(devMode, updateMirrors bool) ([]pkg.PackageInterface, error) {
	source := t.nonDev
	if devMode {
		source = t.dev
	}

	packages := []pkg.PackageInterface{}
	for _, p := range source {
		if p == nil {
			continue
		}
		if _, ok := p.(pkg.Alias); ok {
			continue
		}

		// if we're just updating mirrors we need to reset everything to the same as currently "present" packages' references to keep the lock file as-is
		if updateMirrors && !t.presentMap.has(p) {
			var err error
			if p, err = t.updateMirrorAndUrls(p); err != nil {
				return nil, err
			}
		}

		packages = append(packages, p)
	}

	return packages, nil
}

var (
	knownDistURLRegex = php.MustCompile(`{^https?://(?:(?:www\.)?bitbucket\.org|(api\.)?github\.com|(?:www\.)?gitlab\.com)/}i`)
	distRefRegex      = php.MustCompile(`{(?<=/|sha=)[a-f0-9]{40}(?=/|$)}i`)
)

// updateMirrorAndUrls ports updateMirrorAndUrls.
func (t *LockTransaction) updateMirrorAndUrls(p pkg.PackageInterface) (pkg.PackageInterface, error) {
	for _, presentPackage := range t.presentMap.list {
		if presentPackage == nil {
			continue
		}
		if p.Name() != presentPackage.Name() {
			continue
		}

		if p.Version() != presentPackage.Version() {
			continue
		}

		if !presentPackage.SourceReference().Valid {
			continue
		}

		if !nullStringEqual(presentPackage.SourceType(), p.SourceType()) {
			continue
		}

		if present, ok := pkg.AsPackage(presentPackage); ok {
			present.SetSourceURL(p.SourceURL())
			present.SetSourceMirrors(p.SourceMirrors())
		}

		// if the dist type changed, we only update the source url/mirrors
		if !nullStringEqual(presentPackage.DistType(), p.DistType()) {
			return presentPackage, nil
		}

		// update dist url if it is in a known format
		if p.DistURL().Valid && presentPackage.DistReference().Valid {
			known, err := knownDistURLRegex.IsMatch(p.DistURL().S)
			if err != nil {
				return nil, err
			}
			if known {
				url, _, err := distRefRegex.Replace(p.DistURL().S, presentPackage.DistReference().S, -1)
				if err != nil {
					return nil, err
				}
				presentPackage.SetDistURL(pkg.Str(url))
			}
		}
		presentPackage.SetDistMirrors(p.DistMirrors())

		return presentPackage, nil
	}

	return p, nil
}

// Aliases ports getAliases: the entries of aliases (root alias arrays
// with a 'package' key) used by alias packages of the result, sorted by
// package name.
func (t *LockTransaction) Aliases(aliases *php.Array) *php.Array {
	remaining := aliases.Clone()
	var usedAliases []*php.Array
	for _, p := range t.all {
		if _, ok := p.(pkg.Alias); !ok {
			continue
		}
		var used []php.Key
		for index, alias := range remaining.All() {
			a, ok := alias.(*php.Array)
			if !ok {
				continue
			}
			if name, _ := a.Get("package"); name == p.Name() {
				usedAliases = append(usedAliases, a)
				used = append(used, index)
			}
		}
		for _, index := range used {
			remaining.DeleteKey(index)
		}
	}

	php.SortSlice(usedAliases, func(a, b *php.Array) int {
		nameA, _ := a.Get("package")
		nameB, _ := b.Get("package")

		return strings.Compare(php.ToString(nameA), php.ToString(nameB))
	})

	result := php.NewArray()
	for _, a := range usedAliases {
		result.Append(a)
	}

	return result
}

// NewLocalRepoTransaction ports new LocalRepoTransaction($lockedRepository,
// $localRepository): from the installed packages to the locked ones.
func NewLocalRepoTransaction(lockedRepository repository.RepositoryInterface, localRepository repository.InstalledRepositoryInterface) (*Transaction, error) {
	present, err := localRepository.Packages()
	if err != nil {
		return nil, err
	}
	result, err := lockedRepository.Packages()
	if err != nil {
		return nil, err
	}

	return NewTransaction(present, result), nil
}

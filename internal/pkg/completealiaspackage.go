// Ports src/Composer/Package/CompleteAliasPackage.php.

package pkg

import "github.com/stubbedev/maestro/internal/php"

// CompleteAliasPackage ports Composer\Package\CompleteAliasPackage: an
// alias of a complete package.
type CompleteAliasPackage struct {
	AliasPackage

	complete CompletePackageInterface
}

// NewCompleteAliasPackage ports CompleteAliasPackage::__construct.
func NewCompleteAliasPackage(aliasOf CompletePackageInterface, version, prettyVersion string) *CompleteAliasPackage {
	a := &CompleteAliasPackage{complete: aliasOf}
	a.init(aliasOf, version, prettyVersion)

	return a
}

// Class returns ClassCompleteAliasPackage.
func (a *CompleteAliasPackage) Class() string { return ClassCompleteAliasPackage }

// Clone ports `clone $alias` (the aliased package stays shared).
func (a *CompleteAliasPackage) Clone() *CompleteAliasPackage {
	c := *a
	c.clearForClone()

	return &c
}

// Scripts ports CompleteAliasPackage::getScripts.
func (a *CompleteAliasPackage) Scripts() *php.Array { return a.complete.Scripts() }

// SetScripts ports CompleteAliasPackage::setScripts.
func (a *CompleteAliasPackage) SetScripts(scripts *php.Array) { a.complete.SetScripts(scripts) }

// Repositories ports CompleteAliasPackage::getRepositories.
func (a *CompleteAliasPackage) Repositories() *php.Array { return a.complete.Repositories() }

// SetRepositories ports CompleteAliasPackage::setRepositories.
func (a *CompleteAliasPackage) SetRepositories(repositories *php.Array) {
	a.complete.SetRepositories(repositories)
}

// License ports CompleteAliasPackage::getLicense.
func (a *CompleteAliasPackage) License() *php.Array { return a.complete.License() }

// SetLicense ports CompleteAliasPackage::setLicense.
func (a *CompleteAliasPackage) SetLicense(license *php.Array) { a.complete.SetLicense(license) }

// Keywords ports CompleteAliasPackage::getKeywords.
func (a *CompleteAliasPackage) Keywords() *php.Array { return a.complete.Keywords() }

// SetKeywords ports CompleteAliasPackage::setKeywords.
func (a *CompleteAliasPackage) SetKeywords(keywords *php.Array) { a.complete.SetKeywords(keywords) }

// Description ports CompleteAliasPackage::getDescription.
func (a *CompleteAliasPackage) Description() NullString { return a.complete.Description() }

// SetDescription ports CompleteAliasPackage::setDescription.
func (a *CompleteAliasPackage) SetDescription(description NullString) {
	a.complete.SetDescription(description)
}

// Homepage ports CompleteAliasPackage::getHomepage.
func (a *CompleteAliasPackage) Homepage() NullString { return a.complete.Homepage() }

// SetHomepage ports CompleteAliasPackage::setHomepage.
func (a *CompleteAliasPackage) SetHomepage(homepage NullString) { a.complete.SetHomepage(homepage) }

// Authors ports CompleteAliasPackage::getAuthors.
func (a *CompleteAliasPackage) Authors() *php.Array { return a.complete.Authors() }

// SetAuthors ports CompleteAliasPackage::setAuthors.
func (a *CompleteAliasPackage) SetAuthors(authors *php.Array) { a.complete.SetAuthors(authors) }

// Support ports CompleteAliasPackage::getSupport.
func (a *CompleteAliasPackage) Support() *php.Array { return a.complete.Support() }

// SetSupport ports CompleteAliasPackage::setSupport.
func (a *CompleteAliasPackage) SetSupport(support *php.Array) { a.complete.SetSupport(support) }

// Funding ports CompleteAliasPackage::getFunding.
func (a *CompleteAliasPackage) Funding() *php.Array { return a.complete.Funding() }

// SetFunding ports CompleteAliasPackage::setFunding.
func (a *CompleteAliasPackage) SetFunding(funding *php.Array) { a.complete.SetFunding(funding) }

// IsAbandoned ports CompleteAliasPackage::isAbandoned.
func (a *CompleteAliasPackage) IsAbandoned() bool { return a.complete.IsAbandoned() }

// ReplacementPackage ports CompleteAliasPackage::getReplacementPackage.
func (a *CompleteAliasPackage) ReplacementPackage() NullString {
	return a.complete.ReplacementPackage()
}

// Abandoned returns the aliased package's abandoned value.
func (a *CompleteAliasPackage) Abandoned() any { return a.complete.Abandoned() }

// SetAbandoned ports CompleteAliasPackage::setAbandoned.
func (a *CompleteAliasPackage) SetAbandoned(abandoned any) { a.complete.SetAbandoned(abandoned) }

// ArchiveName ports CompleteAliasPackage::getArchiveName.
func (a *CompleteAliasPackage) ArchiveName() NullString { return a.complete.ArchiveName() }

// SetArchiveName ports CompleteAliasPackage::setArchiveName.
func (a *CompleteAliasPackage) SetArchiveName(name NullString) { a.complete.SetArchiveName(name) }

// ArchiveExcludes ports CompleteAliasPackage::getArchiveExcludes.
func (a *CompleteAliasPackage) ArchiveExcludes() *php.Array { return a.complete.ArchiveExcludes() }

// SetArchiveExcludes ports CompleteAliasPackage::setArchiveExcludes.
func (a *CompleteAliasPackage) SetArchiveExcludes(excludes *php.Array) {
	a.complete.SetArchiveExcludes(excludes)
}

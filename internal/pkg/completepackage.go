// Ports src/Composer/Package/CompletePackage.php.

package pkg

import "github.com/stubbedev/maestro/internal/php"

// CompletePackage ports Composer\Package\CompletePackage: a package with
// all the information the composer.json schema allows.
type CompletePackage struct {
	Package

	description string
	homepage    string
	archiveName string
	abandoned   any

	repositories    *php.Array
	license         *php.Array
	keywords        *php.Array
	authors         *php.Array
	scripts         *php.Array
	support         *php.Array
	funding         *php.Array
	archiveExcludes *php.Array
}

// NewCompletePackage ports CompletePackage::__construct (Package's).
func NewCompletePackage(name, version, prettyVersion string) *CompletePackage {
	p := &CompletePackage{}
	p.init(name, version, prettyVersion)
	p.abandoned = false

	return p
}

// Class returns ClassCompletePackage.
func (p *CompletePackage) Class() string { return ClassCompletePackage }

// Clone ports `clone $package`.
func (p *CompletePackage) Clone() *CompletePackage {
	c := *p
	c.clearForClone()

	return &c
}

// SetScripts ports CompletePackage::setScripts.
func (p *CompletePackage) SetScripts(scripts *php.Array) {
	p.scripts = scripts
	p.rev++
}

// Scripts ports CompletePackage::getScripts.
func (p *CompletePackage) Scripts() *php.Array { return orEmpty(p.scripts) }

// SetRepositories ports CompletePackage::setRepositories.
func (p *CompletePackage) SetRepositories(repositories *php.Array) {
	p.repositories = repositories
	p.rev++
}

// Repositories ports CompletePackage::getRepositories.
func (p *CompletePackage) Repositories() *php.Array { return orEmpty(p.repositories) }

// SetLicense ports CompletePackage::setLicense.
func (p *CompletePackage) SetLicense(license *php.Array) {
	p.license = license
	p.rev++
}

// License ports CompletePackage::getLicense.
func (p *CompletePackage) License() *php.Array { return orEmpty(p.license) }

// SetKeywords ports CompletePackage::setKeywords.
func (p *CompletePackage) SetKeywords(keywords *php.Array) {
	p.keywords = keywords
	p.rev++
}

// Keywords ports CompletePackage::getKeywords.
func (p *CompletePackage) Keywords() *php.Array { return orEmpty(p.keywords) }

// SetAuthors ports CompletePackage::setAuthors.
func (p *CompletePackage) SetAuthors(authors *php.Array) {
	p.authors = authors
	p.rev++
}

// Authors ports CompletePackage::getAuthors.
func (p *CompletePackage) Authors() *php.Array { return orEmpty(p.authors) }

// SetDescription ports CompletePackage::setDescription.
func (p *CompletePackage) SetDescription(description NullString) {
	p.setNullable(setDescription, &p.description, description)
}

// Description ports CompletePackage::getDescription.
func (p *CompletePackage) Description() NullString { return p.nullable(setDescription, p.description) }

// SetHomepage ports CompletePackage::setHomepage.
func (p *CompletePackage) SetHomepage(homepage NullString) {
	p.setNullable(setHomepage, &p.homepage, homepage)
}

// Homepage ports CompletePackage::getHomepage.
func (p *CompletePackage) Homepage() NullString { return p.nullable(setHomepage, p.homepage) }

// SetSupport ports CompletePackage::setSupport.
func (p *CompletePackage) SetSupport(support *php.Array) {
	p.support = support
	p.rev++
}

// Support ports CompletePackage::getSupport.
func (p *CompletePackage) Support() *php.Array { return orEmpty(p.support) }

// SetFunding ports CompletePackage::setFunding.
func (p *CompletePackage) SetFunding(funding *php.Array) {
	p.funding = funding
	p.rev++
}

// Funding ports CompletePackage::getFunding.
func (p *CompletePackage) Funding() *php.Array { return orEmpty(p.funding) }

// IsAbandoned ports CompletePackage::isAbandoned.
func (p *CompletePackage) IsAbandoned() bool { return php.ToBool(p.abandoned) }

// SetAbandoned ports CompletePackage::setAbandoned: true, false or the
// name of the replacement package (any PHP value is kept as given).
func (p *CompletePackage) SetAbandoned(abandoned any) {
	p.abandoned = abandoned
	p.rev++
}

// Abandoned returns the abandoned value as set.
func (p *CompletePackage) Abandoned() any { return p.abandoned }

// ReplacementPackage ports CompletePackage::getReplacementPackage.
func (p *CompletePackage) ReplacementPackage() NullString {
	if s, ok := p.abandoned.(string); ok {
		return Str(s)
	}

	return NullString{}
}

// SetArchiveName ports CompletePackage::setArchiveName.
func (p *CompletePackage) SetArchiveName(name NullString) {
	p.setNullable(setArchiveName, &p.archiveName, name)
}

// ArchiveName ports CompletePackage::getArchiveName.
func (p *CompletePackage) ArchiveName() NullString { return p.nullable(setArchiveName, p.archiveName) }

// SetArchiveExcludes ports CompletePackage::setArchiveExcludes.
func (p *CompletePackage) SetArchiveExcludes(excludes *php.Array) {
	p.archiveExcludes = excludes
	p.rev++
}

// ArchiveExcludes ports CompletePackage::getArchiveExcludes.
func (p *CompletePackage) ArchiveExcludes() *php.Array { return orEmpty(p.archiveExcludes) }

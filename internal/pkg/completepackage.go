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

// PHPClass returns ClassCompletePackage.
func (p *CompletePackage) PHPClass() string { return ClassCompletePackage }

// Clone ports `clone $package`.
func (p *CompletePackage) Clone() *CompletePackage {
	p.need()
	c := *p
	c.clearForClone()

	return &c
}

// SetScripts ports CompletePackage::setScripts.
func (p *CompletePackage) SetScripts(scripts *php.Array) {
	p.need()
	p.scripts = scripts
	p.changed()
}

// Scripts ports CompletePackage::getScripts.
func (p *CompletePackage) Scripts() *php.Array {
	p.need()

	return orEmpty(p.scripts)
}

// SetRepositories ports CompletePackage::setRepositories.
func (p *CompletePackage) SetRepositories(repositories *php.Array) {
	p.need()
	p.repositories = repositories
	p.changed()
}

// Repositories ports CompletePackage::getRepositories.
func (p *CompletePackage) Repositories() *php.Array {
	p.need()

	return orEmpty(p.repositories)
}

// SetLicense ports CompletePackage::setLicense.
func (p *CompletePackage) SetLicense(license *php.Array) {
	p.need()
	p.license = license
	p.changed()
}

// License ports CompletePackage::getLicense.
func (p *CompletePackage) License() *php.Array {
	p.need()

	return orEmpty(p.license)
}

// SetKeywords ports CompletePackage::setKeywords.
func (p *CompletePackage) SetKeywords(keywords *php.Array) {
	p.need()
	p.keywords = keywords
	p.changed()
}

// Keywords ports CompletePackage::getKeywords.
func (p *CompletePackage) Keywords() *php.Array {
	p.need()

	return orEmpty(p.keywords)
}

// SetAuthors ports CompletePackage::setAuthors.
func (p *CompletePackage) SetAuthors(authors *php.Array) {
	p.need()
	p.authors = authors
	p.changed()
}

// Authors ports CompletePackage::getAuthors.
func (p *CompletePackage) Authors() *php.Array {
	p.need()

	return orEmpty(p.authors)
}

// SetDescription ports CompletePackage::setDescription.
func (p *CompletePackage) SetDescription(description NullString) {
	p.need()
	setNullable(&p.Package, setDescription, &p.description, description)
}

// Description ports CompletePackage::getDescription.
func (p *CompletePackage) Description() NullString {
	p.need()
	return nullable(&p.Package, setDescription, p.description)
}

// SetHomepage ports CompletePackage::setHomepage.
func (p *CompletePackage) SetHomepage(homepage NullString) {
	p.need()
	setNullable(&p.Package, setHomepage, &p.homepage, homepage)
}

// Homepage ports CompletePackage::getHomepage.
func (p *CompletePackage) Homepage() NullString {
	p.need()

	return nullable(&p.Package, setHomepage, p.homepage)
}

// SetSupport ports CompletePackage::setSupport.
func (p *CompletePackage) SetSupport(support *php.Array) {
	p.need()
	p.support = support
	p.changed()
}

// Support ports CompletePackage::getSupport.
func (p *CompletePackage) Support() *php.Array {
	p.need()

	return orEmpty(p.support)
}

// SetFunding ports CompletePackage::setFunding.
func (p *CompletePackage) SetFunding(funding *php.Array) {
	p.need()
	p.funding = funding
	p.changed()
}

// Funding ports CompletePackage::getFunding.
func (p *CompletePackage) Funding() *php.Array {
	p.need()

	return orEmpty(p.funding)
}

// IsAbandoned ports CompletePackage::isAbandoned.
func (p *CompletePackage) IsAbandoned() bool { return php.ToBool(p.abandoned) }

// SetAbandoned ports CompletePackage::setAbandoned: true, false or the
// name of the replacement package (any PHP value is kept as given).
func (p *CompletePackage) SetAbandoned(abandoned any) {
	p.need()
	p.abandoned = abandoned
	p.changed()
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
	p.need()
	setNullable(&p.Package, setArchiveName, &p.archiveName, name)
}

// ArchiveName ports CompletePackage::getArchiveName.
func (p *CompletePackage) ArchiveName() NullString {
	p.need()
	return nullable(&p.Package, setArchiveName, p.archiveName)
}

// SetArchiveExcludes ports CompletePackage::setArchiveExcludes.
func (p *CompletePackage) SetArchiveExcludes(excludes *php.Array) {
	p.need()
	p.archiveExcludes = excludes
	p.changed()
}

// ArchiveExcludes ports CompletePackage::getArchiveExcludes.
func (p *CompletePackage) ArchiveExcludes() *php.Array {
	p.need()

	return orEmpty(p.archiveExcludes)
}

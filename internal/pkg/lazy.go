// Ports nothing: skeleton packages, whose properties beyond those the
// solver reads are set on first use (deliberate deviation 3, speed).

package pkg

import (
	"reflect"
	"sync"
	"sync/atomic"
)

// lazyRest is what a skeleton package (NewSkeletonPackage) still has to
// set: load returns the package as the full load builds it, whose other
// properties the skeleton then takes (adoptRest), after the configuring
// put off until then (ConfigureLater).
type lazyRest struct {
	mu    sync.Mutex
	done  atomic.Bool
	owner *CompletePackage
	load  func() (*CompletePackage, error)
	later []func(PackageInterface)
}

// NewSkeletonPackage makes p, a package built with the properties a
// skeleton keeps from the start (skeletonFields: its name, versions,
// type, links, default branch flag and abandoned value), a skeleton: the
// first use of any other property, or of a setter, sets all the others
// from load, which builds the same package in full. load must not fail
// (it is called when its caller can no longer tell): a caller makes
// skeletons only of packages whose full load it knows to succeed alike.
func NewSkeletonPackage(p *CompletePackage, load func() (*CompletePackage, error)) {
	p.lazy = &lazyRest{owner: p, load: load}
}

// skeletonOf is the skeleton package p is or aliases, whose other
// properties are not set yet; nil for any other package.
func skeletonOf(p PackageInterface) *lazyRest {
	if a, ok := p.(Alias); ok {
		p = a.AliasOf()
	}
	cp, ok := p.(*CompletePackage)
	if !ok || cp.lazy == nil || cp.lazy.done.Load() {
		return nil
	}

	return cp.lazy
}

// IsSkeleton reports whether p is, or aliases, a skeleton package whose
// other properties are not set yet.
func IsSkeleton(p PackageInterface) bool { return skeletonOf(p) != nil }

// ConfigureLater is configure(p) for a configure that only reads and
// sets properties beyond a skeleton's (and whose result does not depend
// on when it runs): for a skeleton package, or an alias of one, it runs on
// the package the skeleton completes from, once the skeleton is completed;
// for any other package at once.
func ConfigureLater(p PackageInterface, configure func(PackageInterface)) {
	if l := skeletonOf(p); l != nil {
		l.mu.Lock()
		if !l.done.Load() {
			l.later = append(l.later, configure)
			l.mu.Unlock()

			return
		}
		l.mu.Unlock()
	}
	configure(p)
}

// need sets the properties of a skeleton package it does not have yet.
func (p *Package) need() {
	if l := p.lazy; l != nil && !l.done.Load() {
		l.run()
	}
}

func (l *lazyRest) run() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.done.Load() {
		return
	}
	full, err := l.load()
	if err != nil {
		panic("pkg: the full load of skeleton package " + l.owner.prettyName + " " + l.owner.prettyVersion + " failed: " + err.Error())
	}
	for _, configure := range l.later {
		configure(full)
	}
	l.owner.adoptRest(full)
	l.load, l.later = nil, nil
	l.done.Store(true)
}

// adoptRest sets the properties of p beyond skeletonFields to full's.
func (p *CompletePackage) adoptRest(full *CompletePackage) {
	// Package
	p.targetDir = full.targetDir
	p.installationSource = full.installationSource
	p.sourceType = full.sourceType
	p.sourceURL = full.sourceURL
	p.sourceReference = full.sourceReference
	p.distType = full.distType
	p.distURL = full.distURL
	p.distReference = full.distReference
	p.distSha1Checksum = full.distSha1Checksum
	p.notificationURL = full.notificationURL
	p.releaseDate = full.releaseDate
	p.sourceMirrors = full.sourceMirrors
	p.distMirrors = full.distMirrors
	p.extra = full.extra
	p.binaries = full.binaries
	p.suggests = full.suggests
	p.autoload = full.autoload
	p.devAutoload = full.devAutoload
	p.includePaths = full.includePaths
	p.transportOptions = full.transportOptions
	p.phpExt = full.phpExt
	p.set = p.set&skeletonSetBits | full.set&^skeletonSetBits

	// CompletePackage
	p.description = full.description
	p.homepage = full.homepage
	p.archiveName = full.archiveName
	p.repositories = full.repositories
	p.license = full.license
	p.keywords = full.keywords
	p.authors = full.authors
	p.scripts = full.scripts
	p.support = full.support
	p.funding = full.funding
	p.archiveExcludes = full.archiveExcludes
}

// SkeletonMatches reports whether skeleton, a skeleton package (or its
// alias) whose other properties are then loaded, is the package (or
// alias) eager, the same version loaded in full: equal in every property
// but the change counters.
func SkeletonMatches(skeleton, eager PackageInterface) bool {
	sa, sIsAlias := skeleton.(*CompleteAliasPackage)
	ea, eIsAlias := eager.(*CompleteAliasPackage)
	if sIsAlias != eIsAlias {
		return false
	}
	if sIsAlias {
		s, e := sa.AliasPackage, ea.AliasPackage
		s.rev, e.rev = 0, 0
		s.idRev, e.idRev = 0, 0
		s.aliasOf, e.aliasOf = nil, nil
		if !reflect.DeepEqual(s, e) {
			return false
		}
		skeleton, eager = sa.AliasOf(), ea.AliasOf()
	}

	s, sOK := skeleton.(*CompletePackage)
	e, eOK := eager.(*CompletePackage)
	if !sOK || !eOK || s.lazy == nil || e.lazy != nil {
		return false
	}
	s.need()
	sc, ec := *s, *e
	sc.rev, ec.rev = 0, 0
	sc.idRev, ec.idRev = 0, 0
	sc.lazy = nil

	return reflect.DeepEqual(sc, ec)
}

// skeletonSetBits are the bits of Package.set of the skeleton's
// properties.
const skeletonSetBits = setType

// skeletonFields are the fields of Package and CompletePackage a skeleton
// has from the start, which adoptRest leaves alone; it sets all others
// (adoptedFields). Methods reading only these need not materialize.
var skeletonFields = []string{
	"basePackage", "version", "prettyVersion", "typ", "requires", "conflicts", "provides",
	"replaces", "devRequires", "stability", "isDefaultBranch", "lazy", "abandoned",
}

// adoptedFields are the fields adoptRest sets ("set": its other bits).
var adoptedFields = []string{
	"targetDir", "installationSource", "sourceType", "sourceURL", "sourceReference", "distType",
	"distURL", "distReference", "distSha1Checksum", "notificationURL", "releaseDate",
	"sourceMirrors", "distMirrors", "extra", "binaries", "suggests", "autoload", "devAutoload",
	"includePaths", "transportOptions", "phpExt", "set",
	"description", "homepage", "archiveName", "repositories", "license", "keywords", "authors",
	"scripts", "support", "funding", "archiveExcludes",
}

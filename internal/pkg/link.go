// Ports src/Composer/Package/Link.php.

package pkg

import (
	"iter"
	"strconv"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// Link types: Link::TYPE_*. The first five are the methods of
// BasePackage::$supportedLinkTypes and Link::$TYPES.
const (
	TypeRequire        = "requires"
	TypeDevRequire     = "devRequires"
	TypeProvide        = "provides"
	TypeConflict       = "conflicts"
	TypeReplace        = "replaces"
	TypeDoesNotRequire = "does not require"
	// TypeUnknown is Link::TYPE_UNKNOWN (private in PHP), the default
	// description.
	TypeUnknown = "relates to"
)

// LinkTypes returns Link::$TYPES.
func LinkTypes() [5]string {
	return [5]string{TypeRequire, TypeDevRequire, TypeProvide, TypeConflict, TypeReplace}
}

// Link ports Composer\Package\Link: a relation from one package to a
// target and its constraint.
//
// Links are the most numerous objects the loaders create, so the struct
// is kept to 80 bytes: the usual descriptions are a code, and only other
// descriptions are stored as a string.
type Link struct {
	source            string
	target            string
	constraint        semver.ConstraintInterface
	prettyConstraint  string
	customDescription *string
	description       uint8 // index into linkDescriptions; 0 means customDescription
	hasPretty         bool
}

// linkDescriptions are the descriptions Composer's links carry.
var linkDescriptions = [...]string{
	"", TypeRequire, "requires (for development)", TypeProvide, TypeConflict, TypeReplace, TypeDoesNotRequire, TypeUnknown,
}

// NewLink ports Link::__construct. source and target are lowercased, and
// TypeDevRequire becomes the description "requires (for development)".
func NewLink(source, target string, constraint semver.ConstraintInterface, description string, prettyConstraint NullString) *Link {
	l := &Link{
		source:           php.Strtolower(source),
		target:           php.Strtolower(target),
		constraint:       constraint,
		prettyConstraint: prettyConstraint.S,
		hasPretty:        prettyConstraint.Valid,
	}

	if description == TypeDevRequire {
		description = "requires (for development)"
	}

	for i := 1; i < len(linkDescriptions); i++ {
		if linkDescriptions[i] == description {
			l.description = uint8(i)

			return l
		}
	}

	// a copy taken here only, so the common path does not move
	// description to the heap
	custom := description
	l.customDescription = &custom

	return l
}

// Description ports Link::getDescription.
func (l *Link) Description() string {
	if l.description == 0 {
		return *l.customDescription
	}

	return linkDescriptions[l.description]
}

// Source ports Link::getSource.
func (l *Link) Source() string { return l.source }

// Target ports Link::getTarget.
func (l *Link) Target() string { return l.target }

// Constraint ports Link::getConstraint.
func (l *Link) Constraint() semver.ConstraintInterface { return l.constraint }

// RawPrettyConstraint returns the pretty constraint as stored, null
// included.
func (l *Link) RawPrettyConstraint() NullString {
	return NullString{S: l.prettyConstraint, Valid: l.hasPretty}
}

// isSelfVersion reports 'self.version' === $link->getPrettyConstraint()
// (a link without pretty constraint is not one).
func (l *Link) isSelfVersion() bool { return l.hasPretty && l.prettyConstraint == "self.version" }

// PrettyConstraint ports Link::getPrettyConstraint; it fails as PHP does
// when the link was built without one.
func (l *Link) PrettyConstraint() (string, error) {
	if !l.hasPretty {
		return "", &util.UnexpectedValueError{Message: "Link " + l.String() + " has been misconfigured and had no prettyConstraint given."}
	}

	return l.prettyConstraint, nil
}

// String ports Link::__toString.
func (l *Link) String() string {
	return l.source + " " + l.Description() + " " + l.target + " (" + l.constraint.String() + ")"
}

// PrettyString ports Link::getPrettyString.
func (l *Link) PrettyString(sourcePackage PackageInterface) string {
	return sourcePackage.PrettyString() + " " + l.Description() + " " + l.target + " " + l.constraint.PrettyString()
}

// Links is a PHP array<string, Link> as the package link getters return
// it: an ordered map from array key to link. In Composer the key is
// almost always the link's target; alias packages and callers passing
// lists can produce others. Keys are kept in their PHP string form ("0"
// for the int key 0), which identifies a PHP key uniquely.
//
// Links is immutable and cheap to copy; the zero value is the empty
// array.
type Links struct{ d *linksData }

type linksData struct {
	links []*Link
	keys  []string // nil when every key is its link's target
}

// LinksOf returns [$l->getTarget() => $l, ...]: later links with the same
// target replace earlier ones in place.
func LinksOf(links ...*Link) Links {
	var b LinksBuilder
	for _, l := range links {
		b.Set(l.target, l)
	}

	return b.Build()
}

// Len returns count($links).
func (l Links) Len() int {
	if l.d == nil {
		return 0
	}

	return len(l.d.links)
}

// Key returns the key of the i-th entry.
func (l Links) Key(i int) string {
	if l.d.keys == nil {
		return l.d.links[i].target
	}

	return l.d.keys[i]
}

// At returns the link of the i-th entry.
func (l Links) At(i int) *Link { return l.d.links[i] }

// All iterates over the keys and links in order.
func (l Links) All() iter.Seq2[string, *Link] {
	return func(yield func(string, *Link) bool) {
		for i := range l.Len() {
			if !yield(l.Key(i), l.d.links[i]) {
				return
			}
		}
	}
}

// Values iterates over the links in order.
func (l Links) Values() iter.Seq[*Link] {
	return func(yield func(*Link) bool) {
		if l.d == nil {
			return
		}

		for _, link := range l.d.links {
			if !yield(link) {
				return
			}
		}
	}
}

// index returns the position of key, or -1.
func (l Links) index(key string) int {
	for i := range l.Len() {
		if l.Key(i) == key {
			return i
		}
	}

	return -1
}

// Get returns $links[$key]; key is in PHP string form.
func (l Links) Get(key string) (*Link, bool) {
	if i := l.index(key); i >= 0 {
		return l.d.links[i], true
	}

	return nil, false
}

// Has reports isset($links[$key]).
func (l Links) Has(key string) bool { return l.index(key) >= 0 }

// With returns a copy with $links[$key] = $link.
func (l Links) With(key string, link *Link) Links {
	var b LinksBuilder
	b.Grow(l.Len() + 1)

	for k, v := range l.All() {
		b.Set(k, v)
	}

	b.Set(key, link)

	return b.Build()
}

// Without returns a copy with unset($links[$key]).
func (l Links) Without(key string) Links {
	var b LinksBuilder

	for k, v := range l.All() {
		if k != key {
			b.Set(k, v)
		}
	}

	return b.Build()
}

// hasZeroKey reports whether $links[0] is set (Package::setRequires' isset
// check for a list).
func (l Links) hasZeroKey() bool { return l.Has("0") }

// LinksListError is what Package::setRequires (setConflicts, ...;
// setter is the method name) does with a list-shaped $links: its
// convertLinksToMap raises an E_USER_NOTICE, which Composer's
// ErrorHandler throws as this \ErrorException before anything is set. It
// is nil for a map (no "0" key). Package's Go setters convert a list
// silently, as PHP does without the ErrorHandler; callers handing over
// links from PHP (the plugin shim) check this first.
func LinksListError(setter string, links Links) error {
	if !links.hasZeroKey() {
		return nil
	}

	return &util.ErrorException{
		Message: "Package::" + setter + " must be called with a map of lowercased package name => Link object, got a indexed array, this is deprecated and you should fix your usage.",
	}
}

// mergeList ports array_merge($links, $list): int keys of links are
// renumbered from 0, string keys kept, and the list appended.
func (l Links) mergeList(list []*Link) Links {
	if len(list) == 0 && !l.hasIntKey() {
		return l
	}

	var b LinksBuilder
	b.Grow(l.Len() + len(list))

	next := 0
	for k, v := range l.All() {
		if php.StrKey(k).IsInt() {
			b.Append(strconv.Itoa(next), v)
			next++

			continue
		}

		b.Set(k, v)
	}

	for _, v := range list {
		b.Append(strconv.Itoa(next), v)
		next++
	}

	return b.Build()
}

func (l Links) hasIntKey() bool {
	for i := range l.Len() {
		if php.StrKey(l.Key(i)).IsInt() {
			return true
		}
	}

	return false
}

// LinksBuilder builds a Links with PHP assignment semantics ($links[$key]
// = $link). The zero value is empty and ready to use; after Build the
// builder must not be used again.
type LinksBuilder struct {
	links []*Link
	keys  []string // nil while every key is its link's target
	index map[string]int
}

// linearLimit is the size up to which Set scans for an existing key
// instead of keeping a map.
const linearLimit = 16

// Grow makes room for n entries.
func (b *LinksBuilder) Grow(n int) {
	if n > cap(b.links) {
		b.links = append(make([]*Link, 0, n), b.links...)
	}
}

func (b *LinksBuilder) key(i int) string {
	if b.keys == nil {
		return b.links[i].target
	}

	return b.keys[i]
}

func (b *LinksBuilder) find(key string) int {
	if b.index == nil && len(b.links) > linearLimit {
		b.index = make(map[string]int, 2*len(b.links))
		for i := range b.links {
			b.index[b.key(i)] = i
		}
	}

	if b.index != nil {
		if i, ok := b.index[key]; ok {
			return i
		}

		return -1
	}

	for i := range b.links {
		if b.key(i) == key {
			return i
		}
	}

	return -1
}

// Set performs $links[$key] = $link: an existing key keeps its position.
func (b *LinksBuilder) Set(key string, link *Link) {
	if i := b.find(key); i >= 0 {
		// The key stays; with implied keys, record them before the
		// new link's target would change this one.
		if b.keys == nil && link.target != key {
			b.materializeKeys()
		}

		b.links[i] = link

		return
	}

	b.Append(key, link)
}

// Append adds $links[$key] = $link for a key known not to be present
// yet (it skips Set's lookup).
func (b *LinksBuilder) Append(key string, link *Link) {
	if b.keys == nil && link.target != key {
		b.materializeKeys()
	}

	b.links = append(b.links, link)
	if b.keys != nil {
		b.keys = append(b.keys, key)
	}

	if b.index != nil {
		b.index[key] = len(b.links) - 1
	}
}

func (b *LinksBuilder) materializeKeys() {
	b.keys = make([]string, len(b.links), cap(b.links))
	for i, l := range b.links {
		b.keys[i] = l.target
	}
}

// Build returns the links built.
func (b *LinksBuilder) Build() Links {
	if len(b.links) == 0 {
		return Links{}
	}

	return Links{&linksData{links: b.links, keys: b.keys}}
}

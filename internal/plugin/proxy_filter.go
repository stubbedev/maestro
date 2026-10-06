// Platform requirement filters written in PHP (docs/PLUGINS.md §4.7): an
// implementation of PlatformRequirementFilterInterface other than
// Composer's three, given to an Installer, an AutoloadGenerator or
// VersionSelector::findBestCandidate().

package plugin

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
)

// phpFilter is a filter class of a plugin's own as maestro uses it: its
// isIgnored() and isUpperBoundIgnored() are the PHP object's. A failing
// call (PHP ending, or maestro's parallel work calling it off the PHP
// baton, docs/PLUGINS.md §5.14) reads false.
type phpFilter struct {
	r   *Runtime
	obj *rpc.PHPObject
}

func (r *Runtime) phpFilter(obj *rpc.PHPObject) *phpFilter { return &phpFilter{r: r, obj: obj} }

func (f *phpFilter) phpObject() *rpc.PHPObject { return f.obj }

// IsIgnored implements filter.PlatformRequirementFilter.
func (f *phpFilter) IsIgnored(req string) bool {
	v, err := f.r.callObject(f.obj, "isIgnored", req)

	return err == nil && php.ToBool(v)
}

// IsUpperBoundIgnored implements filter.PlatformRequirementFilter.
func (f *phpFilter) IsUpperBoundIgnored(req string) bool {
	v, err := f.r.callObject(f.obj, "isUpperBoundIgnored", req)

	return err == nil && php.ToBool(v)
}

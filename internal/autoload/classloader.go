// Ports the registration half of src/Composer/Autoload/ClassLoader.php
// (add, addPsr4, set, setPsr4, addClassMap and the property layout), which
// AutoloadGenerator::createLoader and getStaticFile rely on. The loading
// half runs in PHP: the verbatim file is embedded as ClassLoaderPHP.

package autoload

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// ClassLoader holds what a Composer\Autoload\ClassLoader instance holds
// after registration: the PSR-4, PSR-0 and class map arrays, with PHP's key
// order and coercion. It is what createLoader builds, and the source of the
// static properties of autoload_static.php.
type ClassLoader struct {
	// VendorDir is the constructor's $vendorDir ("" for null).
	VendorDir string

	PrefixLengthsPsr4 *php.Array // first byte => (prefix => length)
	PrefixDirsPsr4    *php.Array // prefix => list of dirs
	FallbackDirsPsr4  *php.Array // list of dirs
	PrefixesPsr0      *php.Array // first byte => (prefix => list of dirs)
	FallbackDirsPsr0  *php.Array // list of dirs
	ClassMap          *php.Array // class => path
}

// NewClassLoader ports new ClassLoader($vendorDir).
func NewClassLoader(vendorDir string) *ClassLoader {
	return &ClassLoader{
		VendorDir:         vendorDir,
		PrefixLengthsPsr4: php.NewArray(),
		PrefixDirsPsr4:    php.NewArray(),
		FallbackDirsPsr4:  php.NewArray(),
		PrefixesPsr0:      php.NewArray(),
		FallbackDirsPsr0:  php.NewArray(),
		ClassMap:          php.NewArray(),
	}
}

// property is one array property of a ClassLoader, named as in PHP.
type property struct {
	name  string
	value *php.Array
}

// properties returns the array properties in declaration order, which is
// the order `(array) $loader` lists them in.
func (l *ClassLoader) properties() [6]property {
	return [6]property{
		{"prefixLengthsPsr4", l.PrefixLengthsPsr4},
		{"prefixDirsPsr4", l.PrefixDirsPsr4},
		{"fallbackDirsPsr4", l.FallbackDirsPsr4},
		{"prefixesPsr0", l.PrefixesPsr0},
		{"fallbackDirsPsr0", l.FallbackDirsPsr0},
		{"classMap", l.ClassMap},
	}
}

// AddClassMap ports ClassLoader::addClassMap.
func (l *ClassLoader) AddClassMap(classMap *php.Array) {
	if l.ClassMap.Len() > 0 {
		l.ClassMap = php.ArrayMerge(l.ClassMap, classMap)
	} else {
		l.ClassMap = classMap
	}
}

// Add ports ClassLoader::add: registers PSR-0 directories for a prefix,
// appending (or prepending) to the ones set before.
func (l *ClassLoader) Add(prefix php.Key, paths *php.Array, prepend bool) error {
	if !php.ToBool(prefix.Value()) {
		l.FallbackDirsPsr0 = mergeLists(l.FallbackDirsPsr0, paths, prepend)

		return nil
	}

	first, err := firstByte(prefix)
	if err != nil {
		return err
	}

	byFirst := l.PrefixesPsr0.ArrayAtOrCreate(first)
	current, ok := byFirst.GetKey(prefix)
	if !ok {
		byFirst.SetKey(prefix, paths)

		return nil
	}
	list, _ := current.(*php.Array)
	byFirst.SetKey(prefix, mergeLists(list, paths, prepend))

	return nil
}

// AddPsr4 ports ClassLoader::addPsr4: registers PSR-4 directories for a
// namespace, appending (or prepending) to the ones set before.
func (l *ClassLoader) AddPsr4(prefix php.Key, paths *php.Array, prepend bool) error {
	if !php.ToBool(prefix.Value()) {
		// Register directories for the root namespace.
		l.FallbackDirsPsr4 = mergeLists(l.FallbackDirsPsr4, paths, prepend)

		return nil
	}

	if current, ok := l.PrefixDirsPsr4.GetKey(prefix); ok {
		list, _ := current.(*php.Array)
		l.PrefixDirsPsr4.SetKey(prefix, mergeLists(list, paths, prepend))

		return nil
	}

	// Register directories for a new namespace.
	return l.SetPsr4(prefix, paths)
}

// Set ports ClassLoader::set: registers PSR-0 directories for a prefix,
// replacing the ones set before.
func (l *ClassLoader) Set(prefix php.Key, paths *php.Array) error {
	if !php.ToBool(prefix.Value()) {
		l.FallbackDirsPsr0 = paths

		return nil
	}

	first, err := firstByte(prefix)
	if err != nil {
		return err
	}
	l.PrefixesPsr0.ArrayAtOrCreate(first).SetKey(prefix, paths)

	return nil
}

// SetPsr4 ports ClassLoader::setPsr4: registers PSR-4 directories for a
// namespace, replacing the ones set before.
func (l *ClassLoader) SetPsr4(prefix php.Key, paths *php.Array) error {
	if !php.ToBool(prefix.Value()) {
		l.FallbackDirsPsr4 = paths

		return nil
	}

	first, err := firstByte(prefix)
	if err != nil {
		return err
	}
	s := prefix.String()
	if s[len(s)-1] != '\\' {
		return &util.InvalidArgumentError{Message: "A non-empty PSR-4 prefix must end with a namespace separator."}
	}
	l.PrefixLengthsPsr4.ArrayAtOrCreate(first).SetKey(prefix, int64(len(s)))
	l.PrefixDirsPsr4.SetKey(prefix, paths)

	return nil
}

// firstByte is $prefix[0] as an array key. An int prefix (a numeric
// namespace PHP coerced to an int key) has no offsets: PHP warns, which
// Composer's error handler turns into an exception (PHP 8.3's message;
// before 8.3 it read "... on value of type int").
func firstByte(prefix php.Key) (php.Key, error) {
	if prefix.IsInt() {
		return php.Key{}, &util.ErrorException{Message: "Trying to access array offset on int"}
	}

	return php.StrKey(prefix.String()[:1]), nil
}

// mergeLists is array_merge($a, $b), or array_merge($b, $a) to prepend.
func mergeLists(a, b *php.Array, prepend bool) *php.Array {
	if prepend {
		return php.ArrayMerge(b, a)
	}

	return php.ArrayMerge(a, b)
}

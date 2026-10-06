package util

import (
	"fmt"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/phperr"
)

// PHPClassOf names the class get_class($e) shows, including the library
// subclasses util's generic types stand for.
func TestPHPClassOf(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&RuntimeError{Message: "x"}, "RuntimeException"},
		{&RuntimeError{Message: "x", Class: ClassProcessRuntime}, `Symfony\Component\Process\Exception\RuntimeException`},
		{fmt.Errorf("wrapped: %w", &RuntimeError{Message: "x", Class: ClassProcessRuntime}), `Symfony\Component\Process\Exception\RuntimeException`},
		{&LogicError{Message: "x"}, "LogicException"},
		{&LogicError{Message: "x", Class: ClassProcessLogic}, `Symfony\Component\Process\Exception\LogicException`},
		{&LogicError{Message: "x", Class: "BadMethodCallException"}, "BadMethodCallException"},
		{&InvalidArgumentError{Message: "x"}, "InvalidArgumentException"},
		{&InvalidArgumentError{Message: "x", Class: ClassDirectoryNotFound}, `Symfony\Component\Finder\Exception\DirectoryNotFoundException`},
		{&InvalidArgumentError{Message: "x", Class: ClassProcessInvalidArg}, `Symfony\Component\Process\Exception\InvalidArgumentException`},
		{&UnexpectedValueError{Message: "x"}, "UnexpectedValueException"},
		{&UnexpectedValueError{Message: "x", Class: ClassAccessDenied}, `Symfony\Component\Finder\Exception\AccessDeniedException`},
		{&IOError{Message: "x"}, `Symfony\Component\Filesystem\Exception\IOException`},
		{&IOError{Message: "x", Class: ClassFileNotFound}, `Symfony\Component\Filesystem\Exception\FileNotFoundException`},
		{&SecurityError{Message: "x"}, `Composer\Exception\SecurityException`},
		{&ErrorException{Message: "x"}, "ErrorException"},
		{fmt.Errorf("plain"), "RuntimeException"},
	}
	for _, c := range cases {
		if got, _ := PHPClassOf(c.err); got != c.want {
			t.Errorf("PHPClassOf(%T %q) = %s, want %s", c.err, c.err, got, c.want)
		}
	}
}

// The symfony/process and symfony/finder exceptions maestro raises carry
// their library classes and throw sites (vendor/symfony/process/Process.php,
// vendor/symfony/finder/Finder.php of Composer 2.10.3's vendor).
func TestSymfonyExceptionClasses(t *testing.T) {
	missing := t.TempDir() + "/missing"

	_, cwdErr := NewProcess([]string{"true"}, missing, nil, time.Minute).Run(nil)
	_, waitErr := NewProcess([]string{"true"}, "", nil, time.Minute).Wait()
	_, finderErr := Size(missing + "/")
	_, _, finderInErr := finderIn(missing)

	cases := []struct {
		name  string
		err   error
		class string
		file  string
		line  int
	}{
		{"Process::start (cwd)", cwdErr, ClassProcessRuntime, "Process.php", 350},
		{"Process::wait", waitErr, ClassProcessLogic, "Process.php", 1618},
		{"Finder::in", finderInErr, ClassDirectoryNotFound, "Finder.php", 592},
	}
	for _, c := range cases {
		if c.err == nil {
			t.Errorf("%s: no error", c.name)

			continue
		}
		if got, _ := PHPClassOf(c.err); got != c.class {
			t.Errorf("%s: class %s, want %s", c.name, got, c.class)
		}
		if !phperr.Is(c.err, c.file, c.line) {
			site, _ := phperr.SiteOf(c.err)
			t.Errorf("%s: site %s:%d, want %s:%d", c.name, site.File, site.Line, c.file, c.line)
		}
		// they extend the SPL classes Composer catches
		if c.class == ClassProcessRuntime && !IsRuntimeException(c.err) {
			t.Errorf("%s: not a \\RuntimeException", c.name)
		}
	}
	_ = finderErr
}

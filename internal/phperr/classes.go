package phperr

// parents is the class hierarchy of every PHP exception class maestro's
// errors stand for: class => its parent class ("" for a root, Exception
// and Error). PHP's own classes are PHP 8.4's; the others are Composer
// 2.10.3's, the libraries its phar ships and maestro's shim's.
// TestClasses checks it against PHP (tools/oracle/phperr), and that every
// exception class maestro names is here.
var parents = map[string]string{
	// PHP: Throwable's two roots
	"Exception": "",
	"Error":     "",

	// PHP: \Exception's subclasses
	"ErrorException":               "Exception",
	"JsonException":                "Exception",
	"PharException":                "Exception",
	"ReflectionException":          "Exception",
	"DateException":                "Exception",
	"DateMalformedStringException": "DateException",
	"LogicException":               "Exception",
	"BadFunctionCallException":     "LogicException",
	"BadMethodCallException":       "BadFunctionCallException",
	"DomainException":              "LogicException",
	"InvalidArgumentException":     "LogicException",
	"LengthException":              "LogicException",
	"OutOfRangeException":          "LogicException",
	"RuntimeException":             "Exception",
	"OutOfBoundsException":         "RuntimeException",
	"OverflowException":            "RuntimeException",
	"RangeException":               "RuntimeException",
	"UnderflowException":           "RuntimeException",
	"UnexpectedValueException":     "RuntimeException",

	// PHP: \Error's subclasses (the engine's)
	"TypeError":           "Error",
	"ArgumentCountError":  "TypeError",
	"ValueError":          "Error",
	"ArithmeticError":     "Error",
	"DivisionByZeroError": "ArithmeticError",
	"CompileError":        "Error",
	"ParseError":          "CompileError",
	"UnhandledMatchError": "Error",
	"AssertionError":      "Error",
	"FiberError":          "Error",

	// Composer
	`Composer\Downloader\TransportException`:              "RuntimeException",
	`Composer\Downloader\MaxFileSizeExceededException`:    `Composer\Downloader\TransportException`,
	`Composer\Downloader\FilesystemException`:             "Exception",
	`Composer\Exception\IrrecoverableDownloadException`:   "RuntimeException",
	`Composer\Exception\NoSslException`:                   "RuntimeException",
	`Composer\Exception\SecurityException`:                "UnexpectedValueException",
	`Composer\DependencyResolver\SolverBugException`:      "RuntimeException",
	`Composer\DependencyResolver\SolverProblemsException`: "RuntimeException",
	`Composer\EventDispatcher\ScriptExecutionException`:   "RuntimeException",
	`Composer\Json\JsonValidationException`:               "Exception",
	`Composer\Package\Loader\InvalidPackageException`:     "Exception",
	`Composer\Plugin\PluginBlockedException`:              "UnexpectedValueException",
	`Composer\Repository\InvalidRepositoryException`:      "Exception",
	`Composer\Repository\RepositorySecurityException`:     "Exception",

	// composer/pcre, seld/jsonlint
	`Composer\Pcre\UnexpectedNullMatchException`: `Composer\Pcre\PcreException`,
	`Composer\Pcre\PcreException`:                "RuntimeException",
	`Seld\JsonLint\ParsingException`:             "Exception",
	`Seld\JsonLint\DuplicateKeyException`:        `Seld\JsonLint\ParsingException`,
	`Seld\JsonLint\InvalidEncodingException`:     `Seld\JsonLint\ParsingException`,

	// symfony/console
	`Symfony\Component\Console\Exception\CommandNotFoundException`:   "InvalidArgumentException",
	`Symfony\Component\Console\Exception\NamespaceNotFoundException`: `Symfony\Component\Console\Exception\CommandNotFoundException`,
	`Symfony\Component\Console\Exception\InvalidArgumentException`:   "InvalidArgumentException",
	`Symfony\Component\Console\Exception\InvalidOptionException`:     "InvalidArgumentException",
	`Symfony\Component\Console\Exception\LogicException`:             "LogicException",
	`Symfony\Component\Console\Exception\RuntimeException`:           "RuntimeException",
	`Symfony\Component\Console\Exception\MissingInputException`:      `Symfony\Component\Console\Exception\RuntimeException`,

	// symfony/filesystem, symfony/finder, symfony/process, symfony/string
	`Symfony\Component\Filesystem\Exception\IOException`:            "RuntimeException",
	`Symfony\Component\Filesystem\Exception\FileNotFoundException`:  `Symfony\Component\Filesystem\Exception\IOException`,
	`Symfony\Component\Finder\Exception\DirectoryNotFoundException`: "InvalidArgumentException",
	`Symfony\Component\Finder\Exception\AccessDeniedException`:      "UnexpectedValueException",
	`Symfony\Component\Process\Exception\RuntimeException`:          "RuntimeException",
	`Symfony\Component\Process\Exception\LogicException`:            "LogicException",
	`Symfony\Component\Process\Exception\InvalidArgumentException`:  "InvalidArgumentException",
	`Symfony\Component\Process\Exception\ProcessTimedOutException`:  `Symfony\Component\Process\Exception\RuntimeException`,
	`Symfony\Component\Process\Exception\ProcessSignaledException`:  `Symfony\Component\Process\Exception\RuntimeException`,
	`Symfony\Component\String\Exception\InvalidArgumentException`:   "InvalidArgumentException",

	// maestro's shim (internal/plugin/php)
	`Maestro\Shim\UnsupportedApiException`: "LogicException",
}

// implements are the interfaces of the class table's classes that maestro
// asks about, by the class declaring them (a subclass inherits them).
var implements = map[string][]string{
	`Symfony\Component\Console\Exception\CommandNotFoundException`: {ClassConsoleException},
	`Symfony\Component\Console\Exception\InvalidArgumentException`: {ClassConsoleException},
	`Symfony\Component\Console\Exception\InvalidOptionException`:   {ClassConsoleException},
	`Symfony\Component\Console\Exception\LogicException`:           {ClassConsoleException},
	`Symfony\Component\Console\Exception\RuntimeException`:         {ClassConsoleException},
}

// ClassConsoleException is symfony/console's ExceptionInterface, which
// every console exception implements.
const ClassConsoleException = `Symfony\Component\Console\Exception\ExceptionInterface`

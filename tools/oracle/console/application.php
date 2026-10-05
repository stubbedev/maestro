<?php
// Generates internal/console/testdata/oracle/application.json:
//  - "render": Application::renderThrowable() of many exceptions at several
//    terminal widths, verbosities and decorations. Exceptions get a fixed
//    file/line through reflection and are created at the top level of this
//    script, so their PHP trace is empty and the verbose output is fully
//    deterministic.
//  - "find": Application::find()/findNamespace() results and errors for
//    several command sets.
//  - "run": Application::run() of ArgvInput vectors against a fixed
//    application (stdout, stderr, exit code).
// Run: php tools/oracle/console/application.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Symfony\Component\Console\Application;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Exception;
use Symfony\Component\Console\Input\ArgvInput;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\ConsoleOutput;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Console\Output\StreamOutput;

error_reporting(E_ALL);
putenv('COLORTERM');
putenv('TERMINAL_EMULATOR');
putenv('KONSOLE_VERSION');
putenv('SHELL_VERBOSITY');
putenv('NO_COLOR');
unset($_SERVER['IDEA_INITIAL_DIRECTORY'], $_SERVER['SHELL_VERBOSITY'], $_ENV['SHELL_VERBOSITY']);
$_SERVER['PHP_SELF'] = 'bin/console';

function withLocation(\Throwable $e, string $file, int $line): \Throwable
{
    $r = new \ReflectionObject($e);
    while (!$r->hasProperty('file')) {
        $r = $r->getParentClass();
    }
    foreach (['file' => $file, 'line' => $line] as $k => $v) {
        $p = $r->getProperty($k);
        $p->setAccessible(true);
        $p->setValue($e, $v);
    }

    return $e;
}

// [class, message, code, file, line, previous-index or null]
$specs = [
    ['Exception', 'Simple message', 0, '/app/src/Foo.php', 12, null],
    ['Exception', 'With code', 42, '/app/src/Foo.php', 13, null],
    ['Exception', '', 0, '/app/src/Empty.php', 7, null],
    ['Exception', '', 5, '', 0, null],
    ['RuntimeException', "  surrounding whitespace\n", 0, '/app/src/Ws.php', 1, null],
    ['InvalidArgumentException', "multi\nline\r\nmessage\n\nwith blank", 0, '/app/src/Lines.php', 99, null],
    ['Exception', 'Tags <info>are</info> escaped <error>x</error> and \\<backslash>', 0, '/app/Tags.php', 3, null],
    ['Exception', 'averyveryveryveryveryveryveryveryveryveryveryverylongwordwithoutanyspacesthatmustbesplitbywidth', 0, '/app/Long.php', 4, null],
    ['Exception', 'A long sentence with many words that will certainly need to be wrapped when the terminal is narrow enough to force it', 0, '/app/Long.php', 5, null],
    ['Exception', 'エラーメッセージが発生しました。コマンドの実行中に問題があります。', 0, '/app/Wide.php', 6, null],
    ['Exception', 'héllo wörld ünïcödé çharacters', 0, '/app/Utf8.php', 8, null],
    ['Exception', "tab\tinside and trailing backslash \\", 0, '/app/Tab.php', 9, null],
    ['Exception', "invalid \xff\xfe utf8 bytes", 0, '/app/Bin.php', 10, null],
    ['Exception', 'Root cause', 0, '/app/Chain.php', 20, null],
    ['RuntimeException', 'Middle', 3, '/app/Chain.php', 21, 13],
    ['LogicException', 'Top <comment>level</comment>', 0, '/app/Chain.php', 22, 14],
    [Exception\CommandNotFoundException::class, 'Command "foo" is not defined.', 0, '/vendor/symfony/console/Application.php', 720, null],
    [Exception\RuntimeException::class, 'The "--foo" option does not exist.', 0, '/vendor/symfony/console/Input/ArgvInput.php', 220, null],
    [Exception\InvalidArgumentException::class, '', 0, '/vendor/symfony/console/Input/Input.php', 110, null],
    [Exception\LogicException::class, 'Console exception with previous', 0, '/vendor/symfony/console/Command/Command.php', 208, 0],
    [Exception\NamespaceNotFoundException::class, "There are no commands defined in the \"foos\" namespace.\n\nDid you mean this?\n    foo", 0, '/vendor/symfony/console/Application.php', 650, null],
    ['Exception', 'x', 0, 'relative/path.php', 1, null],
    ['Exception', 'y', 0, '/', 2, null],
];
$exceptions = [];
foreach ($specs as $i => [$class, $message, $code, $file, $line, $prev]) {
    $previous = null === $prev ? null : $exceptions[$prev];
    $e = is_a($class, Exception\CommandNotFoundException::class, true) ? new $class($message, [], $code, $previous) : new $class($message, $code, $previous);
    $exceptions[$i] = withLocation($e, $file, $line);
}

$out = ['exceptions' => [], 'render' => [], 'find' => [], 'run' => []];
foreach ($specs as $i => [$class, $message, $code, $file, $line, $prev]) {
    $out['exceptions'][] = ['class' => $class, 'message' => $message, 'code' => $code, 'file' => $file, 'line' => $line, 'previous' => $prev];
}

$app = new Application();
foreach ($exceptions as $i => $e) {
    foreach ([120, 80, 32, 10, 5, 1, 0] as $columns) {
        putenv('COLUMNS='.$columns);
        foreach ([OutputInterface::VERBOSITY_NORMAL, OutputInterface::VERBOSITY_VERBOSE, OutputInterface::VERBOSITY_QUIET] as $verbosity) {
            foreach ([false, true] as $decorated) {
                if ($decorated && 80 !== $columns) {
                    continue;
                }
                $stream = fopen('php://memory', 'w+');
                $output = new StreamOutput($stream, $verbosity, $decorated);
                $case = ['exception' => $i, 'columns' => $columns, 'verbosity' => $verbosity, 'decorated' => $decorated];
                try {
                    $app->renderThrowable($e, $output);
                } catch (\Throwable $err) {
                    $case['error'] = $err->getMessage();
                }
                rewind($stream);
                $case['output'] = stream_get_contents($stream);
                $out['render'][] = $case;
            }
        }
    }
}

// Command sets: [name, aliases, description, hidden]
$sets = [
    'foo' => [
        ['foo:bar', ['afoobar'], 'The foo:bar command', false],
        ['foo:bar1', ['afoobar1'], 'The foo:bar1 command', false],
        ['foo1:bar', ['afoobar2'], 'The foo1:bar command', false],
        ['foo3:bar', [], 'The foo3:bar command', false],
        ['foo3:bar:toh', [], '', false],
        ['foo:hidden', ['afoohidden'], '', true],
        ['bar:buc', [], '', false],
        ['foobar:foo', [], 'The foobar:foo command', false],
        ['foo:bar:baz', ['foobarbaz'], 'The foo:bar:baz command', false],
        ['foo:go:bret', ['foobargo'], 'The foo:bar:go command', false],
        ['test-ambiguous', ['test'], 'The test-ambiguous command', false],
        ['test-ambiguous2', [], 'The test-ambiguous2 command', false],
    ],
    'case' => [
        ['foo:BAR', [], 'foo:BAR command', false],
        ['foo:bar', [], 'foo:bar command', false],
        ['Cache:Clear', ['cc'], 'Clears the cache with a really long description that will be truncated when the terminal is narrow enough', false],
        ['cache:warmup', [], 'Warms up the cache', false],
        ['cache:pool:clear', [], 'Clears pools', false],
        ['cache:pool:prune', [], 'Prunes pools', true],
        ['debug:router', [], 'Displays routes', false],
        ['débug:ünicode', [], 'Unicode name ✓', false],
    ],
    'composer' => [
        ['install', ['i'], 'Installs the project dependencies from the composer.lock file if present, or falls back on the composer.json', false],
        ['update', ['u', 'upgrade'], 'Updates your dependencies to the latest version according to composer.json, and updates the composer.lock file', false],
        ['require', ['r'], 'Adds required packages to your composer.json and installs them', false],
        ['remove', ['rm', 'uninstall'], 'Removes a package from the require or require-dev', false],
        ['run-script', ['run'], 'Runs the scripts defined in composer.json', false],
        ['dump-autoload', ['dumpautoload'], 'Dumps the autoloader', false],
        ['self-update', ['selfupdate'], 'Updates composer.phar to the latest version', false],
        ['show', ['info'], 'Shows information about packages', false],
        ['status', [], 'Shows a list of locally modified packages', false],
        ['search', [], 'Searches for packages', false],
        ['suggests', [], 'Shows package suggestions', false],
        ['config', [], 'Sets config options', false],
        ['create-project', [], 'Creates new project from a package into given directory', false],
        ['depends', ['why'], 'Shows which packages cause the given package to be installed', false],
        ['prohibits', ['why-not'], 'Shows which packages prevent the given package from being installed', false],
        ['diagnose', [], 'Diagnoses the system to identify common errors', false],
        ['global', [], 'Allows running commands in the global composer dir ($COMPOSER_HOME)', false],
        ['outdated', [], 'Shows a list of installed packages that have updates available, including their latest version', false],
        ['reinstall', [], 'Uninstalls and reinstalls the given package names', false],
        ['audit', [], 'Checks for security vulnerability advisories for installed packages', false],
        ['bump', [], 'Increases the lower limit of your composer.json requirements to the currently installed versions', false],
        ['check-platform-reqs', [], 'Check that platform requirements are satisfied', false],
        ['clear-cache', ['clearcache', 'cc'], 'Clears composer\'s internal package cache', false],
        ['exec', [], 'Executes a vendored binary/script', false],
        ['fund', [], 'Discover how to help fund the maintenance of your dependencies', false],
        ['licenses', [], 'Shows information about licenses of dependencies', false],
        ['validate', [], 'Validates a composer.json and composer.lock', false],
        ['archive', [], 'Creates an archive of this composer package', false],
        ['browse', ['home'], 'Opens the package\'s repository URL or homepage in your browser', false],
    ],
];
$lookups = [
    'foo' => ['foo:bar', 'f:bar', 'f:b', 'a', 'f', 'foo', 'foo:', 'foo:b', 'foo:baR', 'foo2:bar', 'foo3:', 'foo3:bar', 'foo3:bar:toh', 'f::t', 'foo::bar', 'foo3:barr', 'fooo3:bar', 'bar1', 'Unknown command', 'Unknown-namespace:Unknown-command', 'foo2:command', 'test', 'afoohidden', 'foo:hidden', 'f:f', 'FOO:BAR', 'b', 'bar', 'h', 'li', 'help', 'x', '', ':', 'foo:bar:', "foo\xff"],
    'case' => ['f:B', 'f:BAR', 'f:b', 'f:bar', 'FoO:BaR', 'cache', 'cache:', 'c:c', 'C:C', 'ca:p', 'cache:pool:p', 'cc', 'cache:wrmup', 'cahce:clear', 'debug', 'd', 'débug', 'DÉBUG:Ü', 'ro'],
    'composer' => ['i', 'in', 'up', 'u', 're', 'r', 'run', 'ru', 'dump', 's', 'se', 'self', 'sh', 'c', 'cl', 'cc', 'instal', 'udpate', 'requier', 'remvoe', 'why', 'wh', 'satus', 'outdate', 'licence', 'archive', 'glob', 'lis', 'hel', 'com', 'completion', 'diag', 'fund', 'zzz'],
];
foreach ($sets as $setName => $set) {
    foreach ([120, 60, 20] as $columns) {
        putenv('COLUMNS='.$columns);
        $app = new Application();
        foreach ($set as [$name, $aliases, $desc, $hidden]) {
            $app->register($name)->setAliases($aliases)->setDescription($desc)->setHidden($hidden);
        }
        foreach ($lookups[$setName] as $lookup) {
            $case = ['set' => $setName, 'columns' => $columns, 'name' => $lookup];
            try {
                $case['result'] = $app->find($lookup)->getName();
            } catch (\Throwable $e) {
                $case['error'] = get_class($e);
                $case['message'] = $e->getMessage();
                $case['alternatives'] = method_exists($e, 'getAlternatives') ? $e->getAlternatives() : null;
            }
            try {
                $case['namespace'] = $app->findNamespace($lookup);
            } catch (\Throwable $e) {
                $case['namespaceError'] = $e->getMessage();
                $case['namespaceAlternatives'] = method_exists($e, 'getAlternatives') ? $e->getAlternatives() : null;
            }
            $out['find'][] = $case;
        }
        if (120 === $columns) {
            $out['namespaces'][$setName] = $app->getNamespaces();
        }
    }
}

function fmtv($v): string
{
    if (null === $v) {
        return 'null';
    }
    if (is_bool($v)) {
        return $v ? 'true' : 'false';
    }
    if (is_array($v)) {
        return '['.implode(',', array_map('fmtv', $v)).']';
    }

    return '"'.$v.'"';
}

class DumpCommand extends Command
{
    protected function configure()
    {
        $this->setName('app:dump')
            ->setAliases(['dump'])
            ->setDescription('Dumps its input')
            ->setHelp("The <info>%command.name%</info> command dumps.\n\n  <info>%command.full_name% x</info>")
            ->addArgument('first', \Symfony\Component\Console\Input\InputArgument::REQUIRED, 'The first argument')
            ->addArgument('rest', \Symfony\Component\Console\Input\InputArgument::IS_ARRAY, 'More arguments', null)
            ->addOption('opt', 'o', \Symfony\Component\Console\Input\InputOption::VALUE_REQUIRED, 'A value option', 'def')
            ->addOption('flag', 'f', \Symfony\Component\Console\Input\InputOption::VALUE_NONE, 'A flag')
            ->addOption('multi', 'm', \Symfony\Component\Console\Input\InputOption::VALUE_REQUIRED | \Symfony\Component\Console\Input\InputOption::VALUE_IS_ARRAY, 'Many values')
            ->addOption('maybe', null, \Symfony\Component\Console\Input\InputOption::VALUE_OPTIONAL, 'An optional value')
            ->addOption('color', null, \Symfony\Component\Console\Input\InputOption::VALUE_NEGATABLE, 'Negatable');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        foreach ($input->getArguments() as $k => $v) {
            $output->writeln('arg '.$k.'='.fmtv($v));
        }
        foreach ($input->getOptions() as $k => $v) {
            $output->writeln('opt '.$k.'='.fmtv($v));
        }
        $output->writeln('verbosity='.$output->getVerbosity().' decorated='.($output->isDecorated() ? 1 : 0).' interactive='.($input->isInteractive() ? 1 : 0));
        $output->writeln('<info>styled</info>', OutputInterface::VERBOSITY_VERBOSE);

        return (int) $input->getOption('opt') ?: 0;
    }
}

class FailCommand extends Command
{
    protected function configure()
    {
        $this->setName('app:fail')->setDescription('Fails')->addArgument('code', \Symfony\Component\Console\Input\InputArgument::OPTIONAL, '', '0');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $output->writeln('failing');
        $e = new \RuntimeException('Something <info>bad</info> happened', (int) $input->getArgument('code'));
        throw withLocation($e, '/app/src/FailCommand.php', 33);
    }
}

$argvs = [
    [], ['list'], ['list', '--raw'], ['list', 'app'], ['list', 'ap'], ['list', 'nope'], ['help'], ['help', 'app:dump'], ['help', 'list'],
    ['app:dump', '--help'], ['-h'], ['--help'], ['help', '--help'], ['--version'], ['-V'], ['-V', 'list'],
    ['app:dump', 'a'], ['app:dump', 'a', 'b', 'c', '-o', 'x', '-f', '--multi=1', '-m2', '--maybe', '--no-color'], ['dump', 'a', '--', '-o', '--flag'],
    ['app:dump', 'a', '-fo5'], ['app:dump', 'a', '--opt=7'], ['app:dump', 'a', '--opt='], ['app:dump', 'a', '--maybe='], ['app:dump', 'a', '--color'],
    ['app:dump'], ['app:dump', 'a', '--nope'], ['app:dump', 'a', '-x'], ['app:dump', 'a', '-fx'], ['app:dump', 'a', '--flag=1'], ['app:dump', 'a', '--opt'],
    ['app:dump', 'a', '--no-color=1'], ['app:d', 'a'], ['a:d', 'x'], ['ap', 'x'], ['xyz'], ['dmp', 'x'], ['app:dmup'], ['app:fail'], ['app:fail', '7'], ['app:fail', '300'],
    ['app:dump', 'a', '-v'], ['app:dump', 'a', '-vv'], ['app:dump', 'a', '-vvv'], ['app:dump', 'a', '--verbose=2'], ['app:dump', 'a', '-q'], ['app:dump', 'a', '--quiet'],
    ['app:dump', 'a', '--ansi'], ['app:dump', 'a', '--no-ansi'], ['app:dump', 'a', '-n'], ['--ansi', 'list'], ['-q', 'app:fail'], ['-v', '-q', 'app:dump', 'a'],
    ['app:dump', 'a', 'b', '--', '--verbose'], ['--no-interaction', 'app:dump', 'a'], ['-o', 'x', 'app:dump', 'y'], ['completion', 'nosuchshell'], ['help', 'nosuch'],
];
foreach ([false, true] as $decorated) {
    foreach ($argvs as $argv) {
        if ($decorated && !in_array($argv, [[], ['list'], ['help', 'app:dump'], ['app:fail'], ['xyz'], ['app:dump', 'a', '--no-ansi']], true)) {
            continue;
        }
        putenv('COLUMNS=80');
        putenv('SHELL_VERBOSITY');
        unset($_SERVER['SHELL_VERBOSITY'], $_ENV['SHELL_VERBOSITY']);
        $app = new Application('Oracle App', '1.2.3');
        $app->setAutoExit(false);
        $app->add(new DumpCommand());
        $app->add(new FailCommand());
        $input = new ArgvInput(array_merge(['bin/console'], $argv));
        $input->setInteractive(false);
        $output = new ConsoleOutput(OutputInterface::VERBOSITY_NORMAL, $decorated);
        $stdout = fopen('php://memory', 'w+');
        $stderr = fopen('php://memory', 'w+');
        $errorOutput = new StreamOutput($stderr);
        $errorOutput->setFormatter($output->getFormatter());
        $errorOutput->setVerbosity($output->getVerbosity());
        $errorOutput->setDecorated($output->isDecorated());
        $output->setErrorOutput($errorOutput);
        $r = new \ReflectionObject($output);
        $p = $r->getParentClass()->getProperty('stream');
        $p->setAccessible(true);
        $p->setValue($output, $stdout);
        $code = $app->run($input, $output);
        rewind($stdout);
        rewind($stderr);
        $out['run'][] = ['argv' => $argv, 'decorated' => $decorated, 'code' => $code, 'stdout' => stream_get_contents($stdout), 'stderr' => str_replace(dirname(__DIR__, 3).'/', '', stream_get_contents($stderr)), 'shellVerbosity' => getenv('SHELL_VERBOSITY')];
    }
}
putenv('SHELL_VERBOSITY');

// Strings that are not valid UTF-8 are written as {"b64": "..."}.
function encodeBinary($v)
{
    if (is_string($v) && !preg_match('//u', $v)) {
        return ['b64' => base64_encode($v)];
    }
    if (is_array($v)) {
        return array_map('encodeBinary', $v);
    }

    return $v;
}

file_put_contents(dirname(__DIR__, 3).'/internal/console/testdata/oracle/application.json', json_encode(encodeBinary($out), JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR)."\n");

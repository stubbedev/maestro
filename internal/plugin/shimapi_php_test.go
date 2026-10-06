package plugin

// The shim's Composer classes against maestro's ports: PHP code reads and
// changes maestro's objects through the API (test.eval), and what it sees
// is compared with what Go has.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/ui"
	"github.com/stubbedev/maestro/internal/util"
)

// evalPHP runs code (a function body getting $vars) in the shim.
func evalPHP(t *testing.T, rt *Runtime, code string, vars *php.Array) any {
	t.Helper()

	if vars == nil {
		vars = php.NewArray()
	}
	v, err := rt.Call("test.eval", php.ArrayOf("code", code, "vars", vars))
	if err != nil {
		t.Fatalf("%v\n%s", err, code)
	}

	return v
}

func loadPackage(t *testing.T, config string) pkg.PackageInterface {
	t.Helper()

	data, err := php.JSONDecode(config, true)
	if err != nil {
		t.Fatal(err)
	}
	p, err := loader.NewArrayLoader(pkg.NewVersionParser(), true).Load(data.(*php.Array), pkg.ClassCompletePackage)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestShimAPI_Packages(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	p := loadPackage(t, `{
		"name": "Acme/Lib", "version": "1.2.3", "type": "library", "time": "2024-01-02T03:04:05+00:00",
		"require": {"php": ">=8.1", "psr/log": "^1.0 || ^2.0", "acme/core": "self.version"},
		"replace": {"acme/old": "self.version"},
		"extra": {"branch-alias": {"dev-main": "1.x-dev"}, "list": [1, 2]},
		"autoload": {"psr-4": {"Acme\\": "src/"}},
		"bin": ["bin/acme"],
		"dist": {"type": "zip", "url": "https://example.org/%package%/%version%.zip", "reference": "0123456789012345678901234567890123456789"},
		"source": {"type": "git", "url": "https://example.org/acme.git", "reference": "0123456789012345678901234567890123456789"},
		"description": "A lib", "license": ["MIT"], "abandoned": "acme/new"
	}`)
	alias := pkg.NewCompleteAliasPackage(p.(pkg.CompletePackageInterface), "1.3.0.0", "1.3.0")

	got := evalPHP(t, rt, `
		$p = $vars['p'];
		$a = $vars['a'];
		$links = [];
		foreach ($p->getRequires() as $k => $l) {
			$links[] = $k.'='.$l->getPrettyConstraint().'|'.$l->getConstraint().'|'.$l->getDescription();
		}
		$aliasLinks = [];
		foreach ($a->getRequires() as $k => $l) {
			$aliasLinks[] = $k.'='.$l->getPrettyConstraint();
		}
		return [
			'class' => get_class($p),
			'names' => $p->getNames(),
			'name' => $p->getName().'|'.$p->getPrettyName().'|'.$p->getVersion().'|'.$p->getPrettyVersion().'|'.$p->getStability().'|'.var_export($p->isDev(), true),
			'unique' => $p->getUniqueName().'|'.$p.'|'.$p->getPrettyString(),
			'full' => $p->getFullPrettyVersion().'|'.$p->getFullPrettyVersion(false, \Composer\Package\PackageInterface::DISPLAY_SOURCE_REF),
			'date' => $p->getReleaseDate()->format(DATE_ATOM),
			'distUrls' => $p->getDistUrls(),
			'links' => $links,
			'extra' => $p->getExtra(),
			'autoload' => $p->getAutoload(),
			'binaries' => $p->getBinaries(),
			'abandoned' => var_export($p->isAbandoned(), true).'|'.$p->getReplacementPackage(),
			'license' => $p->getLicense(),
			'alias' => get_class($a).'|'.$a.'|'.$a->getPrettyVersion().'|'.$a->getType().'|'.var_export($a->getAliasOf() === $p, true),
			'aliasLinks' => $aliasLinks,
			'aliasReplaces' => array_keys($a->getReplaces()),
			'equals' => var_export($a->equals($p), true),
			'repository' => var_export($p->getRepository(), true),
		];
	`, php.ArrayOf("p", rt.packageObject(p), "a", rt.packageObject(alias)))

	want := map[string]any{
		"class":      `Composer\Package\CompletePackage`,
		"name":       "acme/lib|Acme/Lib|1.2.3.0|1.2.3|stable|false",
		"unique":     "acme/lib-1.2.3.0|acme/lib-1.2.3.0|Acme/Lib 1.2.3",
		"full":       p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) + "|" + p.FullPrettyVersion(false, pkg.DisplaySourceRef),
		"date":       "2024-01-02T03:04:05+00:00",
		"abandoned":  "true|acme/new",
		"alias":      `Composer\Package\CompleteAliasPackage|acme/lib-1.3.0.0 (alias of 1.2.3.0)|1.3.0|library|true`,
		"equals":     "true",
		"repository": "NULL",
	}
	a := got.(*php.Array)
	for k, w := range want {
		if v, _ := a.Get(k); v != w {
			t.Errorf("%s = %v, want %v", k, v, w)
		}
	}
	listOf := func(k string) []string {
		v, _ := a.GetArray(k)
		var out []string
		for _, x := range v.Values() {
			out = append(out, php.ToString(x))
		}

		return out
	}
	if got := strings.Join(listOf("distUrls"), ","); got != strings.Join(p.DistURLs(), ",") {
		t.Errorf("distUrls = %s, want %v", got, p.DistURLs())
	}
	var wantLinks []string
	for k, l := range p.Requires().All() {
		pc, _ := l.PrettyConstraint()
		wantLinks = append(wantLinks, k+"="+pc+"|"+l.Constraint().String()+"|"+l.Description())
	}
	if got := strings.Join(listOf("links"), ";"); got != strings.Join(wantLinks, ";") {
		t.Errorf("links\n%s\nwant\n%s", got, strings.Join(wantLinks, ";"))
	}
	var wantAliasLinks []string
	for k, l := range alias.Requires().All() {
		pc, _ := l.PrettyConstraint()
		wantAliasLinks = append(wantAliasLinks, k+"="+pc)
	}
	if got := strings.Join(listOf("aliasLinks"), ";"); got != strings.Join(wantAliasLinks, ";") {
		t.Errorf("alias links %s, want %s", got, strings.Join(wantAliasLinks, ";"))
	}
	if got := strings.Join(listOf("names"), ","); got != strings.Join(p.Names(true), ",") {
		t.Errorf("names %s, want %v", got, p.Names(true))
	}
	if got := strings.Join(listOf("aliasReplaces"), ","); got != strings.Join(linkKeys(alias.Replaces()), ",") {
		t.Errorf("alias replaces %s", got)
	}
	extra, _ := a.GetArray("extra")
	if enc, _ := php.JSONEncode(extra, 0); enc != `{"branch-alias":{"dev-main":"1.x-dev"},"list":[1,2]}` {
		t.Errorf("extra %s", enc)
	}

	// Setters change maestro's package; an alias forwards to its package.
	evalPHP(t, rt, `
		$vars['a']->setDistUrl('https://example.org/new.zip');
		$vars['p']->setExtra(['changed' => true]);
		$vars['p']->setReleaseDate(new \DateTime('2025-05-06T07:08:09+02:00'));
		$vars['p']->setRequires(['x/y' => new \Composer\Package\Link('acme/lib', 'x/y', new \Composer\Semver\Constraint\MatchAllConstraint(), \Composer\Package\Link::TYPE_REQUIRE, '*')]);
		return null;
	`, php.ArrayOf("p", rt.packageObject(p), "a", rt.packageObject(alias)))
	if got := p.DistURL(); got.S != "https://example.org/new.zip" {
		t.Errorf("dist url %v", got)
	}
	if v, _ := p.Extra().Get("changed"); v != true {
		t.Errorf("extra %v", p.Extra())
	}
	if date, ok := p.ReleaseDate(); !ok || date.UTC().Format("2006-01-02T15:04:05") != "2025-05-06T05:08:09" {
		t.Errorf("release date %v", date)
	}
	if l, ok := p.Requires().Get("x/y"); !ok || l.Constraint().String() != "*" || l.Description() != "requires" {
		t.Errorf("requires %v", linkKeys(p.Requires()))
	}
	// And PHP sees the change at once.
	if got := evalPHP(t, rt, `return $vars['p']->getDistUrl().'|'.implode(',', array_keys($vars['p']->getRequires()));`, php.ArrayOf("p", rt.packageObject(p))); got != "https://example.org/new.zip|x/y" {
		t.Errorf("PHP sees %v", got)
	}
}

func linkKeys(l pkg.Links) []string {
	var keys []string
	for k := range l.All() {
		keys = append(keys, k)
	}

	return keys
}

func TestShimAPI_Utilities(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)
	dir := t.TempDir()

	got := evalPHP(t, rt, `
		$fs = new \Composer\Util\Filesystem();
		$dir = $vars['dir'];
		$json = new \Composer\Json\JsonFile($dir.'/x.json');
		$json->write(['b' => ['x' => 1], 'a' => 'é/']);
		file_put_contents($dir.'/tab.json', "{\n\t\"a\": 1\n}\n");
		$tab = new \Composer\Json\JsonFile($dir.'/tab.json');
		$data = $tab->read();
		$data['b'] = 2;
		$tab->write($data);
		$process = new \Composer\Util\ProcessExecutor();
		$code = $process->execute(['sh', '-c', 'echo out; echo err >&2; exit 3'], $output);
		try {
			\Composer\Json\JsonFile::parseJson('{"a": 1,}', 'broken.json');
			$parse = 'no error';
		} catch (\Exception $e) {
			$parse = get_class($e).': '.$e->getMessage();
		}
		return [
			'normalize' => $fs->normalizePath('/a/./b/../c//d/'),
			'shortest' => $fs->findShortestPath('/a/b/c', '/a/d/e'),
			'code' => $fs->findShortestPathCode('/a/b/c.php', '/a/d/e', true),
			'absolute' => var_export($fs->isAbsolutePath('x/y'), true),
			'written' => file_get_contents($dir.'/x.json'),
			'tab' => file_get_contents($dir.'/tab.json'),
			'encode' => \Composer\Json\JsonFile::encode(['a' => 'é/'], JSON_UNESCAPED_SLASHES),
			'process' => $code.'|'.$output.'|'.$process->getErrorOutput(),
			'escape' => \Composer\Util\ProcessExecutor::escape("it's"),
			'lines' => $process->splitLines("a\r\nb\n"),
			'parse' => $parse,
			'upgrade' => var_export(\Composer\Package\Version\VersionParser::isUpgrade('1.0.0.0', '1.1.0.0'), true).var_export(\Composer\Package\Version\VersionParser::isUpgrade('2.0.0.0', '1.1.0.0'), true),
			'pairs' => (new \Composer\Package\Version\VersionParser())->parseNameVersionPairs(['a/b', '1.0', 'c/d:^2', 'ext-json']),
			'platform' => var_export(\Composer\Repository\PlatformRepository::isPlatformPackage('ext-json'), true),
			'timeout' => \Composer\Util\ProcessExecutor::getTimeout(),
		];
	`, php.ArrayOf("dir", dir))
	a := got.(*php.Array)

	shortest, _ := util.FindShortestPath("/a/b/c", "/a/d/e", false, false)
	code, _ := util.FindShortestPathCode("/a/b/c.php", "/a/d/e", true, false, false)
	want := map[string]any{
		"normalize": util.NormalizePath("/a/./b/../c//d/"),
		"shortest":  shortest,
		"code":      code,
		"absolute":  "false",
		"written":   "{\n    \"b\": {\n        \"x\": 1\n    },\n    \"a\": \"é/\"\n}\n",
		"tab":       "{\n\t\"a\": 1,\n\t\"b\": 2\n}\n",
		"encode":    "{\"a\":\"" + "\\" + "u00e9/\"}", // json_encode's escape, built so that editors keep it
		"process":   "3|out\n|err\n",
		"escape":    util.Escape("it's"),
		"upgrade":   "truefalse",
		"platform":  "true",
		"timeout":   int64(util.GetProcessTimeout()),
	}
	for k, w := range want {
		if v, _ := a.Get(k); v != w {
			t.Errorf("%s = %#v, want %#v", k, v, w)
		}
	}
	if v, _ := a.GetString("parse"); !strings.HasPrefix(v, `Seld\JsonLint\ParsingException: "broken.json" does not contain valid JSON`) {
		t.Errorf("parse error %q", v)
	}
	pairs, _ := a.GetArray("pairs")
	if enc, _ := php.JSONEncode(pairs, php.JSONUnescapedSlashes); enc != `[{"name":"a/b","version":"1.0"},{"name":"c/d","version":"^2"},{"name":"ext-json"}]` {
		t.Errorf("pairs %s", enc)
	}
	if lines, _ := a.GetArray("lines"); lines.Len() != 2 {
		t.Errorf("lines %v", lines.Values())
	}
	if _, err := os.Stat(filepath.Join(dir, "x.json")); err != nil {
		t.Error(err)
	}
}

func TestShimAPI_IO(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	out, err := io.NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	out.SetUserInputs([]string{"no", "maybe", "42"})

	got := evalPHP(t, rt, `
		$io = $vars['io'];
		$io->write(['one', 'two']);
		$io->writeError('<info>err</info>', false);
		$io->writeError('');
		$io->debug('dbg');
		$io->warning('careful', ['a' => 1]);
		$confirmed = $io->askConfirmation('Sure? ', true);
		$answer = $io->askAndValidate('Number? ', function ($v) {
			if (!is_numeric($v)) {
				throw new \RuntimeException('not a number: '.$v);
			}

			return (int) $v;
		}, 3);
		$io->setAuthentication('example.org', 'user', 'pass');
		return [
			'confirmed' => $confirmed,
			'answer' => $answer,
			'auth' => $io->getAuthentication('example.org'),
			'has' => $io->hasAuthentication('example.org'),
			'interactive' => $io->isInteractive(),
			'class' => get_class($io),
		];
	`, php.ArrayOf("io", rt.ioObject(out)))
	a := got.(*php.Array)
	if v, _ := a.Get("confirmed"); v != false {
		t.Errorf("confirmed %v", v)
	}
	if v, _ := a.Get("answer"); v != int64(42) {
		t.Errorf("answer %v", v)
	}
	if v, _ := a.Get("interactive"); v != true {
		t.Errorf("interactive %v", v)
	}
	if v, _ := a.Get("class"); v != `Composer\IO\BufferIO` {
		t.Errorf("class %v", v)
	}
	if auth := out.Authentication("example.org"); auth.Username == nil || *auth.Username != "user" {
		t.Errorf("auth %+v", auth)
	}
	wantOut := "one\ntwo\nerr\n<warning>careful {\"a\":1}</warning>\nSure? Number? not a number: maybe\nNumber? "
	if got := strings.ReplaceAll(out.Output(), "\r", ""); got != wantOut {
		t.Errorf("output %q, want %q", got, wantOut)
	}
}

// A list-shaped setRequires throws the \ErrorException Composer's
// ErrorHandler makes of Package::convertLinksToMap's notice; a
// RootAliasPackage has set its own links by then (Composer 2.10.3).
func TestShimAPI_ListShapedLinks(t *testing.T) {
	requirePHP(t)
	withComposerRoot(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	root := pkg.NewRootPackage("acme/app", "1.0.0.0", "1.0.0")
	alias := pkg.NewRootAliasPackage(root, "2.0.0.0", "2.0.0")

	got := evalPHP(t, rt, `
		$out = [];
		foreach (['p', 'a'] as $k) {
			try {
				$vars[$k]->setRequires([new \Composer\Package\Link('acme/app', 'x/y', new \Composer\Semver\Constraint\MatchAllConstraint(), \Composer\Package\Link::TYPE_REQUIRE, '*')]);
				$out[] = 'no exception';
			} catch (\ErrorException $e) {
				$out[] = get_class($e).': '.$e->getMessage();
				if ($k === 'p') {
					$out[] = substr($e->getFile(), strpos($e->getFile(), '/src/')).':'.$e->getLine();
				}
			}
		}

		return implode("\n", $out);
	`, php.ArrayOf("p", rt.packageObject(root), "a", rt.packageObject(alias)))

	// thrown where Composer's Package.php raises the notice (maestro's
	// RootAliasPackage has no PHP location)
	msg := "ErrorException: Package::setRequires must be called with a map of lowercased package name => Link object, got a indexed array, this is deprecated and you should fix your usage."
	if got != msg+"\n/src/Composer/Package/Package.php:716\n"+msg {
		t.Errorf("got %v", got)
	}
	if root.Requires().Len() != 0 {
		t.Errorf("root requires %v", linkKeys(root.Requires()))
	}
	if keys := linkKeys(alias.Requires()); len(keys) != 1 || keys[0] != "0" {
		t.Errorf("alias requires %v", keys)
	}
}

// Composer has one ErrorHandler: a deprecation notice plugin code raises
// after one maestro's code raised is hidden below -v (the static
// $hasShownDeprecationNotice is shared). How notices look is maestro's
// (#13): internal/ui renders them as maestro's own, without Composer's
// source paths or PHP stacks, on an IO created in PHP as on maestro's
// (styled when its error output is decorated).
func TestShimAPI_ErrorHandler(t *testing.T) {
	requirePHP(t)
	withComposerRoot(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)
	t.Cleanup(util.ResetErrorHandler)

	trigger := `
		$io = isset($vars['io']) ? $vars['io'] : new \Composer\IO\BufferIO('', $vars['verbosity']);
		\Composer\Util\ErrorHandler::register($io);
		trigger_error('an old API', E_USER_DEPRECATED);
		trigger_error('another old API', E_USER_DEPRECATED);
		\Composer\Util\ErrorHandler::register(null);

		return isset($vars['io']) ? '' : $io->getOutput();
	`

	// maestro's code showed a notice: plugin code's are hidden
	util.ResetErrorHandler()
	util.SetDeprecationNoticeShown(1)
	got := evalPHP(t, rt, trigger, php.ArrayOf("verbosity", int64(32)))
	if php.NormalizeEOL(php.ToString(got)) != "Note: More deprecation notices were hidden, run again with `-v` to show them.\n" {
		t.Errorf("after maestro's notice: %q", got)
	}
	if n := util.DeprecationNoticeShown(); n != 2 {
		t.Errorf("hasShownDeprecationNotice = %d, want 2", n)
	}

	// at -v, every notice, without a location or stack
	util.ResetErrorHandler()
	got = evalPHP(t, rt, trigger, php.ArrayOf("verbosity", int64(64)))
	if n := util.DeprecationNoticeShown(); n != 1 {
		t.Errorf("hasShownDeprecationNotice = %d, want 1", n)
	}
	if out := php.NormalizeEOL(php.ToString(got)); out != "Deprecated: an old API\nDeprecated: another old API\n" {
		t.Errorf("at -v: %q", out)
	}

	// maestro's IO, decorated: styled by internal/ui
	util.ResetErrorHandler()
	out, err := io.NewBufferIO("", 0, console.NewOutputFormatter(true))
	if err != nil {
		t.Fatal(err)
	}
	evalPHP(t, rt, trigger, php.ArrayOf("io", rt.ioObject(out)))
	var lines []string
	for _, d := range []ui.Diagnostic{
		{Kind: ui.Deprecation, Message: "an old API"},
		{Kind: ui.Note, Message: "More deprecation notices were hidden, run again with `-v` to show them."},
	} {
		lines = append(lines, d.Lines(ui.Options{Decorated: true})...)
	}
	want := strings.Join(lines, "\n") + "\n"
	if got := php.NormalizeEOL(out.Output()); got != want || !strings.Contains(got, "\x1b[") {
		t.Errorf("maestro's IO: %q, want %q", got, want)
	}
}

// An IO created in PHP gets the calls maestro's parallel work makes, in
// their order, as Composer's one thread makes them: a process's output
// (ProcessExecutor's outputHandler), and calls from other goroutines while
// the goroutine holding the PHP baton waits for them.
func TestShimAPI_IOFromParallelWork(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	rt.Handle("test.parallelIO", func(v any) (any, error) {
		a := argsOf("test.parallelIO", v)
		out, _, err := rt.ioParam(a, 0)
		if err != nil {
			return nil, err
		}
		work := make(chan struct{})
		go func() {
			defer close(work)
			out.WriteError("from a goroutine", true, io.Normal)
			if out.IsVerbose() {
				out.WriteError("verbose", true, io.Normal)
			}
			out.WriteError("its last line", true, io.Normal)
		}()
		util.WaitServing(work)
		out.WriteError("after the work", true, io.Normal)

		return nil, nil
	})
	start(t, rt)

	got := evalPHP(t, rt, `
		$io = new \Composer\IO\BufferIO('', \Symfony\Component\Console\Output\OutputInterface::VERBOSITY_VERBOSE);
		$out = [];
		// php rather than sh's printf and sleep: cmd.exe runs it on Windows
		(new \Composer\Util\ProcessExecutor($io))->execute(escapeshellarg(PHP_BINARY).' -r '.escapeshellarg("echo 'one', chr(10); usleep(100000); fwrite(STDERR, 'two'.chr(10)); usleep(100000); echo 'three', chr(10);"));
		$out[] = $io->getOutput();

		$io = new \Composer\IO\BufferIO('', \Symfony\Component\Console\Output\OutputInterface::VERBOSITY_VERBOSE);
		\Maestro\Shim\Rpc::call('test.parallelIO', [$io]);
		$out[] = $io->getOutput();

		return implode('|', $out);
	`, nil)
	// the BufferIOs end lines with PHP's PHP_EOL
	if want := "one\ntwo\nthree\n|from a goroutine\nverbose\nits last line\nafter the work\n"; php.NormalizeEOL(php.ToString(got)) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

<?php

/*
 * Hand-written part of the PCRE corpus, read by preg_collect.php:
 *  - patterns: realistic instances of patterns Composer builds at runtime
 *    (preg_quote()d package names, config values, ...) that neither static
 *    extraction nor the captured test runs cover, with their subjects;
 *  - generic: edge-case subjects every pattern is run against;
 *  - families: extra subjects for every pattern whose source matches a regex.
 */

$q = static fn (string $s, ?string $d = null): string => preg_quote($s, $d);
$pkgRe = static fn (string $p, string $wrap = '{^%s$}i'): string => sprintf($wrap, str_replace('\\*', '.*', preg_quote($p)));
$pkgsRe = static fn (array $ps): string => sprintf('{^(?:%s)$}iD', implode('|', array_map(static fn ($p) => str_replace('\\*', '.*', preg_quote($p)), $ps)));

$packageNames = ['vendor/package', 'Vendor/Package', 'symfony/console', 'symfony/polyfill-mbstring', 'monolog/monolog', 'php', 'ext-json', 'ext-mbstring', 'lib-icu', 'php-64bit', 'composer-plugin-api', 'vendor/package-extra', 'other/package', "vendor/package\n", 'vendor/pack.age', 'ext-pdo_mysql', ''];
$versions = ['1.0.0', 'v1.0.0', '1.0', '1', '1.0.0-beta1', '1.0.0-RC2', '1.0.0-alpha.3', '1.0.0-dev', 'dev-master', 'dev-main', 'dev-feature/foo', '1.0.x-dev', '2.x-dev', '1.2.3.4', '1.2.3.4.5', '20100102', '2010-01-02', '2010.01.02.10', 'v1.0.0-patch1', '1.0.0+build.1', '1.0.0-p1', '1.0.0-stable', '1.0.0@dev', '1.0.0 as 2.0.0', '0.1.0#abcdef', 'dev-master#a1b2c3', '~1.2', '^1.2.3', '^0.0.1', '>=1.0 <2.0', '>=1.0,<2.0', '1.0 - 2.0', '1.* || 2.*', '^1.0 | ^2.0', '*', 'x', '1.x', '1.0.*', '!= 1.0', '<>1.0', '===1.0', 'v', '', ' 1.0 ', '1.0.0-', "1.0.0\n", 'dev-ünïcode', '1.0.0-beta.1+exp.sha.5114f85', '99999999999999999999.0'];

return [
    'patterns' => [
        '{[^a-z0-9._]}i' => ['sources' => ['src/Composer/Cache.php:124'], 'subjects' => ['https---repo.packagist.org-packages.json', 'provider-vendor~package.json', 'Ünïcode/Path\\x', 'a b?c*d', '']],
        '{[^a-z0-9.]}i' => ['sources' => ['src/Composer/Cache.php:124'], 'subjects' => ['github.com', 'gitlab.example.org:8080', 'repo_name-x', '']],
        '{[^a-z0-9_./]}i' => ['sources' => ['src/Composer/Cache.php:124'], 'subjects' => ['vendor/package/1.0.0-abcdef.zip', 'a$b~c', 'ü/x']],
        '{[^a-z0-9.$~_]}i' => ['sources' => ['src/Composer/Cache.php:124'], 'subjects' => ['provider-vendor$package~dev.json', 'a/b', 'x-y']],
        $pkgRe('vendor/*') => ['sources' => ['src/Composer/Command/CompletionTrait.php:240', 'src/Composer/Command/RemoveCommand.php:192'], 'subjects' => $packageNames],
        $pkgRe('ext-*') => ['sources' => ['src/Composer/Command/CompletionTrait.php:240'], 'subjects' => $packageNames],
        $pkgRe('symfony/console') => ['sources' => ['src/Composer/Command/RemoveCommand.php:192'], 'subjects' => $packageNames],
        $pkgsRe(['vendor/package', 'symfony/*', 'php']) => ['sources' => ['src/Composer/Command/UpdateCommand.php:370', 'src/Composer/Repository/ComposerRepository.php:445', 'src/Composer/Filter/PlatformRequirementFilter/IgnoreListPlatformRequirementFilter.php:85'], 'subjects' => $packageNames],
        $pkgsRe(['ext-*', 'lib-*']) => ['sources' => ['src/Composer/Filter/PlatformRequirementFilter/IgnoreListPlatformRequirementFilter.php:81'], 'subjects' => $packageNames],
        $pkgsRe([]) => ['sources' => ['src/Composer/Filter/PlatformRequirementFilter/IgnoreListPlatformRequirementFilter.php:81'], 'subjects' => $packageNames],
        $pkgsRe(['*']) => ['sources' => ['src/Composer/DependencyResolver/PoolBuilder.php:664'], 'subjects' => $packageNames],
        '/\d\d:\d\d:\d\d\s+\[' . 'a1b2c3d4e5' . '\]/' => ['sources' => ['src/Composer/Downloader/FossilDownloader.php:101'], 'subjects' => ['=== 2023-01-02 ===', '12:34:56 [a1b2c3d4e5] Commit message (user: bob tags: trunk)', '12:34:56  [a1b2c3d4e5f6] other', '1:34:56 [a1b2c3d4e5]', '']],
        '{^[a-f0-9]+ refs/remotes/((?:[^/]+)/'.$q('feature/foo-1.x').')$}mi' => ['sources' => ['src/Composer/Downloader/GitDownloader.php:268'], 'subjects' => ["a1b2c3 refs/remotes/origin/feature/foo-1.x\nd4e5f6 refs/remotes/upstream/feature/foo-1.x\n", 'A1B2C3 REFS/REMOTES/origin/FEATURE/FOO-1.X', "a1b2c3 refs/remotes/origin/feature/foo-1x\n", "a1b2c3 refs/heads/feature/foo-1.x"]],
        '{\b'.$q('vendor/bin/phpunit').'$}' => ['sources' => ['src/Composer/EventDispatcher/EventDispatcher.php:370'], 'subjects' => ['vendor/bin/phpunit', 'bin/vendor/bin/phpunit', 'xvendor/bin/phpunit', 'vendor/bin/phpunit --filter x', '']],
        '{\b'.$q('phpstan').'$}' => ['sources' => ['src/Composer/EventDispatcher/EventDispatcher.php:370'], 'subjects' => ['bin/phpstan', 'bin/my-phpstan', 'bin/myphpstan', 'phpstan']],
        '{^'.$q('vendor/bin/phpunit').'}' => ['sources' => ['src/Composer/EventDispatcher/EventDispatcher.php:372'], 'subjects' => ['vendor/bin/phpunit --colors=always', ' vendor/bin/phpunit', 'vendor/bin/phpunitx']],
        '{/*'.str_replace('/', '/+', $q('Symfony/Component/Console')).'/?$}' => ['sources' => ['src/Composer/Installer/LibraryInstaller.php:264'], 'subjects' => ['vendor/symfony/console/Symfony/Component/Console', 'vendor/symfony/console//Symfony//Component/Console/', 'vendor/symfony/console/Symfony/Component/Console/Other', 'Symfony/Component/Console']],
        '#^{'.'    '.'#' => ['sources' => ['src/Composer/Json/JsonManipulator.php:564'], 'subjects' => ["{    \"a\": 1}", "{\n    \"a\": 1}", '{  "a": 1}', '{}']],
        '#^{'.'#' => ['sources' => ['src/Composer/Json/JsonManipulator.php:564'], 'subjects' => ['{"a": 1}', '{}', '[]']],
        '#<url>.*/(' . 'trunk' . '|(' . 'branches' . '|' . 'tags' . ')/(.*))</url>#' => ['sources' => ['src/Composer/Package/Version/VersionGuesser.php:412'], 'subjects' => ["<?xml version=\"1.0\"?>\n<info>\n<entry kind=\"dir\" path=\".\" revision=\"123\">\n<url>https://svn.example.org/repo/trunk</url>\n</entry>\n</info>\n", '<url>https://svn.example.org/repo/branches/1.0</url>', '<url>https://svn.example.org/repo/tags/v1.0.0/sub</url>', '<url>svn://host/repo</url>', '']],
        '#<url>.*/(' . $q('my/trunk', '#') . '|(' . $q('my/branches', '#') . '|' . $q('rel+tags', '#') . ')/(.*))</url>#' => ['sources' => ['src/Composer/Package/Version/VersionGuesser.php:412'], 'subjects' => ['<url>https://svn.example.org/repo/my/trunk</url>', '<url>https://svn.example.org/repo/rel+tags/1.0</url>', '<url>https://svn.example.org/repo/reltags/1.0</url>']],
        '{^vendor/.*$}i' => ['sources' => ['src/Composer/Repository/ComposerRepository.php:2060'], 'subjects' => $packageNames],
        '{^git@' . '(' . implode('|', array_map('preg_quote', ['github.com', 'github.example.org'])) . ')' . ':(.+?)\.git$}i' => ['sources' => ['src/Composer/Util/Git.php:192'], 'subjects' => ['git@github.com:composer/composer.git', 'git@GITHUB.COM:Composer/Composer.git', 'git@github.example.org:a/b.git', 'git@githubXcom:a/b.git', 'git@github.com:a/b', 'https://github.com/composer/composer.git']],
        '{^[\s*]*v?'.$q('1.0.x').'$}m' => ['sources' => ['src/Composer/Util/Git.php:435'], 'subjects' => ["  master\n* 1.0.x\n  2.0.x\n", "  v1.0.x\n", "  1.0x\n  1.0.x-dev\n", "* (HEAD detached at 1.0.x)\n"]],
        '{^[\s*]*'.$q('feature/a+b').'$}m' => ['sources' => ['src/Composer/Util/Git.php:436'], 'subjects' => ["  master\n* feature/a+b\n", "  feature/aab\n", "\tfeature/a+b\r\n"]],
        '{^(.+(?://|@)'.$q('repo.example.org').'(?::\d+)?)(?:[/\?].*)?$}' => ['sources' => ['src/Composer/Util/Http/CurlDownloader.php:564', 'src/Composer/Util/RemoteFilesystem.php:678'], 'subjects' => ['https://repo.example.org/packages.json', 'https://user:pass@repo.example.org:8443/p2/vendor/package.json?x=1', 'https://repo.example.org', 'https://repoXexample.org/x', 'https://other.org/repo.example.org/x']],
        '#<url>(.*)</url>#' => ['sources' => ['src/Composer/Downloader/SvnDownloader.php:204'], 'subjects' => ["<info>\n<entry>\n<url>https://svn.example.org/repo/trunk</url>\n<url>second</url>\n</entry>\n</info>", '<url></url>', '<URL>x</URL>']],
        '{^(?:/tmp/excluded|/home/user/project/tests)($|/)}' => ['sources' => ['vendor/composer/class-map-generator/src/ClassMapGenerator.php:182'], 'subjects' => ['/tmp/excluded', '/tmp/excluded/Foo.php', '/tmp/excludedX/Foo.php', '/home/user/project/tests/FooTest.php', '/home/user/project/src/Foo.php']],
    ],

    'generic' => [
        '',
        "\n",
        '1.0.0',
        'vendor/package',
        'ünïcödé ✓ 😀',
        "\xff\xfe invalid \xc3",
        "a\r\nb\n",
    ],

    'families' => [
        [
            'sources' => '{^vendor/composer/semver/|^src/Composer/Package/Version/|^src/Composer/Package/Loader/|^src/Composer/Semver}',
            'subjects' => $versions,
        ],
        [
            'sources' => '{^src/Composer/(Util/(Git|Svn|Hg|Perforce|Url|GitHub|GitLab|Bitbucket|Forgejo|AuthHelper)|Repository/Vcs/|Downloader/)}',
            'subjects' => [
                'https://github.com/composer/composer.git',
                'https://github.com/composer/composer',
                'git@github.com:composer/composer.git',
                'git://github.com/composer/composer',
                'ssh://git@gitlab.com:2222/group/sub/project.git',
                'https://gitlab.com/group/sub/project.git',
                'https://user:p%40ss@bitbucket.org/user/repo.git',
                'https://api.github.com/repos/composer/composer/zipball/abc123',
                'svn+ssh://svn.example.org/repo/trunk',
                'C:\\Users\\me\\repo',
                "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0 refs/heads/main\nb1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0 refs/tags/v1.0.0\n",
                "* main\n  feature/foo\n  remotes/origin/HEAD -> origin/main\n",
                "Revision: 1234\nLast Changed Rev: 1230\nURL: https://svn.example.org/repo/trunk\n",
                'git version 2.43.0',
            ],
        ],
        [
            'sources' => '{^vendor/composer/class-map-generator/|^src/Composer/Autoload/}',
            'subjects' => [
                "<?php\nnamespace Foo\\Bar;\n\nclass Baz extends Qux implements \\Countable\n{\n    public function x() { return 'class Fake {}'; }\n}\n",
                "<?php\n/* class Commented */\n// interface Nope\n\$x = <<<EOT\nclass InHeredoc {}\nEOT;\ntrait T1 {}\nenum Suit: string { case Hearts = 'H'; }\n",
                "<?php\nnamespace A { class B {} }\nnamespace { interface C {} }\n\$o = new class {};\necho Foo::class;\n",
                "<?php\n\$s = <<<'NOW'\nfoo\\nbar\n  NOW;\nabstract class Ünïcode_Clâss {}\nfinal readonly class R {}\n",
                "<?php ?>\n<html>class NotPhp {}</html>\n<?php class AfterHtml {} ?>",
            ],
        ],
        [
            'sources' => '{^src/Composer/(Json/|Config/)|^vendor/seld/jsonlint/}',
            'subjects' => [
                "{\n    \"name\": \"vendor/package\",\n    \"require\": {\n        \"php\": \">=8.1\",\n        \"vendor/other\": \"^1.0\"\n    },\n    \"repositories\": [\n        {\"type\": \"vcs\", \"url\": \"https://github.com/x/y\"}\n    ]\n}\n",
                '{"a":1,"b":[true,false,null],"c":{"d":"e\\"f\\\\g\\/h\\u00e9"}}',
                "{\r\n\t\"config\": {}\r\n}",
                '[1, 2.5e10, -0.0, "x"]',
                '{"unterminated": "abc',
                "{\"a\": 1,}",
            ],
        ],
        [
            'sources' => '{^vendor/composer/spdx-licenses/}',
            'subjects' => ['MIT', '(MIT or GPL-2.0-or-later)', 'Apache-2.0 AND (MIT OR BSD-3-Clause)', 'GPL-2.0+', 'GPL-3.0-only WITH Classpath-exception-2.0', 'LicenseRef-Custom', 'proprietary', 'mit', '(MIT', 'MIT or', ''],
        ],
        [
            'sources' => '{^src/Composer/(Util/(Filesystem|Platform|ProcessExecutor|Silencer)|Installer/|Package/Archiver/)}',
            'subjects' => ['/home/user/project/vendor/bin/phpunit', 'C:\\Users\\me\\project\\vendor', 'c:/Users/me', '\\\\server\\share\\dir', './relative/../path/', 'vendor//double///slashes/', 'file with spaces.txt', '/', '.', '..', 'phar:///path/to/composer.phar/src', 'vfs://root/dir', '*.php', '!/keep/this', '/build/**/cache'],
        ],
    ],
];

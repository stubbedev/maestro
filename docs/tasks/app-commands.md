# Task: internal/command (application + commands) and cmd/maestro

Port .ref/composer/src/Composer/Console/Application.php (Composer's application on top of internal/console: global options --working-dir/-d, --profile, --no-plugins, --no-scripts, --no-cache; the logo/long version "Composer version 2.10.3 2026-..": decide how maestro presents its own version without breaking tools that parse `composer --version` — keep the first line identical to Composer's format and document; plugin commands via the plugin runtime seam; script commands (ScriptAliasCommand) from composer.json; the root-warning, xdebug warning (n/a — document), the "Do not run Composer as root/super user!" prompt, COMPOSER_ALLOW_SUPERUSER, the cwd/working-dir handling, error rendering hooks) and Command/BaseCommand.php, BaseConfigCommand.php, BaseDependencyCommand.php, CompletionTrait.php, PackageDiscoveryTrait.php, then EVERY command in .ref/composer/src/Composer/Command. They are split across agents: this spec is shared; your agent prompt says which group is yours. Groups:
- A: install, update, require, remove, reinstall, dump-autoload, run-script, exec, global, create-project, init, bump, script aliases (ScriptAliasCommand).
- B: show, outdated, depends (why), prohibits (why-not), licenses, fund, suggests, search, home (browse), status, check-platform-reqs, audit, validate, archive.
- C: config, repository, policy, diagnose, clear-cache, about, self-update (maestro's own: update the maestro binary from GitHub releases github.com/stubbedev/maestro, with Composer's command name, options and output style — the only deliberate behaviour change; document), and the Application itself + BaseCommand + traits + cmd/maestro main (also behaving as `composer` when invoked as such; exit codes; signal handling as Composer's).
Group C owns Application/BaseCommand/traits and cmd/maestro; groups A and B build on them (coordinate via HANDOFF.md; start with your commands' logic against the Composer/Factory API and wire into the Application as soon as it lands).

Every command: definition (arguments, options, shortcuts, defaults, suggestions/completion), help text verbatim, behaviour, output byte-identical, exit codes.

Tests: port .ref/composer/tests/Composer/Test/Command/* for your commands (every case; they use ApplicationTester-style helpers — port those helpers once, in group C, into a test helper package others use), plus Test/ApplicationTest.php and Test/Console/* (group C), and functional fixtures (tests/Composer/Test/Fixtures/functional) for your commands.

Scope: internal/command (one file per command), cmd/maestro (group C). Build on internal/composer.
Report: commands done, test counts, divergences, lint/test status.

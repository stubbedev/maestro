# Task: plugin runtime, phase 2 — core plugin API

docs/PLUGINS.md is the specification (phase 1 is committed in internal/plugin; read the phase-1 notes in HANDOFF.md). Implement section 8 "Phase 2: core plugin API" completely:
- The Go PluginManager port (policy: Composer's src/Composer/Plugin/PluginManager.php — load order, allow-plugins incl. the interactive prompt and its exact text, --no-plugins, plugin-api version checks, global plugins, the autoload map) implementing internal/composer's PluginManager hook and internal/installer's PluginManager interface; the PHP PluginManager mechanism (eval-rename for already-loaded classes).
- Composer, Config, IO and EventDispatcher proxies; Event, Script\Event and PackageEvent mirrors; the Package family, Link and constraint mirrors; the local repository mirror; InstallationManager::getInstallPath.
- Util\Filesystem, ProcessExecutor, Platform; JsonFile; InstalledVersions reload; AutoloadGenerator (buildPackageMap, parseAutoloads, createLoader).
- PHP-callable scripts, makeAutoloader, the dispatch bracket: make plugin.Runtime implement internal/eventdispatcher's ScriptRuntime; native Config::disableProcessTimeout stays native.
- In-place mutation of root and local packages (setAutoload, setExtra, setBinaries) flowing back into Go before the next step.
- Wire it into internal/composer's Factory hooks (plugin manager, script runtime, COMPOSER_BINARY launcher) — the xdebug-restart env vars note in HANDOFF.md applies.
- Fixtures per §9.3 for phase 2: stubbedev ×5 (the kontainer project's stubbedev/*-mcp packages), laravel scripts (Illuminate\Foundation\ComposerScripts), pest (event), phpstan/extension-installer, infection/extension-installer, dealerdirect/phpcodesniffer-composer-installer, captainhook/hook-installer, phpro/grumphp, symfony/runtime, composer/package-versions-deprecated, tbachert/spi, drupal events, plus the scenario fixtures of §9.2. Each fixture compares against real Composer 2.10.3 (official phar, checksum-pinned, in the test cache) under MAESTRO_E2E=1.

Scope: internal/plugin (+ php shim), the wiring points in internal/composer and internal/eventdispatcher (small, noted in HANDOFF.md). Command agents are concurrently writing internal/command and cmd/maestro (the Application's plugin-command seam is phase 4 — don't wire commands now); an async-ordering agent is editing internal/util promise/loop, internal/util/http, internal/downloader and internal/installer: don't edit those.
Report: what's implemented, fixtures passing, PLUGINS.md updates, test/lint status.

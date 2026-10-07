# maestro

Composer, natively. maestro is a line-by-line port of
[Composer](https://getcomposer.org) 2.10.3 to Go, and a drop-in replacement
for the `composer` command. What tools and scripts depend on is identical
to Composer's, byte for byte: commands and options, exit codes,
machine-readable output, questions, scripts and events, and the files
written (`composer.json`, `composer.lock`, `vendor/composer/*`,
`vendor/bin` proxies, the installed packages). Errors, warnings and
progress are presented in maestro's own way. It never runs or ships
`composer.phar`.

What you get on top:

- **A shared package store, pnpm style.** Every release is extracted once
  per machine into a content-addressed store and imported into `vendor/` by
  reflink, hardlink or copy. A second project or git worktree installs
  without downloading or unzipping anything.
- **Native speed.** Resolution, extraction, autoload dumping and metadata
  handling run in Go, in parallel where the result stays identical. See
  [docs/BENCHMARKS.md](docs/BENCHMARKS.md).
- **No external extractors.** zip, tar, gz, bz2 and xz are handled natively,
  reproducing what Composer gets from `unzip` and `tar`.

`php` is still used where Composer itself runs PHP code for your project:
scripts, plugins, platform detection and `composer exec`.

## Install

Homebrew:

```sh
brew install stubbedev/tap/maestro
```

maestro uses the `php` first on your `PATH`, so the formula does not pull
in Homebrew's php; add `--with-php` (or `brew install php`) if you have
none. On Apple Silicon, install with the arm64 Homebrew
(`/opt/homebrew/bin/brew`): an Intel Homebrew in `/usr/local` installs the
x86_64 binary, which runs under Rosetta.

Install script (Linux, macOS, FreeBSD; picks the binary for the hardware,
checks it against the release's `checksums.txt`, installs to
`~/.local/bin` or `$MAESTRO_INSTALL_DIR`, and takes a release tag in
`$MAESTRO_VERSION`):

```sh
curl -fsSL https://raw.githubusercontent.com/stubbedev/maestro/main/install.sh | sh
```

Nix (the package also provides `composer`):

```nix
# flake.nix
inputs.maestro.url = "github:stubbedev/maestro";
# then add inputs.maestro.packages.${system}.default to your packages
```

Binaries for Linux, macOS, Windows and FreeBSD are attached to every
[release](https://github.com/stubbedev/maestro/releases), with a
`checksums.txt`. `maestro self-update` keeps an installed binary current.

To use it as `composer`, put a `composer` symlink to `maestro` on your
`PATH` (the Nix package already does).

## Compatibility

maestro follows Composer 2.10.3. Composer's own test suite is ported and
passes, including all 209 installer fixtures, and an end-to-end suite runs
real Composer and maestro side by side on the same projects (Laravel,
Symfony and a large private application among them) comparing output, lock
files and `vendor/` trees.

Plugins run unchanged: maestro embeds a PHP layer that provides Composer's
public plugin API (`Composer\Composer`, events, installers, commands,
repositories, ...) backed by maestro's own state. Tested with, among
others, composer/installers, pestphp/pest-plugin, phpstan and infection
extension installers, dealerdirect/phpcodesniffer-composer-installer,
wikimedia/composer-merge-plugin, cweagans/composer-patches,
ergebnis/composer-normalize, drupal's scaffold and Laravel's package
discovery scripts. See [docs/PLUGINS.md](docs/PLUGINS.md).

[docs/PORTING.md](docs/PORTING.md#the-contract) says exactly what is
identical and what is maestro's own, and lists the
[deliberate deviations](docs/PORTING.md#deliberate-deviations); the ones
you may notice:

- packages are imported from the shared store, so file modification times
  in `vendor/` are not the archive's, and hard-linked files are shared
  between projects (plugin packages are never hard-linked);
- `self-update` updates maestro;
- `--version` adds a `Maestro version` line on stderr, and the `list`
  banner names maestro.

Set `MAESTRO_PACKAGE_IMPORT_METHOD` to `clone`, `hardlink` or `copy` to
force an import method (default: reflink, else hardlink, else copy).

maestro keeps its store and its other caches in `MAESTRO_CACHE_DIR`
(default `$XDG_CACHE_HOME/maestro`, else the platform cache directory);
`maestro clear-cache` clears them along with Composer's caches.

## Development

The dev environment is [devenv](https://devenv.sh): `devenv shell` provides
Go, golangci-lint, php, unzip and the rest. `ref-sync` checks out the exact
Composer sources being ported into `.ref/`. Tests, vet and lint run in a
Docker dev container (`compose.yaml`), which the justfile drives:

```sh
just check       # every gate CI runs: vet, lint, deadcode, tidy-check, test, build
just test        # the test suite, php-driven tests included (args go to go test)
just test-race   # race detector, plus the php-driven tests
just e2e         # compare against the real Composer phar (network, slow)
just shell       # a shell in the dev container
```

[docs/PORTING.md](docs/PORTING.md) is the porting contract: what must match
Composer, the layout, the rules every port follows, how tests are ported
from Composer's suite and generated from Composer's own PHP, and the test
switches. [docs/PLUGINS.md](docs/PLUGINS.md) specifies the plugin runtime.

## License

MIT. maestro is a port of Composer and embeds some of its files; see
[LICENSE](LICENSE).

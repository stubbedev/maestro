# maestro

Composer, natively. maestro is a line-by-line port of
[Composer](https://getcomposer.org) 2.10.3 to Go, and a drop-in replacement
for the `composer` command: same commands, options, output, exit codes and
files written (`composer.lock`, `vendor/composer/*`, `vendor/bin` proxies),
byte for byte. It never runs or ships `composer.phar`.

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

The deliberate differences from Composer are listed in
[docs/PORTING.md](docs/PORTING.md#deliberate-deviations-the-only-ones):

- packages are imported from the shared store, so file modification times
  in `vendor/` are not the archive's, and hardlinked files are shared
  between projects (plugin packages are always copied);
- `self-update` updates maestro;
- `--version` adds a `maestro version` line on stderr.

Set `MAESTRO_PACKAGE_IMPORT_METHOD` to `clone`, `hardlink` or `copy` to
force an import method (default `auto`: reflink, else hardlink, else copy).

## Development

The dev environment is [devenv](https://devenv.sh): `devenv shell` provides
Go, golangci-lint, php, unzip and the rest. `ref-sync` checks out the exact
Composer sources being ported into `.ref/`.

```sh
just check       # vet, lint, test, build
just test-race   # race detector, plus the php-driven tests
just e2e         # compare against the real Composer phar (network, slow)
```

[docs/PORTING.md](docs/PORTING.md) is the porting contract: layout, rules,
and how tests are ported from Composer's suite and generated from Composer's
own PHP.

## License

MIT. maestro is a port of Composer and embeds some of its files; see
[LICENSE](LICENSE).

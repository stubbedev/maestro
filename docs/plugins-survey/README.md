# Plugin survey data

This is the raw data behind [`../PLUGINS.md`](../PLUGINS.md). It was collected
on 2026-10-05 from the latest stable release of each package on Packagist.

| File | Contents |
| --- | --- |
| `packages.tsv` | Each surveyed package: its version, `extra.class`, its `composer-plugin-api` constraint, the number of PHP files scanned, and how many of those touch Composer's API |
| `usage-matrix.tsv` | Every Composer or bundled-library symbol a plugin references (class, `extends`, `implements`, `new`, constant, static call, method name), with the number of distinct packages that use it and which ones. Rows are ranked by that count. A method row matches by name only: it counts calls to `->name(` where `name` is a public method of some class in Composer 2.10.3, so names that several classes share are ambiguous |
| `plugins.md` | Behaviour notes for each plugin, read from the source: which events it listens to, what it changes, which internals it touches, and the traps for an out-of-process host |
| `tools/` | Scripts that regenerate the TSVs |

To regenerate, from a scratch directory, inside `devenv shell`:

```sh
php  <repo>/docs/plugins-survey/tools/apiindex.php       # writes apiindex.json (public API of .ref/composer/src via reflection)
python3 <repo>/docs/plugins-survey/tools/fetch.py composer/installers cweagans/composer-patches@1 ...   # downloads and extracts dists
python3 <repo>/docs/plugins-survey/tools/scan.py         # writes symbols.tsv and packages.tsv
python3 <repo>/docs/plugins-survey/tools/matrix.py       # writes usage-matrix.tsv
```

`scan.py` skips tests and vendor directories. For four large packages it
scans only the plugin directory:

- `laravel/framework`: only `Illuminate/Foundation/ComposerScripts.php`
- `symfony/runtime`: only `Internal/`
- `phpro/grumphp`: only `src/Composer`
- `php-http/discovery`: only `src/Composer`

The scan is a regex scan, not a parser. It resolves `use` imports, including
group imports. It does not resolve namespace-relative names such as
`Command\BaseCommand` after `use Composer\Command;`. Those cases were read by
hand and are covered in `plugins.md`.

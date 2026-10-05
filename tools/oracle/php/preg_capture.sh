#!/usr/bin/env bash
# Captures real preg_* calls (pattern, subject, call site) made by Composer's
# code while running the PHPUnit suites of composer/composer and the
# composer/* libraries from .ref/, for preg_collect.php.
#
# Usage (from the repo root, inside the devenv shell; needs network for the
# dev dependencies and phpunit):
#   tools/oracle/php/preg_capture.sh WORKDIR
#   php tools/oracle/php/preg_collect.php WORKDIR/plog.txt WORKDIR/plog2.txt
#   php tools/oracle/php/preg_golden.php
#
# .ref/ is never modified: everything happens in a copy under WORKDIR.
set -euo pipefail

repo=$(cd "$(dirname "$0")/../../.." && pwd)
work=$(mkdir -p "$1" && cd "$1" && pwd)
here="$repo/tools/oracle/php"

rm -rf "$work/composer" "$work"/tests-* "$work"/plog*.txt
cp -a "$repo/.ref/composer" "$work/composer"
curl -sSfL -o "$work/composer.phar" https://getcomposer.org/download/2.10.3/composer.phar
curl -sSfL -o "$work/phpunit.phar" https://phar.phpunit.de/phpunit-9.6.phar
(cd "$work/composer" && php ../composer.phar install --no-interaction --no-progress)

# route every direct preg_* call through the logging wrappers; the Preg
# wrapper of composer/pcre is rewritten too, which covers all Preg:: calls
cd "$work/composer"
grep -rlE '\bpreg_(match|match_all|replace|replace_callback|replace_callback_array|split|grep)\(' \
    src vendor/composer vendor/seld --include='*.php' \
    | grep -v -e 'vendor/composer/autoload' -e 'BinaryInstaller.php' -e '/PHPStan/' \
    | xargs sed -i -E 's/\\?\bpreg_(match_all|match|replace_callback_array|replace_callback|replace|split|grep)\(/\\__plog_preg_\1(/g'

# load the wrappers in every php process, including the ones tests spawn
mkdir -p "$work/ini"
echo "auto_prepend_file=$here/preg_capture_log.php" > "$work/ini/zz-plog.ini"
scan=$(php --ini | sed -n 's/^Scan for additional .ini files in: *//p')
export PHP_INI_SCAN_DIR="$scan:$work/ini"

for lib in semver class-map-generator spdx-licenses metadata-minifier; do
    cp -a "$repo/.ref/$lib/tests" "$work/tests-$lib"
    (cd "$repo/.ref/$lib" && PLOG_FILE="$work/plog2.txt" php -d memory_limit=-1 "$work/phpunit.phar" \
        --bootstrap "$work/composer/vendor/autoload.php" --do-not-cache-result "$work/tests-$lib" || true)
done
PLOG_FILE="$work/plog.txt" php -d memory_limit=-1 "$work/phpunit.phar" --exclude-group legacy --do-not-cache-result || true

<?php
// Measures ArrayLoader::loadPackages over the p2 samples, for comparison
// with BenchmarkLoadPackages in internal/pkg/loader.
//
// Run: php tools/oracle/pkg/bench.php
require __DIR__.'/common.php';

use Composer\Package\Loader\ArrayLoader;
use Composer\Package\Version\VersionParser;

$inputs = p2_inputs($root);
$count = array_sum(array_map('count', $inputs));
$rounds = 10;
$start = microtime(true);
for ($i = 0; $i < $rounds; $i++) {
    // VersionParser caches parsed constraints statically, like the Go
    // benchmark's shared parser
    foreach ($inputs as $versions) {
        (new ArrayLoader(new VersionParser()))->loadPackages($versions);
    }
}
$elapsed = microtime(true) - $start;
printf("%d packages x %d rounds: %.0f packages/s, peak memory %.1f MB\n", $count, $rounds, $count * $rounds / $elapsed, memory_get_peak_usage() / 1048576);

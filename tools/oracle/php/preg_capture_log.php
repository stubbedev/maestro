<?php
// Auto-prepended (auto_prepend_file) while running the PHPUnit suites in
// preg_capture.sh: logs every preg_* call (pattern, subject, call site) made
// through the rewritten __plog_preg_* wrappers to $PLOG_FILE, deduped per process.
function __plog_w($fn, $p, $s) {
    static $seen = [], $fh = null;
    if (!is_string($s) && !is_int($s)) { if (is_array($s)) { foreach ($s as $x) __plog_w($fn, $p, $x); } return; }
    $s = (string) $s;
    if (is_array($p)) { foreach ($p as $k => $x) __plog_w($fn, is_string($k) && !is_string($x) ? $k : $x, $s); return; }
    if (!is_string($p)) return;
    $sub = strlen($s) > 4096 ? substr($s, 0, 4096) : $s;
    $h = md5($fn."\0".$p."\0".$sub, true);
    if (isset($seen[$h])) return;
    $seen[$h] = true;
    if ($fh === null) { $fh = fopen(getenv('PLOG_FILE') ?: '/tmp/plog.txt', 'a'); }
    $bt = debug_backtrace(DEBUG_BACKTRACE_IGNORE_ARGS, 12);
    $src = '';
    foreach ($bt as $f) { if (isset($f['file']) && $f['file'] !== __FILE__ && strpos($f['file'], '/pcre/src/') === false) { $src = $f['file'].':'.$f['line']; break; } }
    if ($src === '' && isset($bt[1]['file'])) { $src = $bt[1]['file'].':'.$bt[1]['line']; }
    fwrite($fh, base64_encode(serialize([$fn, $p, $sub, $src]))."\n");
}
function __plog_preg_match($p, $s, &$m = null, $f = 0, $o = 0) { __plog_w('match', $p, $s); return \preg_match($p, $s, $m, $f, $o); }
function __plog_preg_match_all($p, $s, &$m = null, $f = 0, $o = 0) { __plog_w('match_all', $p, $s); return \preg_match_all($p, $s, $m, $f, $o); }
function __plog_preg_replace($p, $r, $s, $l = -1, &$c = null) { __plog_w('replace', $p, $s); return \preg_replace($p, $r, $s, $l, $c); }
function __plog_preg_replace_callback($p, $cb, $s, $l = -1, &$c = null, $f = 0) { __plog_w('replace_callback', $p, $s); return \preg_replace_callback($p, $cb, $s, $l, $c, $f); }
function __plog_preg_replace_callback_array($p, $s, $l = -1, &$c = null, $f = 0) { __plog_w('replace_callback_array', $p, $s); return \preg_replace_callback_array($p, $s, $l, $c, $f); }
function __plog_preg_split($p, $s, $l = -1, $f = 0) { __plog_w('split', $p, $s); return \preg_split($p, $s, $l, $f); }
function __plog_preg_grep($p, $a, $f = 0) { __plog_w('grep', $p, $a); return \preg_grep($p, $a, $f); }

<?php
// Router of the errors oracle's local HTTP server (php -S ... router.php):
// /status/<code>/... answers with that HTTP status and a short body; any
// other path is served from the document root (internal/command/testdata/
// errors/_server), 404 when missing.
$path = parse_url($_SERVER['REQUEST_URI'], PHP_URL_PATH);
if (preg_match('{^/status/(\d{3})(/|$)}', $path, $m)) {
    http_response_code((int) $m[1]);
    header('Content-Type: text/plain');
    echo 'status '.$m[1]."\n";

    return true;
}
$file = $_SERVER['DOCUMENT_ROOT'].$path;
if (is_file($file)) {
    header('Content-Type: '.(str_ends_with($file, '.json') ? 'application/json' : 'application/octet-stream'));
    readfile($file);

    return true;
}
http_response_code(404);
header('Content-Type: text/plain');
echo "not found\n";

return true;

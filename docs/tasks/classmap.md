# Task: internal/classmap — composer/class-map-generator 1.7.3

Complete, faithful port of .ref/class-map-generator/src: ClassMapGenerator (scanPaths with excluded-dirs regex, autoload type psr-0/psr-4/classmap and namespace filtering, PSR violation detection with exact warning messages, ambiguous class tracking, the exact file iteration order — it decides which file wins for duplicate classes, so replicate it incl. sorting), ClassMap (getMap, getClasses, getAmbiguousClasses with duplicate filtering, getPsrViolations, sort), PhpFileParser (findClasses), PhpFileCleaner (the hand-written scanner stripping strings/comments/heredocs), FileList. Check how .ref/composer/src/Composer/Autoload/AutoloadGenerator.php calls it and expose what that needs; the AutoloadGenerator port builds on this API.

Regexes: PCRE with possessive quantifiers etc. Use regexp2 (atomic groups) or a hand-written scanner (internal/php may have a PCRE wrapper by now; use it if solid), but results must equal PHP for every input. PHP files may be any encoding: work on bytes.

Speed: runs over all of vendor/ on every dump. Parse files in parallel while keeping results and order identical to the sequential PHP; read each file once; few allocations.

Tests:
1. Port every test in .ref/class-map-generator/tests with all fixtures (copied verbatim into testdata, with the license notice).
2. Differential oracle: tools/oracle/classmap/*.php runs the real generator (.ref/composer/vendor/autoload.php has 1.7.3) over (a) the fixtures, (b) every .php under .ref/composer/{src,vendor,tests}, (c) synthetic tricky files (heredoc/nowdoc variants, enums, traits, interfaces, readonly classes, `class` in strings/comments, `?>` mid-file, short open tags, attributes, braced and multiple namespaces), recording findClasses per file and full generate() maps with PSR violations and ambiguities. Keep goldens reasonably sized (digests for the big corpus, full results for the synthetic set).
3. Fuzz PhpFileCleaner/findClasses (never panics), benchmarks.

Scope: only internal/classmap, tools/oracle/classmap, go.mod/go.sum via `go get`.
Report: public API, test counts, divergences, performance vs PHP on .ref/composer/vendor, lint/test status.

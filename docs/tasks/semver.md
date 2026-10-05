# Task: internal/semver — composer/semver 3.4.4

Complete, faithful port of .ref/semver/src: VersionParser (normalize, normalizeBranch, normalizeDefaultBranch, parseNumericAliasPrefix, parseStability, parseConstraints with every operator form, all exception messages verbatim), Constraint (matches, getLowerBound/getUpperBound, Bound), MultiConstraint (incl. create() optimisations), MatchAllConstraint, MatchNoneConstraint, Intervals (get, compactConstraint, isSubsetOf, haveIntersections — output incl. pretty strings identical), Comparator, Semver (satisfies, satisfiedBy, sort, rsort), CompilingMatcher (a cached matcher with identical results). Also owns PHP's `version_compare` (exact port of php-src's algorithm, operators included), exported for other packages.

Regex: use github.com/dlclark/regexp2 or hand-written parsing (internal/php may have a PCRE wrapper by now; use it if it's there and solid, otherwise don't wait for it), but behaviour must be identical to PCRE for every input (possessive quantifiers need atomic groups in regexp2).

String forms matter: constraints are printed into lock files, error messages and `why-not` output, so String() must match PHP's __toString exactly.

Tests:
1. Port every test in .ref/semver/tests with all data-provider cases.
2. Differential oracle: tools/oracle/semver/*.php (autoload via .ref/composer/vendor/autoload.php, which has semver 3.4.4) over a large generated corpus (valid/invalid versions and constraints, dev branches, ` as ` aliases, stability suffixes, wildcards, hyphen ranges, ^/~ with 0.x, `||` `|` `,` combos, whitespace variants) recording normalize, parseConstraints→String, a matches matrix, Intervals::compactConstraint/get, sort results and exception messages into goldens.
3. Fuzz: parse→String→parse stability, matching never panics.
4. Benchmarks: the solver calls matches millions of times — allocation-free matching, cached parsed constraints where PHP's CompilingMatcher caches.

Scope: only internal/semver, tools/oracle/semver, go.mod/go.sum via `go get`.
Report: public API, test counts (ported, oracle cases), divergences found, lint/test status.

// Package resolver ports Composer\DependencyResolver: the request, the
// pool and how it is built and optimized, the rules, the SAT solver with
// conflict learning, the problem messages and the transactions. The
// operations are in the operation subpackage, which the downloaders and
// installers below this package import.
//
// The solver reproduces Composer's choices exactly, not just a valid
// solution: every iteration order that affects a result follows PHP's,
// sorts use internal/php's port of zend_sort, and the policy caches keep
// PHP's keys where they could collide.
//
// For speed the data is kept compact: literals are int32 package ids,
// decisions and watch chains are slices indexed by package id, rules and
// their literals come from shared blocks, and the pool's whatProvides
// cache is keyed by constraint string as in PHP.
//
// Composer keys several maps by spl_object_id; here they are keyed by the
// package (or rule, or pool) itself.
package resolver

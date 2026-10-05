// Package policy ports Composer\Policy (src/Composer/Policy): the parsed
// form of config.policy, with its backwards-compatible fallback to
// config.audit, as Config and the auditor use it.
//
// The classes are readonly in Composer; the Go values are never modified
// after construction either, and the with* methods return new values that
// may share unchanged parts with the receiver.
//
// Composer declares strict_types, so a raw config value of the wrong type
// (which the composer.json schema rejects) makes PHP throw a TypeError
// from a constructor. Those values are converted as PHP's casts would
// instead.
package policy

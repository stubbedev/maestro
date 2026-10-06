// Ports the differences between the Zend language scanners
// (Zend/zend_language_scanner.l) of PHP 7.2 to 8.5 that change what
// php_strip_whitespace() returns.

package classmap

// phpVersion is the PHP minor version (major*100 + minor) whose scanner
// the stripper follows: 702 (PHP 7.2) to 805 (PHP 8.5).
//
// Composer 2.10 runs on PHP 7.2.5 and later. Every scanner rule, and the
// zend_strip() and php_strip_whitespace() of every minor version from 7.2
// to 8.5, was compared (php-src as shipped in the official php:X.Y-cli
// images); within a minor version the rules that matter here did not
// change. The differences that change the output, each one seen in the
// real PHPs' output by tools/oracle/classmap/versions.sh:
//
//   - 7.3 (flexible heredoc and nowdoc syntax, RFC
//     flexible_heredoc_nowdoc_syntaxes): before it, a closing marker is
//     only recognised at the start of a line and followed by an optional
//     ";" and a newline, and there is no scan-ahead pass.
//   - 7.4: numbers may contain "_" separators; "??=" is one token; a
//     character the scanner does not know (control characters, DEL) is a
//     T_BAD_CHARACTER token that zend_strip() writes, where earlier
//     versions skip it silently (`goto restart`).
//   - 8.0: "#[" opens an attribute, where it used to start a "#" comment;
//     line comments end before their newline (which becomes whitespace),
//     where they used to include it; namespaced names ("A\B", "\A",
//     "namespace\A") and "?->" are single tokens; the CLI SAPI's
//     CG(skip_shebang) applies to every file scanned, so
//     php_strip_whitespace() drops a "#!" line (7.4 only skipped it in the
//     main script, earlier versions not at all); unmatched and mismatched
//     brackets throw, which stops a heredoc scan-ahead.
//   - 8.2: comments are allowed in ST_LOOKING_FOR_PROPERTY (after "->"),
//     where the scanner used to leave that state first.
//   - 8.3: "yield" and "from" may be separated by comments, which are then
//     part of the T_YIELD_FROM token and kept.
//   - 8.5: the "(void)" cast (whose spaces and tabs are part of the
//     token) and "|>" are tokens.
//
// Some changes are modelled although they cannot change the output, so
// that the scanner stays the version's: 8.1's explicit octal numbers
// ("0o17") and 8.4's "public(set)", "protected(set)" and "private(set)"
// (asymmetric visibility) contain no whitespace or comments, cannot be the
// token zend_strip() copies after a heredoc's closing marker (which is
// followed by a non-label character) and raise no error in a heredoc
// scan-ahead; the oracle confirms that 8.1 and 8.4 strip every case
// exactly as 8.0 and 8.3 do. An unterminated comment throws since 8.0 (a
// warning before), but it runs to the end of the file, where a scan-ahead
// stops anyway. Other changes (enum,
// readonly, match, fn, the "&" tokens of 8.1, the "<?php" at end of file
// rule of 7.4, __PROPERTY__, property hooks, the deprecated casts of 8.5)
// only change token types, or split verbatim text differently, so they
// cannot change the output and are not modelled.
type phpVersion uint16

const (
	php72 phpVersion = 702
	php73 phpVersion = 703
	php74 phpVersion = 704
	php80 phpVersion = 800
	php81 phpVersion = 801
	php82 phpVersion = 802
	php83 phpVersion = 803
	php84 phpVersion = 804
	php85 phpVersion = 805

	// defaultPHPVersion is followed when the PHP version is unknown.
	defaultPHPVersion = php84
)

// phpVersionOf returns the scanner version for a PHP_VERSION_ID: 0
// (unknown) is defaultPHPVersion, versions before 7.2 use 7.2's scanner and
// versions after 8.5 use 8.5's (the latest known).
func phpVersionOf(versionID int) phpVersion {
	if versionID <= 0 {
		return defaultPHPVersion
	}
	v := phpVersion(min(versionID/10000*100+versionID/100%100, int(php85)))

	return max(v, php72)
}

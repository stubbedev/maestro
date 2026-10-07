package testutil

import "testing"

// TestNormalizeOracle: what one machine's run has of its own becomes the
// placeholders every machine's run has.
func TestNormalizeOracle(t *testing.T) {
	run := OracleRun{Dir: "/tmp/maestro-errors.abc/run", Server: "127.0.0.1:4711", Composer: "/src/composer"}
	for _, tc := range []struct{ name, in, want string }{
		{
			"paths", "Reading /tmp/maestro-errors.abc/run/p/composer.json from http://127.0.0.1:4711/x in /src/composer/src/A.php\n",
			"Reading @DIR@/p/composer.json from http://@SERVER@/x in @COMPOSER@/src/A.php\n",
		},
		{
			"ca bundle",
			"Checked CA file /etc/pki/tls/certs/ca-bundle.crt does not exist or it is not a file.\n" +
				"Checked directory /etc/pki/tls/certs does not exist or it is not a directory.\n" +
				"Checked file or directory /etc/ssl/cert.pem is not readable.\n" +
				"Checked CA file /nix/store/gp6rxb1rjdx6lalk7qc8mav293imv0x1-nss-cacert-3.126/etc/ssl/certs/ca-bundle.crt: valid\n",
			"Checked CA file @CAFILE@: valid\n",
		},
		{
			"php.ini",
			"To enable extensions, verify that they are enabled in your .ini files:\n    - /etc/php/8.4/cli/php.ini\n    - /etc/php/8.4/cli/conf.d/10-opcache.ini\nYou can also run `php --ini`\n",
			"To enable extensions, verify that they are enabled in your .ini files:\n    - @PHPINI@\nYou can also run `php --ini`\n",
		},
		{
			"unzip", "Executing async command (CWD): '/usr/bin/unzip' '-qq' 'a.zip'\nFailed to extract x/y: (9) /nix/store/7af934aj137dhmnjpjdxsmvi5x9jmlvw-unzip-6.0/bin/unzip -qq a.zip\n",
			"Executing async command (CWD): '@BIN@/unzip' '-qq' 'a.zip'\nFailed to extract x/y: (9) @BIN@/unzip -qq a.zip\n",
		},
		{
			"connect time", "Failed to connect to 127.0.0.1 port 1 after 3 ms: Could not connect\n",
			"Failed to connect to 127.0.0.1 port 1 after 0 ms: Could not connect\n",
		},
		{
			"diagnose",
			"Composer version: 2.10.3\nMaestro version: 1.2.0\nPHP version: 8.4.25\nPHP binary path: /usr/bin/php8.4\nOpenSSL version: OpenSSL 3.0.13\ncurl version: 8.5.0 libz 1.3\nzip: extension present, unzip present\nChecking git settings: OK git version 2.43.0\n",
			"Composer version: 2.10.3\nPHP version: @PHPVERSION@\nPHP binary path: @MACHINE@\nOpenSSL version: @MACHINE@\ncurl version: @MACHINE@\nzip: @MACHINE@\nChecking git settings: OK git version @GITVERSION@\n",
		},
		{
			"diagnose platform override",
			"PHP version: 7.0.0 - Package overridden via config.platform, actual: 8.4.25\n",
			"PHP version: 7.0.0 - Package overridden via config.platform, actual: @PHPVERSION@\n",
		},
	} {
		if got := NormalizeOracle(tc.in, run); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

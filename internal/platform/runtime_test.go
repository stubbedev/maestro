// Ports tests/Composer/Test/Platform/RuntimeTest.php.

package platform

import "testing"

func TestRuntime_ParseExtensionInfo(t *testing.T) {
	cases := map[string][2]string{
		"pdo_sqlite": {
			`<h2><a name="module_pdo_sqlite" href="#module_pdo_sqlite">pdo_sqlite</a></h2>
<table>
<tr><td class="e">PDO Driver for SQLite 3.x </td><td class="v">enabled </td></tr>
<tr><td class="e">SQLite Library </td><td class="v">3.40.1 </td></tr>
</table>`,
			`pdo_sqlite

PDO Driver for SQLite 3.x => enabled
SQLite Library => 3.40.1`,
		},
	}

	for name, c := range cases {
		if got := ParseHtmlExtensionInfo(c[0]); got != c[1] {
			t.Errorf("%s: got %q, want %q", name, got, c[1])
		}
	}
}

func TestCallable_String(t *testing.T) {
	if got := Func("inet_pton").String(); got != "inet_pton" {
		t.Error(got)
	}

	if got := StaticMethod("IntlChar", "getUnicodeVersion").String(); got != "IntlChar::getUnicodeVersion" {
		t.Error(got)
	}
}

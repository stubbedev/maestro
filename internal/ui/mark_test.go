package ui

import "testing"

func TestCount(t *testing.T) {
	for c, want := range map[Count]string{
		{0, "removal"}: "0 removals",
		{1, "install"}: "1 install",
		{3, "update"}:  "3 updates",
	} {
		if got := c.String(); got != want {
			t.Errorf("%v = %q, want %q", c, got, want)
		}
	}
}

// For a formatter that knows only Composer's tags, roles become their
// inline styles and marks and faint roles lose their tags; other tags
// stay.
func TestForeign(t *testing.T) {
	in := MarkInstall.Item() + "Installing <info>a</info> " + RoleDanger.Wrap("x") + RoleMuted.Wrap("y") + "<maestro-unknown>z</maestro-unknown>"
	want := "  - Installing <info>a</info> <fg=red;options=bold>x</>y<maestro-unknown>z</maestro-unknown>"
	if got := Foreign(in); got != want {
		t.Errorf("Foreign = %q, want %q", got, want)
	}
	if got := Foreign("<info>a</info>"); got != "<info>a</info>" {
		t.Errorf("Foreign changed Composer's markup: %q", got)
	}
}

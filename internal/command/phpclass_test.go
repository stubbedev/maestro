package command_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
)

// Every command names its own class (get_class($command)), which plugins
// read through the application's mirrors and which telemetry and
// messages print: a class of Composer's or Symfony's console, and never
// the class of a type it embeds (each Go type has a class no other has,
// and none is the bare Command every command embeds).
func TestCommands_NameTheirOwnPHPClass(t *testing.T) {
	alias, err := command.NewScriptAliasCommand("test", nil, php.NewArray())
	if err != nil {
		t.Fatal(err)
	}
	commands := append(commandtest.NewApplication().DefaultCommands(), alias)

	types := map[string]string{}
	for _, c := range commands {
		class, typ := c.PHPClass(), fmt.Sprintf("%T", c)
		if !strings.HasPrefix(class, `Composer\Command\`) && !strings.HasPrefix(class, `Symfony\Component\Console\Command\`) || class == (&console.Command{}).PHPClass() {
			t.Errorf("%s is of class %q", typ, class)
		}
		if other, ok := types[class]; ok && other != typ {
			t.Errorf("%s and %s are both of class %q", other, typ, class)
		}
		types[class] = typ
	}
}

package command

import (
	"github.com/stubbedev/maestro/internal/console"
)

// The commands of Application::getDefaultCommands, in its order. Each
// command's file registers its constructor from an init function:
//
//	func init() { registerCommand(OrderAbout, func() console.Commander { return NewAboutCommand() }) }
//
// The table is filled before main runs and only read afterwards, so it is
// not mutable state in the sense of docs/PLUGINS.md §5.11.
const (
	OrderAbout = iota
	OrderConfig
	OrderDepends
	OrderProhibits
	OrderInit
	OrderInstall
	OrderCreateProject
	OrderUpdate
	OrderSearch
	OrderValidate
	OrderAudit
	OrderShow
	OrderSuggests
	OrderRequire
	OrderDumpAutoload
	OrderStatus
	OrderArchive
	OrderDiagnose
	OrderRunScript
	OrderLicenses
	OrderGlobal
	OrderClearCache
	OrderRemove
	OrderHome
	OrderExec
	OrderOutdated
	OrderCheckPlatformReqs
	OrderFund
	OrderReinstall
	OrderBump
	OrderRepository
	OrderPolicy
	OrderSelfUpdate
	orderCount
)

var commandConstructors [orderCount]func() console.Commander

// registerCommand records the constructor of the command at position order
// of getDefaultCommands. Call it only from init functions.
func registerCommand(order int, ctor func() console.Commander) {
	if commandConstructors[order] != nil {
		panic("command: two commands registered at the same position")
	}
	commandConstructors[order] = ctor
}

// ScriptAliasConstructor builds a ScriptAliasCommand: new
// ScriptAliasCommand($script, $description, $aliases) with description and
// aliases the raw composer.json values (the description is a string unless
// scripts-descriptions holds something else; aliases default to an empty
// *php.Array).
type ScriptAliasConstructor func(script string, description, aliases any) (console.Commander, error)

var scriptAliasConstructor ScriptAliasConstructor

// registerScriptAliasCommand records the ScriptAliasCommand constructor.
// Call it only from an init function.
func registerScriptAliasCommand(ctor ScriptAliasConstructor) {
	scriptAliasConstructor = ctor
}

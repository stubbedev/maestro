package command

// Test hooks of the Application (group C).

// HasScriptAliasCommand reports whether ScriptAliasCommand is registered.
func HasScriptAliasCommand() bool { return scriptAliasConstructor != nil }

// TelemetryCommandName exposes getTelemetryCommandName.
var TelemetryCommandName = telemetryCommandName

// ParseBackupVersion exposes parseBackupVersion.
var ParseBackupVersion = parseBackupVersion

// AllCommandsRegistered reports whether every command of
// getDefaultCommands is registered.
func AllCommandsRegistered() bool {
	for _, c := range commandConstructors {
		if c == nil {
			return false
		}
	}

	return true
}

// Logo is the logo heading the banner of `list`.
const Logo = logo

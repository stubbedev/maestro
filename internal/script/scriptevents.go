// Ports src/Composer/Script/ScriptEvents.php.

// Package script ports Composer\Script: the names of the script events.
//
// Composer\Script\Event is eventdispatcher.ScriptEvent. It extends
// Composer\EventDispatcher\Event and EventDispatcher creates it, so it lives
// in internal/eventdispatcher with the base event; this package holds what
// the dispatcher itself needs from Composer\Script (the ScriptEvents
// constants) and is imported by it.
package script

// The ScriptEvents constants: the events whose listeners are the root
// package's scripts.
const (
	// PreInstallCmd occurs before the install command is executed, it
	// contains one of the listeners for each package.
	PreInstallCmd = "pre-install-cmd"
	// PostInstallCmd occurs after the install command is executed.
	PostInstallCmd = "post-install-cmd"
	// PreUpdateCmd occurs before the update command is executed.
	PreUpdateCmd = "pre-update-cmd"
	// PostUpdateCmd occurs after the update command is executed.
	PostUpdateCmd = "post-update-cmd"
	// PreStatusCmd occurs before the status command is executed.
	PreStatusCmd = "pre-status-cmd"
	// PostStatusCmd occurs after the status command is executed.
	PostStatusCmd = "post-status-cmd"
	// PreAutoloadDump occurs before the autoloader is dumped.
	PreAutoloadDump = "pre-autoload-dump"
	// PostAutoloadDump occurs after the autoloader is dumped.
	PostAutoloadDump = "post-autoload-dump"
	// PostRootPackageInstall occurs after the root package has been
	// installed (create-project).
	PostRootPackageInstall = "post-root-package-install"
	// PostCreateProjectCmd occurs after the create-project command is
	// executed.
	PostCreateProjectCmd = "post-create-project-cmd"
	// PreArchiveCmd occurs before the archive command is executed.
	PreArchiveCmd = "pre-archive-cmd"
	// PostArchiveCmd occurs after the archive command is executed.
	PostArchiveCmd = "post-archive-cmd"
)

// HasConstant reports whether ScriptEvents declares a constant of this
// name, as defined('Composer\Script\ScriptEvents::'.$name) does (class
// constant names are case-sensitive).
func HasConstant(name string) bool {
	switch name {
	case "PRE_INSTALL_CMD", "POST_INSTALL_CMD", "PRE_UPDATE_CMD", "POST_UPDATE_CMD",
		"PRE_STATUS_CMD", "POST_STATUS_CMD", "PRE_AUTOLOAD_DUMP", "POST_AUTOLOAD_DUMP",
		"POST_ROOT_PACKAGE_INSTALL", "POST_CREATE_PROJECT_CMD", "PRE_ARCHIVE_CMD", "POST_ARCHIVE_CMD":
		return true
	}

	return false
}

package command

// Test hooks of InitCommand (InitCommandTest calls its protected and
// private methods through reflection or DummyInitCommand).

// ParseAuthorString exposes parseAuthorString: name, email (nil for null).
func (c *InitCommand) ParseAuthorString(author string) (string, *string, error) {
	a, err := c.parseAuthorString(author)

	return a.name, a.email, err
}

// FormatAuthors exposes formatAuthors.
var FormatAuthors = (*InitCommand).formatAuthors

// GitConfig exposes getGitConfig.
var GitConfig = (*InitCommand).getGitConfig

// AddVendorIgnore exposes addVendorIgnore.
var AddVendorIgnore = addVendorIgnore

// HasVendorIgnore exposes hasVendorIgnore.
var HasVendorIgnore = hasVendorIgnore

package downloader

// InstallPreference is Composer's $preferSource and $preferDist as one
// value. They are two flags, set apart by setPreferSource() and
// setPreferDist(): both may be on (--prefer-source --prefer-dist, or a
// plugin's setters), and then source wins; with neither, the
// preferred-install config decides (auto).
type InstallPreference uint8

// The flags of an InstallPreference; the zero value is PreferAuto.
const (
	PreferSource InstallPreference = 1 << iota
	PreferDist

	PreferAuto InstallPreference = 0
)

// PreferenceOf is the preference a preferred-install config value forces:
// "source" or "dist", auto for anything else ("auto", or a map of package
// patterns, which DownloadManager.SetPreferences takes).
func PreferenceOf(preferredInstall any) InstallPreference {
	switch preferredInstall {
	case "source":
		return PreferSource
	case "dist":
		return PreferDist
	}

	return PreferAuto
}

// With is p with flag turned on or off: setPreferSource($on) for
// PreferSource, setPreferDist($on) for PreferDist.
func (p InstallPreference) With(flag InstallPreference, on bool) InstallPreference {
	if on {
		return p | flag
	}

	return p &^ flag
}

// Has reports whether flag is on: $preferSource or $preferDist.
func (p InstallPreference) Has(flag InstallPreference) bool { return p&flag != 0 }

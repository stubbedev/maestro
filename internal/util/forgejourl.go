// Ports src/Composer/Util/ForgejoUrl.php and
// src/Composer/Util/ForgejoRepositoryData.php.

package util

import (
	"github.com/stubbedev/maestro/internal/php"
)

// ForgejoURLRegex is ForgejoUrl::URL_REGEX.
const ForgejoURLRegex = `{^(?:(?:https?|git)://([^/]+)/|git@([^:]+):/?)([^/]+)/([^/]+?)(?:\.git|/)?$}`

var forgejoURLRegex = php.MustCompile(ForgejoURLRegex)

// ForgejoURL is ForgejoUrl: the parts of a Forgejo repository URL.
type ForgejoURL struct {
	Owner      string
	Repository string
	OriginURL  string
	APIURL     string
}

// CreateForgejoURL is ForgejoUrl::create.
func CreateForgejoURL(repoURL string) (*ForgejoURL, error) {
	if u, err := TryForgejoURL(repoURL); err != nil || u != nil {
		return u, err
	}

	return nil, &InvalidArgumentError{Message: "This is not a valid Forgejo URL: " + repoURL}
}

// TryForgejoURL is ForgejoUrl::tryFrom; nil when repoURL is not a Forgejo
// repository URL. The error is the PcreException Preg::isMatch throws.
func TryForgejoURL(repoURL string) (*ForgejoURL, error) {
	m, err := forgejoURLRegex.Match(repoURL)
	if err != nil || m == nil {
		return nil, err
	}

	origin, ok := m.Group(1)
	if !ok {
		origin = m.Get(2)
	}

	origin = php.Strtolower(origin)
	owner, repo := m.Get(3), m.Get(4)

	return &ForgejoURL{
		Owner:      owner,
		Repository: repo,
		OriginURL:  origin,
		APIURL:     "https://" + origin + "/api/v1/repos/" + owner + "/" + repo,
	}, nil
}

// GenerateSSHURL is ForgejoUrl::generateSshUrl.
func (u *ForgejoURL) GenerateSSHURL() string {
	return "git@" + u.OriginURL + ":" + u.Owner + "/" + u.Repository + ".git"
}

// ForgejoRepositoryData is ForgejoRepositoryData: the repository fields
// read from the Forgejo API.
type ForgejoRepositoryData struct {
	HTMLURL       string
	HTTPCloneURL  string
	SSHURL        string
	IsPrivate     bool
	DefaultBranch string
	HasIssues     bool
	IsArchived    bool
}

// ForgejoRepositoryDataFromRemote is ForgejoRepositoryData::fromRemoteData.
// PHP's typed constructor throws a TypeError for a missing or mistyped
// field; here that is an UnexpectedValueError naming the field.
func ForgejoRepositoryDataFromRemote(data *php.Array) (*ForgejoRepositoryData, error) {
	var (
		d   ForgejoRepositoryData
		err error
	)

	str := func(key string) string {
		v, _ := data.Get(key)
		s, ok := v.(string)
		if !ok && err == nil {
			err = &UnexpectedValueError{Message: "Forgejo repository data field " + key + " must be a string, " + php.GetType(v) + " given"}
		}

		return s
	}
	boolean := func(key string) bool {
		v, _ := data.Get(key)
		b, ok := v.(bool)
		if !ok && err == nil {
			err = &UnexpectedValueError{Message: "Forgejo repository data field " + key + " must be a bool, " + php.GetType(v) + " given"}
		}

		return b
	}

	d.HTMLURL = str("html_url")
	d.HTTPCloneURL = str("clone_url")
	d.SSHURL = str("ssh_url")
	d.IsPrivate = boolean("private")
	d.DefaultBranch = str("default_branch")
	d.HasIssues = boolean("has_issues")
	d.IsArchived = boolean("archived")

	if err != nil {
		return nil, err
	}

	return &d, nil
}

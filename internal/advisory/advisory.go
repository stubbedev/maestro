// Ports src/Composer/Advisory/IgnoredSecurityAdvisory.php and
// src/Composer/Advisory/AuditConfig.php, and aliases the advisory data
// classes internal/repository holds (PartialSecurityAdvisory.php,
// SecurityAdvisory.php).

// Package advisory ports Composer\Advisory: security advisories and the
// Auditor behind composer audit and the post-install audit.
package advisory

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
)

// PartialSecurityAdvisory is Composer\Advisory\PartialSecurityAdvisory.
// It lives in internal/repository because repositories create advisories.
type PartialSecurityAdvisory = repository.PartialSecurityAdvisory

// SecurityAdvisory is Composer\Advisory\SecurityAdvisory.
type SecurityAdvisory = repository.SecurityAdvisory

// Advisory is any PartialSecurityAdvisory (sub)class instance.
type Advisory = repository.Advisory

// CreatePartialSecurityAdvisory ports PartialSecurityAdvisory::create: a
// *SecurityAdvisory when data has a title, sources and reportedAt, else a
// *PartialSecurityAdvisory.
func CreatePartialSecurityAdvisory(packageName string, data *php.Array, parser repository.ConstraintParser) (Advisory, error) {
	return repository.CreatePartialSecurityAdvisory(packageName, data, parser)
}

// IgnoredSecurityAdvisory ports Composer\Advisory\IgnoredSecurityAdvisory:
// an advisory the audit ignores, with the reason.
type IgnoredSecurityAdvisory struct {
	SecurityAdvisory
	IgnoreReason pkg.NullString
}

var _ Advisory = (*IgnoredSecurityAdvisory)(nil)

// JSONSerialize ports IgnoredSecurityAdvisory::jsonSerialize: the
// SecurityAdvisory data with ignoreReason last, left out when null.
func (a *IgnoredSecurityAdvisory) JSONSerialize() *php.Array {
	data := a.SecurityAdvisory.JSONSerialize()
	if a.IgnoreReason.Valid {
		data.Set("ignoreReason", a.IgnoreReason.S)
	}

	return data
}

// ToIgnoredAdvisory ports SecurityAdvisory::toIgnoredAdvisory.
func ToIgnoredAdvisory(a *SecurityAdvisory, ignoreReason pkg.NullString) *IgnoredSecurityAdvisory {
	return &IgnoredSecurityAdvisory{SecurityAdvisory: *a, IgnoreReason: ignoreReason}
}

// AsSecurityAdvisory is `$advisory instanceof SecurityAdvisory`: the
// SecurityAdvisory part of a *SecurityAdvisory or *IgnoredSecurityAdvisory.
func AsSecurityAdvisory(a Advisory) (*SecurityAdvisory, bool) {
	switch a := a.(type) {
	case *SecurityAdvisory:
		return a, true
	case *IgnoredSecurityAdvisory:
		return &a.SecurityAdvisory, true
	}

	return nil, false
}

// AuditConfig ports Composer\Advisory\AuditConfig.
type AuditConfig struct {
	// Audit tells whether to run the audit.
	Audit bool
	// AuditFormat is one of the Format* values.
	AuditFormat string
}

// NewAuditConfig ports new AuditConfig() with its defaults: audit on, in
// the summary format.
func NewAuditConfig() AuditConfig { return AuditConfig{Audit: true, AuditFormat: FormatSummary} }

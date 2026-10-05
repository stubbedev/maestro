// Ports the constants of src/Composer/Policy/ListPolicyConfig.php and
// src/Composer/Policy/PolicyConfig.php that internal/config needs.

package policy

// ListPolicyConfig::AUDIT_*.
const (
	AuditIgnore = "ignore"
	AuditReport = "report"
	AuditFail   = "fail"
)

// Audits is ListPolicyConfig::AUDITS. Treat it as read-only.
var Audits = [...]string{AuditIgnore, AuditReport, AuditFail}

// ListPolicyConfig::BLOCK_SCOPE_*.
const (
	BlockScopeUpdate  = "update"
	BlockScopeInstall = "install"
	BlockScopeAll     = "all"
)

// NonListKeys is PolicyConfig::NON_LIST_KEYS: keys at the top level of the
// policy config that are not list names. Treat it as read-only.
var NonListKeys = [...]string{"ignore-unreachable"}

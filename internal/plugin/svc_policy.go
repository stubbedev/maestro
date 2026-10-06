// The audit and policy configuration of an install (docs/PLUGINS.md §4.7,
// §4.11): AuditConfig as a mirror (its two public properties), and
// `policy.*`, the PolicyConfig BaseCommand::createPolicyConfig() builds.

package plugin

import (
	"sync"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/policy"
)

const classAuditConfig = `Composer\Advisory\AuditConfig`

// auditConfigMirror is an AuditConfig as PHP mirrors it: Composer's value
// object, whose public $audit and $auditFormat code writes directly
// (Maestro\Shim\Adapter\AuditConfigAdapter polls them).
type auditConfigMirror struct {
	c *advisory.AuditConfig

	mu   sync.Mutex
	last advisory.AuditConfig
	rev  uint64
}

// PHPOpaque implements php.Opaque.
func (*auditConfigMirror) PHPOpaque() {}

// PHPClass implements rpc.Object.
func (*auditConfigMirror) PHPClass() string { return classAuditConfig }

// MirrorBase implements rpc.Mirror.
func (*auditConfigMirror) MirrorBase() string { return classAuditConfig }

// Rev implements rpc.Mirror: it moves when maestro changed the config
// since it was last asked.
func (m *auditConfigMirror) Rev() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	if *m.c != m.last {
		m.last = *m.c
		m.rev++
	}

	return m.rev
}

// MirrorSnapshot implements rpc.Mirror.
func (m *auditConfigMirror) MirrorSnapshot() (*php.Array, error) {
	return php.ArrayOf("audit", m.c.Audit, "auditFormat", m.c.AuditFormat), nil
}

// ApplyMirror implements rpc.Mirror: what PHP wrote into the properties.
// AuditConfig's properties are untyped: a value is taken as Composer's
// Installer and Auditor use it ((bool) $audit, the format a string).
func (m *auditConfigMirror) ApplyMirror(fields *php.Array) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if v, ok := fields.Get("audit"); ok {
		m.c.Audit = php.ToBool(v)
	}
	if v, ok := fields.Get("auditFormat"); ok {
		m.c.AuditFormat = php.ToString(v)
	}
	m.last = *m.c

	return nil
}

func (m *auditConfigMirror) goValue() any { return m.c }

// auditConfigObject returns the object an AuditConfig crosses to PHP as.
func (r *Runtime) auditConfigObject(c *advisory.AuditConfig) any {
	if c == nil {
		return nil
	}

	return r.bridge.object(c, func() rpc.Object { return &auditConfigMirror{c: c, last: *c} })
}

// adoptAuditConfig builds maestro's AuditConfig of one created in PHP
// (new AuditConfig(), BaseCommand::createAuditConfig()) when it first
// crosses.
func (r *Runtime) adoptAuditConfig(_ string, snapshot *php.Array) (rpc.Mirror, error) {
	c := advisory.NewAuditConfig()
	m := &auditConfigMirror{c: &c}
	if err := m.ApplyMirror(snapshot); err != nil {
		return nil, err
	}
	r.bridge.object(m.c, func() rpc.Object { return m })

	return m, nil
}

func (r *Runtime) registerPolicy() {
	r.RegisterMirrorFactory(classAuditConfig, r.adoptAuditConfig)

	// BaseCommand::createPolicyConfig(): PolicyConfig::fromConfig($config),
	// then withBlockingDisabled() for --no-blocking and its variables
	// (decided in PHP, in Composer's order). The PolicyConfig is maestro's,
	// for Installer::setPolicyConfig(); its own members are presence-only
	// (docs/PLUGINS.md §4.11).
	r.Handle("policy.fromConfig", func(v any) (any, error) {
		a := argsOf("policy.fromConfig", v)
		cfg, err := param[*config.Config](a, 0)
		if err != nil {
			return nil, err
		}
		pc, err := policy.FromConfig(cfg)
		if err != nil {
			return nil, err
		}

		return r.serviceObject(pc, classPolicyConfig), nil
	})
	r.Handle("policy.withBlockingDisabled", func(v any) (any, error) {
		a := argsOf("policy.withBlockingDisabled", v)
		pc, err := param[*policy.PolicyConfig](a, 0)
		if err != nil {
			return nil, err
		}

		return r.serviceObject(pc.WithBlockingDisabled(), classPolicyConfig), nil
	})
}

const classPolicyConfig = `Composer\Policy\PolicyConfig`

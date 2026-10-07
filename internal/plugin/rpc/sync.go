// The sync engine (docs/PLUGINS.md §5.3, §6.3, D4). State syncs at every
// control transfer: each message carries what changed on its sender's
// side since the previous message, and the receiver applies it before
// anything else, so a putenv(), chdir() or in-place setter on one side is
// visible to the other before it runs again (the only moment it could
// observe it).
//
//	{"reg": [{"tmp": -9, "h": 311}],            PHP-born objects Go adopted
//	 "env": {"set": {...}, "unset": [...]},     environment changes
//	 "cwd": "/p",                                working directory
//	 "st":  {"runningCommand": "update", ...},  Composer's statics
//	 "o":   [{"h": 12, "r": 7, "f": {...}},     mirror fields ("full" for
//	         {"h": 40, "r": 1, "full": {...}}]}  a whole snapshot)
//
// Each side remembers the state it last agreed on with the other and
// sends the difference, so nothing echoes back.

package rpc

import (
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// Static is one of Composer's process-wide statics that PHP code may read
// and change (Composer::$runningCommand, ProcessExecutor::$timeout, ...).
// Values are nil, bool, int64 or string.
type Static struct {
	Get func() any
	Set func(v any)
}

// PHPStatics are the statics the shim keeps, with their initial values in
// a fresh PHP process.
var PHPStatics = map[string]any{
	"runningCommand":   nil,
	"runningOperation": nil,
	"processTimeout":   int64(300),
	// ErrorHandler::$hasShownDeprecationNotice
	"hasShownDeprecationNotice": int64(0),
	// the cycle collector Installer::run disables (false) and enables
	// (true); null as PHP's ini has it
	"gc": nil,
}

// registration is a PHP-born object Go adopted, announced in the next
// sync block.
type registration struct {
	tmp, h Handle
	m      Object
	// rev is the mirror's revision when PHP's object was adopted: what
	// PHP's object holds, so later changes reach it even when they happen
	// before the registration is sent.
	rev uint64
}

// syncState is what Go last agreed on with PHP.
type syncState struct {
	env map[string]string
	// envOf is the os.Environ() env was built from, nil when PHP changed
	// env since: an unchanged environment is told by comparing it with
	// os.Environ() (its strings share their bytes, so that is cheap).
	envOf       []string
	cwd         string
	statics     map[string]Static
	staticNames []string
	sentStatics map[string]any
}

// newSyncState is the state PHP starts in: envOf (os.Environ() when nil)
// and cwd (the working directory when "").
func newSyncState(statics map[string]Static, envOf []string, cwd string) syncState {
	if envOf == nil {
		envOf = os.Environ()
	}
	if cwd == "" {
		cwd, _ = php.Getcwd()
	}

	return syncState{
		env:         environ(envOf),
		envOf:       envOf,
		cwd:         cwd,
		statics:     statics,
		staticNames: slices.Sorted(maps.Keys(statics)),
		sentStatics: maps.Clone(PHPStatics),
	}
}

// environ is an os.Environ() as a map.
func environ(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if kv == "" {
			continue
		}
		// "=C:=C:\x" style entries (Windows) start with "=".
		if i := strings.IndexByte(kv[1:], '='); i >= 0 {
			m[kv[:i+1]] = kv[i+2:]
		}
	}

	return m
}

// normalizeStatic makes a static's value comparable with what decoding
// produces.
func normalizeStatic(v any) any {
	switch v := v.(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	}

	return v
}

// outgoingSync builds the sync block of the next message to PHP with e.
// Nothing is recorded until commit runs (the message is being sent).
func (c *Conn) outgoingSync(e *Encoder) (block *php.Array, commit func(), err error) {
	s := php.NewArray()
	var commits []func()

	var updates []*php.Array
	if len(c.pendingReg) > 0 {
		list := php.NewArrayCap(len(c.pendingReg))
		for _, r := range c.pendingReg {
			list.Append(php.ArrayOf("tmp", int64(r.tmp), "h", int64(r.h)))
			e.sent[r.h] = true
			if m, ok := r.m.(Mirror); ok {
				rev := m.Rev()
				if rev != r.rev {
					// A PHP-born mirror the call that adopted it changed
					// (an input bound by the hook it was passed to): the
					// change goes with its registration, before PHP
					// changes it again from what it had.
					fields, err := m.MirrorSnapshot()
					if err != nil {
						return nil, nil, err
					}
					encoded, _, err := e.array(fields)
					if err != nil {
						return nil, nil, err
					}
					updates = append(updates, php.ArrayOf("h", int64(r.h), "r", int64(rev), "full", encoded)) //nolint:gosec // revisions stay far below 2^63.
				}
				e.newMirrors = append(e.newMirrors, &mirrorState{h: r.h, m: m, rev: rev})
			}
		}
		s.Set("reg", list)
		commits = append(commits, func() { c.pendingReg = nil })
	}

	if envOf := os.Environ(); !slices.Equal(envOf, c.sync.envOf) {
		if err := c.outgoingEnv(e, s, envOf, &commits); err != nil {
			return nil, nil, err
		}
	}

	if wd, err := php.Getcwd(); err == nil && wd != c.sync.cwd {
		v, _, _ := encodeString(wd)
		s.Set("cwd", v)
		commits = append(commits, func() { c.sync.cwd = wd })
	}

	st := php.NewArray()
	changed := map[string]any{}
	for _, name := range c.sync.staticNames {
		v := normalizeStatic(c.sync.statics[name].Get())
		if old, ok := c.sync.sentStatics[name]; ok && old == v {
			continue
		}
		ev, err := e.Value(v)
		if err != nil {
			return nil, nil, err
		}
		st.Set(name, ev)
		changed[name] = v
	}
	if st.Len() > 0 {
		s.Set("st", st)
		commits = append(commits, func() { maps.Copy(c.sync.sentStatics, changed) })
	}

	for _, ms := range c.h.mirrors {
		rev := ms.m.Rev()
		if rev == ms.rev {
			continue
		}
		u := php.ArrayOf("h", int64(ms.h), "r", int64(rev)) //nolint:gosec // revisions stay far below 2^63.
		fields, ok := (*php.Array)(nil), false
		if dm, isDelta := ms.m.(DeltaMirror); isDelta {
			fields, ok = dm.MirrorChanges(ms.rev)
		}
		key := "f"
		if !ok {
			key = "full"
			if fields, err = ms.m.MirrorSnapshot(); err != nil {
				return nil, nil, err
			}
		}
		encoded, _, err := e.array(fields)
		if err != nil {
			return nil, nil, err
		}
		u.Set(key, encoded)
		updates = append(updates, u)
		commits = append(commits, func() { ms.rev = rev })
	}
	if len(updates) > 0 {
		list := php.NewArrayCap(len(updates))
		for _, u := range updates {
			list.Append(u)
		}
		s.Set("o", list)
	}

	commit = func() {
		for _, fn := range commits {
			fn()
		}
	}
	if s.Len() == 0 {
		return nil, commit, nil
	}

	return s, commit, nil
}

// outgoingEnv adds the difference between the environment envOf and the
// agreed one to the sync block s, with what records it as agreed.
func (c *Conn) outgoingEnv(e *Encoder, s *php.Array, envOf []string, commits *[]func()) error {
	env := environ(envOf)
	*commits = append(*commits, func() { c.sync.env, c.sync.envOf = env, envOf })
	if maps.Equal(env, c.sync.env) {
		return nil
	}

	set := php.NewArray()
	for _, k := range slices.Sorted(maps.Keys(env)) {
		if old, ok := c.sync.env[k]; !ok || old != env[k] {
			set.Set(k, env[k])
		}
	}
	var unset []string
	for k := range c.sync.env {
		if _, ok := env[k]; !ok {
			unset = append(unset, k)
		}
	}
	slices.Sort(unset)

	block := php.NewArray()
	if set.Len() > 0 {
		block.Set("set", set)
	}
	if len(unset) > 0 {
		block.Set("unset", php.StringList(unset))
	}
	// Keys pass through the encoder: names need not be UTF-8.
	encoded, _, err := e.array(block)
	if err != nil {
		return err
	}
	s.Set("env", encoded)

	return nil
}

// applySync applies a decoded sync block from PHP.
func (c *Conn) applySync(v any) error {
	s, ok := v.(*php.Array)
	if !ok {
		return &ProtocolError{Message: "invalid sync block"}
	}

	if env, ok := s.GetArray("env"); ok {
		if set, ok := env.GetArray("set"); ok {
			for k, v := range set.All() {
				name, value := k.String(), php.ToString(v)
				if err := os.Setenv(name, value); err != nil {
					return err
				}
				c.sync.env[name] = value
			}
			c.sync.envOf = nil
		}
		if unset, ok := env.GetArray("unset"); ok {
			for _, v := range unset.Values() {
				name := php.ToString(v)
				if err := os.Unsetenv(name); err != nil {
					return err
				}
				delete(c.sync.env, name)
			}
			c.sync.envOf = nil
		}
	}

	if cwd, ok := s.GetString("cwd"); ok {
		if err := os.Chdir(cwd); err != nil {
			return err
		}
		c.sync.cwd = cwd
	}

	if st, ok := s.GetArray("st"); ok {
		for k, v := range st.All() {
			name := k.String()
			if static, ok := c.sync.statics[name]; ok {
				static.Set(v)
			}
			c.sync.sentStatics[name] = normalizeStatic(v)
		}
	}

	if o, ok := s.GetArray("o"); ok {
		for _, item := range o.Values() {
			u, ok := item.(*php.Array)
			if !ok {
				return &ProtocolError{Message: "invalid mirror update"}
			}
			hv, _ := u.Get("h")
			h, _ := hv.(int64)
			ms := c.mirrorState(Handle(h))
			if ms == nil {
				return unknownHandle(Handle(h))
			}
			fields, ok := u.GetArray("f")
			if !ok {
				return &ProtocolError{Message: "invalid mirror update"}
			}
			if err := ms.m.ApplyMirror(fields); err != nil {
				return err
			}
			// PHP has what Go now has: nothing to send back.
			ms.rev = ms.m.Rev()
		}
	}

	return nil
}

// mirrorState returns the mirror PHP holds as h.
func (c *Conn) mirrorState(h Handle) *mirrorState { return c.h.mirrorIdx[h] }

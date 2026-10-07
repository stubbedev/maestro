package policy

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/testutil"
	"github.com/stubbedev/maestro/internal/util"
)

// Compares FromConfig with the goldens of tools/oracle/config/policy.php.

type fakeConfig struct{ policy, audit any }

func (c fakeConfig) Get(key string, _ int) (any, error) {
	if key == "policy" {
		return c.policy, nil
	}

	return c.audit, nil
}

// decodePHP turns the oracle's encoding of a PHP value into one.
func decodePHP(t *testing.T, v any) any {
	switch v := v.(type) {
	case map[string]any:
		a := php.NewArray()
		for _, pair := range v["a"].([]any) {
			kv := pair.([]any)
			k := kv[0]
			if n, ok := k.(json.Number); ok {
				i, err := n.Int64()
				if err != nil {
					t.Fatal(err)
				}
				k = i
			}
			a.Set(k, decodePHP(t, kv[1]))
		}

		return a
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			t.Fatal(err)
		}

		return i
	}

	return v
}

var policyEnvVars = []string{
	"COMPOSER_POLICY", "COMPOSER_POLICY_ADVISORIES_BLOCK", "COMPOSER_POLICY_MALWARE_BLOCK",
	"COMPOSER_POLICY_ABANDONED_BLOCK", "COMPOSER_SECURITY_BLOCKING_ABANDONED", "COMPOSER_AUDIT_ABANDONED",
}

func errorClass(err error) string {
	if phperr.Of(err) == nil {
		return "?"
	}

	return phperr.Class(err)
}

// normalise round-trips v through JSON so it compares equal to decoded
// goldens.
func normalise(t *testing.T, v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}

	return out
}

func TestOracle_FromConfig(t *testing.T) {
	data, err := testutil.ReadGoldenFile("testdata/oracle/policy.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var cases []struct {
		Policy, Audit any
		Env           map[string]string
		Result        any
		Error         []string
	}
	if err := dec.Decode(&cases); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, name := range policyEnvVars {
			util.ClearEnv(name)
		}
	})

	failures := 0
	for i, c := range cases {
		for _, name := range policyEnvVars {
			if v, ok := c.Env[name]; ok {
				util.PutEnv(name, v)
			} else {
				util.ClearEnv(name)
			}
		}

		p, err := FromConfig(fakeConfig{decodePHP(t, c.Policy), decodePHP(t, c.Audit)})
		var got any
		if err != nil {
			got = []any{errorClass(err), err.Error()}
		} else {
			got = DumpDerived(p)
		}
		want := normalise(t, c.Result)
		if c.Error != nil {
			want = normalise(t, []any{c.Error[0], c.Error[1]})
		}
		if got = normalise(t, got); !reflect.DeepEqual(want, got) {
			failures++
			if failures <= 5 {
				w, _ := json.Marshal(want)
				g, _ := json.Marshal(got)
				t.Errorf("case %d (env %v):\nwant %s\n got %s", i, c.Env, w, g)
			}
		}
	}
	if failures > 0 {
		t.Errorf("%d of %d cases differ", failures, len(cases))
	}
}

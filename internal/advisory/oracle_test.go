package advisory_test

import (
	"os"
	"testing"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
)

// TestAuditor_Oracle runs the Auditor over the cases of
// tools/oracle/advisory/oracle.php and compares the exit code and output
// with what Composer's Auditor produced.
func TestAuditor_Oracle(t *testing.T) {
	data, err := os.ReadFile("testdata/oracle/audit.json")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := php.JSONDecode(string(data), true)
	if err != nil {
		t.Fatal(err)
	}
	golden := decoded.(*php.Array)
	repoConfig, _ := golden.GetArray("repo")

	n := 0
	for k, v := range golden.All() {
		name := k.String()
		if name == "repo" {
			continue
		}
		n++
		c := v.(*php.Array)
		t.Run(name, func(t *testing.T) {
			var packages []pkg.PackageInterface
			list, _ := c.GetArray("packages")
			for _, raw := range list.All() {
				fields := raw.(*php.Array).Values()
				p := pkg.NewCompletePackage(fields[0].(string), fields[1].(string), fields[2].(string))
				p.SetAbandoned(fields[3])
				if homepage, ok := fields[4].(string); ok {
					p.SetHomepage(pkg.Str(homepage))
				}
				packages = append(packages, p)
			}

			cfg := config.New(false, "")
			if raw, _ := c.GetArray("policy"); raw.Len() > 0 {
				if err := cfg.Merge(php.ArrayOf("config", php.ArrayOf("policy", raw)), "test"); err != nil {
					t.Fatal(err)
				}
			}
			policyConfig, err := policy.FromConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}

			repo, err := repository.NewPackageRepository(repoConfig.Clone())
			if err != nil {
				t.Fatal(err)
			}
			repoSet, err := repository.NewRepositorySet("stable", nil, nil, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := repoSet.AddRepository(repo); err != nil {
				t.Fatal(err)
			}
			providerSet, err := filterlist.CreateFilterListProviderSet(policyConfig, []repository.RepositoryInterface{repo}, nil)
			if err != nil {
				t.Fatal(err)
			}

			format, _ := c.GetString("format")
			warningOnly, _ := c.Get("warningOnly")
			b := newBufferIO(t)
			result, err := advisory.Auditor{}.Audit(b, repoSet, policyConfig, packages, format, warningOnly == true, providerSet)

			want, _ := c.Get("result")
			if e, ok := want.(*php.Array); ok {
				if err == nil {
					t.Fatalf("no error, want %v", e)
				}
				msg, _ := e.Values()[1].(string)
				if err.Error() != msg {
					t.Errorf("error %q, want %q", err, msg)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if int64(result) != want {
					t.Errorf("result = %d, want %v", result, want)
				}
			}
			// The goldens were recorded where PHP_EOL is "\n".
			if wantOutput, _ := c.GetString("output"); php.NormalizeEOL(b.Output()) != wantOutput {
				t.Errorf("output:\n%s\nwant:\n%s", b.Output(), wantOutput)
			}
		})
	}
	if n == 0 {
		t.Fatal("no cases")
	}
}

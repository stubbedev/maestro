package filter

// Ports tests/Composer/Test/Filter/PlatformRequirementFilter/*Test.php.

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

func TestIgnoreAllPlatformRequirementFilter_IsIgnored(t *testing.T) {
	for name, tc := range map[string]struct {
		req    string
		expect bool
	}{
		"php is ignored":                 {"php", true},
		"monolog/monolog is not ignored": {"monolog/monolog", false},
	} {
		t.Run(name, func(t *testing.T) {
			f := IgnoreAll{}
			if got := f.IsIgnored(tc.req); got != tc.expect {
				t.Errorf("IsIgnored(%q) = %v", tc.req, got)
			}
			if got := f.IsUpperBoundIgnored(tc.req); got != tc.expect {
				t.Errorf("IsUpperBoundIgnored(%q) = %v", tc.req, got)
			}
		})
	}
}

func TestIgnoreListPlatformRequirementFilter_IsIgnored(t *testing.T) {
	for name, tc := range map[string]struct {
		list   []string
		req    string
		expect bool
	}{
		"ext-json is ignored if listed":                                    {[]string{"ext-json", "monolog/monolog"}, "ext-json", true},
		"php is not ignored if not listed":                                 {[]string{"ext-json", "monolog/monolog"}, "php", false},
		"monolog/monolog is not ignored even if listed":                    {[]string{"ext-json", "monolog/monolog"}, "monolog/monolog", false},
		"ext-json is ignored if ext-* is listed":                           {[]string{"ext-*"}, "ext-json", true},
		"php is ignored if php* is listed":                                 {[]string{"ext-*", "php*"}, "php", true},
		"ext-json is ignored if * is listed":                               {[]string{"foo", "*"}, "ext-json", true},
		"php is ignored if * is listed":                                    {[]string{"*", "foo"}, "php", true},
		"monolog/monolog is not ignored even if * or monolog/* are listed": {[]string{"*", "monolog/*"}, "monolog/monolog", false},
		"empty list entry does not ignore":                                 {[]string{""}, "ext-foo", false},
		"empty array does not ignore":                                      {[]string{}, "ext-foo", false},
		"list entries are not completing each other":                       {[]string{"ext-", "foo"}, "ext-foo", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := NewIgnoreList(tc.list).IsIgnored(tc.req); got != tc.expect {
				t.Errorf("IsIgnored(%q) = %v", tc.req, got)
			}
		})
	}
}

func TestIgnoreListPlatformRequirementFilter_IsUpperBoundIgnored(t *testing.T) {
	for name, tc := range map[string]struct {
		list   []string
		req    string
		expect bool
	}{
		"ext-json is ignored if listed and fully ignored":                  {[]string{"ext-json", "monolog/monolog"}, "ext-json", true},
		"ext-json is ignored if listed and upper bound ignored":            {[]string{"ext-json+", "monolog/monolog"}, "ext-json", true},
		"php is not ignored if not listed":                                 {[]string{"ext-json+", "monolog/monolog"}, "php", false},
		"monolog/monolog is not ignored even if listed":                    {[]string{"monolog/monolog"}, "monolog/monolog", false},
		"ext-json is ignored if ext-* is listed":                           {[]string{"ext-*+"}, "ext-json", true},
		"php is ignored if php* is listed":                                 {[]string{"ext-*+", "php*+"}, "php", true},
		"ext-json is ignored if * is listed":                               {[]string{"foo", "*+"}, "ext-json", true},
		"php is ignored if * is listed":                                    {[]string{"*+", "foo"}, "php", true},
		"monolog/monolog is not ignored even if * or monolog/* are listed": {[]string{"*+", "monolog/*+"}, "monolog/monolog", false},
		"empty list entry does not ignore":                                 {[]string{""}, "ext-foo", false},
		"empty array does not ignore":                                      {[]string{}, "ext-foo", false},
		"list entries are not completing each other":                       {[]string{"ext-", "foo"}, "ext-foo", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := NewIgnoreList(tc.list).IsUpperBoundIgnored(tc.req); got != tc.expect {
				t.Errorf("IsUpperBoundIgnored(%q) = %v", tc.req, got)
			}
		})
	}
}

func TestIgnoreNothingPlatformRequirementFilter_IsIgnored(t *testing.T) {
	for _, req := range []string{"php", "monolog/monolog"} {
		f := IgnoreNothing{}
		if f.IsIgnored(req) || f.IsUpperBoundIgnored(req) {
			t.Errorf("%q is ignored", req)
		}
	}
}

func TestPlatformRequirementFilterFactory_FromBoolOrList(t *testing.T) {
	for name, tc := range map[string]struct {
		in    any
		check func(PlatformRequirementFilter) bool
	}{
		"true creates IgnoreAllFilter":      {true, func(f PlatformRequirementFilter) bool { _, ok := f.(IgnoreAll); return ok }},
		"false creates IgnoreNothingFilter": {false, func(f PlatformRequirementFilter) bool { _, ok := f.(IgnoreNothing); return ok }},
		"list creates IgnoreListFilter":     {[]string{"php", "ext-json"}, func(f PlatformRequirementFilter) bool { _, ok := f.(*IgnoreList); return ok }},
	} {
		t.Run(name, func(t *testing.T) {
			f, err := FromBoolOrList(tc.in)
			if err != nil || !tc.check(f) {
				t.Errorf("FromBoolOrList(%v) = %T, %v", tc.in, f, err)
			}
		})
	}
}

func TestPlatformRequirementFilterFactory_FromBoolThrowsExceptionIfTypeIsUnknown(t *testing.T) {
	_, err := FromBoolOrList(nil)
	var iae *util.InvalidArgumentError
	if !errors.As(err, &iae) || iae.Message != "PlatformRequirementFilter: Unknown $boolOrList parameter null. Please report at https://github.com/composer/composer/issues/new." {
		t.Errorf("err = %v", err)
	}
}

func TestPlatformRequirementFilterFactory_IgnoreAll(t *testing.T) {
	if _, ok := IgnoreAllFilter().(version.IgnoreAllPlatformRequirementFilter); !ok {
		t.Error("not an IgnoreAllPlatformRequirementFilter")
	}
}

func TestPlatformRequirementFilterFactory_IgnoreNothing(t *testing.T) {
	if _, ok := IgnoreNothingFilter().(IgnoreNothing); !ok {
		t.Error("not an IgnoreNothingPlatformRequirementFilter")
	}
}

func TestIgnoreList_FilterConstraint(t *testing.T) {
	var _ version.IgnoreListPlatformRequirementFilter = (*IgnoreList)(nil)
	parse := func(s string) semver.ConstraintInterface {
		c, err := pkg.NewVersionParser().ParseConstraints(s)
		if err != nil {
			t.Fatal(err)
		}

		return c
	}
	f := NewIgnoreList([]string{"php+", "ext-json"})
	for _, tc := range []struct{ req, constraint, want string }{
		{"php", "^7.2", "[[>= 7.2.0.0-dev < 8.0.0.0-dev] || >= 8.0.0.0-dev]"},
		{"php", ">=7.2", ">= 7.2.0.0-dev"},
		{"ext-json", "^1.0", "^1.0"},
		{"ext-mbstring", "^1.0", "[>= 1.0.0.0-dev < 2.0.0.0-dev]"},
		{"foo/bar", "^1.0", "[>= 1.0.0.0-dev < 2.0.0.0-dev]"},
	} {
		got := f.FilterConstraint(tc.req, parse(tc.constraint))
		s := got.String()
		if tc.want == "^1.0" {
			s = got.PrettyString()
		}
		if s != tc.want {
			t.Errorf("FilterConstraint(%s, %s) = %s, want %s", tc.req, tc.constraint, s, tc.want)
		}
	}
}

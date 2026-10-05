package platformmock

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/platform"
)

// The shapes of PlatformRepositoryTest's mocks.
func TestRuntime_PlatformRepositoryTestShapes(t *testing.T) {
	has, get := ConstantMap(map[string]any{"PHP_VERSION": "7.0.0", "PHP_DEBUG": false, "ZipArchive::LIBZIP_VERSION": "1.5.0", "NULLED": nil})

	m := &Runtime{
		HasConstantFunc: has,
		GetConstantFunc: get,
		InvokeFunc: InvokeMap([]InvokeEntry{
			{platform.Func("inet_pton"), []any{"::"}, false},
			{platform.Func("curl_version"), nil, php.ArrayOf("version", "8.1.2")},
			{platform.StaticMethod("IntlChar", "getUnicodeVersion"), []any{}, php.ListOf(int64(7), int64(0), int64(0), int64(0))},
		}),
	}

	var rt platform.Runtime = m

	if !rt.HasConstant("LIBZIP_VERSION", "ZipArchive") || rt.HasConstant("NULLED", "") || rt.HasConstant("NOPE", "") {
		t.Error("HasConstant")
	}

	if v, _ := rt.GetConstant("PHP_VERSION", ""); v != "7.0.0" {
		t.Errorf("GetConstant = %v", v)
	}

	if v, _ := rt.GetConstant("NOPE", ""); v != nil {
		t.Errorf("GetConstant(NOPE) = %v", v)
	}

	if v, _ := rt.Invoke(platform.Func("inet_pton"), "::"); v != false {
		t.Errorf("Invoke(inet_pton) = %v", v)
	}

	if v, _ := rt.Invoke(platform.Func("inet_pton"), "::1"); v != nil {
		t.Errorf("unmapped Invoke = %v", v)
	}

	if v, _ := rt.Invoke(platform.Func("curl_version")); v == nil {
		t.Error("Invoke(curl_version) without arguments")
	}

	if v, _ := rt.Invoke(platform.StaticMethod("IntlChar", "getUnicodeVersion")); v == nil {
		t.Error("Invoke(IntlChar::getUnicodeVersion)")
	}

	// Methods without expectations return the PHP zero value.
	if rt.HasClass("Imagick") || rt.HasFunction("x") || len(rt.GetExtensions()) != 0 || rt.GetExtensionVersion("x") != "" {
		t.Error("defaults")
	}

	if info, err := rt.GetExtensionInfo("x"); info != "" || err != nil {
		t.Error("GetExtensionInfo default")
	}

	if v, err := rt.Construct("Imagick"); v != nil || err != nil {
		t.Error("Construct default")
	}

	// expects(self::once())->method('invoke')->with('inet_pton', ['::'])
	if calls := m.CallsTo("invoke"); len(calls) != 4 || calls[0].Arguments[0] != platform.Func("inet_pton") {
		t.Errorf("invoke calls: %v", calls)
	}

	if len(m.Calls()) != 15 {
		t.Errorf("%d calls", len(m.Calls()))
	}

	var d platform.HhvmVersionDetector = HhvmDetector{Version: "2.1.0"}
	if d.GetVersion() != "2.1.0" {
		t.Error("HhvmDetector")
	}
}

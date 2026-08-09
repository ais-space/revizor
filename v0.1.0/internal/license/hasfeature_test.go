package license

import "testing"

func TestHasFeatureInjectApplyWithMcpFull(t *testing.T) {
	lic := &License{
		Tier: "enterprise",
		Lim: Limitations{
			Features: []string{"mcp", "mcp_full", "mcp_basic", "inject", "inject_apply", "postgres", "sse", "analytics"},
		},
	}
	if !HasFeature(lic, "inject_apply") {
		t.Error("inject_apply should be allowed with mcp_full")
	}
}

func TestHasFeatureInjectApplyWithMcpFullOnly(t *testing.T) {
	lic := &License{
		Tier: "pro",
		Lim: Limitations{
			Features: []string{"mcp_full"},
		},
	}
	if !HasFeature(lic, "inject_apply") {
		t.Error("inject_apply should be allowed with mcp_full only")
	}
}

func TestHasFeatureCommunityNoInjectApply(t *testing.T) {
	if HasFeature(nil, "inject_apply") {
		t.Error("inject_apply should NOT be allowed in Community mode")
	}
}

// --- VERSION-001: MaxBinaryVersion tests ---

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"0.1.0", "0.1.0", 0},
		{"1.0.0", "0.9.9", 1},
		{"0.1.0", "0.2.0", -1},
		{"0.1.0", "0.1.1", -1},
		{"2.0.0", "1.5.3", 1},
		{"0.0.1", "0.0.2", -1},
		{"1.2.3", "1.2.3", 0},
	}
	for _, tt := range tests {
		result := compareVersions(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestMaxBinaryVersionOk(t *testing.T) {
	lic := &License{
		Exp:              -1,
		MaxBinaryVersion: "1.0.0",
	}
	err := ValidateMaxBinaryVersion(lic, "0.9.0")
	if err != nil {
		t.Errorf("version 0.9.0 should be <= max 1.0.0, got error: %v", err)
	}
}

func TestMaxBinaryVersionEqual(t *testing.T) {
	lic := &License{
		Exp:              -1,
		MaxBinaryVersion: "1.0.0",
	}
	err := ValidateMaxBinaryVersion(lic, "1.0.0")
	if err != nil {
		t.Errorf("version 1.0.0 should be == max 1.0.0, got error: %v", err)
	}
}

func TestMaxBinaryVersionExceeded(t *testing.T) {
	lic := &License{
		Exp:              -1,
		MaxBinaryVersion: "0.9.0",
	}
	err := ValidateMaxBinaryVersion(lic, "1.0.0")
	if err == nil {
		t.Error("version 1.0.0 should exceed max 0.9.0, but no error returned")
	}
}

func TestMaxBinaryVersionNotPerpetual(t *testing.T) {
	lic := &License{
		Exp:              1735689600, // не perpetual
		MaxBinaryVersion: "0.1.0",
	}
	err := ValidateMaxBinaryVersion(lic, "99.0.0")
	if err != nil {
		t.Errorf("non-perpetual license should not check version, got error: %v", err)
	}
}

func TestMaxBinaryVersionEmpty(t *testing.T) {
	lic := &License{
		Exp:              -1,
		MaxBinaryVersion: "", // не задано
	}
	err := ValidateMaxBinaryVersion(lic, "99.0.0")
	if err != nil {
		t.Errorf("empty max_ver should be unlimited, got error: %v", err)
	}
}

func TestMaxBinaryVersionNilLicense(t *testing.T) {
	err := ValidateMaxBinaryVersion(nil, "1.0.0")
	if err != nil {
		t.Errorf("nil license should pass, got error: %v", err)
	}
}

func TestMaxBinaryVersionDevBuild(t *testing.T) {
	lic := &License{
		Exp:              -1,
		MaxBinaryVersion: "0.1.0",
	}
	// dev-сборка должна проходить без ограничений
	err := ValidateMaxBinaryVersion(lic, "dev")
	if err != nil {
		t.Errorf("dev build should pass without version check, got error: %v", err)
	}
	err = ValidateMaxBinaryVersion(lic, "")
	if err != nil {
		t.Errorf("empty version should pass, got error: %v", err)
	}
}

func TestValidateWithMaxBinaryVersion(t *testing.T) {
	lic := &License{
		Exp:              -1,
		Tier:             "pro",
		MaxBinaryVersion: "0.9.0",
	}
	// Версия больше max_ver — Validate должна вернуть ошибку
	err := Validate(lic, "", "1.0.0")
	if err == nil {
		t.Error("Validate should fail when binary version exceeds max_ver")
	}

	// Версия в пределах — OK
	lic2 := &License{
		Exp:              -1,
		Tier:             "pro",
		MaxBinaryVersion: "1.0.0",
	}
	err = Validate(lic2, "", "1.0.0")
	if err != nil {
		t.Errorf("Validate should pass when version <= max_ver, got: %v", err)
	}
}

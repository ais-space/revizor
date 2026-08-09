package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAudit_ModuleHasTraceImport(t *testing.T) {
	tmpDir := t.TempDir()
	modDir := filepath.Join(tmpDir, "test_module_0_1_0")
	mustMkdir(t, modDir)

	mustWrite(t, filepath.Join(modDir, "test_module_0_1_0.py"),
		"from modules.revizor_core_py_0_1_0 import trace, trace_start\n\ndef foo():\n    trace('test_module.foo.enter')\n    return 42\n")

	result, err := auditModule(modDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Error("expected OK=true, got false")
	}
	if result.TotalFiles != 1 {
		t.Errorf("expected 1 file, got %d", result.TotalFiles)
	}
}

func TestAudit_ModuleHasTraceImportShortForm(t *testing.T) {
	tmpDir := t.TempDir()
	modDir := filepath.Join(tmpDir, "test_mod_0_1_0")
	mustMkdir(t, modDir)

	// Короткая форма импорта (from revizor_core_py_0_1_0 import ...)
	mustWrite(t, filepath.Join(modDir, "test_mod_0_1_0.py"),
		"from revizor_core_py_0_1_0 import trace\n\nclass Foo:\n    pass\n")

	result, err := auditModule(modDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Error("expected OK=true for short import form")
	}
}

func TestAudit_ModuleMissingTraceImport(t *testing.T) {
	tmpDir := t.TempDir()
	modDir := filepath.Join(tmpDir, "no_trace_0_1_0")
	mustMkdir(t, modDir)

	mustWrite(t, filepath.Join(modDir, "no_trace_0_1_0.py"),
		"def foo():\n    return 42\n")

	result, err := auditModule(modDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Error("expected OK=false for module without trace import")
	}
	if len(result.FilesMissing) != 1 {
		t.Errorf("expected 1 missing file, got %d", len(result.FilesMissing))
	}
	if result.FilesMissing[0] != "no_trace_0_1_0.py" {
		t.Errorf("expected 'no_trace_0_1_0.py', got '%s'", result.FilesMissing[0])
	}
}

func TestAudit_MixedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	modDir := filepath.Join(tmpDir, "mixed_0_1_0")
	mustMkdir(t, modDir)

	// Файл с импортом
	mustWrite(t, filepath.Join(modDir, "mixed_0_1_0.py"),
		"from modules.revizor_core_py_0_1_0 import trace\n\ndef foo(): pass\n")

	// Файл без импорта
	mustWrite(t, filepath.Join(modDir, "utils.py"),
		"def bar():\n    return 1\n")

	result, err := auditModule(modDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Error("expected OK=false for mixed module")
	}
	if len(result.FilesMissing) != 1 {
		t.Errorf("expected 1 missing file, got %d", len(result.FilesMissing))
	}
	if result.FilesMissing[0] != "utils.py" {
		t.Errorf("expected 'utils.py', got '%s'", result.FilesMissing[0])
	}
	if result.TotalFiles != 2 {
		t.Errorf("expected 2 total files, got %d", result.TotalFiles)
	}
}

func TestAudit_SkipInitPy(t *testing.T) {
	tmpDir := t.TempDir()
	modDir := filepath.Join(tmpDir, "skip_init_0_1_0")
	mustMkdir(t, modDir)

	mustWrite(t, filepath.Join(modDir, "__init__.py"), "")                             // должен быть пропущен
	mustWrite(t, filepath.Join(modDir, "skip_init_0_1_0.py"), "def foo(): pass\n") // без импорта

	result, err := auditModule(modDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalFiles != 1 {
		t.Errorf("expected 1 file (__init__.py skipped), got %d", result.TotalFiles)
	}
}

func TestAudit_SkipTestDir(t *testing.T) {
	tmpDir := t.TempDir()
	modDir := filepath.Join(tmpDir, "skip_tests_0_1_0")
	mustMkdir(t, modDir)
	mustMkdir(t, filepath.Join(modDir, "tests"))

	mustWrite(t, filepath.Join(modDir, "tests", "test_foo.py"), "def test(): pass\n")

	result, err := auditModule(modDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalFiles != 0 {
		t.Errorf("expected 0 files (tests/ skipped), got %d", result.TotalFiles)
	}
}

func TestAudit_SkipCacheDirs(t *testing.T) {
	tmpDir := t.TempDir()
	modDir := filepath.Join(tmpDir, "skip_cache_0_1_0")
	mustMkdir(t, modDir)
	mustMkdir(t, filepath.Join(modDir, "__pycache__"))
	mustMkdir(t, filepath.Join(modDir, ".pytest_cache"))

	mustWrite(t, filepath.Join(modDir, "__pycache__", "cached.pyc"), "binary")
	mustWrite(t, filepath.Join(modDir, ".pytest_cache", "cache.json"), "{}")

	result, err := auditModule(modDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalFiles != 0 {
		t.Errorf("expected 0 files (cache dirs skipped), got %d", result.TotalFiles)
	}
}

func TestAudit_isRevizorModule(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"revizor_core_py_0_1_0", true},
		{"revizor_core_0_1_0", true},
		{"revizor_middleware_0_1_0", true},
		{"revizor_api_0_1_0", true},
		{"revizor_mcp_0_1_0", true},
		{"auth_0_1_0", false},
		{"my_module_0_1_0", false},
	}
	for _, tt := range tests {
		if got := isExcludedModule(tt.name, nil); got != tt.expected {
			t.Errorf("isExcludedModule(%q) = %v, want %v", tt.name, got, tt.expected)
		}
	}
}

func TestAudit_hasTraceImport(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "full import",
			content:  "from modules.revizor_core_py_0_1_0 import trace, trace_start\n\ndef foo(): pass\n",
			expected: true,
		},
		{
			name:     "short import",
			content:  "from revizor_core_py_0_1_0 import trace\n\nclass Foo: pass\n",
			expected: true,
		},
		{
			name:     "no import",
			content:  "import os\n\ndef foo(): pass\n",
			expected: false,
		},
		{
			name:     "wrong module",
			content:  "from modules.revizor_core_ts_0_1_0 import trace\n",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filepath.Join(tmpDir, tt.name+".py")
			mustWrite(t, f, tt.content)
			if got := hasTraceImport(f, nil); got != tt.expected {
				t.Errorf("hasTraceImport() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// --- Helpers ---

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

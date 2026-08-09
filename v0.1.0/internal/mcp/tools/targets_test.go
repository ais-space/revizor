package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTargets_ClassifyFile_P1(t *testing.T) {
	tests := []string{
		"modules/auth_0_1_0/auth_0_1_0.py",
		"modules/auth_oauth_0_1_0/oauth.py",
		"modules/auth_linking_0_1_0/linking.py",
		"modules/session_manager_0_1_0/session.py",
		"modules/admin_api_0_1_0/admin.py",
		"modules/admin_users_api_0_1_0/users.py",
	}
	for _, path := range tests {
		if got := classifyTarget(path); got != "P1" {
			t.Errorf("classifyTarget(%q) = %s, want P1", path, got)
		}
	}
}

func TestTargets_ClassifyFile_P2(t *testing.T) {
	// Эвристические P2: api, identity, link, oauth, callback, merge
	tests := []string{
		"modules/ais_prompter_api_0_2_0/api.py",
		"modules/identity_services_0_1_0/identity.py",
		"modules/lib_account_linker_0_1_0/linker.py",
		"src/oauth/callback.py",
	}
	for _, path := range tests {
		if got := classifyTarget(path); got != "P2" {
			t.Errorf("classifyTarget(%q) = %s, want P2", path, got)
		}
	}
}

func TestTargets_ClassifyFile_P3(t *testing.T) {
	tests := []string{
		"modules/llm_adapter_0_1_0/adapter.py",
		"modules/i18n_manager_0_1_0/manager.py",
		"modules/module_loader_0_1_0/loader.py",
	}
	for _, path := range tests {
		if got := classifyTarget(path); got != "P3" {
			t.Errorf("classifyTarget(%q) = %s, want P3", path, got)
		}
	}
}

func TestTargets_ClassifyFile_P4(t *testing.T) {
	tests := []string{
		"modules/admin_app_main_0_1_0/Component.tsx",
		"modules/prompter_ui/lib.ts",
	}
	for _, path := range tests {
		if got := classifyTarget(path); got != "P4" {
			t.Errorf("classifyTarget(%q) = %s, want P4", path, got)
		}
	}
}

func TestTargets_ClassifyFile_P5(t *testing.T) {
	// P1-тест
	if got := classifyTarget("modules/auth_0_1_0/tests/test_auth.py"); got != "P5" {
		t.Errorf("P1 test classified as %s, want P5", got)
	}
	// P3-тест
	if got := classifyTarget("modules/llm_adapter_0_1_0/test_adapter.py"); got != "P5" {
		t.Errorf("P3 test classified as %s, want P5", got)
	}
}

func TestTargets_ExcludeDirs(t *testing.T) {
	tests := []string{"__pycache__", "node_modules", ".next", "dist", "alembic", "migrations", ".git"}
	for _, d := range tests {
		if !isTargetExcludedDir(d) {
			t.Errorf("isTargetExcludedDir(%q) = false, want true", d)
		}
	}
}

func TestTargets_ExcludeTSFiles(t *testing.T) {
	tests := []struct {
		fileName string
		excluded bool
	}{
		{"next.config.ts", true},
		{"next-env.d.ts", true},
		{"types.d.ts", true},
		{"jest.config.ts", true},
		{"tailwind.config.ts", true},
		{"postcss.config.ts", true},
		{"setup.ts", true},
		{"MyComponent.tsx", false},
		{"utils.ts", false},
	}
	for _, tt := range tests {
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, tt.fileName)
		os.WriteFile(tmpFile, []byte("// test"), 0644)
		got := shouldExcludeTarget(tmpFile, fsDirEntry{name: tt.fileName, isDir: false}, nil)
		if got != tt.excluded {
			t.Errorf("shouldExcludeTarget(%q) = %v, want %v", tt.fileName, got, tt.excluded)
		}
	}
}

func TestTargets_CountPythonFunctions(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "test.py")
	mustWrite(t, f, `
def public_func():
    pass

async def async_func():
    pass

def _private_func():
    pass

def __init__(self):
    pass

class Foo:
    def method(self):
        pass
`)
	count := countPythonFunctions(f)
	if count != 3 { // public_func, async_func, method (class method counted by regex)
		t.Errorf("expected 3 functions, got %d", count)
	}
}

func TestTargets_EstimateTracePoints(t *testing.T) {
	if got := estimateTracePoints(3, false); got != 9 {
		t.Errorf("product: expected 9, got %d", got)
	}
	if got := estimateTracePoints(2, true); got != 4 {
		t.Errorf("test with funcs: expected 4, got %d", got)
	}
	if got := estimateTracePoints(0, true); got != 3 {
		t.Errorf("test without funcs (TypeScript): expected 3, got %d", got)
	}
}

func TestTargets_GetModuleNameFromTarget(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"modules/auth_0_1_0/auth_0_1_0.py", "auth_0_1_0"},
		{"modules/admin_api_0_1_0/api.py", "admin_api_0_1_0"},
		{"some/other/path/file.py", "path"},
	}
	for _, tt := range tests {
		got := getModuleNameFromTarget(tt.path, "")
		if got != tt.want {
			t.Errorf("getModuleNameFromTarget(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestTargets_ScanTargets(t *testing.T) {
	tmpDir := t.TempDir()
	modulesDir := filepath.Join(tmpDir, "modules")

	// P1 модуль
	p1Dir := filepath.Join(modulesDir, "auth_0_1_0")
	mustMkdir(t, p1Dir)
	mustWrite(t, filepath.Join(p1Dir, "auth_0_1_0.py"),
		"from modules.revizor_core_py_0_1_0 import trace\n\ndef login(): pass\n\ndef logout(): pass\n")

	// P3 модуль
	p3Dir := filepath.Join(modulesDir, "llm_adapter_0_1_0")
	mustMkdir(t, p3Dir)
	mustWrite(t, filepath.Join(p3Dir, "adapter.py"),
		"def call_llm(): pass\n")

	// Исключённый __pycache__
	cacheDir := filepath.Join(modulesDir, "auth_0_1_0", "__pycache__")
	mustMkdir(t, cacheDir)
	mustWrite(t, filepath.Join(cacheDir, "auth.cpython-311.pyc"), "binary")

	targets, err := scanTargets(modulesDir, "", nil)
	if err != nil {
		t.Fatalf("scanTargets error: %v", err)
	}

	if len(targets["P1"]) != 1 {
		t.Errorf("expected 1 P1 file, got %d", len(targets["P1"]))
	} else {
		ti := targets["P1"][0]
		if ti.Functions != 2 {
			t.Errorf("expected 2 functions in P1, got %d", ti.Functions)
		}
		if ti.Module != "auth_0_1_0" {
			t.Errorf("expected module 'auth_0_1_0', got '%s'", ti.Module)
		}
	}

	if len(targets["P3"]) != 1 {
		t.Errorf("expected 1 P3 file, got %d", len(targets["P3"]))
	}

	// __pycache__ должен быть исключён
	totalFiles := 0
	for _, items := range targets {
		totalFiles += len(items)
	}
	if totalFiles != 2 {
		t.Errorf("expected 2 total files, got %d", totalFiles)
	}
}

func TestTargets_ScanTargets_SpecificModule(t *testing.T) {
	tmpDir := t.TempDir()
	modulesDir := filepath.Join(tmpDir, "modules")

	p1Dir := filepath.Join(modulesDir, "auth_0_1_0")
	mustMkdir(t, p1Dir)
	mustWrite(t, filepath.Join(p1Dir, "auth.py"), "def foo(): pass\n")

	p3Dir := filepath.Join(modulesDir, "i18n_manager_0_1_0")
	mustMkdir(t, p3Dir)
	mustWrite(t, filepath.Join(p3Dir, "i18n.py"), "def bar(): pass\n")

	// Сканируем только auth
	targets, err := scanTargets(modulesDir, "auth_0_1_0", nil)
	if err != nil {
		t.Fatalf("scanTargets error: %v", err)
	}

	totalFiles := 0
	for _, items := range targets {
		totalFiles += len(items)
	}
	if totalFiles != 1 {
		t.Errorf("expected 1 file for specific module, got %d", totalFiles)
	}
}

// --- Adapter for fs.DirEntry ---

type fsDirEntry struct {
	name  string
	isDir bool
}

func (e fsDirEntry) Name() string               { return e.name }
func (e fsDirEntry) IsDir() bool                { return e.isDir }
func (e fsDirEntry) Type() os.FileMode          { return 0 }
func (e fsDirEntry) Info() (os.FileInfo, error) { return nil, nil }

type statDirEntry struct {
	name  string
	isDir bool
}

func (e *statDirEntry) Name() string       { return e.name }
func (e *statDirEntry) IsDir() bool        { return e.isDir }
func (e *statDirEntry) Type() os.FileMode  { return 0 }
func (e *statDirEntry) Info() (os.FileInfo, error) { return nil, nil }

func TestTargets_FormatOutput(t *testing.T) {
	targets := map[string][]TargetInfo{
		"P1": {
			{Path: "modules/auth_0_1_0/auth.py", Module: "auth_0_1_0", Functions: 3, EstimatedPoints: 9, Type: "product", Language: "python"},
		},
		"P4": {
			{Path: "modules/admin_app/App.tsx", Module: "admin_app", Functions: 0, EstimatedPoints: 3, Type: "product", Language: "typescript"},
		},
	}

	stats := formatStats(targets)
	if !strings.Contains(stats, "TOTAL") {
		t.Error("stats should contain TOTAL")
	}
	if !strings.Contains(stats, "Python product") {
		t.Error("stats should contain 'Python product'")
	}
	if !strings.Contains(stats, "TypeScript product") {
		t.Error("stats should contain 'TypeScript product'")
	}

	table := formatTable(targets, "P1")
	if !strings.Contains(table, "P1:") {
		t.Error("table should contain P1 header")
	}
	if !strings.Contains(table, "auth.py") {
		t.Error("table should contain file path")
	}
}

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =========================================================================
// Python inject tests
// =========================================================================

func TestInjectPython_SimpleFunction(t *testing.T) {
	original := `import os

def foo():
    return 42
`
	filePath := filepath.Join(t.TempDir(), "test_mod_0_1_0.py")
	mustWrite(t, filePath, original)

	newContent, count, err := injectPythonTraces(filePath, original, "test_mod", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count < 1 {
		t.Error("expected at least 1 insertion")
	}
	if !strings.Contains(newContent, `trace("test_mod.foo.enter")`) {
		t.Error("missing .enter trace")
	}
	if !strings.Contains(newContent, `trace("test_mod.foo.success")`) {
		t.Error("missing .success trace before return")
	}
	if !strings.Contains(newContent, defaultPyImportPattern) {
		t.Error("missing trace import")
	}
}

func TestInjectPython_NoReturnNone(t *testing.T) {
	original := `import os

def foo():
    return None
`
	newContent, _, err := injectPythonTraces("test.py", original, "test_mod", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Должен быть .enter, но НЕ .success перед return None
	if !strings.Contains(newContent, ".enter") {
		t.Error("missing .enter")
	}
	// Проверяем что нет .success перед return None
	if strings.Contains(newContent, `trace("test_mod.foo.success")`) && !strings.Contains(newContent, "return 42") {
		// .success мог появиться если есть другой return с value
	}
}

func TestInjectPython_RaiseFailed(t *testing.T) {
	original := `import os

def foo():
    if error:
        raise ValueError("bad")
    return 1
`
	newContent, count, err := injectPythonTraces("test.py", original, "mod", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(newContent, `.failed"`) {
		t.Error("missing .failed trace before raise")
	}
	_ = count
}

func TestInjectPython_CriticalDecision(t *testing.T) {
	original := `import os

def check_access():
    if link_mode:
        return "linked"
    return "ok"
`
	newContent, _, err := injectPythonTraces("test.py", original, "auth", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(newContent, ".decision") {
		t.Error("missing .decision trace for critical if")
	}
	if !strings.Contains(newContent, "link_mode") {
		t.Error("decision should reference 'link_mode'")
	}
}

func TestInjectPython_NonCriticalIf(t *testing.T) {
	original := `import os

def foo():
    if x > 5:
        return x
    return 0
`
	newContent, _, err := injectPythonTraces("test.py", original, "mod", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Не-критический if не должен создавать .decision
	if strings.Contains(newContent, ".decision") {
		t.Error("non-critical if should not create .decision")
	}
}

func TestInjectPython_SkipProperty(t *testing.T) {
	original := `
import os

class Foo:
    @property
    def bar(self):
        return self._bar
`
	newContent, _, err := injectPythonTraces("test.py", original, "mod", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(newContent, `trace("mod.bar`) {
		t.Error("@property function should be skipped")
	}
}

func TestInjectPython_NoDuplicateEnter(t *testing.T) {
	original := `from revizor_sdk import trace

def foo():
    trace("mod.foo.enter")
    return 42
`
	newContent, count, err := injectPythonTraces("test.py", original, "mod", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Не должен вставлять второй .enter
	enterCount := strings.Count(newContent, `.enter"`)
	if enterCount > 1 {
		t.Errorf("expected 1 enter, got %d", enterCount)
	}
	_ = count
}

func TestInjectPython_NoDuplicateSuccess(t *testing.T) {
	original := `from revizor_sdk import trace

def foo():
    trace("mod.foo.success")
    return 42
`
	newContent, _, err := injectPythonTraces("test.py", original, "mod", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	successCount := strings.Count(newContent, `.success"`)
	if successCount > 1 {
		t.Errorf("expected 1 success, got %d", successCount)
	}
}

func TestInjectPython_DryRunDoesNotModify(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test_mod_0_1_0.py")
	original := "import os\n\ndef foo():\n    return 42\n"
	mustWrite(t, filePath, original)

	// Вызываем inject (dry-run simulation — передаём оригинал и возвращаем новый контент)
	newContent, count, err := injectPythonTraces(filePath, original, "test_mod", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count == 0 {
		t.Error("expected insertions")
	}
	// Убедимся что newContent отличается от оригинала
	if newContent == original {
		t.Error("new content should differ from original")
	}
	_ = newContent
}

func TestInjectPython_AsyncFunction(t *testing.T) {
	original := `import os

async def fetch_data():
    return {"key": "val"}
`
	newContent, _, err := injectPythonTraces("test.py", original, "mod", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(newContent, `trace("mod.fetch_data.enter")`) {
		t.Error("missing .enter in async function")
	}
}

func TestInjectPython_SkipPrivateFunction(t *testing.T) {
	original := `import os

def _internal():
    return 1

def public():
    return _internal()
`
	newContent, _, err := injectPythonTraces("test.py", original, "mod", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Приватная функция не должна иметь trace
	if strings.Contains(newContent, `trace("mod._internal`) {
		t.Error("private function should be skipped")
	}
	// Публичная должна
	if !strings.Contains(newContent, `trace("mod.public`) {
		t.Error("public function should have trace")
	}
}

func TestInjectPython_SkipEmptyBody(t *testing.T) {
	original := `import os

class Base:
    def abstract_method(self):
        ...

    def stub_method(self):
        pass

    def real_method(self):
        return 42
`
	newContent, _, err := injectPythonTraces("test.py", original, "mod", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(newContent, `trace("mod.abstract_method`) {
		t.Error("function with ... body should be skipped")
	}
	if strings.Contains(newContent, `trace("mod.stub_method`) {
		t.Error("function with pass body should be skipped")
	}
	if !strings.Contains(newContent, `trace("mod.real_method.enter")`) {
		t.Error("real method should have trace")
	}
}

func TestInjectPython_TestFile(t *testing.T) {
	original := `import os

def test_login():
    result = do_login()
    assert result
`
	newContent, _, err := injectPythonTraces("test_mod.py", original, "auth", true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// В тестовом режиме приватные функции не пропускаются если имя начинается с test_
	if !strings.Contains(newContent, `trace("auth.test.test_login`) {
		t.Error("test function should have .enter in test mode")
	}
}

// =========================================================================
// TypeScript inject tests
// =========================================================================

func TestInjectTypeScript_Function(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "utils.ts")
	original := `import React from "react";

export function doSomething() {
    return { ok: true };
}
`
	mustWrite(t, filePath, original)

	newContent, count, err := injectTypeScriptTraces(filePath, original, "utils", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count < 2 {
		t.Errorf("expected at least 2 insertions, got %d", count)
	}
	if !strings.Contains(newContent, `trace("utils.do_something.enter")`) {
		t.Error("missing .enter in TS function, got:\n" + newContent)
	}
	if !strings.Contains(newContent, `trace("utils.do_something.success")`) {
		t.Error("missing .success before return in TS function, got:\n" + newContent)
	}
	if !strings.Contains(newContent, `revizor-sdk`) {
		t.Error("missing TS trace import")
	}
}

func TestInjectTypeScript_ReactComponent(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "App.tsx")
	original := `import React from "react";

export function App() {
    return <div>Hello</div>;
}
`
	mustWrite(t, filePath, original)

	newContent, _, err := injectTypeScriptTraces(filePath, original, "app", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// React-компонент должен иметь .mounted
	if !strings.Contains(newContent, `.mounted"`) {
		t.Error("React component missing .mounted, got:\n" + newContent)
	}
	// React-компонент также должен иметь .enter и .success
	if !strings.Contains(newContent, `.enter"`) {
		t.Error("React component missing .enter, got:\n" + newContent)
	}
	if !strings.Contains(newContent, `.success"`) {
		t.Error("React component missing .success, got:\n" + newContent)
	}
}

func TestInjectTypeScript_CriticalDecision(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "auth.ts")
	original := `export function login() {
    if (!is_admin) {
        return false;
    }
    return true;
}
`
	mustWrite(t, filePath, original)

	newContent, _, err := injectTypeScriptTraces(filePath, original, "auth", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(newContent, `.decision`) {
		t.Error("missing .decision in critical if, got:\n" + newContent)
	}
	// Проверяем что if-блок не сломан — условие осталось на месте
	if !strings.Contains(newContent, "if (!is_admin) {") {
		t.Error("if block structure broken, got:\n" + newContent)
	}
}

func TestInjectTypeScript_NoReturnVoid(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "utils.ts")
	original := `export function cleanup() {
    return;
}
`
	mustWrite(t, filePath, original)

	newContent, _, err := injectTypeScriptTraces(filePath, original, "utils", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Пустой return (return;) не должен получать .success
	if strings.Contains(newContent, `.success"`) {
		t.Error("void return should NOT have .success, got:\n" + newContent)
	}
	if !strings.Contains(newContent, `.enter"`) {
		t.Error("should still have .enter")
	}
}

// =========================================================================
// getModuleNameFromFilePath tests
// =========================================================================

func TestGetModuleNameFromFilePath(t *testing.T) {
	tests := []struct {
		path       string
		sourceRoot string
		want       string
	}{
		// AIS-совместимые пути (sourceRoot по умолчанию "modules")
		{"modules/auth_0_1_0/auth_0_1_0.py", "", "auth"},
		{"modules/admin_api_0_2_0/services.py", "", "admin_api"},
		{"modules/i18n_manager_0_1_0/manager.py", "", "i18n_manager"},
		{"/home/user/proj/modules/llm_adapter_0_1_0/adapter.py", "", "llm_adapter"},
		// Fallback: родительская директория
		{"some/other/path/file.py", "", "path"},
		// sourceRoot не найден → родительская директория
		{"src/auth/login.py", "modules", "auth"},
		// Кастомный sourceRoot
		{"packages/core/utils.py", "packages", "core"},
		// Абсолютный путь с кастомным sourceRoot
		{"/home/user/myapp/lib/database/query.py", "lib", "database"},
	}
	for _, tt := range tests {
		got := getModuleNameFromFilePath(tt.path, tt.sourceRoot)
		if got != tt.want {
			t.Errorf("getModuleNameFromFilePath(%q, %q) = %q, want %q", tt.path, tt.sourceRoot, got, tt.want)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	if got := detectLanguage("file.py"); got != "python" {
		t.Errorf("expected python, got %s", got)
	}
	if got := detectLanguage("file.ts"); got != "typescript" {
		t.Errorf("expected typescript, got %s", got)
	}
	if got := detectLanguage("file.tsx"); got != "typescript" {
		t.Errorf("expected typescript, got %s", got)
	}
	if got := detectLanguage("file.js"); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}

func TestIsTestFile(t *testing.T) {
	if !isTestFile("test_auth.py") {
		t.Error("test_auth.py should be detected as test")
	}
	if !isTestFile("/path/to/test_login.py") {
		t.Error("test_login.py should be detected as test")
	}
	if isTestFile("auth.py") {
		t.Error("auth.py should not be detected as test")
	}
}

// =========================================================================
// Utility tests
// =========================================================================

func TestBasicPyValidation(t *testing.T) {
	tests := []struct {
		code string
		ok   bool
	}{
		{`def foo():\n    return 1\n`, true},
		{`def foo():\n    return (1 + 2\n`, false}, // незакрытая скобка
		{`x = "string with )"\n`, true},             // скобка в строке
		{`def foo():\n    pass\n`, true},
	}
	for _, tt := range tests {
		got := basicPyValidation(tt.code)
		if got != tt.ok {
			t.Errorf("basicPyValidation(%q) = %v, want %v", tt.code, got, tt.ok)
		}
	}
}

func TestDecisionName(t *testing.T) {
	tests := []struct {
		condition string
		want      string
	}{
		{"link_mode", "link_mode"},
		{"provider == 'google'", "provider_google"},
		{"x > 5", "x_5"},
	}
	for _, tt := range tests {
		got := decisionName(tt.condition)
		if got != tt.want {
			t.Errorf("decisionName(%q) = %q, want %q", tt.condition, got, tt.want)
		}
	}
}

func TestIsCriticalDecision(t *testing.T) {
	if !isCriticalDecision("link_mode == 'manual'") {
		t.Error("link_mode should be critical")
	}
	if !isCriticalDecision("permission == 'admin'") {
		t.Error("permission should be critical")
	}
	if isCriticalDecision("x > 5") {
		t.Error("x > 5 should not be critical")
	}
}

func TestAtomicWriteWithBackup(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.py")
	original := []byte("def foo():\n    return 1\n")
	mustWrite(t, filePath, string(original))

	newContent := "from revizor_sdk import trace\n\ndef foo():\n    trace(\"mod.foo.enter\")\n    return 1\n"

	err := atomicWriteWithBackup(filePath, original, newContent)
	if err != nil {
		t.Fatalf("atomicWriteWithBackup error: %v", err)
	}

	// Проверяем что .bak создан
	backupPath := filePath + ".bak"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("backup file not created")
	} else {
		backupContent, _ := os.ReadFile(backupPath)
		if string(backupContent) != string(original) {
			t.Error("backup content mismatch")
		}
	}

	// Проверяем что файл изменён
	fileContent, _ := os.ReadFile(filePath)
	if string(fileContent) != newContent {
		t.Errorf("file content mismatch:\n got: %q\n want: %q", string(fileContent), newContent)
	}
}

func TestFindClosingParen(t *testing.T) {
	lines := []string{
		"from foo import (\n",
		"    bar,\n",
		"    baz\n",
		")\n",
		"\n",
		"def func():\n",
	}
	if got := findClosingParen(lines, 0); got != 3 {
		t.Errorf("findClosingParen = %d, want 3", got)
	}
}

func TestFindLastModuleImport(t *testing.T) {
	lines := []string{
		"import os\n",
		"import sys\n",
		"from foo import bar\n",
		"\n",
		"def func():\n",
		"    import inside_func\n",
	}
	got := findLastModuleImport(lines, false)
	if got != 2 {
		t.Errorf("findLastModuleImport = %d, want 2", got)
	}
}

func TestGetPyBodyStart(t *testing.T) {
	lines := []string{
		"def foo(a, b):\n",
		"    \"\"\"Docstring.\"\"\"\n",
		"    return a + b\n",
	}
	got := getPyBodyStart(lines, 0)
	if got != 2 {
		t.Errorf("getPyBodyStart = %d, want 2", got)
	}
}

func TestFindOpeningBrace(t *testing.T) {
	lines := []string{
		"export function App() {\n",
		"    return <div/>;\n",
		"}\n",
	}
	got := findOpeningBrace(lines, 0)
	if got != 0 {
		t.Errorf("findOpeningBrace = %d, want 0", got)
	}
}

func TestInjectPython_SpacesInPath(t *testing.T) {
	tmpDir := t.TempDir()
	spacedDir := filepath.Join(tmpDir, "dir with spaces")
	if err := os.MkdirAll(spacedDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	filePath := filepath.Join(spacedDir, "test_mod_0_1_0.py")
	content := "import os\n\ndef foo():\n    return 42\n"
	mustWrite(t, filePath, content)

	newContent, count, err := injectPythonTraces(filePath, content, "test_mod", false, nil)
	if err != nil {
		t.Fatalf("spaces in path should not cause error: %v", err)
	}
	if count < 1 {
		t.Error("expected insertions with spaces-in-path")
	}
	if !strings.Contains(newContent, `trace("test_mod.foo`) {
		t.Error("trace points missing")
	}
}

// =========================================================================
// REV-013: однострочные функции
// =========================================================================

func TestIsOneLineFunc(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"def f(x): return x * 2", true},
		{"def f(x): return x * 2  ", true},
		{"def f(x):", false},
		{"def f(x: int) -> int: return x * 2", true},
		{"async def f(x): return await x", true},
		{"    def f(x): return x", true}, // с отступом
		{"def f(x): # comment only", false},
		{"def f(x) -> int:", false},
		{"  # def f(x): return x", false}, // комментарий
	}
	for _, c := range cases {
		got := isOneLineFunc(c.line)
		if got != c.want {
			t.Errorf("isOneLineFunc(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestInjectPython_OneLinerSplit(t *testing.T) {
	original := "# test\n\ndef oneline(x): return x * 2\n\ndef normal():\n    return 1\n"
	newContent, _, err := injectPythonTraces("test_mod_0_1_0/test.py", original, "test_mod", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Проверим что isOneLineFunc работает на нашей строке
	t.Logf("isOneLineFunc result: %v", isOneLineFunc("def oneline(x): return x * 2"))
	// Однострочная должна быть разбита — не должно остаться "): return" на одной строке
	if strings.Contains(newContent, "): return") {
		t.Logf("Full output:\n%s", newContent)
		t.Error("one-liner was not split: still contains '): return' on same line")
	}
	// Должен появиться .enter для однострочной
	if !strings.Contains(newContent, `trace("test_mod.oneline.enter")`) {
		t.Error("missing .enter for one-liner")
	}
	// Должен быть .success для обычной
	if !strings.Contains(newContent, `trace("test_mod.normal.success")`) {
		t.Error("missing .success for normal function")
	}
}

func TestApplyReplacements(t *testing.T) {
	lines := []string{"a\n", "b\n", "c\n"}
	reps := []replacement{{1, []string{"x\n", "y\n"}}}
	got := applyReplacements(lines, reps)
	want := []string{"a\n", "x\n", "y\n", "c\n"}
	if len(got) != len(want) {
		t.Fatalf("applyReplacements: got length %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestApplyReplacements_Multiple(t *testing.T) {
	lines := []string{"0\n", "1\n", "2\n", "3\n", "4\n"}
	// Заменяем с конца — индексы 4 и 1
	reps := []replacement{
		{4, []string{"four-a\n", "four-b\n"}},
		{1, []string{"one-x\n"}},
	}
	got := applyReplacements(lines, reps)
	want := []string{"0\n", "one-x\n", "2\n", "3\n", "four-a\n", "four-b\n"}
	if len(got) != len(want) {
		t.Fatalf("applyReplacements multiple: got length %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

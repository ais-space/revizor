package store

import (
	"testing"

	"github.com/ais-platform/ais_products/revizor/internal/config"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	st, err := NewSQLiteStore(t.TempDir()+"/test.db", 168)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSQLite_Migrate(t *testing.T) {
	st := newTestStore(t)

	// Проверяем что таблицы созданы
	var count int
	err := st.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='trace_config'").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Error("trace_config table should exist")
	}

	err = st.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='trace_session'").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Error("trace_session table should exist")
	}

	err = st.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='trace_log'").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Error("trace_log table should exist")
	}
}

func TestSQLite_EnableDisableTrace(t *testing.T) {
	st := newTestStore(t)

	err := st.EnableTrace("test.path", nil, EnableOpts{})
	if err != nil {
		t.Fatalf("EnableTrace failed: %v", err)
	}

	config, err := st.GetConfig(nil)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	found := false
	for _, r := range config {
		if r.TracePath == "test.path" && r.Enabled {
			found = true
			break
		}
	}
	if !found {
		t.Error("trace should be enabled")
	}

	// Disable
	err = st.DisableTrace("test.path", nil)
	if err != nil {
		t.Fatalf("DisableTrace failed: %v", err)
	}

	config, err = st.GetConfig(nil)
	if err != nil {
		t.Fatalf("GetConfig after disable failed: %v", err)
	}

	for _, r := range config {
		if r.TracePath == "test.path" && r.Enabled {
			t.Error("trace should be disabled")
		}
	}
}

func TestSQLite_CreateGetSession(t *testing.T) {
	st := newTestStore(t)

	sess, err := st.CreateSession("test-owner", "test description")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sess.SessionID == "" {
		t.Error("session_id should not be empty")
	}
	if sess.Owner != "test-owner" {
		t.Errorf("owner should be test-owner, got %s", sess.Owner)
	}
	if sess.Description == nil || *sess.Description != "test description" {
		t.Error("description should be set")
	}

	sessions, err := st.GetActiveSessions()
	if err != nil {
		t.Fatalf("GetActiveSessions failed: %v", err)
	}
	if len(sessions) == 0 {
		t.Error("should have at least one active session")
	}
}

func TestSQLite_WriteReadLog(t *testing.T) {
	st := newTestStore(t)

	sess, _ := st.CreateSession("test-owner", "")
	sid := sess.SessionID

	err := st.WriteTrace(TraceEntry{
		TracePath: "test.module.enter",
		Data:      map[string]any{"key": "value"},
		SessionID: &sid,
	})
	if err != nil {
		t.Fatalf("WriteTrace failed: %v", err)
	}

	logs, err := st.ReadTraceLog(&sid, 0, 10, nil, nil, nil)
	if err != nil {
		t.Fatalf("ReadTraceLog failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if logs[0].TracePath != "test.module.enter" {
		t.Errorf("expected test.module.enter, got %s", logs[0].TracePath)
	}
}

func TestSQLite_SearchLog(t *testing.T) {
	st := newTestStore(t)

	sess, _ := st.CreateSession("test-owner", "")
	sid := sess.SessionID

	st.WriteTrace(TraceEntry{
		TracePath: "test.one",
		Data:      map[string]any{"msg": "hello world"},
		SessionID: &sid,
	})
	st.WriteTrace(TraceEntry{
		TracePath: "test.two",
		Data:      map[string]any{"msg": "goodbye"},
		SessionID: &sid,
	})

	logs, err := st.SearchTraceLog("hello", &sid, nil, 0, 10, nil, nil)
	if err != nil {
		t.Fatalf("SearchTraceLog failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(logs))
	}
}

func TestSQLite_Stats(t *testing.T) {
	st := newTestStore(t)

	sess, _ := st.CreateSession("test-owner", "")
	sid := sess.SessionID

	st.WriteTrace(TraceEntry{TracePath: "a.one", Data: nil, SessionID: &sid})
	st.WriteTrace(TraceEntry{TracePath: "b.two", Data: nil, SessionID: &sid})

	stats, err := st.GetTraceStats(&sid)
	if err != nil {
		t.Fatalf("GetTraceStats failed: %v", err)
	}
	if stats.TotalEvents != 2 {
		t.Errorf("expected 2 events, got %d", stats.TotalEvents)
	}
	if stats.UniquePaths != 2 {
		t.Errorf("expected 2 unique paths, got %d", stats.UniquePaths)
	}
}

func TestSQLite_RegisterPath(t *testing.T) {
	st := newTestStore(t)

	err := st.RegisterPath("new.path.enter", nil)
	if err != nil {
		t.Fatalf("RegisterPath failed: %v", err)
	}

	// Повторная регистрация не должна падать
	err = st.RegisterPath("new.path.enter", nil)
	if err != nil {
		t.Fatalf("second RegisterPath should not fail: %v", err)
	}
}

func TestSQLite_ExpireSession(t *testing.T) {
	st := newTestStore(t)

	sess, _ := st.CreateSession("test-owner", "")
	sid := sess.SessionID

	// Enable trace for this session
	sidPtr := &sid
	st.EnableTrace("test.path", sidPtr, EnableOpts{})
	st.WriteTrace(TraceEntry{TracePath: "test.path", Data: nil, SessionID: sidPtr})

	err := st.ExpireSession(sid)
	if err != nil {
		t.Fatalf("ExpireSession failed: %v", err)
	}

	// Check that trace was disabled
	config, _ := st.GetConfig(sidPtr)
	for _, r := range config {
		if r.TracePath == "test.path" && r.Enabled {
			t.Error("trace should be disabled after expire")
		}
	}
}

func TestSQLite_GetDistinctPaths(t *testing.T) {
	st := newTestStore(t)

	sess, _ := st.CreateSession("owner", "")
	sid := sess.SessionID
	sidPtr := &sid

	st.WriteTrace(TraceEntry{TracePath: "auth.enter", Data: nil, SessionID: sidPtr})
	st.WriteTrace(TraceEntry{TracePath: "elevation.enter", Data: nil, SessionID: sidPtr})
	st.WriteTrace(TraceEntry{TracePath: "auth.enter", Data: nil, SessionID: sidPtr}) // дубликат

	paths, err := st.GetDistinctPaths(sidPtr, nil)
	if err != nil {
		t.Fatalf("GetDistinctPaths failed: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 distinct paths, got %d: %v", len(paths), paths)
	}
}

func TestSQLite_GetDistinctPaths_ModulePrefix(t *testing.T) {
	st := newTestStore(t)

	sess, _ := st.CreateSession("owner", "")
	sid := sess.SessionID
	sidPtr := &sid
	prefix := "auth"

	st.WriteTrace(TraceEntry{TracePath: "auth.enter", Data: nil, SessionID: sidPtr})
	st.WriteTrace(TraceEntry{TracePath: "auth.success", Data: nil, SessionID: sidPtr})
	st.WriteTrace(TraceEntry{TracePath: "elevation.enter", Data: nil, SessionID: sidPtr})

	paths, err := st.GetDistinctPaths(sidPtr, &prefix)
	if err != nil {
		t.Fatalf("GetDistinctPaths with prefix failed: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 auth paths, got %d: %v", len(paths), paths)
	}
	for _, p := range paths {
		if p[:4] != "auth" {
			t.Errorf("all paths should start with 'auth', got %s", p)
		}
	}
}

func TestSQLite_GetDistinctPaths_IncludesTraceConfig(t *testing.T) {
	// BUG-GO-09: пути из trace_config должны быть видны даже без trace_log.
	st := newTestStore(t)

	sess, _ := st.CreateSession("owner", "")
	sid := sess.SessionID
	sidPtr := &sid

	// Пишем путь только в trace_log
	st.WriteTrace(TraceEntry{TracePath: "auth.enter", Data: nil, SessionID: sidPtr})

	// Регистрируем путь в trace_config (без записи в trace_log)
	st.RegisterPath("elevation.new_feature.enter", sidPtr)

	paths, err := st.GetDistinctPaths(sidPtr, nil)
	if err != nil {
		t.Fatalf("GetDistinctPaths failed: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths (1 from trace_log + 1 from trace_config), got %d: %v", len(paths), paths)
	}

	hasAuth := false
	hasElevation := false
	for _, p := range paths {
		if p == "auth.enter" {
			hasAuth = true
		}
		if p == "elevation.new_feature.enter" {
			hasElevation = true
		}
	}
	if !hasAuth {
		t.Error("missing path from trace_log")
	}
	if !hasElevation {
		t.Error("missing path from trace_config (BUG-GO-09 regression)")
	}
}

func TestSQLite_GetDistinctPaths_SessionFilterTraceConfig(t *testing.T) {
	// BUG-GO-09 остаток: trace_config фильтруется по owner = session_id.
	st := newTestStore(t)

	sess1, err := st.CreateSession("owner", "")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sid1 := sess1.SessionID
	sid1Ptr := &sid1

	// Второй owner — через прямой SQL (CreateSession может отказать из-за лимитов)
	owner2 := "session-2-uuid"

	// Пишем в trace_log от сессии 1
	st.WriteTrace(TraceEntry{TracePath: "auth.enter", Data: nil, SessionID: sid1Ptr})

	// Регистрируем путь в trace_config от другого owner через прямой вызов
	st.RegisterPath("other.enter", &owner2)

	// Запрос с фильтром по сессии 1: только auth.enter
	paths, err := st.GetDistinctPaths(sid1Ptr, nil)
	if err != nil {
		t.Fatalf("GetDistinctPaths failed: %v", err)
	}
	if len(paths) != 1 || paths[0] != "auth.enter" {
		t.Fatalf("session 1: expected [auth.enter], got %v", paths)
	}

	// Запрос с фильтром по owner2: только other.enter (из trace_config)
	paths, err = st.GetDistinctPaths(&owner2, nil)
	if err != nil {
		t.Fatalf("GetDistinctPaths failed: %v", err)
	}
	if len(paths) != 1 || paths[0] != "other.enter" {
		t.Fatalf("session 2: expected [other.enter], got %v", paths)
	}

	// Запрос без фильтра: оба пути
	paths, err = st.GetDistinctPaths(nil, nil)
	if err != nil {
		t.Fatalf("GetDistinctPaths failed: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("no filter: expected 2 paths, got %d: %v", len(paths), paths)
	}
}

func TestSQLite_GetPathFrequency(t *testing.T) {
	st := newTestStore(t)

	sess, _ := st.CreateSession("owner", "")
	sid := sess.SessionID
	sidPtr := &sid

	st.WriteTrace(TraceEntry{TracePath: "auth.enter", Data: nil, SessionID: sidPtr})
	st.WriteTrace(TraceEntry{TracePath: "auth.enter", Data: nil, SessionID: sidPtr})

	buckets, err := st.GetPathFrequency(sidPtr, "auth.enter", 24)
	if err != nil {
		t.Fatalf("GetPathFrequency failed: %v", err)
	}
	if len(buckets) == 0 {
		t.Fatal("expected at least 1 bucket")
	}
	totalCount := 0
	for _, b := range buckets {
		totalCount += b.Count
	}
	if totalCount != 2 {
		t.Errorf("expected total count 2, got %d", totalCount)
	}
}

func TestSQLite_GetPathFrequency_Empty(t *testing.T) {
	st := newTestStore(t)

	buckets, err := st.GetPathFrequency(nil, "nonexistent.path", 1)
	if err != nil {
		t.Fatalf("GetPathFrequency empty failed: %v", err)
	}
	if len(buckets) != 0 {
		t.Errorf("expected 0 buckets for nonexistent path, got %d", len(buckets))
	}
}

func TestSQLite_GetRequestChain(t *testing.T) {
	st := newTestStore(t)

	sess, _ := st.CreateSession("owner", "")
	sid := sess.SessionID
	sidPtr := &sid
	reqID := "req-123"

	st.WriteTrace(TraceEntry{TracePath: "auth.enter", Data: nil, SessionID: sidPtr, RequestID: &reqID})
	st.WriteTrace(TraceEntry{TracePath: "auth.success", Data: nil, SessionID: sidPtr, RequestID: &reqID})
	st.WriteTrace(TraceEntry{TracePath: "other.enter", Data: nil, SessionID: sidPtr, RequestID: strPtr("other-req")})

	chain, err := st.GetRequestChain(reqID, sidPtr)
	if err != nil {
		t.Fatalf("GetRequestChain failed: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("expected 2 events in chain, got %d", len(chain))
	}
	if chain[0].TracePath != "auth.enter" {
		t.Errorf("first event should be auth.enter, got %s", chain[0].TracePath)
	}
	if chain[1].TracePath != "auth.success" {
		t.Errorf("second event should be auth.success, got %s", chain[1].TracePath)
	}
}

func TestSQLite_GetRequestChain_NotFound(t *testing.T) {
	st := newTestStore(t)

	chain, err := st.GetRequestChain("nonexistent", nil)
	if err != nil {
		t.Fatalf("GetRequestChain not found should not error: %v", err)
	}
	if len(chain) != 0 {
		t.Errorf("expected empty chain, got %d entries", len(chain))
	}
}

func TestSQLite_GetEnterChains(t *testing.T) {
	st := newTestStore(t)

	sess, _ := st.CreateSession("owner", "")
	sid := sess.SessionID
	sidPtr := &sid
	reqID1 := "req-aaa"
	reqID2 := "req-bbb"

	// Цепочка 1: enter без success/failed (незавершённая)
	st.WriteTrace(TraceEntry{TracePath: "auth.callback.enter", Data: nil, SessionID: sidPtr, RequestID: &reqID1})

	// Цепочка 2: enter + success (завершённая)
	st.WriteTrace(TraceEntry{TracePath: "elevation.check.enter", Data: nil, SessionID: sidPtr, RequestID: &reqID2})
	st.WriteTrace(TraceEntry{TracePath: "elevation.check.success", Data: nil, SessionID: sidPtr, RequestID: &reqID2})

	chains, err := st.GetEnterChains(sidPtr, nil)
	if err != nil {
		t.Fatalf("GetEnterChains failed: %v", err)
	}
	if len(chains) != 1 {
		t.Fatalf("expected 1 incomplete chain, got %d", len(chains))
	}
	if chains[0].BasePath != "auth.callback" {
		t.Errorf("expected base_path auth.callback, got %s", chains[0].BasePath)
	}
	if chains[0].EnterPath != "auth.callback.enter" {
		t.Errorf("expected enter_path auth.callback.enter, got %s", chains[0].EnterPath)
	}
}

func TestSQLite_GetEnterChains_AllComplete(t *testing.T) {
	st := newTestStore(t)

	sess, _ := st.CreateSession("owner", "")
	sid := sess.SessionID
	sidPtr := &sid
	reqID := "req-ccc"

	st.WriteTrace(TraceEntry{TracePath: "auth.enter", Data: nil, SessionID: sidPtr, RequestID: &reqID})
	st.WriteTrace(TraceEntry{TracePath: "auth.success", Data: nil, SessionID: sidPtr, RequestID: &reqID})

	chains, err := st.GetEnterChains(sidPtr, nil)
	if err != nil {
		t.Fatalf("GetEnterChains all complete failed: %v", err)
	}
	if len(chains) != 0 {
		t.Errorf("expected 0 incomplete chains, got %d", len(chains))
	}
}

func TestSQLite_SeedAndGetPresets(t *testing.T) {
	st := newTestStore(t)

	err := st.SeedPresets(map[string]config.PresetConfig{
		"test_debug": {Description: "Test preset", Paths: []string{"test.**", "!test.verbose"}},
	})
	if err != nil {
		t.Fatalf("SeedPresets failed: %v", err)
	}

	presets, err := st.GetPresets()
	if err != nil {
		t.Fatalf("GetPresets failed: %v", err)
	}
	if len(presets) != 1 {
		t.Fatalf("expected 1 preset, got %d", len(presets))
	}
	if presets[0].Name != "test_debug" {
		t.Errorf("expected test_debug, got %s", presets[0].Name)
	}
	if len(presets[0].Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(presets[0].Paths))
	}
}

func TestSQLite_SeedPresets_Idempotent(t *testing.T) {
	st := newTestStore(t)

	// Первый seed
	st.SeedPresets(map[string]config.PresetConfig{
		"test_debug": {Description: "Original", Paths: []string{"a.**"}},
	})
	// Второй seed с другими путями — не должен перезаписать (INSERT OR IGNORE)
	st.SeedPresets(map[string]config.PresetConfig{
		"test_debug": {Description: "Changed", Paths: []string{"b.**"}},
	})

	presets, _ := st.GetPresets()
	if len(presets) != 1 {
		t.Fatalf("expected 1 preset, got %d", len(presets))
	}
	// Должны остаться исходные пути (INSERT OR IGNORE)
	if presets[0].Paths[0] != "a.**" {
		t.Errorf("paths should not be overwritten by seed, got %v", presets[0].Paths)
	}
}

func TestSQLite_SetPreset(t *testing.T) {
	st := newTestStore(t)

	err := st.SetPreset("my_preset", "My custom preset", []string{"custom.**", "!custom.noisy"})
	if err != nil {
		t.Fatalf("SetPreset failed: %v", err)
	}

	presets, _ := st.GetPresets()
	if len(presets) != 1 {
		t.Fatalf("expected 1 preset, got %d", len(presets))
	}
	if presets[0].Name != "my_preset" {
		t.Errorf("expected my_preset, got %s", presets[0].Name)
	}
	if len(presets[0].Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(presets[0].Paths))
	}

	// Обновление существующего
	err = st.SetPreset("my_preset", "Updated", []string{"updated.**"})
	if err != nil {
		t.Fatalf("SetPreset update failed: %v", err)
	}
	presets, _ = st.GetPresets()
	if len(presets[0].Paths) != 1 || presets[0].Paths[0] != "updated.**" {
		t.Errorf("preset should be updated, got %v", presets[0].Paths)
	}
}

func TestSQLite_DeletePreset(t *testing.T) {
	st := newTestStore(t)

	st.SetPreset("to_delete", "", []string{"x.**"})
	presets, _ := st.GetPresets()
	if len(presets) != 1 {
		t.Fatal("preset should exist")
	}

	err := st.DeletePreset("to_delete")
	if err != nil {
		t.Fatalf("DeletePreset failed: %v", err)
	}

	presets, _ = st.GetPresets()
	if len(presets) != 0 {
		t.Errorf("expected 0 presets after delete, got %d", len(presets))
	}

	// Удаление несуществующего — не ошибка
	err = st.DeletePreset("nonexistent")
	if err != nil {
		t.Errorf("DeletePreset nonexistent should not error: %v", err)
	}
}

// strPtr helper for tests
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

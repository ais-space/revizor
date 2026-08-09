package store

import (
	"context"
	"testing"

	"github.com/ais-platform/ais_products/revizor/internal/config"
)

const testPostgresURL = "postgres://postgres:postgres@localhost:5432/revizor_test"

// postgresStoreFixture создаёт чистый PostgresStore для тестов.
func postgresStoreFixture(t *testing.T) *PostgresStore {
	t.Helper()
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	cleanTables(t, store)
	t.Cleanup(func() {
		cleanTables(t, store)
		store.Close()
	})
	return store
}

func cleanTables(t *testing.T, s *PostgresStore) {
	ctx := context.Background()
	tables := []string{"trace_log", "trace_config", "trace_session", "trace_preset", "audit_log"}
	for _, table := range tables {
		s.pool.Exec(ctx, "DELETE FROM "+table)
	}
}

func TestPostgres_Migrate(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	var count int
	err = store.pool.QueryRow(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public'").Scan(&count)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	if count < 4 {
		t.Errorf("expected at least 4 tables, got %d", count)
	}
	t.Logf("Tables created: %d", count)
}

func TestPostgres_MigrateIdempotent(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	// Повторный Migrate не должен падать
	if err := store.Migrate(); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}
	store.Close()
}

func TestPostgres_EnableDisableTrace(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.pool.Exec(ctx, "DELETE FROM trace_config")

	desc := "test trace point"
	sessionID := "test-session"
	err = store.EnableTrace("test.module.enter", &sessionID, EnableOpts{Description: &desc})
	if err != nil {
		t.Fatalf("EnableTrace: %v", err)
	}

	configs, err := store.GetConfig(&sessionID)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if len(configs) < 1 {
		t.Fatal("expected at least 1 config row")
	}
	found := false
	for _, c := range configs {
		if c.TracePath == "test.module.enter" && c.Enabled {
			found = true
			if c.Description == nil || *c.Description != "test trace point" {
				t.Errorf("description mismatch: %v", c.Description)
			}
			break
		}
	}
	if !found {
		t.Error("trace point not found or not enabled")
	}

	// Disable
	err = store.DisableTrace("test.module.enter", &sessionID)
	if err != nil {
		t.Fatalf("DisableTrace: %v", err)
	}

	configs, _ = store.GetConfig(&sessionID)
	for _, c := range configs {
		if c.TracePath == "test.module.enter" && c.Enabled {
			t.Error("trace point should be disabled")
		}
	}
}

func TestPostgres_WriteReadLog(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.pool.Exec(ctx, "DELETE FROM trace_log")

	sessionID := "test-session-pg"
	entry := TraceEntry{
		TracePath: "test.module.enter",
		Data:      map[string]any{"key": "value", "num": 42},
		SessionID: &sessionID,
	}
	if err := store.WriteTrace(entry); err != nil {
		t.Fatalf("WriteTrace: %v", err)
	}

	entries, err := store.ReadTraceLog(nil, 0, 10, nil, nil, nil)
	if err != nil {
		t.Fatalf("ReadTraceLog: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].TracePath != "test.module.enter" {
		t.Errorf("trace_path mismatch: %s", entries[0].TracePath)
	}

	data, ok := entries[0].Data.(map[string]any)
	if !ok {
		t.Fatalf("data is not map[string]any: %T", entries[0].Data)
	}
	if data["key"] != "value" {
		t.Errorf("data.key mismatch: %v", data["key"])
	}
}

func TestPostgres_SearchLog(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	// Полная очистка — другие тесты могли оставить записи
	store.pool.Exec(ctx, "DELETE FROM trace_log")

	sessionID := "test-session-search"
	for i, path := range []string{"auth.login.enter", "auth.login.success", "download.request"} {
		entry := TraceEntry{
			TracePath: path,
			Data:      map[string]any{"provider": "google", "seq": i},
			SessionID: &sessionID,
		}
		if err := store.WriteTrace(entry); err != nil {
			t.Fatalf("WriteTrace %d: %v", i, err)
		}
	}

	// Поиск по подстроке — только внутри сессии
	results, err := store.SearchTraceLog("auth", &sessionID, nil, 0, 10, nil, nil)
	if err != nil {
		t.Fatalf("SearchTraceLog: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'auth', got %d", len(results))
	}

	// Поиск по данным — все записи (их 3: 2 auth + 1 download, все с "google")
	results, err = store.SearchTraceLog("google", nil, nil, 0, 10, nil, nil)
	if err != nil {
		t.Fatalf("SearchTraceLog google: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results for 'google', got %d", len(results))
	}
}

func TestPostgres_SessionCRUD(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.pool.Exec(ctx, "DELETE FROM trace_session")

	session, err := store.CreateSession("test-user", "test session")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.SessionID == "" {
		t.Error("session_id is empty")
	}

	sessions, err := store.GetActiveSessions()
	if err != nil {
		t.Fatalf("GetActiveSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 active session, got %d", len(sessions))
	}

	err = store.ExpireSession(session.SessionID)
	if err != nil {
		t.Fatalf("ExpireSession: %v", err)
	}

	sessions, _ = store.GetActiveSessions()
	if len(sessions) != 0 {
		t.Errorf("expected 0 active sessions after expire, got %d", len(sessions))
	}
}

func TestPostgres_Stats(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.pool.Exec(ctx, "DELETE FROM trace_log")

	sessionID := "test-stats-session"
	for i := 0; i < 5; i++ {
		entry := TraceEntry{
			TracePath: "test.stats.enter",
			Data:      map[string]any{"i": i},
			SessionID: &sessionID,
		}
		store.WriteTrace(entry)
	}

	stats, err := store.GetTraceStats(nil)
	if err != nil {
		t.Fatalf("GetTraceStats: %v", err)
	}
	if stats.TotalEvents != 5 {
		t.Errorf("expected 5 events, got %d", stats.TotalEvents)
	}
	if stats.UniquePaths != 1 {
		t.Errorf("expected 1 unique path, got %d", stats.UniquePaths)
	}
}

func TestPostgres_RegisterPath(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.pool.Exec(ctx, "DELETE FROM trace_config WHERE trace_path = 'test.registered.path'")

	sessionID := "test-session-reg"
	err = store.RegisterPath("test.registered.path", &sessionID)
	if err != nil {
		t.Fatalf("RegisterPath: %v", err)
	}

	// Повторная регистрация не должна падать
	err = store.RegisterPath("test.registered.path", &sessionID)
	if err != nil {
		t.Fatalf("RegisterPath second call: %v", err)
	}

	paths, err := store.GetDistinctPaths(&sessionID, nil)
	if err != nil {
		t.Fatalf("GetDistinctPaths: %v", err)
	}
	found := false
	for _, p := range paths {
		if p == "test.registered.path" {
			found = true
			break
		}
	}
	if !found {
		t.Error("registered path not found in distinct paths")
	}
}

func TestPostgres_Presets(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.pool.Exec(ctx, "DELETE FROM trace_preset")

	presets := map[string]config.PresetConfig{
		"test-preset": {
			Description: "Test preset",
			Paths:       []string{"test.path.one", "test.path.two"},
		},
	}
	err = store.SeedPresets(presets)
	if err != nil {
		t.Fatalf("SeedPresets: %v", err)
	}

	all, err := store.GetPresets()
	if err != nil {
		t.Fatalf("GetPresets: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 preset, got %d", len(all))
	}
	if all[0].Name != "test-preset" {
		t.Errorf("preset name mismatch: %s", all[0].Name)
	}

	// Set — update
	err = store.SetPreset("test-preset", "Updated", []string{"new.path"})
	if err != nil {
		t.Fatalf("SetPreset: %v", err)
	}

	all, _ = store.GetPresets()
	if all[0].Description != "Updated" {
		t.Errorf("preset not updated: %s", all[0].Description)
	}

	// Delete
	err = store.DeletePreset("test-preset")
	if err != nil {
		t.Fatalf("DeletePreset: %v", err)
	}

	all, _ = store.GetPresets()
	if len(all) != 0 {
		t.Errorf("expected 0 presets after delete, got %d", len(all))
	}
}

func TestPostgres_AuditLog(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.pool.Exec(ctx, "DELETE FROM audit_log")

	err = store.WriteAudit(AuditEntry{
		ToolName:   "trace_start",
		Args:       `{"paths":"auth.*"}`,
		Result:     "ok",
		DurationMs: 42,
	})
	if err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}

	var count int
	store.pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_log").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 audit entry, got %d", count)
	}
}

func TestPostgres_RequestChain(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.pool.Exec(ctx, "DELETE FROM trace_log")

	requestID := "req-chain-test"
	sessionID := "chain-session"
	for i, path := range []string{"auth.callback.enter", "auth.callback.fetch_profile", "auth.callback.success"} {
		entry := TraceEntry{
			TracePath: path,
			Data:      map[string]any{"step": i},
			SessionID: &sessionID,
			RequestID: &requestID,
		}
		store.WriteTrace(entry)
	}

	chain, err := store.GetRequestChain(requestID, nil)
	if err != nil {
		t.Fatalf("GetRequestChain: %v", err)
	}
	if len(chain) != 3 {
		t.Errorf("expected 3 entries in chain, got %d", len(chain))
	}
}

func TestPostgres_EnterChains(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.pool.Exec(ctx, "DELETE FROM trace_log")

	requestID := "enter-chain-test"
	sessionID := "enter-session"
	// .enter без .success/.failed — должен найтись
	entry := TraceEntry{
		TracePath: "module.op.enter",
		Data:      map[string]any{"mode": "test"},
		SessionID: &sessionID,
		RequestID: &requestID,
	}
	store.WriteTrace(entry)

	chains, err := store.GetEnterChains(&sessionID, nil)
	if err != nil {
		t.Fatalf("GetEnterChains: %v", err)
	}
	if len(chains) < 1 {
		t.Error("expected at least 1 enter chain")
	}
}

func TestPostgres_CountByPath(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.pool.Exec(ctx, "DELETE FROM trace_log")

	sessionID := "count-session"
	for i := 0; i < 3; i++ {
		store.WriteTrace(TraceEntry{TracePath: "auth.login.enter", Data: map[string]any{"i": i}, SessionID: &sessionID})
	}
	for i := 0; i < 2; i++ {
		store.WriteTrace(TraceEntry{TracePath: "auth.login.success", Data: map[string]any{"i": i}, SessionID: &sessionID})
	}

	counts, err := store.CountByPath("", nil, nil, &sessionID, nil, nil)
	if err != nil {
		t.Fatalf("CountByPath: %v", err)
	}
	if counts["auth.login.enter"] != 3 {
		t.Errorf("expected 3 auth.login.enter, got %d", counts["auth.login.enter"])
	}
	if counts["auth.login.success"] != 2 {
		t.Errorf("expected 2 auth.login.success, got %d", counts["auth.login.success"])
	}
}

func TestPostgres_WriteTraceBatch(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.pool.Exec(ctx, "DELETE FROM trace_log")

	sessionID := "batch-session"
	entries := []TraceEntry{
		{TracePath: "batch.test.one", Data: map[string]any{"n": 1}, SessionID: &sessionID},
		{TracePath: "batch.test.two", Data: map[string]any{"n": 2}, SessionID: &sessionID},
	}
	if err := store.WriteTraceBatch(entries); err != nil {
		t.Fatalf("WriteTraceBatch: %v", err)
	}

	var count int
	store.pool.QueryRow(ctx, "SELECT COUNT(*) FROM trace_log WHERE session_id = $1", sessionID).Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 batch entries, got %d", count)
	}
}

func TestPostgres_Close(t *testing.T) {
	store, err := NewPostgresStore(testPostgresURL, 168)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	store.Close()
	// Повторный Close не должен паниковать
	store.Close()
}

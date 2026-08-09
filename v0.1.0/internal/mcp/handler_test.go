package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/license"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// mockStore — минимальная заглушка TraceStore для тестов диспетчера.
type mockStore struct{}

func (m *mockStore) EnableTrace(path string, sessionID *string, opts store.EnableOpts) error { return nil }
func (m *mockStore) DisableTrace(path string, sessionID *string) error                        { return nil }
func (m *mockStore) GetConfig(sessionID *string) ([]store.TraceConfigRow, error)              { return nil, nil }
func (m *mockStore) CreateSession(owner, description string) (*store.Session, error) {
	return &store.Session{SessionID: "test-session", Owner: owner}, nil
}
func (m *mockStore) GetActiveSessions() ([]store.Session, error)                { return nil, nil }
func (m *mockStore) ExpireSession(sessionID string) error                       { return nil }
func (m *mockStore) ExpireOutdatedSessions() (int, error)                       { return 0, nil }
func (m *mockStore) WriteTrace(entry store.TraceEntry) error                     { return nil }
func (m *mockStore) WriteTraceBatch(entries []store.TraceEntry) error            { return nil }
func (m *mockStore) ReadTraceLog(sessionID *string, offset int, limit int, pathFilter *string, since, until *time.Time) ([]store.TraceEntry, error) {
	return nil, nil
}
func (m *mockStore) SearchTraceLog(search string, sessionID *string, pathFilter *string, offset int, limit int, since, until *time.Time) ([]store.TraceEntry, error) {
	return nil, nil
}
func (m *mockStore) SearchTraceLogWithContext(search string, sessionID *string, pathFilter *string, dataFilter *string, offset int, limit int, since, until *time.Time, contextLines int) ([]store.TraceEntry, error) {
	return nil, nil
}
func (m *mockStore) CountByPath(search string, dataFilter *string, pathFilter *string, sessionID *string, since, until *time.Time) (map[string]int64, error) {
	return nil, nil
}
func (m *mockStore) GetTraceStats(sessionID *string) (*store.TraceStats, error) { return nil, nil }
func (m *mockStore) RegisterPath(path string, sessionID *string) error          { return nil }
func (m *mockStore) GetDistinctPaths(sessionID *string, modulePrefix *string) ([]string, error) {
	return nil, nil
}
func (m *mockStore) GetPathFrequency(sessionID *string, path string, hours int) ([]store.FrequencyBucket, error) {
	return nil, nil
}
func (m *mockStore) GetRequestChain(requestID string, sessionID *string) ([]store.TraceEntry, error) {
	return nil, nil
}
func (m *mockStore) GetEnterChains(sessionID *string, modulePrefix *string) ([]store.EnterChain, error) {
	return nil, nil
}
func (m *mockStore) SeedPresets(presets map[string]config.PresetConfig) error { return nil }
func (m *mockStore) GetPresets() ([]store.Preset, error)                        { return nil, nil }
func (m *mockStore) SetPreset(name, description string, paths []string) error   { return nil }
func (m *mockStore) DeletePreset(name string) error                             { return nil }
func (m *mockStore) Migrate() error                                             { return nil }
func (m *mockStore) WriteAudit(entry store.AuditEntry) error                   { return nil }
func (m *mockStore) SetEffectiveLimits(lim license.Limitations)                 {}
func (m *mockStore) Close() error                                               { return nil }

// Ensure mockStore satisfies TraceStore
var _ store.TraceStore = (*mockStore)(nil)

func newTestHandler() *MCPHandler {
	cfg := &config.Config{}
	return NewMCPHandler(&mockStore{}, cfg, nil, nil)
}

func TestHandleToolsList(t *testing.T) {
	h := newTestHandler()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)

	resp := h.HandleJSONRPC(context.Background(), body)

	var parsed JSONRPCResponse
	if err := json.Unmarshal(resp, &parsed); err != nil {
		t.Fatalf("failed to unmarshal response: %v\nraw: %s", err, string(resp))
	}
	if parsed.Error != nil {
		t.Fatalf("unexpected error: %+v", parsed.Error)
	}
	if parsed.ID == nil || *parsed.ID != 1 {
		t.Errorf("expected id=1, got %v", parsed.ID)
	}
	if parsed.Result == nil {
		t.Fatal("expected result with tools list")
	}
}

func TestHandleToolsCall_UnknownTool(t *testing.T) {
	h := newTestHandler()
	body := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`)

	resp := h.HandleJSONRPC(context.Background(), body)

	var parsed JSONRPCResponse
	if err := json.Unmarshal(resp, &parsed); err != nil {
		t.Fatalf("failed to unmarshal response: %v\nraw: %s", err, string(resp))
	}
	if parsed.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestHandleToolsCall_InvalidJSON(t *testing.T) {
	h := newTestHandler()
	body := []byte(`not json`)

	resp := h.HandleJSONRPC(context.Background(), body)

	var parsed JSONRPCResponse
	if err := json.Unmarshal(resp, &parsed); err != nil {
		t.Fatalf("response should be valid JSON, got: %s", string(resp))
	}
	if parsed.Error == nil {
		t.Fatal("expected parse error")
	}
}

func TestHandleJSONRPC_Notification(t *testing.T) {
	h := newTestHandler()
	// Notification — запрос без id
	body := []byte(`{"jsonrpc":"2.0","method":"tools/list","params":{}}`)

	resp := h.HandleJSONRPC(context.Background(), body)
	if resp != nil {
		t.Errorf("notification should return nil, got: %s", string(resp))
	}
}

func TestHandleJSONRPC_MethodNotFound(t *testing.T) {
	h := newTestHandler()
	body := []byte(`{"jsonrpc":"2.0","id":3,"method":"invalid/method","params":{}}`)

	resp := h.HandleJSONRPC(context.Background(), body)

	var parsed JSONRPCResponse
	if err := json.Unmarshal(resp, &parsed); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if parsed.Error == nil {
		t.Fatal("expected error for invalid method")
	}
}

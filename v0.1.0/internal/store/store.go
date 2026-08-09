// Пакет store — интерфейс хранилища и доменные типы для подсистемы Ревизор.
package store

import (
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/license"
)

// TraceEntry — запись в логе трассировки.
type TraceEntry struct {
	ID               int64          `json:"id"`
	TracePath        string         `json:"trace_path"`
	Data             any            `json:"data"`
	SessionID        *string        `json:"session_id"`
	RequestID        *string        `json:"request_id"`
	CreatedAt        string         `json:"created_at"`
	OrchestratorMeta map[string]any `json:"_orchestrator,omitempty"`
}

// Session — сессия трассировки.
type Session struct {
	SessionID   string  `json:"session_id"`
	Owner       string  `json:"owner"`
	Description *string `json:"description"`
	CreatedAt   string  `json:"created_at"`
	ExpiresAt   *string `json:"expires_at"`
}

// TraceStats — статистика сессии.
type TraceStats struct {
	TotalEvents int     `json:"total_events"`
	UniquePaths int     `json:"unique_paths"`
	LastEventAt *string `json:"last_event_at"`
	SessionID   *string `json:"session_id"`
}

// TraceConfigRow — строка конфигурации trace-точки.
type TraceConfigRow struct {
	TracePath   string
	Enabled     bool
	OutputFile  *string
	SampleRate  float64
	Owner       *string
	Description *string
}

// EnableOpts — опции для включения trace-точки.
type EnableOpts struct {
	Description *string
	OutputFile  *string
	SampleRate  *float64
}

// FrequencyBucket — интервал частоты для trace_cost.
type FrequencyBucket struct {
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
}

// EnterChain — незавершённая enter-цепочка (enter без success/failed).
type EnterChain struct {
	BasePath  string `json:"base_path"`
	EnterPath string `json:"enter_path"`
}

// Preset — пресет (набор trace-путей с описанием).
type Preset struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Paths       []string `json:"paths"`
}

// AuditEntry — запись аудита MCP-операции (кто, когда, какой инструмент, результат).
type AuditEntry struct {
	ToolName   string `json:"tool"`
	Args       string `json:"args,omitempty"`
	Result     string `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// TraceStore — интерфейс хранилища Ревизора.
type TraceStore interface {
	// Config
	EnableTrace(path string, sessionID *string, opts EnableOpts) error
	DisableTrace(path string, sessionID *string) error
	GetConfig(sessionID *string) ([]TraceConfigRow, error)

	// Session
	CreateSession(owner, description string) (*Session, error)
	GetActiveSessions() ([]Session, error)
	ExpireSession(sessionID string) error
	ExpireOutdatedSessions() (int, error)

	// Log
	WriteTrace(entry TraceEntry) error
	WriteTraceBatch(entries []TraceEntry) error
	ReadTraceLog(sessionID *string, offset int, limit int, pathFilter *string, since, until *time.Time) ([]TraceEntry, error)
	SearchTraceLog(search string, sessionID *string, pathFilter *string, offset int, limit int, since, until *time.Time) ([]TraceEntry, error)
	SearchTraceLogWithContext(search string, sessionID *string, pathFilter *string, dataFilter *string, offset int, limit int, since, until *time.Time, contextLines int) ([]TraceEntry, error)
	CountByPath(search string, dataFilter *string, pathFilter *string, sessionID *string, since, until *time.Time) (map[string]int64, error)

	// Stats
	GetTraceStats(sessionID *string) (*TraceStats, error)

	// Auto-registration
	RegisterPath(path string, sessionID *string) error

	// Advanced queries (для MCP-инструментов)
	GetDistinctPaths(sessionID *string, modulePrefix *string) ([]string, error)
	GetPathFrequency(sessionID *string, path string, hours int) ([]FrequencyBucket, error)
	GetRequestChain(requestID string, sessionID *string) ([]TraceEntry, error)
	GetEnterChains(sessionID *string, modulePrefix *string) ([]EnterChain, error)

	// Presets
	SeedPresets(presets map[string]config.PresetConfig) error
	GetPresets() ([]Preset, error)
	SetPreset(name, description string, paths []string) error
	DeletePreset(name string) error

	// License
	SetEffectiveLimits(lim license.Limitations)

	// Audit
	WriteAudit(entry AuditEntry) error

	// Maintenance
	Migrate() error
	Close() error
}

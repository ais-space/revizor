// Пакет config — загрузка конфигурации Ревизора из YAML и переменных окружения.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config — корневая структура конфигурации.
type Config struct {
	Server       ServerConfig            `yaml:"server"`
	Storage      StorageConfig           `yaml:"storage"`
	Security     SecurityConfig          `yaml:"security"`
	Session      SessionConfig           `yaml:"session"`
	Logging      LoggingConfig           `yaml:"logging"`
	Presets      map[string]PresetConfig `yaml:"presets"`
	Cleanup      CleanupConfig           `yaml:"cleanup"`
	Integrations IntegrationsConfig      `yaml:"integrations"`
	LicenseKey   string                  `yaml:"license_key"`
	Project      ProjectConfig           `yaml:"project"`
}

// ServerConfig — настройки HTTP-сервера.
type ServerConfig struct {
	Host              string `yaml:"host"`
	Port              int    `yaml:"port"`
	ReadTimeoutSec    int    `yaml:"read_timeout_sec"`
	WriteTimeoutSec   int    `yaml:"write_timeout_sec"`
	LicenseServerURL  string `yaml:"license_server_url"`
	LicensePublicKey  string `yaml:"license_public_key"`
}

// StorageConfig — настройки хранилища.
type StorageConfig struct {
	Type        string `yaml:"type"`
	SQLitePath  string `yaml:"sqlite_path"`
	PostgresURL string `yaml:"postgres_url"`
}

// SecurityConfig — настройки безопасности.
type SecurityConfig struct {
	APIKey         string          `yaml:"api_key"`
	RateLimit      RateLimitConfig `yaml:"rate_limit"`
	PIISanitization bool           `yaml:"pii_sanitization"`
}

// RateLimitConfig — настройки rate limiting.
type RateLimitConfig struct {
	LogPerSec    int `yaml:"log_per_sec"`
	ConfigPerSec int `yaml:"config_per_sec"`
}

// SessionConfig — настройки сессий.
type SessionConfig struct {
	DefaultTTLHours int  `yaml:"default_ttl_hours"`
	MaxPerUser      int  `yaml:"max_per_user"`
	AutoExpireCheck bool `yaml:"auto_expire_check"`
}

// LoggingConfig — настройки логирования.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	Output string `yaml:"output"`
}

// PresetConfig — конфигурация пресета.
type PresetConfig struct {
	Description string   `yaml:"description"`
	Paths       []string `yaml:"paths"`
}

// IntegrationsConfig — настройки внешних интеграций.
type IntegrationsConfig struct {
	Foreman  ForemanConfig   `yaml:"foreman"`
	Webhooks []WebhookConfig `yaml:"webhooks"`
}

// ForemanConfig — настройки интеграции с Прорабом (оркестратор AI-агентов).
type ForemanConfig struct {
	Enabled    bool   `yaml:"enabled"`
	WebhookURL string `yaml:"webhook_url"`
}

// WebhookConfig — настройка webhook-уведомления (REV-W-001).
type WebhookConfig struct {
	ID         string `yaml:"id"`
	URL        string `yaml:"url"`
	PathFilter string `yaml:"path_filter"`
	Enabled    bool   `yaml:"enabled"`
}

// ProjectConfig — настройки структуры проекта для сканирования и инжекции.
// Все поля опциональны. Zero-value = AIS Platform defaults (обратная совместимость).
type ProjectConfig struct {
	// SourceRoot — корень исходников относительно CWD.
	// Пустая строка → используется "modules" (AIS Platform).
	// Для других проектов: "" (сканировать CWD), "src", "lib", "packages" и т.д.
	SourceRoot string `yaml:"source_root"`

	// TraceImports — настраиваемые формы импорта trace.
	TraceImports TraceImportsConfig `yaml:"trace_imports"`

	// ExcludedDirs — имена директорий, исключаемых из targets и audit.
	ExcludedDirs []string `yaml:"excluded_dirs"`
}

// TraceImportsConfig — формы импорта trace для разных языков.
// Все поля опциональны. Zero-value = AIS-совместимые дефолты.
type TraceImportsConfig struct {
	// PythonImportStmt — оператор импорта, вставляемый инжектором (с \n).
	// По умолчанию: "from modules.revizor_core_py_0_1_0 import trace, trace_start\n"
	PythonImportStmt string `yaml:"python_import_stmt"`

	// PythonImportPattern — паттерн для проверки наличия импорта (без \n, частичное совпадение).
	// По умолчанию: "from modules.revizor_core_py_0_1_0 import trace"
	PythonImportPattern string `yaml:"python_import_pattern"`

	// TypeScriptImportStmt — оператор импорта, вставляемый инжектором.
	// По умолчанию: import { trace } from "@ais-platform/revizor-sdk";\n
	TypeScriptImportStmt string `yaml:"typescript_import_stmt"`

	// AuditPythonPatterns — строки, которые trace_audit ищет в .py файлах.
	// По умолчанию: ["from modules.revizor_core_py_0_1_0 import", "from revizor_core_py_0_1_0 import"]
	AuditPythonPatterns []string `yaml:"audit_python_patterns"`

	// AuditTypeScriptPatterns — строки, которые trace_audit ищет в .ts/.tsx файлах.
	// По умолчанию: ["@ais-platform/revizor_core_ts_0_1_0"]
	AuditTypeScriptPatterns []string `yaml:"audit_typescript_patterns"`
}

// CleanupConfig — настройки очистки.
type CleanupConfig struct {
	LogRetentionHours  int `yaml:"log_retention_hours"`
	FileRetentionHours int `yaml:"file_retention_hours"`
	CleanupIntervalMin int `yaml:"cleanup_interval_min"`
}

// LoadConfig загружает конфигурацию из YAML-файла с переопределением через env.
// Приоритет: env > yaml > defaults.
func LoadConfig(path string) (*Config, error) {
	cfg := defaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("чтение конфига: %w", err)
			}
			// Файл не существует — используем defaults
		} else {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("парсинг YAML: %w", err)
			}
		}
	}

	applyEnvOverrides(cfg)

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:            "127.0.0.1",
			Port:            9876,
			ReadTimeoutSec:  10,
			WriteTimeoutSec: 10,
		},
		Storage: StorageConfig{
			Type:       "sqlite",
			SQLitePath: "./revizor.db",
		},
		Security: SecurityConfig{
			PIISanitization: true,
			RateLimit: RateLimitConfig{
				LogPerSec:    100,
				ConfigPerSec: 10,
			},
		},
		Session: SessionConfig{
			DefaultTTLHours: 168,
			MaxPerUser:      10,
			AutoExpireCheck: true,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
			Output: "stdout",
		},
		Project: ProjectConfig{
			TraceImports: TraceImportsConfig{
				AuditPythonPatterns:     []string{"from revizor_sdk import", "from modules.revizor_core_py_0_1_0 import", "from revizor_core_py_0_1_0 import"},
				AuditTypeScriptPatterns: []string{"@ais-platform/revizor-sdk", "@ais-platform/revizor_core_ts_0_1_0"},
			},
		},
		Cleanup: CleanupConfig{
			LogRetentionHours:  168,
			FileRetentionHours: 72,
			CleanupIntervalMin: 60,
		},
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("REVIZOR_SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = port
		}
	}
	if v := os.Getenv("REVIZOR_STORAGE_TYPE"); v != "" {
		cfg.Storage.Type = v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" && cfg.Storage.Type == "postgres" {
		cfg.Storage.PostgresURL = v
	}
	if v := os.Getenv("REVIZOR_API_KEY"); v != "" {
		cfg.Security.APIKey = v
	}
	if v := os.Getenv("REVIZOR_LICENSE_KEY"); v != "" {
		cfg.LicenseKey = v
	}
	if v := os.Getenv("REVIZOR_LICENSE_SERVER_URL"); v != "" {
		cfg.Server.LicenseServerURL = v
	}
	if v := os.Getenv("REVIZOR_LICENSE_PUBLIC_KEY"); v != "" {
		cfg.Server.LicensePublicKey = v
	}
	if v := os.Getenv("REVIZOR_LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("REVIZOR_SOURCE_ROOT"); v != "" {
		cfg.Project.SourceRoot = v
	}
}

func validate(cfg *Config) error {
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("невалидный порт: %d", cfg.Server.Port)
	}
	if cfg.Storage.Type != "sqlite" && cfg.Storage.Type != "postgres" {
		return fmt.Errorf("неподдерживаемый тип хранилища: %s", cfg.Storage.Type)
	}
	// REV-015: если sqlite_path не указан — использовать конфигурационную директорию
	if cfg.Storage.Type == "sqlite" && cfg.Storage.SQLitePath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			configDir = filepath.Join(os.Getenv("HOME"), ".config")
		}
		cfg.Storage.SQLitePath = filepath.Join(configDir, "ais_tools", "revizor", "revizor.db")
	}
	return nil
}

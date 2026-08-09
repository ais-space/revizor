package config

import (
	"os"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig with empty path should not error: %v", err)
	}
	if cfg.Server.Port != 9876 {
		t.Errorf("default port should be 9876, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("default host should be 127.0.0.1")
	}
	if cfg.Storage.Type != "sqlite" {
		t.Errorf("default storage type should be sqlite")
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("default log level should be info")
	}
	if cfg.Session.DefaultTTLHours != 168 {
		t.Errorf("default TTL should be 168 hours")
	}
}

func TestYAMLLoading(t *testing.T) {
	yamlContent := `
server:
  port: 9999
  host: "0.0.0.0"
storage:
  type: sqlite
  sqlite_path: "/tmp/test.db"
logging:
  level: debug
`
	tmpFile := t.TempDir() + "/test.yaml"
	if err := os.WriteFile(tmpFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp yaml: %v", err)
	}

	cfg, err := LoadConfig(tmpFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("port should be 9999, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("host should be 0.0.0.0")
	}
	if cfg.Storage.SQLitePath != "/tmp/test.db" {
		t.Errorf("sqlite_path should be /tmp/test.db")
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("log level should be debug")
	}
}

func TestEnvOverride(t *testing.T) {
	os.Setenv("REVIZOR_SERVER_PORT", "7777")
	defer os.Unsetenv("REVIZOR_SERVER_PORT")

	os.Setenv("REVIZOR_LOG_LEVEL", "warn")
	defer os.Unsetenv("REVIZOR_LOG_LEVEL")

	yamlContent := `
server:
  port: 8888
`
	tmpFile := t.TempDir() + "/test.yaml"
	os.WriteFile(tmpFile, []byte(yamlContent), 0644)

	cfg, err := LoadConfig(tmpFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	// Env should override YAML
	if cfg.Server.Port != 7777 {
		t.Errorf("env should override yaml port: got %d", cfg.Server.Port)
	}
	if cfg.Logging.Level != "warn" {
		t.Errorf("env should override log level: got %s", cfg.Logging.Level)
	}
}

func TestInvalidPort(t *testing.T) {
	yamlContent := `
server:
  port: 0
`
	tmpFile := t.TempDir() + "/test.yaml"
	os.WriteFile(tmpFile, []byte(yamlContent), 0644)

	_, err := LoadConfig(tmpFile)
	if err == nil {
		t.Error("port 0 should cause validation error")
	}
}

func TestInvalidStorageType(t *testing.T) {
	yamlContent := `
storage:
  type: mysql
`
	tmpFile := t.TempDir() + "/test.yaml"
	os.WriteFile(tmpFile, []byte(yamlContent), 0644)

	_, err := LoadConfig(tmpFile)
	if err == nil {
		t.Error("invalid storage type should cause validation error")
	}
}

func TestMissingConfigFile(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("missing config file should not error: %v", err)
	}
	if cfg.Server.Port != 9876 {
		t.Errorf("should use defaults when file missing")
	}
}

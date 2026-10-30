package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigLoad_DefaultValues(t *testing.T) {
	// Создаем временный файл конфигурации без опциональных полей
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.json")

	content := `{
		"apiBaseUrl": "https://api-ru.iiko.services",
		"apiKey": "test-key"
	}`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("ошибка записи временного файла: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load вернул ошибку: %v", err)
	}

	if cfg.Port != "8080" {
		t.Errorf("Port = %q; want 8080", cfg.Port)
	}
	if cfg.DeliveryCity != "Назрань" {
		t.Errorf("DeliveryCity = %q; want Назрань", cfg.DeliveryCity)
	}
	if cfg.TransportToFrontTimeout != 60 {
		t.Errorf("TransportToFrontTimeout = %d; want 60", cfg.TransportToFrontTimeout)
	}
	if cfg.APIKey != "test-key" {
		t.Errorf("APIKey = %q; want test-key", cfg.APIKey)
	}
}

func TestConfigLoad_EnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.json")

	content := `{"port": "8080", "apiKey": "file-key"}`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("ошибка записи временного файла: %v", err)
	}

	t.Setenv("PORT", "9090")
	t.Setenv("IIKO_API_KEY", "env-key")
	t.Setenv("TEST_ORDER_TOKEN", "secret-token")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load вернул ошибку: %v", err)
	}

	if cfg.Port != "9090" {
		t.Errorf("Port = %q; want 9090", cfg.Port)
	}
	if cfg.APIKey != "env-key" {
		t.Errorf("APIKey = %q; want env-key", cfg.APIKey)
	}
	if cfg.TestToken != "secret-token" {
		t.Errorf("TestToken = %q; want secret-token", cfg.TestToken)
	}
}

func TestConfigLoad_FallbackToEnvWhenFileMissing(t *testing.T) {
	t.Setenv("PORT", "3000")
	t.Setenv("IIKO_API_KEY", "standalone-key")
	t.Setenv("IIKO_APP_ID", "standalone-app")

	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("Load должен успешно загрузить конфигурацию из ENV: %v", err)
	}

	if cfg.Port != "3000" {
		t.Errorf("Port = %q; want 3000", cfg.Port)
	}
	if cfg.APIKey != "standalone-key" {
		t.Errorf("APIKey = %q; want standalone-key", cfg.APIKey)
	}
}

func TestConfigLoad_ErrorWhenMissingBothFileAndEnv(t *testing.T) {
	// Убеждаемся, что ENV пусты
	t.Setenv("IIKO_API_KEY", "")
	t.Setenv("IIKO_APP_ID", "")

	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err == nil {
		t.Error("Load должен вернуть ошибку при отсутствии файла и переменных окружения")
	}
}


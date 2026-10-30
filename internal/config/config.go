package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// OrderTypes хранит GUID типов заказов в iiko
type OrderTypes struct {
	DeliveryByCourier string `json:"deliveryByCourier"`
	DeliveryPickUp    string `json:"deliveryPickUp"`
}

// PaymentTypes хранит GUID способов оплаты в iiko
type PaymentTypes struct {
	Cash string `json:"cash"`
}

// Config содержит все параметры подключения к iikoCloud и конфигурацию HTTP-сервера
type Config struct {
	Port            string       `json:"port"`
	APIBaseURL      string       `json:"apiBaseUrl"`
	AppID           string       `json:"appId"`
	ClientSecret    string       `json:"clientSecret"`
	APIKey          string       `json:"apiKey"`
	OrganizationID  string       `json:"organizationId"`
	TerminalGroupID string       `json:"terminalGroupId"`
	OrderTypes      OrderTypes   `json:"orderTypes"`
	PaymentTypes    PaymentTypes `json:"paymentTypes"`

	// DeliveryCity — город для адреса доставки (iiko ищет улицу по названию и городу)
	DeliveryCity string `json:"deliveryCity"`
	// TransportToFrontTimeout — сколько секунд iikoFront может забирать заказ из облака,
	// после этого заказ аннулируется (в iiko по умолчанию 8 секунд)
	TransportToFrontTimeout int `json:"transportToFrontTimeout"`
	// TestToken — секрет для /api/test-order; пустой — тест доступен только с localhost
	TestToken string `json:"testToken"`
	// AllowedOrigins — разрешенные домены для CORS (по умолчанию production-домены и localhost)
	AllowedOrigins []string `json:"allowedOrigins"`
}

// Load загружает конфигурацию из файла (по умолчанию iiko_config.json) и применяет ENV-переменные
func Load(path string) (*Config, error) {
	if path == "" {
		path = "iiko_config.json"
	}

	cfg := &Config{
		Port:                    "8080", // Порт по умолчанию
		APIBaseURL:              "https://api-ru.iiko.services",
		DeliveryCity:            "Назрань",
		TransportToFrontTimeout: 60,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Если файл отсутствует, пробуем применить переменные окружения (12-Factor App)
			applyEnv(cfg)
			if cfg.APIKey != "" || cfg.AppID != "" {
				return cfg, nil
			}
			return nil, fmt.Errorf("конфигурационный файл %s не найден и обязательные переменные окружения не заданы: %w", path, err)
		}
		return nil, fmt.Errorf("ошибка чтения конфигурационного файла %s: %w", path, err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("ошибка парсинга конфигурации: %w", err)
	}

	applyEnv(cfg)
	return cfg, nil
}

// applyEnv переопределяет параметры конфигурации из переменных окружения
func applyEnv(cfg *Config) {
	if envPort := os.Getenv("PORT"); envPort != "" {
		cfg.Port = envPort
	}
	if envBaseURL := os.Getenv("IIKO_API_BASE_URL"); envBaseURL != "" {
		cfg.APIBaseURL = envBaseURL
	}
	if envCity := os.Getenv("DELIVERY_CITY"); envCity != "" {
		cfg.DeliveryCity = envCity
	}
	if envTimeout := os.Getenv("TRANSPORT_TIMEOUT_SEC"); envTimeout != "" {
		if t, err := strconv.Atoi(envTimeout); err == nil && t > 0 {
			cfg.TransportToFrontTimeout = t
		}
	}
	if envAPIKey := os.Getenv("IIKO_API_KEY"); envAPIKey != "" {
		cfg.APIKey = envAPIKey
	}
	if envAppID := os.Getenv("IIKO_APP_ID"); envAppID != "" {
		cfg.AppID = envAppID
	}
	if envSecret := os.Getenv("IIKO_CLIENT_SECRET"); envSecret != "" {
		cfg.ClientSecret = envSecret
	}
	if envOrgID := os.Getenv("IIKO_ORGANIZATION_ID"); envOrgID != "" {
		cfg.OrganizationID = envOrgID
	}
	if envTerminalID := os.Getenv("IIKO_TERMINAL_GROUP_ID"); envTerminalID != "" {
		cfg.TerminalGroupID = envTerminalID
	}
	if envToken := os.Getenv("TEST_ORDER_TOKEN"); envToken != "" {
		cfg.TestToken = envToken
	}
	if envOrigins := os.Getenv("ALLOWED_ORIGINS"); envOrigins != "" {
		parts := strings.Split(envOrigins, ",")
		var origins []string
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				origins = append(origins, trimmed)
			}
		}
		cfg.AllowedOrigins = origins
	}
}

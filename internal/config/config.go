package config

import (
	"encoding/json"
	"fmt"
	"os"
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
}

// Load загружает конфигурацию из файла (по умолчанию iiko_config.json) и применяет ENV-переменные
func Load(path string) (*Config, error) {
	if path == "" {
		path = "iiko_config.json"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения конфигурационного файла %s: %w", path, err)
	}

	cfg := &Config{
		Port: "8080", // Порт по умолчанию
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("ошибка парсинга конфигурации: %w", err)
	}

	// Переопределение через переменные окружения (стандарт 12-Factor App для продакшена)
	if envPort := os.Getenv("PORT"); envPort != "" {
		cfg.Port = envPort
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

	return cfg, nil
}


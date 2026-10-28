package iiko

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"beini-backend/internal/config"
	"beini-backend/internal/domain"
)

// Client — потокобезопасный HTTP-клиент для работы с iikoCloud API v2
type Client struct {
	cfg        *config.Config
	httpClient *http.Client
	logger     *slog.Logger

	// Кэш токена авторизации с двойной блокировкой RWMutex
	mu             sync.RWMutex
	cachedToken    string
	tokenExpiresAt time.Time
}

// NewClient инициализирует клиент iikoCloud
func NewClient(cfg *config.Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 35 * time.Second, // Таймаут сетевых запросов
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				TLSHandshakeTimeout:   20 * time.Second,
				ResponseHeaderTimeout: 25 * time.Second,
				MaxIdleConns:          10,
				IdleConnTimeout:       90 * time.Second,
			},
		},
		logger: logger,
	}
}

// GetToken возвращает валидный JWT-токен, обновляя его при необходимости
func (c *Client) GetToken(ctx context.Context) (string, error) {
	// 1. Быстрое чтение под Read Lock
	c.mu.RLock()
	if c.cachedToken != "" && time.Now().Before(c.tokenExpiresAt) {
		token := c.cachedToken
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	// 2. Эксклюзивная блокировка для обновления токена
	c.mu.Lock()
	defer c.mu.Unlock()

	// Повторная проверка (double-checked locking) на случай гонки горутин
	if c.cachedToken != "" && time.Now().Before(c.tokenExpiresAt) {
		return c.cachedToken, nil
	}

	c.logger.Info("запрос нового JWT-токена в iikoCloud API v2", "url", c.cfg.APIBaseURL+"/api/v2/access_token")

	reqBody := AccessTokenRequest{
		APILogin:     c.cfg.APIKey,
		AppID:        c.cfg.AppID,
		ClientSecret: c.cfg.ClientSecret,
	}

	var respBody AccessTokenResponse
	err := c.postJSON(ctx, "/api/v2/access_token", reqBody, &respBody, "")
	if err != nil {
		return "", fmt.Errorf("ошибка авторизации в iikoCloud: %w", err)
	}

	if respBody.Token == "" {
		return "", errors.New("получен пустой токен от iikoCloud API")
	}

	c.cachedToken = respBody.Token
	// Токен iiko действует 60 минут, обновляем его за 10 минут до истечения срока
	c.tokenExpiresAt = time.Now().Add(50 * time.Minute)

	c.logger.Info("JWT-токен iikoCloud успешно обновлен", "expiresAt", c.tokenExpiresAt.Format(time.RFC3339))
	return c.cachedToken, nil
}

// CreateDeliveryOrder отправляет заказ на кассовый терминал iikoFront
func (c *Client) CreateDeliveryOrder(ctx context.Context, order *domain.Order) (*CreateDeliveryResponse, error) {
	if err := order.Validate(); err != nil {
		return nil, fmt.Errorf("валидация заказа не пройдена: %w", err)
	}

	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	// Определение типа заказа в кассе
	orderTypeID := c.cfg.OrderTypes.DeliveryPickUp
	if order.DeliveryType == domain.DeliveryByCourier {
		orderTypeID = c.cfg.OrderTypes.DeliveryByCourier
	}

	// Формирование позиций блюд
	iikoItems := make([]IikoOrderItem, 0, len(order.Items))
	for _, it := range order.Items {
		iikoItems = append(iikoItems, IikoOrderItem{
			ProductID: it.ProductID,
			Price:     it.Price,
			Type:      "Product",
			Amount:    it.Amount,
		})
	}

	// Точка доставки (для курьера)
	var deliveryPoint *IikoDeliveryPoint
	if order.DeliveryType == domain.DeliveryByCourier && order.Address != "" {
		deliveryPoint = &IikoDeliveryPoint{
			Address: IikoAddress{
				Street: IikoStreet{Name: order.Address},
			},
		}
	}

	reqPayload := CreateDeliveryRequest{
		OrganizationID:  c.cfg.OrganizationID,
		TerminalGroupID: c.cfg.TerminalGroupID,
		Order: IikoOrder{
			Phone:         order.Phone,
			OrderTypeID:   orderTypeID,
			Customer:      IikoCustomer{Name: order.CustomerName, Type: "regular"},
			DeliveryPoint: deliveryPoint,
			Items:         iikoItems,
			Payments: []IikoPayment{
				{
					PaymentTypeKind: "Cash",
					Sum:             order.TotalSum(),
					PaymentTypeID:   c.cfg.PaymentTypes.Cash,
					IsPaid:          false,
				},
			},
			Comment: order.Comment,
		},
	}

	c.logger.Info("отправка заказа на кассовый терминал",
		"terminalGroupId", c.cfg.TerminalGroupID,
		"customer", order.CustomerName,
		"totalSum", order.TotalSum(),
	)

	var resp CreateDeliveryResponse
	err = c.postJSON(ctx, "/api/1/deliveries/create", reqPayload, &resp, token)
	if err != nil {
		return nil, fmt.Errorf("ошибка вызова /api/1/deliveries/create: %w", err)
	}

	return &resp, nil
}

// SendTestOrder отправляет тестовый заказ самовывоза на 0 ₽ для живой проверки в ресторане
func (c *Client) SendTestOrder(ctx context.Context) (*CreateDeliveryResponse, error) {
	testOrder := &domain.Order{
		CustomerName: "ТЕСТ САЙТА (Хамзат)",
		Phone:        "+79204827777",
		DeliveryType: domain.DeliveryPickUp,
		Comment:      "★ ТЕСТОВЫЙ ЗАКАЗ С САЙТА ★ Проверка связки сайта и кассы iikoFront. Не готовить!",
		Items: []domain.OrderItem{
			{
				ProductID: "04583ff0-44e7-4c5d-8476-fd549af5457f", // Турецкий кофе из номенклатуры BEINI
				Name:      "Турецкий кофе (Тест)",
				Price:     0,
				Amount:    1,
			},
		},
	}

	return c.CreateDeliveryOrder(ctx, testOrder)
}

// postJSON вспомогательный метод для выполнения POST запросов с сериализацией JSON
func (c *Client) postJSON(ctx context.Context, endpoint string, reqData any, respData any, bearerToken string) error {
	jsonBytes, err := json.Marshal(reqData)
	if err != nil {
		return fmt.Errorf("ошибка маршалинга JSON: %w", err)
	}

	fullURL := c.cfg.APIBaseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return fmt.Errorf("ошибка создания HTTP-запроса: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("сетевой сбой при обращении к %s: %w", fullURL, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ошибка чтения тела ответа: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("iiko API вернул HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if respData != nil {
		if err := json.Unmarshal(bodyBytes, respData); err != nil {
			return fmt.Errorf("ошибка десериализации JSON ответа: %w (тело: %s)", err, string(bodyBytes))
		}
	}

	return nil
}

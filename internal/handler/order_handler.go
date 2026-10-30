package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"beini-backend/internal/catalog"
	"beini-backend/internal/domain"
	"beini-backend/internal/idempotency"
	"beini-backend/internal/iiko"
)

// MaxOrderBodyBytes — максимальный размер JSON тела запроса оформления заказа (32 КБ)
const MaxOrderBodyBytes = 32 * 1024

// OrderHandler обрабатывает HTTP-запросы оформления заказов
type OrderHandler struct {
	iikoClient       *iiko.Client
	logger           *slog.Logger
	testToken        string
	catalog          catalog.Catalog
	idempotencyStore *idempotency.Store
}

// NewOrderHandler инициализирует OrderHandler с дефолтным каталогом и хранилищем идемпотентности
func NewOrderHandler(client *iiko.Client, logger *slog.Logger, testToken string) *OrderHandler {
	return NewOrderHandlerWithDeps(client, logger, testToken, nil, nil)
}

// NewOrderHandlerWithDeps инициализирует OrderHandler с явными зависимостями для внедрения и тестирования
func NewOrderHandlerWithDeps(
	client *iiko.Client,
	logger *slog.Logger,
	testToken string,
	cat catalog.Catalog,
	store *idempotency.Store,
) *OrderHandler {
	if logger == nil {
		logger = slog.Default()
	}
	if cat == nil {
		cat = catalog.DefaultBeiniCatalog()
	}
	if store == nil {
		store = idempotency.NewStore(15 * time.Minute)
	}
	return &OrderHandler{
		iikoClient:       client,
		logger:           logger,
		testToken:        testToken,
		catalog:          cat,
		idempotencyStore: store,
	}
}

// CreateOrder обрабатывает POST /api/order с валидацией, серверным расчетом цен и идемпотентностью
func (h *OrderHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Разрешен только метод POST")
		return
	}

	// 1. Проверка заголовка Content-Type (защита от CSRF и Simple Requests)
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "application/json") {
		h.sendError(w, http.StatusUnsupportedMediaType, "Поддерживается только Content-Type: application/json")
		return
	}

	// 2. Ограничение размера тела запроса (32 КБ) для предотвращения DoS-атак
	r.Body = http.MaxBytesReader(w, r.Body, MaxOrderBodyBytes)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			h.logger.Warn("превышен максимальный размер тела заказа", "limit", MaxOrderBodyBytes)
			h.sendError(w, http.StatusRequestEntityTooLarge, "Превышен максимальный размер тела запроса (32 КБ)")
			return
		}
		h.logger.Warn("ошибка чтения тела заказа", "error", err)
		h.sendError(w, http.StatusBadRequest, "Некорректный формат данных заказа")
		return
	}

	// 3. Вычисление отпечатка полезной нагрузки (SHA-256)
	sum := sha256.Sum256(rawBody)
	payloadHash := hex.EncodeToString(sum[:])

	// 4. Проверка Idempotency Key (защита от повторной отправки по RFC 9651)
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
	}

	var completeIdemp func(code int, header http.Header, body []byte)
	if idempotencyKey != "" {
		cached, complete, release, err := h.idempotencyStore.Lock(idempotencyKey, payloadHash)
		if err != nil {
			if errors.Is(err, idempotency.ErrConflict) {
				h.sendError(w, http.StatusConflict, "Заказ с данным ключом идемпотентности уже обрабатывается")
				return
			}
			if errors.Is(err, idempotency.ErrPayloadMismatch) {
				h.sendError(w, http.StatusUnprocessableEntity, "Ключ идемпотентности уже использован для другого заказа")
				return
			}
			h.sendError(w, http.StatusBadRequest, "Некорректный ключ идемпотентности (от 1 до 128 символов)")
			return
		}

		// Если ответ уже был успешно сформирован ранее — возвращаем кэшированный результат
		if cached != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Cache-Lookup", "HIT")
			w.WriteHeader(cached.StatusCode)
			_, _ = w.Write(cached.Body)
			return
		}

		defer release()
		completeIdemp = complete
	}

	// 5. Декодирование JSON структуры
	var order domain.Order
	if err := json.Unmarshal(rawBody, &order); err != nil {
		h.logger.Warn("ошибка парсинга JSON тела заказа", "error", err)
		h.sendError(w, http.StatusBadRequest, "Некорректный формат данных заказа")
		return
	}

	// 6. Сверка с серверным каталогом: сервер принимает ТОЛЬКО productId и количество,
	// полностью игнорируя присланные браузером price, name или sum, и вычисляет цену сам
	if err := order.ResolveAndVerifyPrices(h.catalog); err != nil {
		h.logger.Warn("ошибка сверки позиций заказа с каталогом", "error", err)
		h.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 7. Валидация доменных полей (телефон, адрес, ограничения длин)
	if err := order.Validate(); err != nil {
		h.logger.Warn("валидация заказа не пройдена", "error", err)
		h.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 8. Отправка проверенного заказа на кассу ресторана через iiko
	res, err := h.iikoClient.CreateDeliveryOrder(r.Context(), &order)
	if err != nil {
		h.handleOrderError(w, err)
		return
	}

	respData := map[string]any{
		"success":        true,
		"correlationId":  res.CorrelationID,
		"creationStatus": res.OrderInfo.CreationStatus,
		// pending — касса ещё не подтвердила приём; заказ сохранён и будет передан
		"pending":   res.OrderInfo.CreationStatus != iiko.StatusSuccess,
		"totalSum":  order.TotalSum(),
		"orderInfo": res.OrderInfo,
	}

	bodyBytes, err := json.Marshal(respData)
	if err != nil {
		h.logger.Error("ошибка маршалинга ответа заказа", "error", err)
		h.sendError(w, http.StatusInternalServerError, "Ошибка формирования ответа")
		return
	}

	// Фиксируем результат для идемпотентности
	if completeIdemp != nil {
		respHeader := make(http.Header)
		respHeader.Set("Content-Type", "application/json; charset=utf-8")
		completeIdemp(http.StatusOK, respHeader, bodyBytes)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bodyBytes)
}

// handleOrderError безопасно переводит ошибку клиента iiko в HTTP-ответ без раскрытия внутренних секретов
func (h *OrderHandler) handleOrderError(w http.ResponseWriter, err error) {
	var orderErr *iiko.OrderError
	if errors.As(err, &orderErr) {
		h.logger.Error("касса отклонила заказ",
			"status", orderErr.Status,
		)
		h.sendError(w, http.StatusUnprocessableEntity, "Касса ресторана не смогла принять заказ. Пожалуйста, оформите заказ по телефону")
		return
	}

	// Логируем на сервере без вывода секретов в тело клиенту
	h.logger.Error("ошибка передачи заказа в iikoCloud", "error", err)
	h.sendError(w, http.StatusBadGateway, "Не удалось передать заказ в кассу ресторана")
}

// testAllowed разрешает тестовый заказ по токену либо только с прямого localhost без проксирования
func (h *OrderHandler) testAllowed(r *http.Request) bool {
	if h.testToken != "" {
		got := r.Header.Get("X-Test-Token")
		return subtle.ConstantTimeCompare([]byte(got), []byte(h.testToken)) == 1
	}
	// Если токен не задан, запрещаем доступ при наличии внешних прокси-заголовков (защита за Nginx)
	if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-IP") != "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// SendTestOrder обрабатывает POST /api/test-order (тестовый прогон на кассу)
func (h *OrderHandler) SendTestOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Разрешен только метод POST")
		return
	}
	if !h.testAllowed(r) {
		h.sendError(w, http.StatusForbidden, "Тестовый заказ недоступен")
		return
	}

	h.logger.Info("запуск тестового заказа через API")
	res, err := h.iikoClient.SendTestOrder(r.Context())
	if err != nil {
		h.handleOrderError(w, err)
		return
	}

	h.sendJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"message":        "Тестовый заказ отправлен на терминал",
		"correlationId":  res.CorrelationID,
		"creationStatus": res.OrderInfo.CreationStatus,
		"orderInfo":      res.OrderInfo,
	})
}

// HealthCheck обрабатывает GET /api/health
func (h *OrderHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	h.sendJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "BEINI Restaurant iiko Gateway",
		"time":    time.Now().Format(time.RFC3339),
	})
}

// sendJSON сериализует ответ в формате JSON
func (h *OrderHandler) sendJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}

// sendError возвращает безопасный стандартный JSON с ошибкой
func (h *OrderHandler) sendError(w http.ResponseWriter, statusCode int, message string) {
	h.sendJSON(w, statusCode, map[string]any{
		"success": false,
		"error":   message,
	})
}

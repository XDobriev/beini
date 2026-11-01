package handler

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"beini-backend/internal/catalog"
	"beini-backend/internal/domain"
	"beini-backend/internal/idempotency"
	"beini-backend/internal/iiko"
	"beini-backend/internal/orderstate"
)

// MaxOrderBodyBytes — максимальный размер JSON тела запроса оформления заказа (32 КБ)
const MaxOrderBodyBytes = 32 * 1024

// OrderHandler обрабатывает HTTP-запросы оформления заказов
type OrderHandler struct {
	iikoClient *iiko.Client
	logger     *slog.Logger
	testToken  string
	catalog    catalog.Catalog
	states     *orderstate.Store
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	lifeMu     sync.Mutex
	closing    bool
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
	if store == nil {
		return NewOrderHandlerWithState(client, logger, testToken, cat, orderstate.NewMemory())
	}
	return NewOrderHandlerWithState(client, logger, testToken, cat, store.Orders())
}

// NewOrderHandlerWithState запускает опрос сохранённых pending. Файловым Store
// владеет интегратор: сначала Close handler, затем Close Store.
func NewOrderHandlerWithState(client *iiko.Client, logger *slog.Logger, testToken string, cat catalog.Catalog, states *orderstate.Store) *OrderHandler {
	if logger == nil {
		logger = slog.Default()
	}
	if cat == nil {
		cat = catalog.DefaultBeiniCatalog()
	}
	if states == nil {
		states = orderstate.NewMemory()
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &OrderHandler{iikoClient: client, logger: logger, testToken: testToken, catalog: cat, states: states, ctx: ctx, cancel: cancel}
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.track(ctx)
	}()
	return h
}

// Close останавливает worker и ждёт незавершённых create; Store не закрывает.
func (h *OrderHandler) Close() {
	h.lifeMu.Lock()
	h.closing = true
	h.cancel()
	h.lifeMu.Unlock()
	h.wg.Wait()
}

func (h *OrderHandler) beginWork() bool {
	h.lifeMu.Lock()
	defer h.lifeMu.Unlock()
	if h.closing {
		return false
	}
	h.wg.Add(1)
	return true
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

	payloadHash := orderstate.Hash(rawBody)
	key := requestKey(r)
	if key == "" {
		key, err = orderstate.NewID()
		if err != nil {
			h.sendError(w, http.StatusServiceUnavailable, "Не удалось выделить ключ заказа")
			return
		}
	}
	if err := idempotency.ValidateKey(key); err != nil {
		h.sendError(w, http.StatusBadRequest, "Некорректный ключ идемпотентности (от 1 до 128 символов)")
		return
	}
	w.Header().Set("Idempotency-Key", key)
	if !h.beginWork() {
		h.stateError(w)
		return
	}
	defer h.wg.Done()
	// Повтор не зависит от актуального меню: возвращаем уже сохранённую попытку.
	if rec, exists, err := h.states.Lookup(key); err != nil {
		h.stateError(w)
		return
	} else if exists {
		if rec.PayloadHash != payloadHash {
			h.payloadMismatch(w)
			return
		}
		w.Header().Set("X-Cache-Lookup", "HIT")
		h.sendOrderState(w, rec)
		return
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
	cat := h.catalog
	_, dynamic := cat.(interface{ Pin() catalog.Catalog })
	if p, ok := cat.(interface{ Pin() catalog.Catalog }); ok {
		cat = p.Pin()
	}
	if err := order.ResolveAndVerifyPrices(cat); err != nil {
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

	// Ожидаемые цены служат только согласием гостя, расчёт всегда серверный.
	// Проверяем каждую строку: одинаковый итог не скрывает изменение двух цен.
	var expected struct {
		ExpectedPrices []float64 `json:"expectedPrices"`
	}
	_ = json.Unmarshal(rawBody, &expected)
	if dynamic || expected.ExpectedPrices != nil {
		match := len(expected.ExpectedPrices) == len(order.Items)
		for i, item := range order.Items {
			if i >= len(expected.ExpectedPrices) || expected.ExpectedPrices[i] != item.Price {
				match = false
			}
		}
		if !match {
			w.Header().Set("Cache-Control", "no-store")
			h.sendJSON(w, http.StatusConflict, map[string]any{
				"success": false, "code": "price_changed",
				"error": "Цены изменились. Подтвердите обновлённый состав и сумму заказа.",
				"items": order.Items, "totalSum": order.TotalSum(),
			})
			return // До Reserve/create: это ещё не попытка отправки в кассу.
		}
	}

	source, err := json.Marshal(order)
	if err != nil {
		h.stateError(w)
		return
	}
	rec, fresh, err := h.states.ReserveWithNotification(key, payloadHash, order.TotalSum(), string(source))
	if errors.Is(err, orderstate.ErrMismatch) {
		h.payloadMismatch(w)
		return
	}
	if err != nil {
		h.logger.Error("не удалось сохранить попытку заказа", "error", err)
		h.stateError(w)
		return
	}
	if !fresh {
		w.Header().Set("X-Cache-Lookup", "HIT")
		h.sendOrderState(w, rec)
		return
	}
	defer h.states.Release(rec)

	// HTTP disconnect не отменяет создание; остановка handler и отдельный timeout
	// ограничивают запрос. Запись уже на диске, поэтому новый create невозможен.
	ctx, cancel := context.WithTimeout(h.ctx, 35*time.Second)
	defer cancel()
	res, err := h.iikoClient.SubmitDeliveryOrder(ctx, &order, rec.IikoID)
	if err != nil {
		h.logger.Warn("результат создания заказа неизвестен", "orderId", rec.ID, "error", err)
	} else {
		rec.CorrelationID = res.CorrelationID
		applyOrderInfo(&rec, res.OrderInfo)
		if err := h.states.Update(rec); err != nil {
			h.logger.Error("не удалось сохранить результат заказа", "orderId", rec.ID, "error", err)
			h.stateError(w)
			return
		}
	}
	h.sendOrderState(w, rec)
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

	if _, dynamic := h.catalog.(interface{ Pin() catalog.Catalog }); dynamic {
		h.sendError(w, http.StatusServiceUnavailable, "Тестовый заказ не настроен для синхронизированного меню")
		return
	}

	// Диагностический маршрут использует тот же журнал и правила повторов.
	payload, _ := json.Marshal(domain.Order{
		CustomerName: "Хамзат", Phone: "+79204827777", DeliveryType: domain.DeliveryPickUp,
		Comment: "Тестовый заказ, не готовить",
		Items:   []domain.OrderItem{{ProductID: "latte", Amount: 1}},
	})
	request := r.Clone(r.Context())
	request.Body = io.NopCloser(bytes.NewReader(payload))
	request.ContentLength = int64(len(payload))
	request.Header.Set("Content-Type", "application/json")
	h.CreateOrder(w, request)
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

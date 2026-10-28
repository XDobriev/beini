package handler

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"beini-backend/internal/domain"
	"beini-backend/internal/iiko"
)

// OrderHandler обрабатывает HTTP-запросы оформления заказов
type OrderHandler struct {
	iikoClient *iiko.Client
	logger     *slog.Logger
	testToken  string
}

// NewOrderHandler инициализирует OrderHandler.
// testToken защищает /api/test-order; пустой — тест доступен только с localhost.
func NewOrderHandler(client *iiko.Client, logger *slog.Logger, testToken string) *OrderHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &OrderHandler{
		iikoClient: client,
		logger:     logger,
		testToken:  testToken,
	}
}

// CreateOrder обрабатывает POST /api/order
func (h *OrderHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Разрешен только метод POST")
		return
	}

	var order domain.Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		h.logger.Warn("ошибка декодирования тела заказа", "error", err)
		h.sendError(w, http.StatusBadRequest, "Некорректный JSON формат заказа: "+err.Error())
		return
	}

	if err := order.Validate(); err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	res, err := h.iikoClient.CreateDeliveryOrder(r.Context(), &order)
	if err != nil {
		h.handleOrderError(w, err)
		return
	}

	h.sendJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"correlationId":  res.CorrelationID,
		"creationStatus": res.OrderInfo.CreationStatus,
		// pending — касса ещё не подтвердила приём; заказ сохранён и будет передан
		"pending":   res.OrderInfo.CreationStatus != iiko.StatusSuccess,
		"orderInfo": res.OrderInfo,
	})
}

// handleOrderError переводит ошибку клиента iiko в HTTP-ответ
func (h *OrderHandler) handleOrderError(w http.ResponseWriter, err error) {
	var orderErr *iiko.OrderError
	if errors.As(err, &orderErr) {
		h.logger.Error("касса отклонила заказ", "error", err)
		h.sendError(w, http.StatusUnprocessableEntity, "Касса не приняла заказ: "+orderErr.Error())
		return
	}
	h.logger.Error("ошибка передачи заказа в iikoCloud", "error", err)
	h.sendError(w, http.StatusBadGateway, "Не удалось передать заказ в кассу")
}

// testAllowed разрешает тестовый заказ по токену либо только с localhost
func (h *OrderHandler) testAllowed(r *http.Request) bool {
	if h.testToken != "" {
		got := r.Header.Get("X-Test-Token")
		return subtle.ConstantTimeCompare([]byte(got), []byte(h.testToken)) == 1
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

// sendError возвращает стандартный JSON с ошибкой
func (h *OrderHandler) sendError(w http.ResponseWriter, statusCode int, message string) {
	h.sendJSON(w, statusCode, map[string]any{
		"success": false,
		"error":   message,
	})
}

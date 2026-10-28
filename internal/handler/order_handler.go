package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"beini-backend/internal/domain"
	"beini-backend/internal/iiko"
)

// OrderHandler обрабатывает HTTP-запросы оформления заказов
type OrderHandler struct {
	iikoClient *iiko.Client
	logger     *slog.Logger
}

// NewOrderHandler инициализирует OrderHandler
func NewOrderHandler(client *iiko.Client, logger *slog.Logger) *OrderHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &OrderHandler{
		iikoClient: client,
		logger:     logger,
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

	res, err := h.iikoClient.CreateDeliveryOrder(r.Context(), &order)
	if err != nil {
		h.logger.Error("ошибка передачи заказа в iikoCloud", "error", err)
		h.sendError(w, http.StatusInternalServerError, "Ошибка создания заказа в кассе: "+err.Error())
		return
	}

	h.sendJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"correlationId":  res.CorrelationID,
		"creationStatus": res.OrderInfo.CreationStatus,
		"orderInfo":      res.OrderInfo,
	})
}

// SendTestOrder обрабатывает POST /api/test-order (тестовый прогон на кассу)
func (h *OrderHandler) SendTestOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	h.logger.Info("запуск тестового заказа через API")
	res, err := h.iikoClient.SendTestOrder(r.Context())
	if err != nil {
		h.logger.Error("ошибка при выполнении тестового заказа", "error", err)
		h.sendError(w, http.StatusInternalServerError, "Ошибка тестового заказа: "+err.Error())
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


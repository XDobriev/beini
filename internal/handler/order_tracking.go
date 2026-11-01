package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"beini-backend/internal/idempotency"
	"beini-backend/internal/iiko"
	"beini-backend/internal/orderstate"
)

func requestKey(r *http.Request) string {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		key = strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
	}
	return key
}

func applyOrderInfo(rec *orderstate.Record, info iiko.OrderInfo) {
	rec.CreationStatus = info.CreationStatus
	rec.OutcomeUnknown = false
	switch info.CreationStatus {
	case iiko.StatusSuccess:
		rec.Status = orderstate.Confirmed
	case iiko.StatusError, iiko.StatusCancelled:
		rec.Status = orderstate.Rejected
	default:
		rec.Status = orderstate.Pending
		// Новый или пустой статус нельзя трактовать как подтверждение.
		rec.OutcomeUnknown = info.CreationStatus != iiko.StatusInProgress
	}
}

func (h *OrderHandler) track(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		h.ReconcilePending(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ReconcilePending только читает iiko и сохраняет наблюдения. Повторного create
// здесь нет; допустим также запуск из внешнего scheduler, Store исключает гонки.
func (h *OrderHandler) ReconcilePending(ctx context.Context) {
	for _, rec := range h.states.AcquirePending() {
		func() {
			defer h.states.Release(rec)
			if ctx.Err() != nil {
				return
			}
			pollCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			info, err := h.iikoClient.GetOrderInfo(pollCtx, rec.IikoID)
			if err != nil {
				h.logger.Warn("не удалось сверить ожидающий заказ", "orderId", rec.ID, "error", err)
				return
			}
			applyOrderInfo(&rec, *info)
			if err := h.states.Update(rec); err != nil {
				h.logger.Error("не удалось сохранить статус заказа", "orderId", rec.ID, "error", err)
			}
		}()
	}
}

// OrderStatus — GET /api/order/status, доступ по исходному Idempotency-Key.
// Ключ — bearer capability; query string не используется, чтобы не писать его в URL-логи.
func (h *OrderHandler) OrderStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Разрешен только метод GET")
		return
	}
	key := requestKey(r)
	if idempotency.ValidateKey(key) != nil {
		h.sendError(w, http.StatusBadRequest, "Требуется корректный Idempotency-Key")
		return
	}
	rec, exists, err := h.states.Lookup(key)
	if err != nil {
		h.stateError(w)
		return
	}
	if !exists {
		h.sendError(w, http.StatusNotFound, "Заказ не найден")
		return
	}
	h.sendOrderState(w, rec)
}

func (h *OrderHandler) sendOrderState(w http.ResponseWriter, rec orderstate.Record) {
	w.Header().Set("Cache-Control", "no-store")
	if rec.Status == orderstate.Pending {
		w.Header().Set("Retry-After", "5")
	}
	data := map[string]any{
		"success":        rec.Status != orderstate.Rejected,
		"status":         rec.Status,
		"pending":        rec.Status == orderstate.Pending,
		"outcomeUnknown": rec.OutcomeUnknown,
		"orderId":        rec.ID,
		"correlationId":  rec.CorrelationID,
		"creationStatus": rec.CreationStatus,
		"totalSum":       rec.TotalSum,
		"orderInfo":      iiko.OrderInfo{ID: rec.IikoID, CreationStatus: rec.CreationStatus},
	}
	code := http.StatusOK
	data["message"] = "Заказ ожидает подтверждения кассы"
	if rec.Status == orderstate.Confirmed {
		data["message"] = "Заказ принят кассой"
	}
	if rec.Status == orderstate.Rejected {
		code = http.StatusUnprocessableEntity
		data["message"] = "Касса отклонила заказ"
		data["error"] = "Касса ресторана не смогла принять заказ. Пожалуйста, оформите заказ по телефону"
	}
	h.sendJSON(w, code, data)
}

func (h *OrderHandler) payloadMismatch(w http.ResponseWriter) {
	h.sendError(w, http.StatusUnprocessableEntity, "Ключ идемпотентности уже использован для другого заказа")
}

func (h *OrderHandler) stateError(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	h.sendError(w, http.StatusServiceUnavailable, "Не удалось проверить или сохранить состояние заказа. Повторите запрос с тем же ключом и телом")
}

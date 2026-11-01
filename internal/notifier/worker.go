// Package notifier доставляет сохранённые события вне пути оформления заказа.
package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"beini-backend/internal/domain"
	"beini-backend/internal/orderstate"
)

type Source interface {
	NotificationQueue() ([]orderstate.Notification, error)
}
type Transport interface {
	Send(context.Context, string) error
}

// ReceiptStore фиксирует попытки и ack независимо от журнала заказов.
type ReceiptStore interface {
	Lookup(string) (orderstate.Record, bool, error)
	SaveDelivery(string, int, time.Time, bool) error
}

type Worker struct {
	mu        sync.Mutex
	source    Source
	ledger    ReceiptStore
	transport Transport
	logger    *slog.Logger
	now       func() time.Time
}

func New(source Source, ledger ReceiptStore, transport Transport, logger *slog.Logger) *Worker {
	return &Worker{source: source, ledger: ledger, transport: transport, logger: logger, now: time.Now}
}
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		w.Step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Step сериализован также при ручном запуске. Никаких горутин на каждый заказ.
func (w *Worker) Step(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	defer func() {
		if recover() != nil {
			w.logger.Error("сбой worker уведомлений; события сохранены")
		}
	}()
	if !w.ready() {
		return
	}
	queue, err := w.source.NotificationQueue()
	if err != nil {
		w.logger.Error("источник уведомлений недоступен")
		return
	}
	// Группируем по заказу независимо от времени часов: pending всегда первым.
	groups := map[string][]orderstate.Notification{}
	var ids []string
	for _, n := range queue {
		if _, ok := groups[n.OrderID]; !ok {
			ids = append(ids, n.OrderID)
		}
		groups[n.OrderID] = append(groups[n.OrderID], n)
	}
	for _, id := range ids {
		events := groups[id]
		for _, n := range events {
			if n.Status == orderstate.Pending {
				if !w.deliver(ctx, n) {
					goto nextOrder
				}
			}
		}
		for _, n := range events {
			if n.Status != orderstate.Pending {
				if !w.deliver(ctx, n) {
					break
				}
			}
		}
	nextOrder:
		if ctx.Err() != nil {
			return
		}
	}
}
func (w *Worker) ready() bool {
	rec, _, err := w.ledger.Lookup("transport-cooldown")
	if err != nil {
		w.logger.Error("журнал уведомлений недоступен")
		return false
	}
	return !w.now().Before(rec.DeliveryNextAt)
}
func (w *Worker) deliver(ctx context.Context, n orderstate.Notification) bool {
	if !w.ready() {
		return false
	}
	parts, err := messages(n)
	if err != nil {
		w.logger.Error("некорректный источник уведомления", "eventId", n.ID)
		return false
	}
	for i, part := range parts {
		if ctx.Err() != nil {
			return false
		}
		key := fmt.Sprintf("%s:part:%d", n.ID, i)
		rec, ok, err := w.ledger.Lookup(key)
		if err != nil {
			w.logger.Error("журнал уведомлений недоступен")
			return false
		}
		if ok && rec.Status == orderstate.Confirmed {
			continue
		}
		now := w.now()
		if now.Before(rec.DeliveryNextAt) {
			return false
		}
		attempts := rec.DeliveryAttempts + 1
		if attempts > 31 {
			attempts = 31
		}
		delay := time.Second * time.Duration(1<<min(attempts, 9))
		next := now.Add(delay)
		// Фиксация до сети сохраняет backoff при рестарте во время отправки.
		if w.ledger.SaveDelivery(key, attempts, next, false) != nil {
			w.logger.Error("попытка уведомления не сохранена")
			return false
		}
		err = func() error {
			sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return w.transport.Send(sendCtx, part)
		}()
		if err != nil {
			// Интервал сервера начинается после ответа, а не до HTTP-запроса.
			now = w.now()
			next = now.Add(delay)
			var deliveryErr *DeliveryError
			if errors.As(err, &deliveryErr) {
				if deliveryErr.RetryAfter > delay {
					next = now.Add(deliveryErr.RetryAfter)
				}
				if deliveryErr.Permanent {
					next = now.Add(15 * time.Minute)
				}
			}
			// Сначала общая пауза: crash до записи срока части не должен
			// позволять другим заказам обойти 429 после рестарта.
			if w.ledger.SaveDelivery("transport-cooldown", attempts, next, false) != nil {
				w.logger.Error("пауза transport не сохранена")
				return false
			}
			if w.ledger.SaveDelivery(key, attempts, next, false) != nil {
				w.logger.Error("срок повтора уведомления не сохранён")
				return false
			}
			w.logger.Warn("уведомление не доставлено; повтор запланирован", "eventId", n.ID, "attempt", attempts)
			return false
		}
		if w.ledger.SaveDelivery(key, attempts, time.Time{}, true) != nil {
			w.logger.Error("подтверждение уведомления не сохранено", "eventId", n.ID)
			return false
		}
	}
	return true
}

func messages(n orderstate.Notification) ([]string, error) {
	var order domain.Order
	if err := json.Unmarshal(n.Source, &order); err != nil {
		return nil, err
	}
	state := "ОЖИДАНИЕ: результат кассы ещё не подтверждён. Не создавайте повторный заказ; сначала сверьте кассу."
	switch n.Status {
	case orderstate.Confirmed:
		state = "ПОДТВЕРЖДЁН: заказ принят кассой. Это не подтверждение оплаты или фискализации."
	case orderstate.Rejected:
		state = "ОТКАЗ: касса отклонила заказ. Свяжитесь с гостем и проверьте кассу перед ручным оформлением."
	case orderstate.Pending:
	default:
		return nil, errors.New("неизвестный статус")
	}
	kind := "Самовывоз"
	if order.DeliveryType == domain.DeliveryByCourier {
		kind = "Доставка"
	}
	var body strings.Builder
	fmt.Fprintf(&body, "%s\n%s\nКлиент: %s\nТелефон: %s\nАдрес: %s\nКомментарий: %s\nСостав:\n", state, kind, order.CustomerName, order.Phone, order.Address, order.Comment)
	for _, item := range order.Items {
		fmt.Fprintf(&body, "• %s × %d — %.2f ₽\n", item.Name, item.Amount, item.Price*float64(item.Amount))
	}
	fmt.Fprintf(&body, "Итого: %.2f ₽\nСобытие UTC: %s", order.TotalSum(), n.CreatedAt.UTC().Format(time.RFC3339))
	// Plain text: пользовательские данные не интерпретируются как HTML/Markdown.
	// Лимит считаем в UTF-16, включая supplementary characters.
	var chunks []string
	var chunk strings.Builder
	units := 0
	for _, r := range body.String() {
		cost := 1
		if r > 0xffff {
			cost = 2
		}
		if units+cost > 3500 {
			chunks = append(chunks, chunk.String())
			chunk.Reset()
			units = 0
		}
		chunk.WriteRune(r)
		units += cost
	}
	if chunk.Len() > 0 {
		chunks = append(chunks, chunk.String())
	}
	for i := range chunks {
		chunks[i] = fmt.Sprintf("BEINI • %s • часть %d/%d\n%s", n.ID, i+1, len(chunks), chunks[i])
	}
	return chunks, nil
}

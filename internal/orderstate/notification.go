package orderstate

import (
	"encoding/json"
	"sort"
	"time"
)

// Notification — локальное недоставленное событие. ID стабилен после рестарта.
// Transport и durable receipts находятся в internal/notifier и отдельном журнале.
type Notification struct {
	ID        string          `json:"id"`
	OrderID   string          `json:"orderId"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"createdAt"`
	Source    json.RawMessage `json:"source"`
}

// NotificationQueue восстанавливает pending и конечное событие из одного
// транзакционного журнала. Нет окна между записью состояния и enqueue.
// Очередь намеренно не выставлена через HTTP. Notifier фильтрует доставленные ID.
func (s *Store) NotificationQueue() ([]Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.failed {
		return nil, ErrUnavailable
	}
	var queue []Notification
	for _, rec := range s.records {
		if rec.NotificationSource == "" {
			continue
		} // Старые записи без состава.
		queue = append(queue, Notification{ID: rec.ID + ":" + Pending, OrderID: rec.ID,
			Status: Pending, CreatedAt: rec.CreatedAt, Source: json.RawMessage(rec.NotificationSource)})
		if rec.Status != Pending {
			queue = append(queue, Notification{ID: rec.ID + ":" + rec.Status, OrderID: rec.ID,
				Status: rec.Status, CreatedAt: rec.UpdatedAt, Source: json.RawMessage(rec.NotificationSource)})
		}
	}
	sort.Slice(queue, func(i, j int) bool {
		if queue[i].CreatedAt.Equal(queue[j].CreatedAt) {
			return queue[i].ID < queue[j].ID
		}
		return queue[i].CreatedAt.Before(queue[j].CreatedAt)
	})
	return queue, nil
}

package orderstate

import "time"

// SaveDelivery используется только отдельным журналом уведомлений, а не orders.
// Ошибка fsync блокирует этот журнал до переоткрытия; ack не появляется в памяти.
func (s *Store) SaveDelivery(key string, attempts int, next time.Time, delivered bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.failed {
		return ErrUnavailable
	}
	hash := Hash([]byte(key))
	rec, ok := s.records[hash]
	if ok && rec.Status == Confirmed {
		return nil
	}
	if !ok {
		rec = Record{KeyHash: hash, ID: hash, PayloadHash: hash, Status: Pending, CreatedAt: time.Now().UTC()}
	}
	rec.DeliveryAttempts = attempts
	rec.DeliveryNextAt = next
	rec.UpdatedAt = time.Now().UTC()
	if delivered {
		rec.Status = Confirmed
	}
	return s.save(rec)
}

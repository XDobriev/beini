package idempotency

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ErrConflict    = errors.New("запрос с данным ключом идемпотентности уже обрабатывается")
	ErrInvalidKey  = errors.New("некорректный ключ идемпотентности (допустимо от 1 до 128 символов)")
)

type Status int

const (
	StatusPending Status = iota
	StatusCompleted
)

type CachedResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type record struct {
	status    Status
	resp      *CachedResponse
	createdAt time.Time
}

// Store хранит статусы и ответы запросов по Idempotency Key
type Store struct {
	mu      sync.RWMutex
	records map[string]*record
	ttl     time.Duration
}

// NewStore создает хранилище с заданным временем жизни записей (TTL)
func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	s := &Store{
		records: make(map[string]*record),
		ttl:     ttl,
	}
	return s
}

// ValidateKey проверяет формат ключа
func ValidateKey(key string) error {
	k := strings.TrimSpace(key)
	if k == "" || len(k) > 128 {
		return ErrInvalidKey
	}
	return nil
}

// Lock проверяет статус ключа:
// - если ключ уже завершен, возвращает cached resp и nil error
// - если ключ сейчас в обработке (pending), возвращает ErrConflict
// - если ключа нет, резервирует статус pending и возвращает функции complete и release
func (s *Store) Lock(key string) (cached *CachedResponse, complete func(code int, header http.Header, body []byte), release func(), err error) {
	if err := ValidateKey(key); err != nil {
		return nil, nil, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Очистка устаревших записей
	now := time.Now()
	for k, r := range s.records {
		if now.Sub(r.createdAt) > s.ttl {
			delete(s.records, k)
		}
	}

	if r, exists := s.records[key]; exists {
		if r.status == StatusCompleted && r.resp != nil {
			return r.resp, nil, nil, nil
		}
		// Запрос в процессе выполнения
		return nil, nil, nil, ErrConflict
	}

	// Создаем новую запись со статусом Pending
	s.records[key] = &record{
		status:    StatusPending,
		createdAt: now,
	}

	var once sync.Once

	complete = func(code int, header http.Header, body []byte) {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			// Сохраняем заголовок Content-Type
			clonedHeader := make(http.Header)
			if ct := header.Get("Content-Type"); ct != "" {
				clonedHeader.Set("Content-Type", ct)
			}
			s.records[key] = &record{
				status: StatusCompleted,
				resp: &CachedResponse{
					StatusCode: code,
					Header:     clonedHeader,
					Body:       body,
				},
				createdAt: time.Now(),
			}
		})
	}

	release = func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			// Если запись осталась Pending (например, фатальный сбой или паника), удаляем ее
			if r, exists := s.records[key]; exists && r.status == StatusPending {
				delete(s.records, key)
			}
		})
	}

	return nil, complete, release, nil
}


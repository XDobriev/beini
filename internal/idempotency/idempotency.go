package idempotency

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"beini-backend/internal/orderstate"
)

var (
	ErrConflict        = errors.New("запрос с данным ключом идемпотентности уже обрабатывается")
	ErrInvalidKey      = errors.New("некорректный ключ идемпотентности (допустимо от 1 до 128 символов)")
	ErrPayloadMismatch = errors.New("ключ идемпотентности уже использован с другим телом запроса")
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
	status      Status
	payloadHash string
	resp        *CachedResponse
	createdAt   time.Time
}

// Store хранит статусы и ответы запросов по Idempotency Key
type Store struct {
	mu          sync.RWMutex
	records     map[string]*record
	ttl         time.Duration
	stopCleaner chan struct{}
	orders      *orderstate.Store
	closeOnce   sync.Once
}

// NewStore создает хранилище с заданным временем жизни записей (TTL)
func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	s := &Store{
		records:     make(map[string]*record),
		ttl:         ttl,
		stopCleaner: make(chan struct{}),
		orders:      orderstate.NewMemory(),
	}

	// Фоновый сборщик устаревших записей (не блокирует горячий путь Lock)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.Sweep()
			case <-s.stopCleaner:
				return
			}
		}
	}()

	return s
}

// Close останавливает фоновый сборщик мусора
func (s *Store) Close() {
	s.closeOnce.Do(func() { close(s.stopCleaner) })
}

// Orders — совместимость старого конструктора handler. Заказы имеют отдельный
// жизненный цикл и не удаляются сборщиком TTL кэша HTTP-ответов.
func (s *Store) Orders() *orderstate.Store { return s.orders }

// Sweep удаляет устаревшие записи
func (s *Store) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, r := range s.records {
		if r.status == StatusCompleted && now.Sub(r.createdAt) > s.ttl {
			delete(s.records, k)
		}
	}
}

// ValidateKey проверяет формат ключа
func ValidateKey(key string) error {
	k := strings.TrimSpace(key)
	if k == "" || len(k) > 128 {
		return ErrInvalidKey
	}
	return nil
}

// Lock проверяет статус ключа и соответствие полезной нагрузки
func (s *Store) Lock(key, payloadHash string) (cached *CachedResponse, complete func(code int, header http.Header, body []byte), release func(), err error) {
	if err := ValidateKey(key); err != nil {
		return nil, nil, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	if r, exists := s.records[key]; exists {
		// Если запись просрочена — перезаписываем
		if r.status == StatusCompleted && now.Sub(r.createdAt) > s.ttl {
			delete(s.records, key)
		} else {
			// Проверка совпадения полезной нагрузки
			if r.payloadHash != "" && payloadHash != "" && r.payloadHash != payloadHash {
				return nil, nil, nil, ErrPayloadMismatch
			}

			if r.status == StatusCompleted && r.resp != nil {
				return r.resp, nil, nil, nil
			}
			// Запрос в процессе выполнения
			return nil, nil, nil, ErrConflict
		}
	}

	// Создаем новую запись со статусом Pending
	s.records[key] = &record{
		status:      StatusPending,
		payloadHash: payloadHash,
		createdAt:   now,
	}

	var once sync.Once

	complete = func(code int, header http.Header, body []byte) {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			clonedHeader := make(http.Header)
			if ct := header.Get("Content-Type"); ct != "" {
				clonedHeader.Set("Content-Type", ct)
			}
			s.records[key] = &record{
				status:      StatusCompleted,
				payloadHash: payloadHash,
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
			if r, exists := s.records[key]; exists && r.status == StatusPending {
				delete(s.records, key)
			}
		})
	}

	return nil, complete, release, nil
}

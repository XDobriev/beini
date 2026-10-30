package middleware

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type clientRecord struct {
	timestamps []time.Time
}

// RateLimiter реализует алгоритм скользящего окна (sliding window) по IP клиента
type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientRecord
	limit   int
	window  time.Duration
}

// NewRateLimiter инициализирует ограничитель частоты запросов
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = 10
	}
	if window <= 0 {
		window = time.Minute
	}
	return &RateLimiter{
		clients: make(map[string]*clientRecord),
		limit:   limit,
		window:  window,
	}
}

// Allow проверяет, разрешен ли запрос для данного IP, и возвращает retryAfter если превышен
func (rl *RateLimiter) Allow(ip string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	rec, exists := rl.clients[ip]
	if !exists {
		rec = &clientRecord{timestamps: make([]time.Time, 0, rl.limit)}
		rl.clients[ip] = rec
	}

	// Фильтруем устаревшие запросы
	valid := rec.timestamps[:0]
	for _, t := range rec.timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	rec.timestamps = valid

	if len(rec.timestamps) >= rl.limit {
		// Время до освобождения самого раннего слота
		oldest := rec.timestamps[0]
		retryAfter := oldest.Add(rl.window).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return false, retryAfter
	}

	rec.timestamps = append(rec.timestamps, now)
	return true, 0
}

// ExtractIP извлекает IP адрес клиента из RemoteAddr
func ExtractIP(r *http.Request) string {
	// Если настроен заголовок X-Real-IP от локального reverse-proxy (Nginx)
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		return xrip
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// RateLimit возвращает HTTP middleware для защиты эндпоинта от спама и DoS
func RateLimit(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ExtractIP(r)
			allowed, retryAfter := limiter.Allow(ip)
			if !allowed {
				retrySeconds := int(retryAfter.Seconds())
				if retrySeconds <= 0 {
					retrySeconds = 1
				}
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retrySeconds))
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"error":   "Слишком много запросов. Пожалуйста, подождите перед следующим заказом",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}


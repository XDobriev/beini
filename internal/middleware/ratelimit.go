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
	mu          sync.Mutex
	clients     map[string]*clientRecord
	limit       int
	window      time.Duration
	stopCleaner chan struct{}
}

// NewRateLimiter инициализирует ограничитель частоты запросов с автоочисткой устаревших IP
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = 10
	}
	if window <= 0 {
		window = time.Minute
	}
	rl := &RateLimiter{
		clients:     make(map[string]*clientRecord),
		limit:       limit,
		window:      window,
		stopCleaner: make(chan struct{}),
	}

	// Фоновый сборщик устаревших записей для предотвращения утечек памяти
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rl.Sweep()
			case <-rl.stopCleaner:
				return
			}
		}
	}()

	return rl
}

// Close останавливает фоновый сборщик мусора
func (rl *RateLimiter) Close() {
	select {
	case <-rl.stopCleaner:
	default:
		close(rl.stopCleaner)
	}
}

// Sweep удаляет IP-адреса, у которых истекли все временные метки
func (rl *RateLimiter) Sweep() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.window)
	for ip, rec := range rl.clients {
		validCount := 0
		for _, t := range rec.timestamps {
			if t.After(cutoff) {
				validCount++
			}
		}
		if validCount == 0 {
			delete(rl.clients, ip)
		}
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

// ExtractIP извлекает реальный IP адрес клиента с защитой от спуфинга заголовков
func ExtractIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	// Доверяем X-Real-IP и X-Forwarded-For ТОЛЬКО если запрос пришел от локального доверенного reverse-proxy (Nginx)
	remoteIP := net.ParseIP(host)
	if remoteIP != nil && remoteIP.IsLoopback() {
		if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
			return xrip
		}
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			parts := strings.Split(xff, ",")
			first := strings.TrimSpace(parts[0])
			if first != "" {
				return first
			}
		}
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

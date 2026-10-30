package middleware

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// responseWriterWrapper перехватывает код статуса ответа для логирования
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Logger возвращает middleware для структурированного логирования HTTP-запросов через log/slog
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)
			logger.Info("HTTP запрос обработан",
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.statusCode,
				"duration", duration.String(),
				"remote", r.RemoteAddr,
			)
		})
	}
}

// Recoverer перехватывает возможные паники и возвращает 500 Internal Server Error без утечки чувствительных данных
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					// Логируем на сервере факт паники без вывода секретов
					logger.Error("паника при обработке запроса", "panic", rec, "path", r.URL.Path)
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"success": false, "error": "Внутренняя ошибка сервера"}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// DefaultAllowedOrigins — список production-доменов ресторана BEINI по умолчанию
var DefaultAllowedOrigins = []string{
	"https://beini.ru",
	"https://www.beini.ru",
}

// IsOriginAllowed проверяет, входит ли переданный Origin в список разрешенных
// Разрешены: localhost / 127.0.0.1 (любой порт для разработки) и указанные production-домены
func IsOriginAllowed(origin string, allowedOrigins []string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return false
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	hostname := strings.ToLower(u.Hostname())

	// 1. Проверка localhost / 127.0.0.1 для локальной разработки
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return u.Scheme == "http" || u.Scheme == "https"
	}

	// 2. Проверка разрешенных доменов
	originsToCheck := allowedOrigins
	if len(originsToCheck) == 0 {
		originsToCheck = DefaultAllowedOrigins
	}

	for _, allowed := range originsToCheck {
		allowed = strings.TrimSpace(allowed)
		if strings.EqualFold(origin, allowed) {
			return true
		}
	}

	return false
}

// NewCORS создает CORS middleware с белым списком доменов
func NewCORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Если Origin отсутствует (same-origin запрос или curl без браузера), передаем запрос дальше
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Проверяем origin по белому списку
			if IsOriginAllowed(origin, allowedOrigins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Test-Token, Idempotency-Key, X-Idempotency-Key")
				w.Header().Set("Access-Control-Max-Age", "86400")

				// Preflight OPTIONS запрос
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusOK)
					return
				}

				next.ServeHTTP(w, r)
				return
			}

			// Если origin НЕ разрешен:
			if r.Method == http.MethodOptions {
				http.Error(w, `{"error": "CORS origin не разрешен"}`, http.StatusForbidden)
				return
			}

			// Для обычных запросов с недоверенным Origin не ставим Access-Control-Allow-Origin
			next.ServeHTTP(w, r)
		})
	}
}

// CORS — дефолтное безопасное middleware для обратной совместимости
func CORS(next http.Handler) http.Handler {
	return NewCORS(nil)(next)
}

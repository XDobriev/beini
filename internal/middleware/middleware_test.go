package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_AllowedOrigins(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	cors := CORS(nextHandler)

	tests := []struct {
		name         string
		origin       string
		method       string
		wantStatus   int
		wantAllow    string
		wantNoOrigin bool
	}{
		{
			name:       "Localhost development port 3000 OPTIONS",
			origin:     "http://localhost:3000",
			method:     http.MethodOptions,
			wantStatus: http.StatusOK,
			wantAllow:  "http://localhost:3000",
		},
		{
			name:       "127.0.0.1 development port 8080 POST",
			origin:     "http://127.0.0.1:8080",
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantAllow:  "http://127.0.0.1:8080",
		},
		{
			name:       "Production domain beini.ru",
			origin:     "https://beini.ru",
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantAllow:  "https://beini.ru",
		},
		{
			name:       "Production domain www.beini.ru OPTIONS",
			origin:     "https://www.beini.ru",
			method:     http.MethodOptions,
			wantStatus: http.StatusOK,
			wantAllow:  "https://www.beini.ru",
		},
		{
			name:         "Disallowed origin evil.com OPTIONS",
			origin:       "https://evil.com",
			method:       http.MethodOptions,
			wantStatus:   http.StatusForbidden,
			wantNoOrigin: true,
		},
		{
			name:         "Disallowed origin evil.com POST",
			origin:       "https://evil.com",
			method:       http.MethodPost,
			wantStatus:   http.StatusOK, // Запрос обрабатывается, но заголовок CORS браузеру не дается
			wantNoOrigin: true,
		},
		{
			name:         "Same origin (empty Origin header)",
			origin:       "",
			method:       http.MethodGet,
			wantStatus:   http.StatusOK,
			wantNoOrigin: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/order", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()

			cors.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("Code = %d; want %d", w.Code, tc.wantStatus)
			}

			gotAllow := w.Header().Get("Access-Control-Allow-Origin")
			if tc.wantNoOrigin {
				if gotAllow != "" {
					t.Errorf("Access-Control-Allow-Origin = %q; want empty", gotAllow)
				}
			} else {
				if gotAllow != tc.wantAllow {
					t.Errorf("Access-Control-Allow-Origin = %q; want %q", gotAllow, tc.wantAllow)
				}
			}
		})
	}
}

func TestCORS_CustomAllowedOrigins(t *testing.T) {
	customCORS := NewCORS([]string{"https://my-custom-domain.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/order", nil)
	req.Header.Set("Origin", "https://my-custom-domain.com")
	w := httptest.NewRecorder()
	customCORS.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Code = %d; want 200", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://my-custom-domain.com" {
		t.Errorf("Access-Control-Allow-Origin = %q; want https://my-custom-domain.com", got)
	}
}

func TestRecoverer(t *testing.T) {
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("неожиданная критическая ошибка с секретным токеном secret_token_xyz")
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	recoverer := Recoverer(logger)(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()

	recoverer.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Code = %d; want 500", w.Code)
	}

	body := w.Body.String()
	// Проверяем, что секрет не утек в тело ответа
	if body == "" || body != `{"success": false, "error": "Внутренняя ошибка сервера"}` {
		t.Errorf("Body = %s; want safe error message", body)
	}
}

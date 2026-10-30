package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_Allow(t *testing.T) {
	limiter := NewRateLimiter(3, 100*time.Millisecond)
	ip := "192.168.1.10"

	// 1, 2, 3 разрешены
	for i := 1; i <= 3; i++ {
		allowed, _ := limiter.Allow(ip)
		if !allowed {
			t.Fatalf("запрос %d должен быть разрешен", i)
		}
	}

	// 4-й запрос превышает лимит
	allowed, retryAfter := limiter.Allow(ip)
	if allowed {
		t.Fatalf("4-й запрос должен быть заблокирован")
	}
	if retryAfter <= 0 {
		t.Errorf("retryAfter должно быть > 0")
	}

	// Другой IP не блокируется
	otherIP := "192.168.1.11"
	allowedOther, _ := limiter.Allow(otherIP)
	if !allowedOther {
		t.Fatalf("запрос с другого IP должен быть разрешен")
	}

	// Ждем истечения окна
	time.Sleep(120 * time.Millisecond)

	// Снова разрешен
	allowedAfterWait, _ := limiter.Allow(ip)
	if !allowedAfterWait {
		t.Fatalf("запрос после истечения окна должен быть разрешен")
	}
}

func TestRateLimit_Middleware(t *testing.T) {
	limiter := NewRateLimiter(2, time.Second)
	handler := RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req1 := httptest.NewRequest(http.MethodPost, "/api/order", nil)
	req1.RemoteAddr = "10.0.0.1:1234"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Errorf("req1 status = %d; want 200", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/order", nil)
	req2.RemoteAddr = "10.0.0.1:1234"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("req2 status = %d; want 200", w2.Code)
	}

	// 3-й запрос -> 429 Too Many Requests
	req3 := httptest.NewRequest(http.MethodPost, "/api/order", nil)
	req3.RemoteAddr = "10.0.0.1:1234"
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)

	if w3.Code != http.StatusTooManyRequests {
		t.Errorf("req3 status = %d; want 429", w3.Code)
	}
	if w3.Header().Get("Retry-After") == "" {
		t.Errorf("ожидался заголовок Retry-After")
	}

	var resp map[string]any
	_ = json.NewDecoder(w3.Body).Decode(&resp)
	if resp["success"] != false {
		t.Errorf("success = %v; want false", resp["success"])
	}
}


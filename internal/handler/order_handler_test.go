package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"beini-backend/internal/catalog"
	"beini-backend/internal/config"
	"beini-backend/internal/idempotency"
	"beini-backend/internal/iiko"
)

// newMockServer создает локальный mock iikoCloud для безопасного тестирования хендлеров без внешних вызовов
func newMockServer(t *testing.T) (*httptest.Server, *iiko.CreateDeliveryRequest) {
	t.Helper()
	var captured iiko.CreateDeliveryRequest

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(iiko.AccessTokenResponse{Token: "mock-jwt-token"})
	})
	mux.HandleFunc("/api/1/deliveries/create", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		resp := iiko.CreateDeliveryResponse{
			CorrelationID: "corr-12345",
			OrderInfo: iiko.OrderInfo{
				ID:             "order-test-id",
				CreationStatus: iiko.StatusSuccess,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/1/deliveries/by_id", func(w http.ResponseWriter, r *http.Request) {
		info := iiko.OrderInfo{ID: "order-test-id", CreationStatus: iiko.StatusSuccess}
		_ = json.NewEncoder(w).Encode(iiko.OrdersByIDResponse{Orders: []iiko.OrderInfo{info}})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &captured
}

func newTestHandlerWithServer(t *testing.T, srvURL string, testToken string) (*OrderHandler, *catalog.MemoryCatalog, *idempotency.Store) {
	t.Helper()
	cfg := &config.Config{
		APIBaseURL:              srvURL,
		OrganizationID:          "org-id",
		TerminalGroupID:         "term-id",
		DeliveryCity:            "Назрань",
		TransportToFrontTimeout: 60,
		TestToken:               testToken,
		OrderTypes:              config.OrderTypes{DeliveryByCourier: "courier-guid", DeliveryPickUp: "pickup-guid"},
		PaymentTypes:            config.PaymentTypes{Cash: "cash-guid"},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := iiko.NewClient(cfg, logger)
	cat := catalog.DefaultBeiniCatalog()
	store := idempotency.NewStore(10 * time.Minute)
	h := NewOrderHandlerWithDeps(client, logger, testToken, cat, store)
	return h, cat, store
}

func newTestHandler(testToken string) *OrderHandler {
	cfg := &config.Config{
		APIBaseURL: "http://localhost:0",
		TestToken:  testToken,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := iiko.NewClient(cfg, logger)
	return NewOrderHandler(client, logger, testToken)
}

func TestHealthCheck(t *testing.T) {
	h := newTestHandler("")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	h.HealthCheck(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d; want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("ошибка декодирования ответа: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("status = %v; want ok", body["status"])
	}
	if body["service"] != "BEINI Restaurant iiko Gateway" {
		t.Errorf("service = %v; want BEINI Restaurant iiko Gateway", body["service"])
	}
}

func TestCreateOrder_MethodNotAllowed(t *testing.T) {
	h := newTestHandler("")

	req := httptest.NewRequest(http.MethodGet, "/api/order", nil)
	w := httptest.NewRecorder()

	h.CreateOrder(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("StatusCode = %d; want 405", resp.StatusCode)
	}
}

func TestCreateOrder_InvalidJSON(t *testing.T) {
	h := newTestHandler("")

	req := httptest.NewRequest(http.MethodPost, "/api/order", strings.NewReader("{invalid-json"))
	w := httptest.NewRecorder()

	h.CreateOrder(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d; want 400", resp.StatusCode)
	}
}

func TestCreateOrder_ValidationError(t *testing.T) {
	h := newTestHandler("")

	// Заказ без номера телефона и без блюд
	emptyOrder := `{"customerName": "Иван"}`
	req := httptest.NewRequest(http.MethodPost, "/api/order", strings.NewReader(emptyOrder))
	w := httptest.NewRecorder()

	h.CreateOrder(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d; want 400", resp.StatusCode)
	}

	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["success"] != false {
		t.Errorf("success = %v; want false", body["success"])
	}
}

// TestCreateOrder_ServerCalculatesPrices verifies that server ignores client-provided price and name
func TestCreateOrder_ServerCalculatesPrices(t *testing.T) {
	srv, captured := newMockServer(t)
	h, _, _ := newTestHandlerWithServer(t, srv.URL, "")

	// Клиент шлет поддельную цену 10 рублей и поддельное имя "Дешевый суп"
	clientPayload := `{
		"customerName": "Хамзат",
		"phone": "+7 920 482-77-77",
		"deliveryType": "pickup",
		"items": [
			{
				"productId": "beini-grill-01",
				"name": "Фейковое имя",
				"price": 10.0,
				"amount": 2
			}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/order", strings.NewReader(clientPayload))
	w := httptest.NewRecorder()

	h.CreateOrder(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("StatusCode = %d; body: %s", resp.StatusCode, string(body))
	}

	var respBody map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&respBody)
	if respBody["success"] != true {
		t.Errorf("success = %v; want true", respBody["success"])
	}

	// Люля-кебаб стоит 590 руб, 2 порции = 1180 руб
	totalSum, ok := respBody["totalSum"].(float64)
	if !ok || totalSum != 1180 {
		t.Errorf("totalSum = %v; want 1180", totalSum)
	}

	// Проверяем, что в iiko ушел заказ с правильной серверной ценой и номенклатурным IikoID
	if len(captured.Order.Items) != 1 {
		t.Fatalf("captured items count = %d; want 1", len(captured.Order.Items))
	}
	sentItem := captured.Order.Items[0]
	if sentItem.Price != 590 {
		t.Errorf("sentItem.Price = %v; want 590", sentItem.Price)
	}
	if sentItem.ProductID != "0ce65922-6a9f-453a-bb92-b7c35b4950bb" {
		t.Errorf("sentItem.ProductID = %s; want iiko guid", sentItem.ProductID)
	}
	if captured.Order.Payments[0].Sum != 1180 {
		t.Errorf("captured payment sum = %v; want 1180", captured.Order.Payments[0].Sum)
	}
}

// TestCreateOrder_UnknownProductRejected verifies rejection of invalid product IDs
func TestCreateOrder_UnknownProductRejected(t *testing.T) {
	srv, _ := newMockServer(t)
	h, _, _ := newTestHandlerWithServer(t, srv.URL, "")

	payload := `{
		"customerName": "Гость",
		"phone": "+7 920 482-77-77",
		"deliveryType": "pickup",
		"items": [
			{
				"productId": "non-existent-product-id",
				"amount": 1
			}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/order", strings.NewReader(payload))
	w := httptest.NewRecorder()

	h.CreateOrder(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d; want 400", resp.StatusCode)
	}

	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	errMsg, _ := body["error"].(string)
	if !strings.Contains(errMsg, "не найдено в меню") {
		t.Errorf("неожиданное сообщение об ошибке: %s", errMsg)
	}
}

// TestCreateOrder_BodySizeLimit verifies that payload larger than 32KB is rejected
func TestCreateOrder_BodySizeLimit(t *testing.T) {
	srv, _ := newMockServer(t)
	h, _, _ := newTestHandlerWithServer(t, srv.URL, "")

	// Генерируем тело > 32 КБ
	largePadding := strings.Repeat("A", 35*1024)
	payload := map[string]any{
		"customerName": "Гость",
		"phone":        "+7 920 482-77-77",
		"deliveryType": "pickup",
		"comment":      largePadding,
		"items": []map[string]any{
			{"productId": "beini-grill-01", "amount": 1},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/order", bytes.NewReader(payloadBytes))
	w := httptest.NewRecorder()

	h.CreateOrder(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("StatusCode = %d; want 413 (StatusRequestEntityTooLarge)", resp.StatusCode)
	}
}

// TestCreateOrder_IdempotencyKey verifies protection against duplicate order submission
func TestCreateOrder_IdempotencyKey(t *testing.T) {
	var createCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(iiko.AccessTokenResponse{Token: "mock-token"})
	})
	mux.HandleFunc("/api/1/deliveries/create", func(w http.ResponseWriter, r *http.Request) {
		createCalls.Add(1)
		resp := iiko.CreateDeliveryResponse{
			CorrelationID: "corr-first-order",
			OrderInfo: iiko.OrderInfo{
				ID:             "order-1",
				CreationStatus: iiko.StatusSuccess,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	h, _, _ := newTestHandlerWithServer(t, srv.URL, "")

	payload := `{
		"customerName": "Адам",
		"phone": "+7 920 482-77-77",
		"deliveryType": "pickup",
		"items": [
			{"productId": "beini-grill-01", "amount": 1}
		]
	}`

	idempKey := "uniq-req-id-12345"

	// 1. Первый запрос с Idempotency-Key
	req1 := httptest.NewRequest(http.MethodPost, "/api/order", strings.NewReader(payload))
	req1.Header.Set("Idempotency-Key", idempKey)
	w1 := httptest.NewRecorder()
	h.CreateOrder(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("w1.Code = %d; want 200", w1.Code)
	}
	if calls := createCalls.Load(); calls != 1 {
		t.Fatalf("iiko calls = %d; want 1", calls)
	}

	// 2. Повторный запрос с тем же Idempotency-Key
	req2 := httptest.NewRequest(http.MethodPost, "/api/order", strings.NewReader(payload))
	req2.Header.Set("Idempotency-Key", idempKey)
	w2 := httptest.NewRecorder()
	h.CreateOrder(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("w2.Code = %d; want 200", w2.Code)
	}

	// Проверяем, что ответ взят из кэша и iiko НЕ вызывался повторно!
	if calls := createCalls.Load(); calls != 1 {
		t.Fatalf("повторный вызов iiko не должен был произойти! calls = %d; want 1", calls)
	}
	if w2.Header().Get("X-Cache-Lookup") != "HIT" {
		t.Errorf("ожидался заголовок X-Cache-Lookup: HIT")
	}

	var body2 map[string]any
	_ = json.NewDecoder(w2.Body).Decode(&body2)
	if body2["correlationId"] != "corr-first-order" {
		t.Errorf("correlationId = %v; want corr-first-order", body2["correlationId"])
	}
}

// TestCreateOrder_AmountLimits verifies boundary checks for items amount
func TestCreateOrder_AmountLimits(t *testing.T) {
	srv, _ := newMockServer(t)
	h, _, _ := newTestHandlerWithServer(t, srv.URL, "")

	tests := []struct {
		name   string
		amount int
		wantOk bool
	}{
		{"zero amount", 0, false},
		{"negative amount", -5, false},
		{"max allowed amount", 50, true},
		{"exceeded amount", 51, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{
				"phone":        "+7 920 482-77-77",
				"deliveryType": "pickup",
				"items": []map[string]any{
					{"productId": "beini-grill-01", "amount": tc.amount},
				},
			}
			data, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/api/order", bytes.NewReader(data))
			w := httptest.NewRecorder()

			h.CreateOrder(w, req)

			if tc.wantOk && w.Code != http.StatusOK {
				t.Errorf("Code = %d; want 200", w.Code)
			}
			if !tc.wantOk && w.Code != http.StatusBadRequest {
				t.Errorf("Code = %d; want 400", w.Code)
			}
		})
	}
}

func TestCreateOrder_UnsupportedMediaType(t *testing.T) {
	h := newTestHandler("")

	req := httptest.NewRequest(http.MethodPost, "/api/order", strings.NewReader("some-text"))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()

	h.CreateOrder(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("StatusCode = %d; want 415 (StatusUnsupportedMediaType)", resp.StatusCode)
	}
}

func TestCreateOrder_IdempotencyPayloadMismatch(t *testing.T) {
	srv, _ := newMockServer(t)
	h, _, _ := newTestHandlerWithServer(t, srv.URL, "")

	payload1 := `{
		"customerName": "Адам",
		"phone": "+7 920 482-77-77",
		"deliveryType": "pickup",
		"items": [{"productId": "beini-grill-01", "amount": 1}]
	}`
	payload2 := `{
		"customerName": "Магомед",
		"phone": "+7 920 482-77-77",
		"deliveryType": "pickup",
		"items": [{"productId": "beini-grill-02", "amount": 2}]
	}`

	key := "shared-idempotency-key"

	// 1. Первый заказ
	req1 := httptest.NewRequest(http.MethodPost, "/api/order", strings.NewReader(payload1))
	req1.Header.Set("Idempotency-Key", key)
	w1 := httptest.NewRecorder()
	h.CreateOrder(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("первый заказ не прошел: %d", w1.Code)
	}

	// 2. Второй заказ с ДРУГИМ телом и ТЕМ ЖЕ ключом -> 422 Unprocessable Entity
	req2 := httptest.NewRequest(http.MethodPost, "/api/order", strings.NewReader(payload2))
	req2.Header.Set("Idempotency-Key", key)
	w2 := httptest.NewRecorder()
	h.CreateOrder(w2, req2)

	if w2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("StatusCode = %d; want 422 (StatusUnprocessableEntity)", w2.Code)
	}
}

func TestSendTestOrder_MethodNotAllowed(t *testing.T) {
	h := newTestHandler("")

	req := httptest.NewRequest(http.MethodGet, "/api/test-order", nil)
	w := httptest.NewRecorder()

	h.SendTestOrder(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("StatusCode = %d; want 405", resp.StatusCode)
	}
}

func TestSendTestOrder_ForbiddenWhenRemoteAndNoToken(t *testing.T) {
	h := newTestHandler("valid-secret-token")

	// Запрос с внешнего IP без токена
	req := httptest.NewRequest(http.MethodPost, "/api/test-order", nil)
	req.RemoteAddr = "192.168.1.50:12345"
	w := httptest.NewRecorder()

	h.SendTestOrder(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("StatusCode = %d; want 403", resp.StatusCode)
	}
}

func TestSendTestOrder_ProxyForbiddenWhenNoToken(t *testing.T) {
	h := newTestHandler("") // без токена

	// Запрос с loopback, но с заголовком X-Forwarded-For (запрос пришел через внешний прокси)
	req := httptest.NewRequest(http.MethodPost, "/api/test-order", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.195")
	w := httptest.NewRecorder()

	h.SendTestOrder(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("StatusCode = %d; want 403 (запрещено за прокси без токена)", w.Code)
	}
}

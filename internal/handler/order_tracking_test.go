package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"beini-backend/internal/config"
	"beini-backend/internal/iiko"
	"beini-backend/internal/orderstate"
)

const orderPayload = `{"phone":"89204827777","deliveryType":"pickup","items":[{"productId":"beini-grill-01","amount":1}]}`

type trackingMock struct {
	creates       atomic.Int32
	polls         atomic.Int32
	status        atomic.Value
	lookupFailure atomic.Bool
	dropCreate    bool
	entered       chan struct{}
	release       chan struct{}
}

func (m *trackingMock) server(t *testing.T) *httptest.Server {
	t.Helper()
	m.status.Store(iiko.StatusInProgress)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/access_token":
			json.NewEncoder(w).Encode(iiko.AccessTokenResponse{Token: "mock"})
		case "/api/1/deliveries/create":
			m.creates.Add(1)
			var req iiko.CreateDeliveryRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				return
			}
			if req.Order.ID == "" {
				t.Error("нет заранее выделенного ID")
			}
			if m.entered != nil {
				close(m.entered)
				<-m.release
			}
			if m.dropCreate {
				// iiko фактически создал заказ, но оборвал ответ.
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				conn.Close()
				return
			}
			json.NewEncoder(w).Encode(iiko.CreateDeliveryResponse{CorrelationID: "corr", OrderInfo: iiko.OrderInfo{ID: req.Order.ID, CreationStatus: m.status.Load().(string)}})
		case "/api/1/deliveries/by_id":
			m.polls.Add(1)
			if m.lookupFailure.Load() {
				http.Error(w, "offline", 503)
				return
			}
			var req iiko.OrdersByIDRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.OrderIDs) != 1 {
				t.Errorf("lookup: %+v %v", req, err)
				return
			}
			status := m.status.Load().(string)
			var orders []iiko.OrderInfo
			if status != "not-found" {
				orders = append(orders, iiko.OrderInfo{ID: req.OrderIDs[0], CreationStatus: status})
			}
			json.NewEncoder(w).Encode(iiko.OrdersByIDResponse{Orders: orders})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func openTrackingState(t *testing.T, path string) (*orderstate.Store, error) {
	t.Helper()
	s, err := orderstate.Open(path)
	if err == nil {
		t.Cleanup(func() { s.Close() })
	}
	return s, err
}

func trackingHandler(t *testing.T, url string, states *orderstate.Store) *OrderHandler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := iiko.NewClient(&config.Config{APIBaseURL: url, OrganizationID: "org", TerminalGroupID: "term"}, logger)
	h := NewOrderHandlerWithState(client, logger, "test-token", nil, states)
	t.Cleanup(h.Close)
	return h
}

func postOrder(h *OrderHandler, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/order", strings.NewReader(body))
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	h.CreateOrder(w, r)
	return w
}

func statusBody(t *testing.T, h *OrderHandler, key string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/order/status", nil)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	h.OrderStatus(w, r)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("статус допускает HTTP cache")
	}
	return w.Code, body
}

func awaitOrderState(t *testing.T, h *OrderHandler, key, want string, reconcile bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(7 * time.Second)
	for {
		if reconcile {
			h.ReconcilePending(context.Background())
		}
		_, body := statusBody(t, h, key)
		if body["status"] == want {
			return body
		}
		if time.Now().After(deadline) {
			t.Fatalf("want=%s body=%v", want, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPendingTrackingAndTerminalReplay(t *testing.T) {
	for _, terminal := range []string{iiko.StatusSuccess, iiko.StatusError, iiko.StatusCancelled} {
		t.Run(terminal, func(t *testing.T) {
			m := &trackingMock{}
			srv := m.server(t)
			h := trackingHandler(t, srv.URL, orderstate.NewMemory())
			w := postOrder(h, "key", orderPayload)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			_, pending := statusBody(t, h, "key")
			if pending["status"] != "pending" || pending["pending"] != true || pending["outcomeUnknown"] != false {
				t.Fatal(pending)
			}
			m.lookupFailure.Store(true)
			h.ReconcilePending(context.Background())
			_, pending = statusBody(t, h, "key")
			if pending["status"] != "pending" {
				t.Fatal(pending)
			}
			m.lookupFailure.Store(false)
			m.status.Store(terminal)
			want := "rejected"
			code := 422
			if terminal == iiko.StatusSuccess {
				want = "confirmed"
				code = 200
			}
			body := awaitOrderState(t, h, "key", want, true)
			if body["pending"] != false || body["success"] != (terminal == iiko.StatusSuccess) {
				t.Fatal(body)
			}
			replay := postOrder(h, "key", orderPayload)
			if replay.Code != code || m.creates.Load() != 1 || replay.Header().Get("X-Cache-Lookup") != "HIT" {
				t.Fatalf("code=%d calls=%d body=%s", replay.Code, m.creates.Load(), replay.Body)
			}
			m.status.Store(iiko.StatusInProgress)
			h.ReconcilePending(context.Background())
			_, body = statusBody(t, h, "key")
			if body["status"] != want {
				t.Fatal("терминальное состояние откатилось", body)
			}
		})
	}
}

func TestLostCreateResponseAndRestart(t *testing.T) {
	m := &trackingMock{dropCreate: true}
	srv := m.server(t)
	path := filepath.Join(t.TempDir(), "orders.jsonl")
	s, err := openTrackingState(t, path)
	if err != nil {
		t.Fatal(err)
	}
	h := trackingHandler(t, srv.URL, s)
	w := postOrder(h, "durable-key", orderPayload)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	_, body := statusBody(t, h, "durable-key")
	if body["status"] != "pending" || body["outcomeUnknown"] != true {
		t.Fatal(body)
	}
	id := body["orderId"]
	m.status.Store("not-found")
	h.ReconcilePending(context.Background())
	postOrder(h, "durable-key", orderPayload)
	if m.creates.Load() != 1 {
		t.Fatal("повтор create после сетевой ошибки")
	}
	h.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openTrackingState(t, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	m.status.Store(iiko.StatusSuccess)
	h = trackingHandler(t, srv.URL, s)
	// Только автоматический worker после восстановления, без ручного reconcile.
	body = awaitOrderState(t, h, "durable-key", "confirmed", false)
	if body["orderId"] != id || body["outcomeUnknown"] != false {
		t.Fatal(body)
	}
	if w := postOrder(h, "durable-key", orderPayload); w.Code != 200 || m.creates.Load() != 1 {
		t.Fatalf("code=%d calls=%d", w.Code, m.creates.Load())
	}
	if w := postOrder(h, "durable-key", orderPayload+" "); w.Code != 422 {
		t.Fatal(w.Code)
	}
}

func TestConcurrentReplayAndClientDisconnect(t *testing.T) {
	m := &trackingMock{entered: make(chan struct{}), release: make(chan struct{})}
	srv := m.server(t)
	h := trackingHandler(t, srv.URL, orderstate.NewMemory())
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodPost, "/api/order", strings.NewReader(orderPayload)).WithContext(ctx)
	r.Header.Set("Idempotency-Key", "key")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); h.CreateOrder(w, r); done <- w }()
	select {
	case <-m.entered:
	case <-time.After(3 * time.Second):
		close(m.release)
		t.Fatal("create не начался")
	}
	cancel()
	// Повтор во время create читает сохранённый pending и не отправляет ещё один.
	w := postOrder(h, "key", orderPayload)
	if w.Code != 200 || m.creates.Load() != 1 {
		close(m.release)
		t.Fatalf("%d calls=%d", w.Code, m.creates.Load())
	}
	m.status.Store(iiko.StatusSuccess)
	close(m.release)
	select {
	case w := <-done:
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("create не завершился")
	}
	body := awaitOrderState(t, h, "key", "confirmed", true)
	if body["outcomeUnknown"] != false || m.creates.Load() != 1 {
		t.Fatal(body)
	}
}

type lostWriter struct{ header http.Header }

func (w *lostWriter) Header() http.Header { return w.header }
func (w *lostWriter) WriteHeader(int)     {}
func (w *lostWriter) Write([]byte) (int, error) {
	return 0, errors.New("клиент потерял ответ")
}

func TestLostHTTPResponseReplaysAfterRestart(t *testing.T) {
	m := &trackingMock{}
	srv := m.server(t)
	m.status.Store(iiko.StatusSuccess)
	path := filepath.Join(t.TempDir(), "orders.jsonl")
	s, err := openTrackingState(t, path)
	if err != nil {
		t.Fatal(err)
	}
	h := trackingHandler(t, srv.URL, s)
	r := httptest.NewRequest(http.MethodPost, "/api/order", strings.NewReader(orderPayload))
	r.Header.Set("Idempotency-Key", "lost-http")
	h.CreateOrder(&lostWriter{header: make(http.Header)}, r)
	h.Close()
	s.Close()
	s, err = openTrackingState(t, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h = trackingHandler(t, srv.URL, s)
	w := postOrder(h, "lost-http", orderPayload)
	if w.Code != 200 || m.creates.Load() != 1 {
		t.Fatalf("code=%d calls=%d", w.Code, m.creates.Load())
	}
	_, body := statusBody(t, h, "lost-http")
	if body["status"] != "confirmed" {
		t.Fatal(body)
	}
}

func TestRestartBeforeSendNeverCreatesAutomatically(t *testing.T) {
	m := &trackingMock{}
	srv := m.server(t)
	m.status.Store("not-found")
	path := filepath.Join(t.TempDir(), "orders.jsonl")
	s, err := openTrackingState(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Reserve("unsent", orderstate.Hash([]byte(orderPayload)), 590); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = openTrackingState(t, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h := trackingHandler(t, srv.URL, s)
	h.ReconcilePending(context.Background())
	w := postOrder(h, "unsent", orderPayload)
	if w.Code != 200 || m.creates.Load() != 0 {
		t.Fatalf("code=%d calls=%d", w.Code, m.creates.Load())
	}
	_, body := statusBody(t, h, "unsent")
	if body["status"] != "pending" || body["outcomeUnknown"] != true {
		t.Fatal(body)
	}
}

func TestUnavailableStatePreventsCreateAndStatusAccess(t *testing.T) {
	m := &trackingMock{}
	srv := m.server(t)
	s := orderstate.NewMemory()
	h := trackingHandler(t, srv.URL, s)
	if code, _ := statusBody(t, h, ""); code != 400 {
		t.Fatal(code)
	}
	if code, _ := statusBody(t, h, "unknown"); code != 404 {
		t.Fatal(code)
	}
	s.Close()
	w := postOrder(h, "key", orderPayload)
	if w.Code != 503 || m.creates.Load() != 0 {
		t.Fatalf("code=%d calls=%d", w.Code, m.creates.Load())
	}
	if code, _ := statusBody(t, h, "key"); code != 503 {
		t.Fatal(code)
	}
}

func TestBackgroundPollsAfterInitialPending(t *testing.T) {
	m := &trackingMock{}
	srv := m.server(t)
	h := trackingHandler(t, srv.URL, orderstate.NewMemory())
	if w := postOrder(h, "background", orderPayload); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	m.status.Store(iiko.StatusSuccess)
	awaitOrderState(t, h, "background", "confirmed", false)
	if m.polls.Load() == 0 || m.creates.Load() != 1 {
		t.Fatalf("polls=%d creates=%d", m.polls.Load(), m.creates.Load())
	}
}

func TestResultPersistenceFailureRecoversByLookup(t *testing.T) {
	m := &trackingMock{entered: make(chan struct{}), release: make(chan struct{})}
	srv := m.server(t)
	path := filepath.Join(t.TempDir(), "orders.jsonl")
	s, err := openTrackingState(t, path)
	if err != nil {
		t.Fatal(err)
	}
	h := trackingHandler(t, srv.URL, s)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- postOrder(h, "disk-failure", orderPayload) }()
	select {
	case <-m.entered:
	case <-time.After(3 * time.Second):
		close(m.release)
		t.Fatal("create не начался")
	}
	// Попытка уже на диске, но запись результата стала недоступна.
	s.Close()
	m.status.Store(iiko.StatusSuccess)
	close(m.release)
	select {
	case w := <-done:
		if w.Code != 503 {
			t.Fatalf("неподтверждённая запись: %d %s", w.Code, w.Body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("create не завершился")
	}
	h.Close()
	s, err = openTrackingState(t, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h = trackingHandler(t, srv.URL, s)
	awaitOrderState(t, h, "disk-failure", "confirmed", false)
	if w := postOrder(h, "disk-failure", orderPayload); w.Code != 200 || m.creates.Load() != 1 {
		t.Fatalf("%d creates=%d", w.Code, m.creates.Load())
	}
}

func TestImmediateRejectionAndUnknownCreationStatus(t *testing.T) {
	for _, status := range []string{iiko.StatusError, iiko.StatusCancelled, "FutureStatus", ""} {
		t.Run(status, func(t *testing.T) {
			m := &trackingMock{}
			srv := m.server(t)
			m.status.Store(status)
			h := trackingHandler(t, srv.URL, orderstate.NewMemory())
			w := postOrder(h, "key", orderPayload)
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			rejected := status == iiko.StatusError || status == iiko.StatusCancelled
			wantCode := 200
			if rejected {
				wantCode = 422
			}
			if w.Code != wantCode || body["pending"] == rejected || body["outcomeUnknown"] == rejected {
				t.Fatalf("code=%d body=%v", w.Code, body)
			}
			if w := postOrder(h, "key", orderPayload); w.Code != wantCode || m.creates.Load() != 1 {
				t.Fatalf("code=%d calls=%d", w.Code, m.creates.Load())
			}
		})
	}
}

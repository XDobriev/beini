package main

import (
	"beini-backend/internal/notifier"
	"beini-backend/internal/orderstate"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"beini-backend/internal/config"
	"beini-backend/internal/iiko"
)

func TestStateDirectoryOutsideStatic(t *testing.T) {
	web := t.TempDir()
	for _, path := range []string{"", "relative", web, filepath.Join(web, "data")} {
		if _, err := stateDirectory(web, path); err == nil {
			t.Fatalf("accepted public/relative path %s", path)
		}
	}
	if _, err := stateDirectory(web, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestStateLocationDifferentWindowsVolumes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows volume semantics")
	}
	for _, paths := range [][2]string{
		{`D:\a\beini\beini`, `C:\Users\runneradmin\AppData\Local\Temp\state`},
		{`\\server\web\site`, `\\server\state\orders`},
	} {
		if err := checkStateLocation(paths[0], paths[1]); err != nil {
			t.Fatalf("separate volume rejected: %v", err)
		}
	}
	for _, target := range []string{`d:\site\private`, `\\?\D:\site\private`, `\\.\D:\site\private`, `\\.\GLOBALROOT\Device\HarddiskVolume1\site\private`} {
		if err := checkStateLocation(`D:\site`, target); err == nil {
			t.Fatal("same-volume public directory accepted")
		}
	}
	if err := checkStateLocation(`\\server\web\site`, `\\?\UNC\server\web\site\state`); err == nil {
		t.Fatal("extended UNC path evaded containment")
	}
}

// These tests exercise the actual application assembly, durable journal, menu
// worker, price consent, routes and reconciliation against a loopback iiko API.
func TestApplicationMenuOrderRestart(t *testing.T) {
	for _, scenario := range []struct {
		name, final string
		drop        bool
	}{
		{"confirmed", "Success", false}, {"rejected", "Error", false}, {"lost-response", "Success", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var creates atomic.Int32
			var final atomic.Value
			final.Store("InProgress")
			mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/access_token":
					io.WriteString(w, `{"token":"offline-only"}`)
				case "/api/menu/v3/by_id":
					io.WriteString(w, `{"id":"menu","name":"Mock menu","products":[{"id":"11111111-1111-4111-8111-111111111111","orderItemType":"Product"}],"itemsGroups":[{"id":"group","items":[{"productId":"11111111-1111-4111-8111-111111111111","name":"Mock","sizePrices":[{"price":620}]}]}]}`)
				case "/api/1/stop_lists":
					io.WriteString(w, `{"terminalGroupStopLists":[{"organizationId":"org","items":[{"terminalGroupId":"term","items":[]}]}]}`)
				case "/api/1/deliveries/create":
					creates.Add(1)
					var req iiko.CreateDeliveryRequest
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Error(err)
						return
					}
					if req.Order.Items[0].Price != 620 {
						t.Error("untrusted price reached iiko")
					}
					if scenario.drop {
						conn, _, _ := w.(http.Hijacker).Hijack()
						conn.Close()
						return
					}
					json.NewEncoder(w).Encode(iiko.CreateDeliveryResponse{OrderInfo: iiko.OrderInfo{ID: req.Order.ID, CreationStatus: "InProgress"}})
				case "/api/1/deliveries/by_id":
					var req iiko.OrdersByIDRequest
					json.NewDecoder(r.Body).Decode(&req)
					json.NewEncoder(w).Encode(iiko.OrdersByIDResponse{Orders: []iiko.OrderInfo{{ID: req.OrderIDs[0], CreationStatus: final.Load().(string)}}})
				default:
					t.Errorf("unexpected external path %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer mock.Close()
			web, state := t.TempDir(), t.TempDir()
			os.Mkdir(filepath.Join(web, "data"), 0700)
			os.WriteFile(filepath.Join(web, "data", "menu_catalog.json"), []byte(`{"categories":[{"id":"grill","name":"Мангал"}],"items":[{"id":"one","iikoId":"11111111-1111-4111-8111-111111111111","name":"Блюдо","category":"grill","price":590,"available":true}]}`), 0600)
			cfg := &config.Config{APIBaseURL: mock.URL, OrganizationID: "org", TerminalGroupID: "term", ExternalMenuID: "menu"}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			tr := &appNotificationTransport{}
			tr.fail.Store(true)
			cfg.TelegramChatID = "-123"
			app, closeApp, err := newApplicationWithTransport(cfg, logger, web, state, tr)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { closeApp() }()
			call := func(method, path, body string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				r := httptest.NewRequest(method, path, strings.NewReader(body))
				r.Header.Set("Idempotency-Key", "integration-key")
				r.Header.Set("Content-Type", "application/json")
				app.ServeHTTP(w, r)
				return w
			}
			deadline := time.Now().Add(3 * time.Second)
			for call("GET", "/api/menu", "").Code != 200 {
				if time.Now().After(deadline) {
					t.Fatal("menu worker never published")
				}
				time.Sleep(10 * time.Millisecond)
			}
			body := `{"phone":"89990000000","deliveryType":"pickup","items":[{"productId":"one","amount":2}],"expectedPrices":[590]}`
			w := call("POST", "/api/order", body)
			if w.Code != 409 || creates.Load() != 0 {
				t.Fatalf("price consent: %d %s creates=%d", w.Code, w.Body, creates.Load())
			}
			if w = call("GET", "/api/order/status", ""); w.Code != 404 {
				t.Fatal("price conflict reserved an attempt")
			}
			body = strings.Replace(body, `[590]`, `[620]`, 1)
			w = call("POST", "/api/order", body)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"pending"`) {
				t.Fatalf("pending: %d %s", w.Code, w.Body)
			}
			// A lost guest response is handled by replaying exactly the saved body.
			if w = call("POST", "/api/order", body); w.Code != 200 || creates.Load() != 1 {
				t.Fatalf("duplicate before restart: %d", creates.Load())
			}
			deadline = time.Now().Add(3 * time.Second)
			for tr.attempts.Load() == 0 {
				if time.Now().After(deadline) {
					t.Fatal("notification failure was not exercised")
				}
				time.Sleep(10 * time.Millisecond)
			}
			closeApp()
			final.Store(scenario.final)
			tr.fail.Store(false)
			app, closeApp, err = newApplicationWithTransport(cfg, logger, web, state, tr)
			if err != nil {
				t.Fatal(err)
			}
			want, code := `"status":"confirmed"`, 200
			if scenario.final == "Error" {
				want, code = `"status":"rejected"`, 422
			}
			deadline = time.Now().Add(8 * time.Second)
			for {
				w = call("GET", "/api/order/status", "")
				if strings.Contains(w.Body.String(), want) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("restart reconciliation: %s", w.Body)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if w.Code != code {
				t.Fatalf("terminal HTTP code %d", w.Code)
			}
			w = call("POST", "/api/order", body)
			if w.Code != code || creates.Load() != 1 || !strings.Contains(w.Body.String(), `"totalSum":1240`) {
				t.Fatalf("replay after restart: %d %s creates=%d", w.Code, w.Body, creates.Load())
			}
			if w = call("POST", "/api/order", strings.Replace(body, `[620]`, `[590]`, 1)); w.Code != 422 {
				t.Fatal("changed body reused key")
			}
			deadline = time.Now().Add(5 * time.Second)
			for tr.count() < 2 {
				if time.Now().After(deadline) {
					t.Fatal("notifications did not recover after restart")
				}
				time.Sleep(10 * time.Millisecond)
			}
			tr.mu.Lock()
			defer tr.mu.Unlock()
			if len(tr.messages) != 2 || !strings.Contains(tr.messages[0], ":pending") {
				t.Fatal("unexpected event sequence")
			}
			finalStatus := ":confirmed"
			if scenario.final == "Error" {
				finalStatus = ":rejected"
			}
			if !strings.Contains(tr.messages[1], finalStatus) {
				t.Fatal("wrong final notification")
			}

		})
	}
}

type blockingNotificationTransport struct{ started chan struct{} }

func (b *blockingNotificationTransport) Send(ctx context.Context, _ string) error {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

var _ notifier.Transport = (*blockingNotificationTransport)(nil)

func TestNotificationFailureDoesNotBlockApplication(t *testing.T) {
	for _, mode := range []string{"blocked", "broken", "changed-group"} {
		t.Run(mode, func(t *testing.T) {
			state := t.TempDir()
			orders, err := orderstate.Open(filepath.Join(state, "orders.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			rec, _, err := orders.ReserveWithNotification("existing-key", "body", 100, `{"phone":"+79990000000","items":[{"name":"Блюдо","amount":1,"price":100}]}`)
			if err != nil {
				t.Fatal(err)
			}
			orders.Release(rec)
			orders.Close()
			if mode == "broken" {
				os.WriteFile(filepath.Join(state, "notifications.jsonl"), []byte("broken\n"), 0600)
			}
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
			defer api.Close()
			cfg := &config.Config{APIBaseURL: api.URL, OrganizationID: "org", TerminalGroupID: "term", ExternalMenuID: "menu", TelegramChatID: "-123"}
			if mode == "changed-group" {
				receipts, err := orderstate.Open(filepath.Join(state, "notifications.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				_, _, err = receipts.Reserve("telegram-destination", orderstate.Hash([]byte("-456")), 0)
				receipts.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			tr := &blockingNotificationTransport{started: make(chan struct{}, 1)}
			app, closeApp, err := newApplicationWithTransport(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "../..", state, tr)
			if err != nil {
				t.Fatal(err)
			}
			defer closeApp()
			if mode == "changed-group" {
				released, err := orderstate.Open(filepath.Join(state, "notifications.jsonl"))
				if err != nil {
					t.Fatal("mismatched destination still has an active delivery journal")
				}
				released.Close()
				select {
				case <-tr.started:
					t.Fatal("message sent to wrong destination")
				default:
				}
			}
			if mode == "blocked" {
				select {
				case <-tr.started:
				case <-time.After(3 * time.Second):
					t.Fatal("worker did not start")
				}
			}
			req := httptest.NewRequest("GET", "/api/order/status", nil)
			req.Header.Set("Idempotency-Key", "existing-key")
			w := httptest.NewRecorder()
			app.ServeHTTP(w, req)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"pending"`) {
				t.Fatalf("notification failure affected order: %d %s", w.Code, w.Body)
			}
			done := make(chan struct{})
			go func() { closeApp(); close(done) }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("notification shutdown blocked")
			}
		})
	}
}

type appNotificationTransport struct {
	fail     atomic.Bool
	attempts atomic.Int32
	mu       sync.Mutex
	messages []string
}

func (a *appNotificationTransport) Send(_ context.Context, text string) error {
	a.attempts.Add(1)
	if a.fail.Load() {
		return errors.New("offline")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = append(a.messages, text)
	return nil
}
func (a *appNotificationTransport) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.messages)
}

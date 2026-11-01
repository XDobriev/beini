package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
				case "/api/1/nomenclature":
					io.WriteString(w, `{"revision":1,"products":[{"id":"11111111-1111-4111-8111-111111111111","name":"Mock","type":"dish","isDeleted":false,"useBalanceForSell":false,"canSetOpenPrice":false,"sizePrices":[{"sizeId":null,"price":{"currentPrice":620,"isIncludedInMenu":true}}]}]}`)
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
			cfg := &config.Config{APIBaseURL: mock.URL, OrganizationID: "org", TerminalGroupID: "term"}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			app, closeApp, err := newApplication(cfg, logger, web, state)
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
			closeApp()
			final.Store(scenario.final)
			app, closeApp, err = newApplication(cfg, logger, web, state)
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
		})
	}
}

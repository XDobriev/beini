package main

import (
	"bytes"
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

	"beini-backend/internal/access"
	"beini-backend/internal/catalog"
	"beini-backend/internal/config"
	"beini-backend/internal/handler"
	"beini-backend/internal/iiko"
)

type accessClient struct {
	app     http.Handler
	cookies map[string]*http.Cookie
	csrf    string
}

func TestOwnerCannotBypassFreshnessOrStop(t *testing.T) {
	var upstream atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { upstream.Add(1); w.WriteHeader(500) }))
	defer api.Close()
	for _, scenario := range []string{"stale-menu", "stale-stop", "stopped"} {
		t.Run(scenario, func(t *testing.T) {
			cat, err := catalog.NewStore(2*time.Hour, 5*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			menuAt, stopAt := now, now
			if scenario == "stale-menu" {
				menuAt = now.Add(-3 * time.Hour)
			}
			if scenario == "stale-stop" {
				stopAt = now.Add(-10 * time.Minute)
			}
			err = cat.Publish(catalog.Snapshot{SchemaVersion: 1, Version: 1, OrganizationID: "org", TerminalGroupID: "term", SeedDigest: "offline", UpdatedAt: now, MenuUpdatedAt: menuAt, StopListUpdatedAt: stopAt, Categories: []catalog.Category{{ID: "grill", Name: "Мангал"}}, Items: []catalog.Item{{ID: "one", IikoID: "11111111-1111-4111-8111-111111111111", Name: "Блюдо", Category: "grill", Price: 620, Sellable: true, Available: scenario != "stopped"}}})
			if err != nil {
				t.Fatal(err)
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			orders := handler.NewOrderHandlerWithState(iiko.NewClient(&config.Config{APIBaseURL: api.URL}, logger), logger, "", cat, nil)
			defer orders.Close()
			hash, _ := access.HashPassword("offline-password-only")
			gate, err := access.New(access.Config{Mode: "tester", PasswordHash: hash})
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/api/access", gate.Access)
			mux.HandleFunc("/api/tester/login", gate.Login)
			mux.Handle("/api/order", gate.ProtectOrder(http.HandlerFunc(orders.CreateOrder)))
			b := ownerClient(t, mux)
			w := b.call("POST", "/api/order", `{"phone":"89990000000","deliveryType":"pickup","items":[{"productId":"one","amount":1}],"expectedPrices":[620]}`, scenario)
			if w.Code != 400 || upstream.Load() != 0 {
				t.Fatalf("owner bypassed %s: %d %s upstream=%d", scenario, w.Code, w.Body, upstream.Load())
			}
		})
	}
}

func (b *accessClient) call(method, path, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://beinirest.ru"+path, strings.NewReader(body))
	r.Header.Set("Origin", "https://beinirest.ru")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", b.csrf)
	r.Header.Set("Idempotency-Key", key)
	for _, c := range b.cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	b.app.ServeHTTP(w, r)
	if b.cookies == nil {
		b.cookies = map[string]*http.Cookie{}
	}
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	var data struct {
		CSRFToken string `json:"csrfToken"`
	}
	json.Unmarshal(w.Body.Bytes(), &data)
	if data.CSRFToken != "" {
		b.csrf = data.CSRFToken
	}
	return w
}
func ownerClient(t *testing.T, app http.Handler) *accessClient {
	t.Helper()
	b := &accessClient{app: app}
	if w := b.call("GET", "/api/access", "", ""); w.Code != 200 {
		t.Fatalf("access %d %s", w.Code, w.Body)
	}
	if w := b.call("POST", "/api/tester/login", `{"password":"offline-password-only"}`, ""); w.Code != 200 {
		t.Fatalf("login %d %s", w.Code, w.Body)
	}
	return b
}

func TestApplicationAccessModesAndPreservedAttempt(t *testing.T) {
	var creates atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			json.NewDecoder(r.Body).Decode(&req)
			if len(req.Order.Items) != 1 || req.Order.Items[0].Price != 620 {
				t.Error("business validation bypass")
			}
			json.NewEncoder(w).Encode(iiko.CreateDeliveryResponse{OrderInfo: iiko.OrderInfo{ID: req.Order.ID, CreationStatus: "InProgress"}})
		case "/api/1/deliveries/by_id":
			var req iiko.OrdersByIDRequest
			json.NewDecoder(r.Body).Decode(&req)
			json.NewEncoder(w).Encode(iiko.OrdersByIDResponse{Orders: []iiko.OrderInfo{{ID: req.OrderIDs[0], CreationStatus: "InProgress"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	web, state := t.TempDir(), t.TempDir()
	os.Mkdir(filepath.Join(web, "data"), 0700)
	os.WriteFile(filepath.Join(web, "data", "menu_catalog.json"), []byte(`{"categories":[{"id":"grill","name":"Мангал"}],"items":[{"id":"one","iikoId":"11111111-1111-4111-8111-111111111111","name":"Блюдо","sourceName":"Mock","category":"grill","price":620,"available":true}]}`), 0600)
	hash, _ := access.HashPassword("offline-password-only")
	cfg := &config.Config{APIBaseURL: api.URL, OrganizationID: "org", TerminalGroupID: "term", ExternalMenuID: "menu", TesterPasswordHash: hash, AllowedOrigins: []string{"https://beinirest.ru"}}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	body := `{"phone":"89990000000","deliveryType":"pickup","items":[{"productId":"one","amount":1}],"expectedPrices":[620]}`
	for _, mode := range []string{"", "tester", "closed", "public"} {
		cfg.OrderMode = mode
		app, closeApp, err := newApplication(cfg, logger, web, state)
		if err != nil {
			t.Fatal(err)
		}
		b := &accessClient{app: app}
		b.call("GET", "/api/access", "", "")
		deadline := time.Now().Add(3 * time.Second)
		for b.call("GET", "/api/menu", "", "").Code != 200 {
			if time.Now().After(deadline) {
				closeApp()
				t.Fatal("menu unavailable")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if mode != "public" {
			before := creates.Load()
			for _, key := range []string{"guest-key", "integration-key"} {
				if w := b.call("POST", "/api/order", body, key); w.Code != 403 {
					closeApp()
					t.Fatalf("guest mode=%s code=%d %s", mode, w.Code, w.Body)
				}
			}
			if creates.Load() != before {
				t.Fatal("guest reached iiko create")
			}
		}
		if mode == "tester" {
			b = ownerClient(t, app)
			if w := b.call("POST", "/api/order", strings.Replace(body, "[620]", "[590]", 1), "integration-key"); w.Code != 409 {
				t.Fatalf("owner price %d %s", w.Code, w.Body)
			}
			if w := b.call("POST", "/api/order", strings.Replace(body, "one", "hidden", 1), "integration-key"); w.Code != 400 {
				t.Fatal("owner bypassed item check")
			}
			if w := b.call("POST", "/api/order", body, "integration-key"); w.Code != 200 {
				t.Fatalf("owner create %d %s", w.Code, w.Body)
			}
			if w := b.call("POST", "/api/tester/logout", "{}", ""); w.Code != 200 {
				t.Fatal("logout")
			}
		}
		if mode == "closed" || mode == "public" {
			if w := b.call("GET", "/api/order/status", "", "integration-key"); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"pending"`) {
				t.Fatalf("mode lost attempt %s", w.Body)
			}
		}
		if mode == "public" {
			if w := b.call("POST", "/api/order", body, "integration-key"); w.Code != 200 {
				t.Fatalf("public replay %d %s", w.Code, w.Body)
			}
			if creates.Load() != 1 {
				t.Fatal("mode switch duplicated order")
			}
			if w := b.call("POST", "/api/order", body, "new-public-key"); w.Code != 200 || creates.Load() != 2 {
				t.Fatalf("public create %d %s", w.Code, w.Body)
			}
		}
		closeApp()
	}
	for _, secret := range []string{hash, "offline-password-only", "csrfToken", "__Host-beini_tester"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("secret in application logs")
		}
	}
}

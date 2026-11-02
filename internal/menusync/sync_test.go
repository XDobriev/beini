package menusync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"beini-backend/internal/catalog"
)

const first = "11111111-1111-4111-8111-111111111111"
const second = "22222222-2222-4222-8222-222222222222"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
}
func seed() catalog.Seed {
	return catalog.Seed{Categories: []catalog.Category{{ID: "grill", Name: "Мангал"}}, Items: []catalog.Item{
		{ID: "one", IikoID: first, Name: "Редакторское имя", Price: 590, Category: "grill", Available: true, Image: "assets/local.jpg", Description: "Наш текст", Weight: "410 г", Badge: "Хит", IsHalal: true},
		{ID: "two", IikoID: second, Name: "Скрытое", Price: 390, Category: "grill", Available: false},
	}}
}
func setup(t *testing.T, source Source) (*Service, *catalog.Store) {
	t.Helper()
	store, err := catalog.NewStore(time.Hour, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{OrganizationID: "org", TerminalGroupID: "terminal", CachePath: filepath.Join(t.TempDir(), "menu.json"), MenuInterval: time.Hour, StopListInterval: time.Minute, RequestTimeout: time.Second}, source, store, seed())
	if err != nil {
		t.Fatal(err)
	}
	return svc, store
}

type fakeSource struct {
	menu             Nomenclature
	stops            StopResponse
	menuErr, stopErr error
}

func (f *fakeSource) Menu(context.Context, string) (Nomenclature, error) { return f.menu, f.menuErr }
func (f *fakeSource) StopList(context.Context, string, string) (StopResponse, error) {
	return f.stops, f.stopErr
}
func fake(t *testing.T) *fakeSource {
	t.Helper()
	f := &fakeSource{}
	if err := json.Unmarshal(fixture(t, "nomenclature"), &f.menu); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fixture(t, "stop_lists"), &f.stops); err != nil {
		t.Fatal(err)
	}
	return f
}
func clearStops(f *fakeSource) { f.stops.TerminalGroupStopLists[0].Items[1].Items = []StopItem{} }
func mustRefresh(t *testing.T, s *Service) {
	t.Helper()
	if err := s.RefreshMenu(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshPreservesEditorialAndTerminalAvailability(t *testing.T) {
	f := fake(t)
	s, store := setup(t, f)
	mustRefresh(t, s)
	snap := store.Snapshot()
	if snap.Version != 1 || len(snap.Items) != 2 || snap.Items[0].Price != 620 || snap.Items[0].Available || snap.Items[1].Sellable {
		t.Fatalf("bad snapshot: %+v", snap)
	}
	it := snap.Items[0]
	if it.Name != seed().Items[0].Name || it.Image != seed().Items[0].Image || it.Description != "Наш текст" || it.Weight != "410 г" || it.Badge != "Хит" || !it.IsHalal {
		t.Fatal("editorial data changed")
	}
	if _, ok := store.FindProduct(first); ok {
		t.Fatal("stopped product accepted")
	}
	clearStops(f)
	if err := s.RefreshStopList(context.Background()); err != nil {
		t.Fatal(err)
	}
	if it, ok := store.FindProduct(" ONE "); !ok || it.Price != 620 {
		t.Fatal("cleared product missing")
	}
	if _, ok := store.FindProduct(second); ok {
		t.Fatal("editorial exclusion lost")
	}
	if _, ok := store.FindProduct("33333333-3333-4333-8333-333333333333"); ok {
		t.Fatal("unknown product published")
	}
	if !store.Snapshot().MenuUpdatedAt.Equal(snap.MenuUpdatedAt) {
		t.Fatal("stop refresh renewed menu age")
	}
}

func TestFailedRefreshPreservesMemoryAndDisk(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*fakeSource)
		menu   bool
	}{
		{"menu API error", func(f *fakeSource) { f.menuErr = errors.New("private credentials") }, true},
		{"menu missing", func(f *fakeSource) { f.menu = Nomenclature{} }, true},
		{"menu truncated", func(f *fakeSource) { f.menu.Products = nil }, true},
		{"revision regressed", func(f *fakeSource) { v := int64(6); f.menu.Revision = &v }, true},
		{"price missing", func(f *fakeSource) { f.menu.Products[0].SizePrices[0].Price.CurrentPrice = nil }, true},
		{"duplicate product", func(f *fakeSource) { f.menu.Products = append(f.menu.Products, f.menu.Products[0]) }, true},
		{"stop API error with menu", func(f *fakeSource) { f.stopErr = errors.New("secret") }, true},
		{"stop API error", func(f *fakeSource) { f.stopErr = errors.New("secret") }, false},
		{"stop missing", func(f *fakeSource) { f.stops = StopResponse{} }, false},
		{"wrong terminal", func(f *fakeSource) { f.stops.TerminalGroupStopLists[0].Items[1].TerminalGroupID = "another" }, false},
		{"wrong organization", func(f *fakeSource) { f.stops.TerminalGroupStopLists[0].OrganizationID = "another" }, false},
		{"items null", func(f *fakeSource) { f.stops.TerminalGroupStopLists[0].Items[1].Items = nil }, false},
		{"balance missing", func(f *fakeSource) { f.stops.TerminalGroupStopLists[0].Items[1].Items[0].Balance = nil }, false},
		{"duplicate terminal", func(f *fakeSource) {
			f.stops.TerminalGroupStopLists[0].Items = append(f.stops.TerminalGroupStopLists[0].Items, f.stops.TerminalGroupStopLists[0].Items[1])
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fake(t)
			s, store := setup(t, f)
			mustRefresh(t, s)
			before := store.Snapshot()
			disk, _ := os.ReadFile(s.cfg.CachePath)
			tc.mutate(f)
			var err error
			if tc.menu {
				err = s.RefreshMenu(context.Background())
			} else {
				err = s.RefreshStopList(context.Background())
			}
			if err == nil {
				t.Fatal("expected error")
			}
			afterDisk, _ := os.ReadFile(s.cfg.CachePath)
			if !reflect.DeepEqual(before, store.Snapshot()) || !bytes.Equal(disk, afterDisk) {
				t.Fatal("last good version changed")
			}
			v := store.View()
			status := v.StopListStatus
			if tc.menu {
				status = v.MenuStatus
			}
			if status.LastAttemptAt.IsZero() || status.LastError == "" || strings.Contains(status.LastError, "secret") {
				t.Fatal("missing/sensitive failure diagnostics")
			}
		})
	}
}

func TestUnsupportedProductsStayUnavailable(t *testing.T) {
	for _, kind := range []string{"size", "multiple sizes", "modifiers", "group modifiers", "weight", "excluded", "zero", "deleted", "missing"} {
		t.Run(kind, func(t *testing.T) {
			f := fake(t)
			clearStops(f)
			p := &f.menu.Products[0]
			switch kind {
			case "size":
				v := "size"
				p.SizePrices[0].SizeID = &v
			case "multiple sizes":
				p.SizePrices = append(p.SizePrices, p.SizePrices[0])
			case "modifiers":
				p.Modifiers = []json.RawMessage{json.RawMessage(`{}`)}
			case "group modifiers":
				p.GroupModifiers = []json.RawMessage{json.RawMessage(`{}`)}
			case "weight":
				p.IsSoldByWeight = true
			case "excluded":
				v := false
				p.SizePrices[0].Price.IsIncludedInMenu = &v
			case "zero":
				v := 0.
				p.SizePrices[0].Price.CurrentPrice = &v
			case "deleted":
				v := true
				p.IsDeleted = &v
			case "missing":
				f.menu.Products = f.menu.Products[1:]
			}
			s, store := setup(t, f)
			mustRefresh(t, s)
			if _, ok := store.FindProduct(first); ok {
				t.Fatal("unsupported product accepted")
			}
			if err := s.RefreshStopList(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, ok := store.FindProduct(first); ok {
				t.Fatal("stop refresh resurrected unsupported product")
			}
		})
	}
}

func TestRestartAndCorruptCache(t *testing.T) {
	f := fake(t)
	clearStops(f)
	s, store := setup(t, f)
	mustRefresh(t, s)
	restored, _ := catalog.NewStore(time.Hour, 5*time.Minute)
	s2, err := New(s.cfg, f, restored, seed())
	if err != nil {
		t.Fatal(err)
	}
	if err = s2.Restore(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.Snapshot(), restored.Snapshot()) {
		t.Fatal("restore changed snapshot")
	}
	edited := seed()
	edited.Items[0].Description = "New editorial text"
	changed, err := New(s.cfg, f, restored, edited)
	if err != nil {
		t.Fatal(err)
	}
	if err = changed.Restore(); err == nil {
		t.Fatal("cache from another allowlist restored")
	}
	// Outage after restart retains the restored version and its original timestamps.
	f.stopErr = errors.New("offline")
	if err = s2.RefreshStopList(context.Background()); err == nil {
		t.Fatal("expected outage")
	}
	if _, ok := restored.FindProduct(first); !ok {
		t.Fatal("fresh last-good snapshot lost")
	}
	if v := restored.ViewAt(restored.Snapshot().StopListUpdatedAt.Add(5 * time.Minute)); v.OrderingAvailable || v.Items[0].Available {
		t.Fatal("restart renewed freshness")
	}
	old := restored.Snapshot()
	for _, bad := range []string{`{`, `{"snapshot":{},"sha256":"wrong"}`} {
		if err = os.WriteFile(s.cfg.CachePath, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if err = s2.Restore(); err == nil {
			t.Fatal("corrupt cache accepted")
		}
		if !reflect.DeepEqual(old, restored.Snapshot()) {
			t.Fatal("corrupt restore changed memory")
		}
	}
	// Valid JSON with a damaged price must fail the checksum too.
	f.stopErr = nil
	mustRefresh(t, s)
	data, _ := os.ReadFile(s.cfg.CachePath)
	data = bytes.Replace(data, []byte(`"price":620`), []byte(`"price":999`), 1)
	os.WriteFile(s.cfg.CachePath, data, 0600)
	if err = s2.Restore(); err == nil {
		t.Fatal("damaged valid JSON accepted")
	}
	// A fresh successful sync recovers after a corrupt cache.
	mustRefresh(t, s2)
	if err = s2.Restore(); err != nil {
		t.Fatal(err)
	}
	s2.cfg.TerminalGroupID = "other"
	if err = s2.Restore(); err == nil {
		t.Fatal("wrong target restored")
	}
}

func TestDiskFailureDoesNotPublish(t *testing.T) {
	f := fake(t)
	s, store := setup(t, f)
	mustRefresh(t, s)
	before := store.Snapshot()
	// A directory cannot be atomically replaced with a file, on Windows or Unix.
	s.cfg.CachePath = t.TempDir()
	if err := s.RefreshMenu(context.Background()); err == nil {
		t.Fatal("expected disk error")
	}
	if !reflect.DeepEqual(before, store.Snapshot()) {
		t.Fatal("disk failure published memory")
	}
}

func TestHTTPFailuresAndRequests(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       int
	}{
		{"unauthorized", `secret`, 401}, {"rate limited", `secret`, 429}, {"server error", `secret`, 500},
		{"broken", `{`, 200}, {"error envelope", `{"errorCode":"secret"}`, 200},
		{"wrong types", `{"products":"bad"}`, 200}, {"null", `null`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); w.Write([]byte(tc.body)) }))
			defer server.Close()
			a := &API{BaseURL: server.URL, HTTP: server.Client(), Token: func(context.Context) (string, error) { return "token", nil }, ExternalMenuID: "menu"}
			if _, err := a.Menu(context.Background(), "org"); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer token" {
			t.Error("invalid HTTP contract")
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		switch r.URL.Path {
		case "/api/menu/v3/by_id":
			if req["organizationId"] != "org" || req["externalMenuId"] != "menu" || len(req) != 2 {
				t.Error("not full menu request")
			}
			w.Write(fixture(t, "menu_v3"))
		case "/api/1/stop_lists":
			if req["returnSize"] != false || !reflect.DeepEqual(req["terminalGroupsIds"], []any{"terminal"}) {
				t.Error("missing terminal scope")
			}
			w.Write(fixture(t, "stop_lists"))
		default:
			t.Error("unexpected API call")
		}
	}))
	defer server.Close()
	a := &API{BaseURL: server.URL, HTTP: server.Client(), Token: func(context.Context) (string, error) { return "token", nil }, ExternalMenuID: "menu"}
	s, _ := setup(t, a)
	mustRefresh(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Menu(ctx, "org"); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestHTTPTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	a := &API{BaseURL: server.URL, HTTP: server.Client(), Token: func(context.Context) (string, error) { return "token", nil }, ExternalMenuID: "menu"}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := a.Menu(ctx, "org"); err == nil {
		t.Fatal("timeout ignored")
	}
}

type scheduledSource struct {
	f            *fakeSource
	menus, stops atomic.Int32
	updates      chan struct{}
}

func (f *scheduledSource) Menu(ctx context.Context, org string) (Nomenclature, error) {
	f.menus.Add(1)
	return f.f.Menu(ctx, org)
}
func (f *scheduledSource) StopList(ctx context.Context, org, tg string) (StopResponse, error) {
	f.stops.Add(1)
	select {
	case f.updates <- struct{}{}:
	default:
	}
	return f.f.StopList(ctx, org, tg)
}
func TestSchedulerAndConcurrentReaders(t *testing.T) {
	f := &scheduledSource{f: fake(t), updates: make(chan struct{}, 20)}
	s, store := setup(t, f)
	s.cfg.MenuInterval = 80 * time.Millisecond
	s.cfg.StopListInterval = 15 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				v := store.View()
				if v.Version > 0 && len(v.Items) != 2 {
					t.Error("partial publication")
				}
				store.FindProduct(first)
			}
		}()
	}
	deadline := time.After(3 * time.Second)
	for f.menus.Load() < 2 || f.stops.Load() < 4 {
		select {
		case <-f.updates:
		case <-deadline:
			t.Fatal("scheduler did not poll both streams")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
	wg.Wait()
	if f.stops.Load() <= f.menus.Load() {
		t.Fatal("stop-list did not poll independently")
	}
}

func TestStopProductIDNormalization(t *testing.T) {
	balance := 0.
	resp := StopResponse{TerminalGroupStopLists: []OrganizationStops{{OrganizationID: "org", Items: []TerminalStops{{TerminalGroupID: "terminal", Items: []StopItem{{ProductID: " ABCDEF00-1111-4111-8111-111111111111 ", Balance: &balance}}}}}}}
	ids, err := stopped(resp, "org", "terminal")
	if err != nil || !ids["abcdef00-1111-4111-8111-111111111111"] {
		t.Fatalf("stop ID not normalized: %v %v", ids, err)
	}
}

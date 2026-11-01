package menusync

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"beini-backend/internal/catalog"
)

func TestMenuHTTPContract(t *testing.T) {
	f := fake(t)
	clearStops(f)
	s, store := setup(t, f)
	h := MenuHandler(store)
	request := func(method string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/api/menu", nil))
		return w
	}
	if w := request("GET"); w.Code != 503 {
		t.Fatalf("uninitialized status: %d", w.Code)
	}
	mustRefresh(t, s)
	w := request("GET")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("invalid menu HTTP headers")
	}
	var view catalog.View
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.OrderingAvailable || len(view.Items) != 2 || !view.Items[0].Available || view.Items[0].Price != 620 {
		t.Fatal("wrong frontend snapshot")
	}
	// Both frontend availability and order validation close when data expires.
	old := store.Snapshot()
	old.MenuUpdatedAt = time.Now().Add(-2 * time.Hour)
	old.StopListUpdatedAt = old.MenuUpdatedAt
	if err := store.Publish(old); err != nil {
		t.Fatal(err)
	}
	w = request("GET")
	json.Unmarshal(w.Body.Bytes(), &view)
	if w.Code != 200 || view.OrderingAvailable || view.Items[0].Available || !view.MenuStale || !view.StopListStale {
		t.Fatal("stale menu accepted")
	}
	if _, ok := store.FindProduct(first); ok {
		t.Fatal("order validation disagrees with menu")
	}
	if w = request("POST"); w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal("method not restricted")
	}
}

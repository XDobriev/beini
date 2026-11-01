package handler

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"beini-backend/internal/catalog"
	"beini-backend/internal/config"
	"beini-backend/internal/iiko"
	"beini-backend/internal/orderstate"
)

type pinCounter struct{ pins, direct int }

func (c *pinCounter) FindProduct(string) (catalog.Item, bool) {
	c.direct++
	return catalog.Item{}, false
}
func (c *pinCounter) Pin() catalog.Catalog {
	c.pins++
	return catalog.NewMemoryCatalog([]catalog.Item{
		{ID: "one", IikoID: "11111111-1111-4111-8111-111111111111", Name: "Первое", Price: 590},
		{ID: "two", IikoID: "22222222-2222-4222-8222-222222222222", Name: "Второе", Price: 620},
	})
}

func TestDynamicOrderPinsOnceAndChecksEachPrice(t *testing.T) {
	srv, captured := newMockServer(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cat := &pinCounter{}
	states := orderstate.NewMemory()
	h := NewOrderHandlerWithState(iiko.NewClient(&config.Config{APIBaseURL: srv.URL}, logger), logger, "test-token", cat, states)
	defer h.Close()
	const body = `{"phone":"89990000000","deliveryType":"pickup","items":[{"productId":"one","amount":1},{"productId":"two","amount":1}],"expectedPrices":[590,620]}`
	// Same total, different line prices still requires explicit guest consent.
	w := postOrder(h, "price-key", strings.Replace(body, `[590,620]`, `[620,590]`, 1))
	if w.Code != 409 || len(captured.Order.Items) != 0 {
		t.Fatalf("price consent: %d %s", w.Code, w.Body)
	}
	if _, exists, _ := states.Lookup("price-key"); exists {
		t.Fatal("price rejection reserved a key")
	}
	cat.pins = 0
	w = postOrder(h, "price-key", body)
	if w.Code != 200 || len(captured.Order.Items) != 2 || cat.pins != 1 || cat.direct != 0 {
		t.Fatalf("pinned order: code=%d pins=%d direct=%d", w.Code, cat.pins, cat.direct)
	}
	queue, err := states.NotificationQueue()
	if err != nil || len(queue) != 2 || !strings.Contains(string(queue[0].Source), `"phone":"+79990000000"`) {
		t.Fatalf("durable notification source missing: %+v %v", queue, err)
	}
	// Fixed diagnostic dish must not bypass dynamic price consent/allowlist.
	r := httptest.NewRequest("POST", "/api/test-order", nil)
	r.Header.Set("X-Test-Token", "test-token")
	w = httptest.NewRecorder()
	h.SendTestOrder(w, r)
	if w.Code != 503 {
		t.Fatalf("dynamic diagnostic should be disabled: %d", w.Code)
	}
}

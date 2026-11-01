package menusync

import (
	"context"
	"testing"

	"beini-backend/internal/domain"
)

func TestExistingOrderValidationUsesSyncedCatalog(t *testing.T) {
	f := fake(t)
	clearStops(f)
	s, store := setup(t, f)
	mustRefresh(t, s)
	order := domain.Order{Items: []domain.OrderItem{{ProductID: "one", Name: "Клиентская подмена", Price: 1, Amount: 2}}}
	if err := order.ResolveAndVerifyPrices(store.Pin()); err != nil {
		t.Fatal(err)
	}
	if order.Items[0].ProductID != first || order.Items[0].Name != seed().Items[0].Name || order.TotalSum() != 1240 {
		t.Fatalf("client values trusted: %+v", order)
	}
	stopped := fake(t)
	f.stops = stopped.stops
	if err := s.RefreshStopList(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := order.ResolveAndVerifyPrices(store.Pin()); err == nil {
		t.Fatal("existing domain accepted a stopped product")
	}
}

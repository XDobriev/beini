package menusync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"beini-backend/internal/catalog"
)

func v3Fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/menu_v3.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestV3AmbiguousProductCannotBeOrdered(t *testing.T) {
	var menu menuV3
	if err := json.Unmarshal([]byte(v3Fixture(t)), &menu); err != nil {
		t.Fatal(err)
	}
	duplicate := menu.Groups[0].Items[0]
	price := 900.0
	duplicate.Sizes = []priceV3{{Price: &price}}
	menu.Groups = append(menu.Groups, groupV3{ID: "other", Items: []itemV3{duplicate}})
	n, err := normalizeV3(menu, "menu")
	if err != nil {
		t.Fatal(err)
	}
	p, err := products(n)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := salePrice(p[first]); ok {
		t.Fatal("ambiguous product price accepted")
	}
}

func TestV3CompleteEmptyMenuClosesOrdering(t *testing.T) {
	f := fake(t)
	clearStops(f)
	s, store := setup(t, f)
	mustRefresh(t, s)
	n, err := normalizeV3(menuV3{ID: "menu", Name: "Empty", Products: []productV3{}, Groups: []groupV3{}}, "menu")
	if err != nil {
		t.Fatal(err)
	}
	f.menu = n
	mustRefresh(t, s)
	if _, ok := store.FindProduct(first); ok {
		t.Fatal("empty full menu left prior product available")
	}
	if store.Snapshot().Items[0].Sellable {
		t.Fatal("empty menu retained sellable flag")
	}
}

func TestCacheRestorationRequiresSameMenuSource(t *testing.T) {
	f := fake(t)
	s, store := setup(t, f)
	cfg := s.cfg
	cfg.SourceID = "v3:menu:base"
	s, err := New(cfg, f, store, seed())
	if err != nil {
		t.Fatal(err)
	}
	mustRefresh(t, s)
	for _, source := range []string{"v3:menu:base", "v3:other:base", "v3:menu:other-price"} {
		fresh, err := catalog.NewStore(time.Hour, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		cfg.SourceID = source
		restored, err := New(cfg, f, fresh, seed())
		if err != nil {
			t.Fatal(err)
		}
		err = restored.Restore()
		if source == "v3:menu:base" {
			if err != nil || fresh.Snapshot().Version == 0 {
				t.Fatal("same source did not restore")
			}
		} else if err == nil || fresh.Snapshot().Version != 0 {
			t.Fatal("another menu or price category cache accepted")
		}
	}
}

func TestV3MenuHTTPContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/menu/v3/by_id" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer token" {
			t.Error("invalid HTTP credentials or method")
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req["externalMenuId"] != "menu" || req["organizationId"] != "org" || req["priceCategoryId"] != "category" || len(req) != 3 {
			t.Errorf("wrong v3 scope: %v", req)
		}
		w.Write([]byte(v3Fixture(t)))
	}))
	defer server.Close()
	api := &API{BaseURL: server.URL, HTTP: server.Client(), Token: func(context.Context) (string, error) { return "token", nil }, ExternalMenuID: "menu", PriceCategoryID: "category"}
	n, err := api.Menu(context.Background(), "org")
	if err != nil {
		t.Fatal(err)
	}
	p, err := products(n)
	if err != nil {
		t.Fatal(err)
	}
	price, ok := salePrice(p[first])
	if !ok || price != 620 || p[first].Name != "Имя из iiko" {
		t.Fatal("v3 price/name not normalized")
	}
}

func TestV3MenuRejectsUnsupportedSales(t *testing.T) {
	base := v3Fixture(t)
	for _, tc := range []struct {
		name, body          string
		sellable, wantError bool
	}{
		{"ordinary", base, true, false},
		{"optional-type-omitted", strings.Replace(base, `"type":"DISH",`, "", 1), true, false},
		{"hidden-group", strings.Replace(base, `"id":"group"`, `"isHidden":true,"id":"group"`, 1), false, false},
		{"scheduled-group", strings.Replace(base, `"id":"group"`, `"scheduleId":"hours","id":"group"`, 1), false, false},
		{"hidden-item", strings.Replace(base, `"name":"Имя из iiko"`, `"isHidden":true,"name":"Имя из iiko"`, 1), false, false},
		{"hidden-price", strings.Replace(base, `"price":620`, `"isHidden":true,"price":620`, 1), false, false},
		{"weighed", strings.Replace(base, `"useBalanceForSell":false`, `"useBalanceForSell":true`, 1), false, false},
		{"open-price", strings.Replace(base, `"canSetOpenPrice":false`, `"canSetOpenPrice":true`, 1), false, false},
		{"compound", strings.Replace(base, `"orderItemType":"Product"`, `"orderItemType":"Compound"`, 1), false, false},
		{"sized", strings.Replace(base, `"sizeId":null`, `"sizeId":"large"`, 1), false, false},
		{"modifiers", strings.Replace(base, `"name":"Имя из iiko"`, `"modifierGroups":[{"name":"required"}],"name":"Имя из iiko"`, 1), false, false},
		{"multiple-prices", strings.Replace(base, `{"sizeId":null,"price":620}`, `{"sizeId":null,"price":620},{"sizeId":null,"price":700}`, 1), false, false},
		{"wrong-menu", strings.Replace(base, `"id": "menu"`, `"id": "another"`, 1), false, true},
		{"negative-price", strings.Replace(base, `"price":620`, `"price":-1`, 1), false, true},
		{"missing-prices", strings.Replace(base, `"price":620`, `"other":620`, 1), false, true},
		{"missing-dictionary", strings.Replace(base, `"products": [`, `"other": [`, 1), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/menu/v3/by_id" {
					http.NotFound(w, r)
					return
				}
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			api := &API{BaseURL: server.URL, HTTP: server.Client(), Token: func(context.Context) (string, error) { return "token", nil }, ExternalMenuID: "menu"}
			n, err := api.Menu(context.Background(), "org")
			if tc.wantError {
				if err == nil {
					t.Fatal("invalid v3 snapshot accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			p, err := products(n)
			if err != nil {
				t.Fatal(err)
			}
			_, ok := salePrice(p[first])
			if ok != tc.sellable {
				t.Fatalf("sellable=%v want=%v", ok, tc.sellable)
			}
		})
	}
}

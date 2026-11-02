package menusync

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"beini-backend/internal/catalog"
)

func TestReviewedIdentityCannotSellAnotherProduct(t *testing.T) {
	for _, name := range []string{"Творожные шарики", "Имя из iiko"} {
		t.Run(name, func(t *testing.T) {
			f := fake(t)
			clearStops(f)
			f.menu.Products[0].Name = name
			reviewed, err := catalog.ReadSeed(strings.NewReader(`{"categories":[{"id":"grill","name":"Мангал"}],"items":[{"id":"one","iikoId":"` + first + `","name":"Наше витринное имя","sourceName":"Имя из iiko","price":590,"category":"grill","available":true}]}`))
			if err != nil {
				t.Fatal(err)
			}
			store, _ := catalog.NewStore(time.Hour, time.Minute)
			s, err := New(Config{OrganizationID: "org", TerminalGroupID: "terminal", CachePath: filepath.Join(t.TempDir(), "menu.json"), MenuInterval: time.Hour, StopListInterval: time.Minute, RequestTimeout: time.Second}, f, store, reviewed)
			if err != nil {
				t.Fatal(err)
			}
			mustRefresh(t, s)
			item := store.Snapshot().Items[0]
			if name == "Творожные шарики" {
				if item.Available || item.Sellable || item.UnavailableReason != "identity_review_required" {
					t.Fatalf("another product admitted: %+v", item)
				}
				if err := s.RefreshStopList(context.Background()); err != nil {
					t.Fatal(err)
				}
				if _, ok := store.FindProduct(first); ok {
					t.Fatal("stop refresh reopened wrong identity")
				}
			} else if !item.Available || item.Name != "Наше витринное имя" {
				t.Fatalf("reviewed product unavailable or editorial name lost: %+v", item)
			}
		})
	}
}

func TestHiddenReviewedProductIsAbsentFromMenuAndOrdering(t *testing.T) {
	f := fake(t)
	clearStops(f)
	reviewed, err := catalog.ReadSeed(strings.NewReader(`{"categories":[{"id":"grill","name":"Мангал"}],"items":[{"id":"one","iikoId":"` + first + `","name":"NEW Блюдо","sourceName":"Имя из iiko","hidden":true,"price":590,"category":"grill","available":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	store, _ := catalog.NewStore(time.Hour, time.Minute)
	s, err := New(Config{OrganizationID: "org", TerminalGroupID: "terminal", CachePath: filepath.Join(t.TempDir(), "menu.json"), MenuInterval: time.Hour, StopListInterval: time.Minute, RequestTimeout: time.Second}, f, store, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	mustRefresh(t, s)
	if _, ok := store.FindProduct(first); ok {
		t.Fatal("hidden item can be ordered")
	}
	w := httptest.NewRecorder()
	MenuHandler(store).ServeHTTP(w, httptest.NewRequest("GET", "/api/menu", nil))
	if strings.Contains(w.Body.String(), first) {
		t.Fatal("hidden NEW item leaked into public menu")
	}
}

func TestReviewedIdentityNormalizesOnlyCosmeticChanges(t *testing.T) {
	f := fake(t)
	clearStops(f)
	f.menu.Products[0].Name = "  ИМЯ  из\tiiko  "
	s, store := setup(t, f)
	mustRefresh(t, s)
	if _, ok := store.FindProduct(first); !ok {
		t.Fatal("cosmetic whitespace/case closed reviewed item")
	}
}

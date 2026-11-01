package catalog

import (
	"strings"
	"testing"
	"time"
)

func testSnapshot() Snapshot {
	now := time.Now().UTC()
	return Snapshot{SchemaVersion: 1, Version: 1, Revision: 7, OrganizationID: "org", TerminalGroupID: "tg", SeedDigest: "digest", UpdatedAt: now, MenuUpdatedAt: now, StopListUpdatedAt: now, Categories: []Category{{ID: "grill", Name: "Мангал"}}, Items: []Item{{ID: "dish", IikoID: "uuid", Name: "Блюдо", Price: 100, Category: "grill", Available: true, Sellable: true}}}
}
func TestFreshnessAndImmutableSnapshots(t *testing.T) {
	store, err := NewStore(time.Hour, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.FindProduct("dish"); ok {
		t.Fatal("uninitialized catalog accepts orders")
	}
	snap := testSnapshot()
	base := snap.UpdatedAt
	if err = store.Publish(snap); err != nil {
		t.Fatal(err)
	}
	snap.Items[0].Price = 1
	snap.Categories[0].Name = "changed"
	view := store.ViewAt(base)
	if view.Items[0].Price != 100 || view.Categories[0].Name != "Мангал" {
		t.Fatal("caller changed published snapshot")
	}
	view.Items[0].Price = 2
	if store.Snapshot().Items[0].Price != 100 {
		t.Fatal("reader changed snapshot")
	}
	for _, tc := range []struct {
		name                 string
		at                   time.Time
		menuStale, stopStale bool
	}{
		{"fresh", base.Add(59 * time.Second), false, false},
		{"stop limit", base.Add(time.Minute), false, true},
		{"menu limit", base.Add(time.Hour), true, true},
		{"clock rollback", base.Add(-time.Second), true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := store.ViewAt(tc.at)
			if v.MenuStale != tc.menuStale || v.StopListStale != tc.stopStale || v.Items[0].Available == (tc.menuStale || tc.stopStale) {
				t.Fatalf("bad freshness: %+v", v)
			}
		})
	}
	pinned := store.Pin()
	next := store.Snapshot()
	next.Version++
	next.Items[0].Price = 200
	if err = store.Publish(next); err != nil {
		t.Fatal(err)
	}
	if it, ok := pinned.FindProduct(" UUID "); !ok || it.Price != 100 {
		t.Fatal("pinned order changed version")
	}
	if it, ok := store.FindProduct("DISH"); !ok || it.Price != 200 {
		t.Fatal("new version unavailable")
	}
	next.Items[0].Available = false
	if err = store.Publish(next); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.FindProduct("uuid"); ok {
		t.Fatal("unavailable product accepted")
	}
}

func TestRejectInvalidSnapshotsAndSeed(t *testing.T) {
	for _, kind := range []string{"aliases", "category", "availability", "timestamp", "negative"} {
		t.Run(kind, func(t *testing.T) {
			s := testSnapshot()
			switch kind {
			case "aliases":
				it := s.Items[0]
				it.ID = "UUID"
				it.IikoID = "different"
				s.Items = append(s.Items, it)
			case "category":
				s.Items[0].Category = "missing"
			case "availability":
				s.Items[0].Sellable = false
			case "timestamp":
				s.StopListUpdatedAt = time.Time{}
			case "negative":
				s.Items[0].Price = -1
			}
			store, _ := NewStore(time.Hour, time.Minute)
			if err := store.Publish(s); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
			if store.Snapshot().Version != 0 {
				t.Fatal("invalid snapshot published")
			}
		})
	}
	if _, err := ReadSeed(strings.NewReader(`{"categories":[],"items":[]}`)); err == nil {
		t.Fatal("empty allowlist accepted")
	}
	if _, err := NewStore(0, time.Minute); err == nil {
		t.Fatal("invalid freshness accepted")
	}
}

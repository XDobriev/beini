package catalog

import (
	"encoding/json"
	"os"
	"testing"
)

// Every sellable item must resolve by both browser ID and iiko ID at the
// advertised price. Extra server-only fixtures are intentionally permitted.
func TestMenuCatalogConsistency(t *testing.T) {
	data, err := os.ReadFile("../../data/menu_catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var menu struct {
		Items []struct {
			ID        string  `json:"id"`
			IikoID    string  `json:"iikoId"`
			Price     float64 `json:"price"`
			Available *bool   `json:"available"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &menu); err != nil {
		t.Fatal(err)
	}
	catalog := DefaultBeiniCatalog()
	checked := 0
	for _, item := range menu.Items {
		if item.Available != nil && !*item.Available {
			continue
		}
		checked++
		t.Run(item.ID, func(t *testing.T) {
			for _, key := range []string{item.ID, item.IikoID} {
				actual, ok := catalog.FindProduct(key)
				if !ok {
					t.Errorf("menu item %q missing from server catalog", key)
					continue
				}
				if actual.ID != item.ID || actual.IikoID != item.IikoID || actual.Price != item.Price {
					t.Errorf("catalog mismatch for %q: got %+v; want ID=%s iikoId=%s price=%v",
						key, actual, item.ID, item.IikoID, item.Price)
				}
			}
		})
	}
	if checked == 0 {
		t.Fatal("menu must contain available items")
	}
}

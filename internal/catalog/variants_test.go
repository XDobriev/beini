package catalog

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const variantSeed = `{"categories":[{"id":"drinks","name":"Напитки"},{"id":"desserts","name":"Десерты"}],"items":[
{"id":"glass","iikoId":"uuid-glass","name":"Лимонад стакан","sourceName":"Лимонад стакан","price":260,"category":"drinks","variantGroup":"lemonade","variantName":"Лимонад","variantLabel":"Стакан","variantOrder":1},
{"id":"jug","iikoId":"uuid-jug","name":"Лимонад графин","sourceName":"Лимонад графин","price":650,"category":"drinks","variantGroup":"lemonade","variantName":"Лимонад","variantLabel":"Графин","variantOrder":2}]}`

func TestPortionMetadataSurvivesSeedAndSnapshot(t *testing.T) {
	seed, err := ReadSeed(strings.NewReader(variantSeed))
	if err != nil {
		t.Fatal(err)
	}
	snap := testSnapshot()
	snap.Items, snap.Categories = seed.Items, seed.Categories
	store, _ := NewStore(time.Hour, time.Minute)
	if err := store.Publish(snap); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(store.View())
	var data struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"variantGroup": "lemonade", "variantName": "Лимонад", "variantLabel": "Стакан", "variantOrder": float64(1)} {
		if got := data.Items[0][key]; got != want {
			t.Errorf("%s lost between seed and public view: got %v, want %v", key, got, want)
		}
	}
}

func TestRejectAmbiguousPortionGroups(t *testing.T) {
	for _, tc := range []struct{ name, from, to string }{
		{"partial", `"variantLabel":"Стакан",`, ``},
		{"unsafe group", `"variantGroup":"lemonade"`, `"variantGroup":"bad\"group"`},
		{"duplicate label", `"variantLabel":"Графин"`, `"variantLabel":"Стакан"`},
		{"duplicate order", `"variantOrder":2`, `"variantOrder":1`},
		{"different name", `"variantName":"Лимонад","variantLabel":"Графин"`, `"variantName":"Другое","variantLabel":"Графин"`},
		{"different category", `"category":"drinks","variantGroup":"lemonade","variantName":"Лимонад","variantLabel":"Графин"`, `"category":"desserts","variantGroup":"lemonade","variantName":"Лимонад","variantLabel":"Графин"`},
		{"zero order", `"variantOrder":1`, `"variantOrder":0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ReadSeed(strings.NewReader(strings.Replace(variantSeed, tc.from, tc.to, 1))); err == nil {
				t.Fatal("ambiguous portion metadata accepted")
			}
		})
	}
}

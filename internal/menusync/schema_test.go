package menusync

import (
	"encoding/json"
	"strings"
	"testing"
)

// ProductInfo contract from the public iiko OpenAPI (2026-11-01).
// Wrong field names must never turn a weighed/open-price product into a sale.
func TestOfficialProductSaleContract(t *testing.T) {
	const base = `{"id":"11111111-1111-4111-8111-111111111111","name":"Dish","type":"dish","isDeleted":false,"useBalanceForSell":false,"canSetOpenPrice":false,"orderItemType":"Product","sizePrices":[{"sizeId":null,"price":{"currentPrice":620,"isIncludedInMenu":true}}]}`
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"ordinary", base, true},
		{"weighed", strings.Replace(base, `"useBalanceForSell":false`, `"useBalanceForSell":true`, 1), false},
		{"open-price", strings.Replace(base, `"canSetOpenPrice":false`, `"canSetOpenPrice":true`, 1), false},
		{"compound", strings.Replace(base, `"orderItemType":"Product"`, `"orderItemType":"Compound"`, 1), false},
		{"missing-weight-flag", strings.Replace(base, `"useBalanceForSell":false,`, "", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p Product
			if err := json.Unmarshal([]byte(tc.body), &p); err != nil {
				t.Fatal(err)
			}
			price, ok := salePrice(p)
			if ok != tc.want || (ok && price != 620) {
				t.Fatalf("price=%v sellable=%v want=%v", price, ok, tc.want)
			}
		})
	}
}

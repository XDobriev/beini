package menusync

import (
	"errors"
	"math"
	"strings"
)

// V3 keeps product restrictions in a dictionary and sale prices in group items.
// The storefront currently supports one unsized product without modifiers.
type menuV3 struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Products []productV3 `json:"products"`
	Groups   []groupV3   `json:"itemsGroups"`
}
type productV3 struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	OrderItemType    string `json:"orderItemType"`
	ModifierSchemaID string `json:"modifierSchemaId"`
	Splittable       bool   `json:"splittable"`
	OpenPrice        bool   `json:"canSetOpenPrice"`
	Weighed          bool   `json:"useBalanceForSell"`
	Marked           bool   `json:"isMarked"`
}
type groupV3 struct {
	ID         string   `json:"id"`
	Hidden     bool     `json:"isHidden"`
	ScheduleID string   `json:"scheduleId"`
	Items      []itemV3 `json:"items"`
}
type itemV3 struct {
	ProductID      string     `json:"productId"`
	Name           string     `json:"name"`
	Hidden         bool       `json:"isHidden"`
	Sizes          []priceV3  `json:"sizePrices"`
	ModifierGroups []struct{} `json:"modifierGroups"`
}
type priceV3 struct {
	SizeID string   `json:"sizeId"`
	Price  *float64 `json:"price"`
	Hidden bool     `json:"isHidden"`
}

func normalizeV3(menu menuV3, expectedID string) (Nomenclature, error) {
	if menu.ID != expectedID || strings.TrimSpace(menu.Name) == "" || menu.Products == nil || menu.Groups == nil {
		return Nomenclature{}, errors.New("incomplete external menu snapshot")
	}
	revision := int64(0)
	result := Nomenclature{Revision: &revision, Unversioned: true, Products: make([]Product, len(menu.Products))}
	positions := map[string]int{}
	for i, p := range menu.Products {
		key := productKey(p.ID)
		if key == "" {
			return Nomenclature{}, errors.New("invalid external product identity")
		}
		if _, exists := positions[key]; exists {
			return Nomenclature{}, errors.New("duplicate external product identity")
		}
		positions[key] = i
		deleted := true
		result.Products[i] = Product{ID: p.ID, IsDeleted: &deleted}
	}
	seen := map[string]bool{}
	groups := map[string]bool{}
	for _, g := range menu.Groups {
		if g.ID == "" || groups[g.ID] {
			return Nomenclature{}, errors.New("invalid external group identity")
		}
		groups[g.ID] = true
		if g.Hidden || g.ScheduleID != "" {
			continue
		}
		for _, item := range g.Items {
			key := productKey(item.ProductID)
			i, exists := positions[key]
			// Compound items can have no productId. They cannot be ordered here.
			if key == "" {
				continue
			}
			if !exists {
				return Nomenclature{}, errors.New("external product dictionary incomplete")
			}
			if seen[key] {
				deleted := true
				result.Products[i] = Product{ID: menu.Products[i].ID, IsDeleted: &deleted}
				continue
			}
			seen[key] = true
			p := menu.Products[i]
			if item.Hidden || len(item.Sizes) != 1 || len(item.ModifierGroups) > 0 || p.ModifierSchemaID != "" || p.Splittable || p.Weighed || p.OpenPrice || p.Marked || p.OrderItemType != "Product" {
				continue
			}
			typ := ""
			switch p.Type {
			case "", "DISH":
				// V3 type is nullable/optional. Product + the reviewed seed still
				// restrict ordering to ordinary known items; explicit other types
				// below are rejected rather than treated as dishes.
				typ = "dish"
			case "GOODS":
				typ = "good"
			default:
				continue
			}
			price := item.Sizes[0]
			if price.Hidden || price.SizeID != "" {
				continue
			}
			if price.Price == nil || math.IsNaN(*price.Price) || math.IsInf(*price.Price, 0) || *price.Price < 0 || strings.TrimSpace(item.Name) == "" {
				return Nomenclature{}, errors.New("invalid external item price or name")
			}
			no, yes := false, true
			normalized := Product{ID: p.ID, Name: item.Name, Type: typ, IsDeleted: &no, IsIncludedInMenu: &yes, UseBalanceForSell: &no, CanSetOpenPrice: &no, OrderItemType: "Product", SizePrices: []SizePrice{{}}}
			normalized.SizePrices[0].Price.CurrentPrice = price.Price
			normalized.SizePrices[0].Price.IsIncludedInMenu = &yes
			result.Products[i] = normalized
		}
	}
	return result, nil
}

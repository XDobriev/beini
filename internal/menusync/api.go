// Package menusync periodically publishes a reviewed, durable iiko menu snapshot.
package menusync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
)

type Nomenclature struct {
	Revision *int64    `json:"revision"`
	Products []Product `json:"products"`
}
type Product struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Type              string            `json:"type"`
	IsDeleted         *bool             `json:"isDeleted"`
	IsIncludedInMenu  *bool             `json:"isIncludedInMenu"`
	IsSoldByWeight    bool              `json:"isSoldByWeight"`
	UseBalanceForSell *bool             `json:"useBalanceForSell"`
	CanSetOpenPrice   *bool             `json:"canSetOpenPrice"`
	OrderItemType     string            `json:"orderItemType"`
	SizePrices        []SizePrice       `json:"sizePrices"`
	Modifiers         []json.RawMessage `json:"modifiers"`
	GroupModifiers    []json.RawMessage `json:"groupModifiers"`
}
type SizePrice struct {
	SizeID *string `json:"sizeId"`
	Price  struct {
		CurrentPrice     *float64 `json:"currentPrice"`
		IsIncludedInMenu *bool    `json:"isIncludedInMenu"`
	} `json:"price"`
}
type StopResponse struct {
	TerminalGroupStopLists []OrganizationStops `json:"terminalGroupStopLists"`
}
type OrganizationStops struct {
	OrganizationID string          `json:"organizationId"`
	Items          []TerminalStops `json:"items"`
}
type TerminalStops struct {
	TerminalGroupID string     `json:"terminalGroupId"`
	Items           []StopItem `json:"items"`
}
type StopItem struct {
	ProductID string   `json:"productId"`
	Balance   *float64 `json:"balance"`
	SizeID    *string  `json:"sizeId"`
}

type Source interface {
	Menu(context.Context, string) (Nomenclature, error)
	StopList(context.Context, string, string) (StopResponse, error)
}

// API reuses the application's token provider; credentials are never persisted here.
// BaseURL and HTTP must be explicitly supplied (httptest for offline development).
type API struct {
	BaseURL string
	HTTP    *http.Client
	Token   func(context.Context) (string, error)
}

func (a *API) post(ctx context.Context, path string, payload any, result any) error {
	if a.HTTP == nil || a.Token == nil || a.BaseURL == "" {
		return errors.New("incomplete menu API configuration")
	}
	token, err := a.Token(ctx)
	if err != nil || token == "" {
		return errors.New("menu API authentication failed")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("invalid menu request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid menu API URL")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	// Refuse redirects: a bearer token must never be forwarded to an unexpected host.
	client := *a.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("menu API transport failure")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("menu API HTTP %d", resp.StatusCode)
	}
	const maxBody = 16 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(data) > maxBody {
		return errors.New("menu API body unreadable or too large")
	}
	var envelope map[string]json.RawMessage
	if err = json.Unmarshal(data, &envelope); err != nil || envelope == nil {
		return errors.New("invalid menu API JSON")
	}
	for _, key := range []string{"error", "errorCode", "errorDescription"} {
		if v, ok := envelope[key]; ok && string(v) != "null" && string(v) != "\"\"" {
			return errors.New("menu API error response")
		}
	}
	if err = json.Unmarshal(data, result); err != nil {
		return errors.New("invalid menu API schema")
	}
	return nil
}

func (a *API) Menu(ctx context.Context, org string) (Nomenclature, error) {
	var n Nomenclature
	// Always request a full version. A revision delta cannot replace a full snapshot.
	err := a.post(ctx, "/api/1/nomenclature", map[string]any{"organizationId": org, "startRevision": 0}, &n)
	return n, err
}
func (a *API) StopList(ctx context.Context, org, tg string) (StopResponse, error) {
	var s StopResponse
	err := a.post(ctx, "/api/1/stop_lists", map[string]any{"organizationIds": []string{org}, "terminalGroupsIds": []string{tg}, "returnSize": false}, &s)
	return s, err
}

// stopped requires an explicit, complete response for exactly the selected terminal.
// A missing terminal or items:null is an error; only items:[] means an empty stop-list.
func productKey(id string) string { return strings.ToLower(strings.TrimSpace(id)) }

func stopped(s StopResponse, org, tg string) (map[string]bool, error) {
	result := map[string]bool{}
	orgCount, tgCount := 0, 0
	for _, o := range s.TerminalGroupStopLists {
		if o.OrganizationID != org {
			continue
		}
		orgCount++
		if o.Items == nil {
			return nil, errors.New("missing terminal stop-lists")
		}
		for _, t := range o.Items {
			if t.TerminalGroupID != tg {
				continue
			}
			tgCount++
			if t.Items == nil {
				return nil, errors.New("missing terminal items")
			}
			seen := map[string]bool{}
			for _, it := range t.Items {
				if productKey(it.ProductID) == "" || it.Balance == nil || math.IsNaN(*it.Balance) || math.IsInf(*it.Balance, 0) || it.SizeID != nil || seen[productKey(it.ProductID)] {
					return nil, errors.New("invalid stop-list item")
				}
				seen[productKey(it.ProductID)] = true
				result[productKey(it.ProductID)] = *it.Balance <= 0
			}
		}
	}
	if orgCount != 1 || tgCount != 1 {
		return nil, errors.New("selected terminal stop-list missing or duplicated")
	}
	return result, nil
}

func products(n Nomenclature) (map[string]Product, error) {
	if n.Revision == nil || *n.Revision < 0 || len(n.Products) == 0 {
		return nil, errors.New("incomplete nomenclature")
	}
	result := map[string]Product{}
	for _, p := range n.Products {
		if productKey(p.ID) == "" || p.IsDeleted == nil {
			return nil, errors.New("invalid product identity")
		}
		if _, ok := result[productKey(p.ID)]; ok {
			return nil, errors.New("duplicate product")
		}
		for _, sp := range p.SizePrices {
			if sp.Price.IsIncludedInMenu == nil || sp.Price.CurrentPrice == nil || math.IsNaN(*sp.Price.CurrentPrice) || math.IsInf(*sp.Price.CurrentPrice, 0) || *sp.Price.CurrentPrice < 0 {
				return nil, errors.New("invalid product price")
			}
		}
		if !*p.IsDeleted && (strings.TrimSpace(p.Name) == "" || p.Type == "" || len(p.SizePrices) == 0) {
			return nil, errors.New("incomplete active product")
		}
		result[productKey(p.ID)] = p
	}
	return result, nil
}

func salePrice(p Product) (float64, bool) {
	if p.IsDeleted == nil || *p.IsDeleted || (!strings.EqualFold(p.Type, "dish") && !strings.EqualFold(p.Type, "good")) || p.IsSoldByWeight ||
		p.UseBalanceForSell == nil || *p.UseBalanceForSell || p.CanSetOpenPrice == nil || *p.CanSetOpenPrice ||
		(p.OrderItemType != "" && p.OrderItemType != "Product") || len(p.SizePrices) != 1 || len(p.Modifiers) > 0 || len(p.GroupModifiers) > 0 {
		return 0, false
	}
	sp := p.SizePrices[0]
	// Current checkout has no size/modifier support. Never silently choose the first size.
	if sp.SizeID != nil && *sp.SizeID != "" {
		return 0, false
	}
	if p.IsIncludedInMenu != nil && !*p.IsIncludedInMenu {
		return 0, false
	}
	if sp.Price.IsIncludedInMenu == nil || !*sp.Price.IsIncludedInMenu {
		return 0, false
	}
	return *sp.Price.CurrentPrice, *sp.Price.CurrentPrice > 0
}

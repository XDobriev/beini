package catalog

import (
	"strings"
	"sync"
)

// Item описывает блюдо в серверном каталоге номенклатуры BEINI
type Item struct {
	ID           string  `json:"id"`
	IikoID       string  `json:"iikoId"`
	Name         string  `json:"name"`
	SourceName   string  `json:"sourceName,omitempty"`
	Hidden       bool    `json:"hidden,omitempty"`
	Price        float64 `json:"price"`
	Category     string  `json:"category,omitempty"`
	Description  string  `json:"description,omitempty"`
	Image        string  `json:"image,omitempty"`
	Weight       string  `json:"weight,omitempty"`
	VariantGroup string  `json:"variantGroup,omitempty"`
	VariantName  string  `json:"variantName,omitempty"`
	VariantLabel string  `json:"variantLabel,omitempty"`
	VariantOrder int     `json:"variantOrder,omitempty"`
	Badge        string  `json:"badge,omitempty"`
	IsHalal      bool    `json:"isHalal"`
	Available    bool    `json:"available"`
	// Sellable is the reviewed + upstream eligibility before applying stop-list.
	Sellable          bool   `json:"sellable"`
	UnavailableReason string `json:"unavailableReason,omitempty"`
}

// Catalog предоставляет интерфейс для получения проверенных цен и наименований блюд
type Catalog interface {
	FindProduct(idOrIikoID string) (Item, bool)
}

// Inverted/Map каталог для быстрого поиска по ID и IikoID
type MemoryCatalog struct {
	mu    sync.RWMutex
	items map[string]Item
}

// NewMemoryCatalog создает каталог с переданными позициями
func NewMemoryCatalog(items []Item) *MemoryCatalog {
	c := &MemoryCatalog{
		items: make(map[string]Item, len(items)*2),
	}
	for _, it := range items {
		c.addItem(it)
	}
	return c
}

func (c *MemoryCatalog) addItem(it Item) {
	if it.ID != "" {
		c.items[strings.ToLower(strings.TrimSpace(it.ID))] = it
	}
	if it.IikoID != "" {
		c.items[strings.ToLower(strings.TrimSpace(it.IikoID))] = it
	}
}

// FindProduct ищет блюдо по ID или IikoID (регистронезависимо)
func (c *MemoryCatalog) FindProduct(idOrIikoID string) (Item, bool) {
	key := strings.ToLower(strings.TrimSpace(idOrIikoID))
	if key == "" {
		return Item{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	it, ok := c.items[key]
	return it, ok
}

// DefaultBeiniCatalog возвращает актуальный каталог ресторана BEINI
func DefaultBeiniCatalog() *MemoryCatalog {
	return NewMemoryCatalog(defaultBeiniItems)
}

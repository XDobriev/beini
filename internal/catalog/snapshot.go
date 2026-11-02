package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Category preserves the editorial order and presentation of the local menu.
type Category struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"shortName,omitempty"`
	Icon      string `json:"icon,omitempty"`
	SortOrder int    `json:"sortOrder"`
}

type Seed struct {
	Categories []Category `json:"categories"`
	Items      []Item     `json:"items"`
}

// ReadSeed reads the reviewed local allowlist, never an iiko-discovered product list.
func ReadSeed(r io.Reader) (Seed, error) {
	var seed Seed
	d := json.NewDecoder(r)
	if err := d.Decode(&seed); err != nil {
		return seed, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return seed, errors.New("trailing seed data")
	}
	if err := validateItems(seed.Items, seed.Categories); err != nil {
		return seed, err
	}
	for _, item := range seed.Items {
		if strings.TrimSpace(item.SourceName) == "" {
			return seed, fmt.Errorf("missing reviewed source name for %q", item.ID)
		}
	}
	return seed, nil
}

type Snapshot struct {
	SchemaVersion     int        `json:"schemaVersion"`
	Version           uint64     `json:"version"`
	Revision          int64      `json:"revision"`
	OrganizationID    string     `json:"organizationId"`
	TerminalGroupID   string     `json:"terminalGroupId"`
	SeedDigest        string     `json:"seedDigest"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	MenuUpdatedAt     time.Time  `json:"menuUpdatedAt"`
	StopListUpdatedAt time.Time  `json:"stopListUpdatedAt"`
	Categories        []Category `json:"categories"`
	Items             []Item     `json:"items"`
}

type StreamStatus struct {
	LastAttemptAt time.Time `json:"lastAttemptAt"`
	LastError     string    `json:"lastError,omitempty"`
}

type View struct {
	Snapshot
	MenuStale         bool         `json:"menuStale"`
	StopListStale     bool         `json:"stopListStale"`
	OrderingAvailable bool         `json:"orderingAvailable"`
	MenuStatus        StreamStatus `json:"menuStatus"`
	StopListStatus    StreamStatus `json:"stopListStatus"`
}

// Store is shared by order validation and the menu endpoint. Readers receive copies.
// Exactly one menusync writer must own a Store and its cache file.
type Store struct {
	mu                     sync.RWMutex
	snapshot               Snapshot
	menuStatus, stopStatus StreamStatus
	menuMaxAge, stopMaxAge time.Duration
}

func NewStore(menuMaxAge, stopMaxAge time.Duration) (*Store, error) {
	if menuMaxAge <= 0 || stopMaxAge <= 0 {
		return nil, errors.New("positive freshness limits required")
	}
	return &Store{menuMaxAge: menuMaxAge, stopMaxAge: stopMaxAge}, nil
}

func ValidateSnapshot(s Snapshot) error {
	if s.SchemaVersion != 1 || s.Version == 0 || s.Revision < 0 || s.OrganizationID == "" || s.TerminalGroupID == "" || s.SeedDigest == "" {
		return errors.New("invalid snapshot identity")
	}
	if s.MenuUpdatedAt.IsZero() || s.StopListUpdatedAt.IsZero() || s.UpdatedAt.IsZero() || s.MenuUpdatedAt.After(s.UpdatedAt) || s.StopListUpdatedAt.After(s.UpdatedAt) {
		return errors.New("invalid snapshot timestamps")
	}
	if err := validateItems(s.Items, s.Categories); err != nil {
		return err
	}
	for _, it := range s.Items {
		if it.Available && (!it.Sellable || it.Price <= 0) {
			return errors.New("invalid availability")
		}
	}
	return nil
}

func validateItems(items []Item, categories []Category) error {
	if len(items) == 0 || len(categories) == 0 {
		return errors.New("empty reviewed menu")
	}
	cats := map[string]bool{}
	for _, c := range categories {
		if c.ID == "" || c.Name == "" || cats[c.ID] {
			return errors.New("invalid category")
		}
		cats[c.ID] = true
	}
	keys := map[string]bool{}
	for _, it := range items {
		if strings.TrimSpace(it.Name) == "" || it.ID == "" || it.IikoID == "" || !cats[it.Category] || math.IsNaN(it.Price) || math.IsInf(it.Price, 0) || it.Price < 0 {
			return fmt.Errorf("invalid reviewed item %q", it.ID)
		}
		for _, k := range []string{it.ID, it.IikoID} {
			k = strings.ToLower(strings.TrimSpace(k))
			if k == "" || keys[k] {
				return errors.New("duplicate catalog identifier")
			}
			keys[k] = true
		}
	}
	return validateVariants(items)
}

var variantGroupPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// Groups only affect presentation; every portion remains a separate trusted product.
func validateVariants(items []Item) error {
	type group struct {
		name, category string
		labels         map[string]bool
		orders         map[int]bool
	}
	groups := map[string]*group{}
	for _, it := range items {
		if it.VariantGroup == "" && it.VariantName == "" && it.VariantLabel == "" && it.VariantOrder == 0 {
			continue
		}
		if !variantGroupPattern.MatchString(it.VariantGroup) || strings.TrimSpace(it.VariantName) == "" || strings.TrimSpace(it.VariantLabel) == "" || it.VariantOrder < 1 {
			return fmt.Errorf("invalid portion metadata for %q", it.ID)
		}
		g := groups[it.VariantGroup]
		if g == nil {
			g = &group{name: it.VariantName, category: it.Category, labels: map[string]bool{}, orders: map[int]bool{}}
			groups[it.VariantGroup] = g
		}
		label := strings.ToLower(strings.TrimSpace(it.VariantLabel))
		if g.name != it.VariantName || g.category != it.Category || g.labels[label] || g.orders[it.VariantOrder] {
			return fmt.Errorf("ambiguous portion group %q", it.VariantGroup)
		}
		g.labels[label], g.orders[it.VariantOrder] = true, true
	}
	return nil
}

func clone(s Snapshot) Snapshot {
	s.Items = append([]Item{}, s.Items...)
	s.Categories = append([]Category{}, s.Categories...)
	return s
}

// Publish replaces the entire version. Persist and validate it before calling.
func (c *Store) Publish(s Snapshot) error {
	if err := ValidateSnapshot(s); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot = clone(s)
	return nil
}

func (c *Store) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return clone(c.snapshot)
}

func (c *Store) RecordAttempt(menu bool, at time.Time, code string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := StreamStatus{LastAttemptAt: at, LastError: code}
	if menu {
		c.menuStatus = status
	} else {
		c.stopStatus = status
	}
}

func (c *Store) ViewAt(now time.Time) View {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s := clone(c.snapshot)
	stale := func(at time.Time, max time.Duration) bool { return at.IsZero() || now.Before(at) || now.Sub(at) >= max }
	v := View{Snapshot: s, MenuStale: stale(s.MenuUpdatedAt, c.menuMaxAge), StopListStale: stale(s.StopListUpdatedAt, c.stopMaxAge), MenuStatus: c.menuStatus, StopListStatus: c.stopStatus}
	v.OrderingAvailable = s.Version > 0 && !v.MenuStale && !v.StopListStale
	if !v.OrderingAvailable {
		for i := range v.Items {
			v.Items[i].Available = false
		}
	}
	return v
}

func (c *Store) View() View { return c.ViewAt(time.Now()) }

// Pin freezes one version and freshness decision for an entire order request.
func (c *Store) Pin() Catalog { return pinned{view: c.View()} }

type pinned struct{ view View }

func (p pinned) FindProduct(key string) (Item, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return Item{}, false
	}
	for _, it := range p.view.Items {
		if it.Available && (strings.ToLower(it.ID) == key || strings.ToLower(it.IikoID) == key) {
			return it, true
		}
	}
	return Item{}, false
}
func (c *Store) FindProduct(key string) (Item, bool) { return c.Pin().FindProduct(key) }

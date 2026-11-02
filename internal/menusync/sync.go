package menusync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"beini-backend/internal/catalog"
)

type Config struct {
	SourceID         string
	OrganizationID   string
	TerminalGroupID  string
	CachePath        string
	MenuInterval     time.Duration
	StopListInterval time.Duration
	RequestTimeout   time.Duration
}

type Service struct {
	mu     sync.Mutex
	cfg    Config
	source Source
	store  *catalog.Store
	seed   catalog.Seed
	digest string
	now    func() time.Time
}

func New(cfg Config, source Source, store *catalog.Store, seed catalog.Seed) (*Service, error) {
	if cfg.OrganizationID == "" || cfg.TerminalGroupID == "" || cfg.CachePath == "" || cfg.MenuInterval <= 0 || cfg.StopListInterval <= 0 || cfg.RequestTimeout <= 0 || source == nil || store == nil {
		return nil, errors.New("incomplete menu sync configuration")
	}
	// Round-trip validates and detaches the caller's mutable slices.
	b, err := json.Marshal(seed)
	if err != nil {
		return nil, err
	}
	reviewed, err := catalog.ReadSeed(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(b)
	if cfg.SourceID != "" {
		hash = sha256.Sum256(append(append(b, 0), []byte(cfg.SourceID)...))
	}
	return &Service{cfg: cfg, source: source, store: store, seed: reviewed, digest: hex.EncodeToString(hash[:]), now: time.Now}, nil
}

// RefreshMenu requires both successful full menu and stop-list responses. Neither
// memory nor cache changes on an API/schema/disk error.
func (s *Service) RefreshMenu(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	attempt := s.now().UTC()
	n, err := s.fetchMenu(ctx)
	if err != nil {
		return s.fail(true, attempt, "menu_fetch_failed")
	}
	prods, err := products(n)
	if err != nil {
		return s.fail(true, attempt, "menu_invalid")
	}
	old := s.store.Snapshot()
	if !n.Unversioned && old.Version > 0 && *n.Revision < old.Revision {
		return s.fail(true, attempt, "menu_revision_regressed")
	}
	menuAt := s.now().UTC()
	stops, err := s.fetchStops(ctx)
	if err != nil {
		s.store.RecordAttempt(false, menuAt, "stop_list_failed")
		return s.fail(true, attempt, "stop_list_failed")
	}
	now := s.now().UTC()
	next := catalog.Snapshot{SchemaVersion: 1, Version: old.Version + 1, Revision: *n.Revision, OrganizationID: s.cfg.OrganizationID, TerminalGroupID: s.cfg.TerminalGroupID, SeedDigest: s.digest, UpdatedAt: now, MenuUpdatedAt: menuAt, StopListUpdatedAt: now, Categories: s.seed.Categories, Items: make([]catalog.Item, 0, len(s.seed.Items))}
	for _, reviewed := range s.seed.Items {
		it := reviewed
		it.Sellable = false
		it.Available = false
		it.UnavailableReason = "not_sellable"
		if p, ok := prods[productKey(it.IikoID)]; ok {
			price, ok := salePrice(p)
			if ok {
				it.Price = price
				it.Sellable = reviewed.Available
				it.Available = it.Sellable && !stops[productKey(it.IikoID)]
			}
		}
		if it.Available {
			it.UnavailableReason = ""
		} else if it.Sellable {
			it.UnavailableReason = "stop_list"
		}
		next.Items = append(next.Items, it)
	}
	if err := ctx.Err(); err != nil {
		return s.fail(true, attempt, "cancelled")
	}
	if err := s.commit(next); err != nil {
		return s.fail(true, attempt, "snapshot_publish_failed")
	}
	s.store.RecordAttempt(true, attempt, "")
	s.store.RecordAttempt(false, menuAt, "")
	return nil
}

func (s *Service) RefreshStopList(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	attempt := s.now().UTC()
	next := s.store.Snapshot()
	if next.Version == 0 {
		return s.fail(false, attempt, "menu_not_initialized")
	}
	stops, err := s.fetchStops(ctx)
	if err != nil {
		return s.fail(false, attempt, "stop_list_failed")
	}
	next.Version++
	next.UpdatedAt = s.now().UTC()
	next.StopListUpdatedAt = next.UpdatedAt
	for i := range next.Items {
		it := &next.Items[i]
		it.Available = it.Sellable && !stops[productKey(it.IikoID)]
		if it.Available {
			it.UnavailableReason = ""
		} else if it.Sellable {
			it.UnavailableReason = "stop_list"
		}
	}
	if err := ctx.Err(); err != nil {
		return s.fail(false, attempt, "cancelled")
	}
	if err := s.commit(next); err != nil {
		return s.fail(false, attempt, "snapshot_publish_failed")
	}
	s.store.RecordAttempt(false, attempt, "")
	return nil
}

func (s *Service) fetchMenu(ctx context.Context) (Nomenclature, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	return s.source.Menu(ctx, s.cfg.OrganizationID)
}
func (s *Service) fetchStops(ctx context.Context) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	resp, err := s.source.StopList(ctx, s.cfg.OrganizationID, s.cfg.TerminalGroupID)
	if err != nil {
		return nil, err
	}
	return stopped(resp, s.cfg.OrganizationID, s.cfg.TerminalGroupID)
}
func (s *Service) fail(menu bool, at time.Time, code string) error {
	s.store.RecordAttempt(menu, at, code)
	return errors.New(code)
}

// Run performs an immediate full refresh, then polls independently until canceled.
// Failed startup is retried on the stop-list tick until a first snapshot exists.
func (s *Service) Run(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	_ = s.RefreshMenu(ctx)
	menu := time.NewTicker(s.cfg.MenuInterval)
	defer menu.Stop()
	stop := time.NewTicker(s.cfg.StopListInterval)
	defer stop.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-menu.C:
			_ = s.RefreshMenu(ctx)
		case <-stop.C:
			if s.store.Snapshot().Version == 0 {
				_ = s.RefreshMenu(ctx)
			} else {
				_ = s.RefreshStopList(ctx)
			}
		}
	}
}

type diskSnapshot struct {
	Snapshot json.RawMessage `json:"snapshot"`
	SHA256   string          `json:"sha256"`
}

func (s *Service) commit(next catalog.Snapshot) error {
	if err := catalog.ValidateSnapshot(next); err != nil {
		return err
	}
	body, err := json.Marshal(next)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(body)
	data, err := json.Marshal(diskSnapshot{Snapshot: body, SHA256: hex.EncodeToString(hash[:])})
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.cfg.CachePath)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".menu-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Same-directory rename keeps the prior successful file on replacement failure.
	if err = os.Rename(temp, s.cfg.CachePath); err != nil {
		return err
	}
	return s.store.Publish(next)
}

// Restore validates checksum, schema, target and reviewed allowlist before publishing.
// On failure the existing in-memory snapshot is preserved; Run can recover via API.
func (s *Service) Restore() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.restore()
	if err != nil {
		s.store.RecordAttempt(true, s.now().UTC(), "snapshot_restore_failed")
	}
	return err
}
func (s *Service) restore() error {
	f, err := os.Open(s.cfg.CachePath)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if err != nil || len(data) > 16<<20 {
		return errors.New("invalid cache size")
	}
	var disk diskSnapshot
	if err = json.Unmarshal(data, &disk); err != nil {
		return errors.New("invalid snapshot file")
	}
	hash := sha256.Sum256(disk.Snapshot)
	if disk.SHA256 != hex.EncodeToString(hash[:]) {
		return errors.New("snapshot checksum mismatch")
	}
	var next catalog.Snapshot
	if err = json.Unmarshal(disk.Snapshot, &next); err != nil {
		return errors.New("invalid snapshot schema")
	}
	if next.OrganizationID != s.cfg.OrganizationID || next.TerminalGroupID != s.cfg.TerminalGroupID || next.SeedDigest != s.digest {
		return errors.New("snapshot target or allowlist mismatch")
	}
	if next.UpdatedAt.After(s.now().UTC()) {
		return errors.New("snapshot timestamp is in the future")
	}
	if next.Version < s.store.Snapshot().Version {
		return errors.New("snapshot version regressed")
	}
	if len(next.Items) != len(s.seed.Items) {
		return errors.New("snapshot allowlist size mismatch")
	}
	for i, it := range next.Items {
		reviewed := s.seed.Items[i]
		if it.ID != reviewed.ID || it.IikoID != reviewed.IikoID || (it.Sellable && !reviewed.Available) {
			return errors.New("snapshot allowlist mismatch")
		}
	}
	return s.store.Publish(next)
}

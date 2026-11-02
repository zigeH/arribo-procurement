// Package store defines persistence. The interface is stdlib-only; the
// in-memory implementation backs tests and the no-database local demo, and
// internal/store/postgres is the production implementation.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/zigeH/arribo-procurement/internal/domain"
	"github.com/zigeH/arribo-procurement/internal/scoring"
)

var ErrNotFound = errors.New("not found")

// Store is everything the API and worker need.
type Store interface {
	ListSuppliers(ctx context.Context) ([]domain.Supplier, error)
	GetSupplier(ctx context.Context, id string) (domain.Supplier, error)
	// LoadHistory returns all POs (with lines and receipts) ordered on or after since.
	LoadHistory(ctx context.Context, since time.Time) ([]domain.PurchaseOrder, error)
	GetPO(ctx context.Context, id string) (domain.PurchaseOrder, error)
	SaveScorecard(ctx context.Context, sc scoring.Scorecard) error
	LatestScorecard(ctx context.Context, supplierID string) (scoring.Scorecard, error)
}

// Memory is a thread-safe in-memory Store.
type Memory struct {
	mu         sync.RWMutex
	suppliers  []domain.Supplier
	pos        []domain.PurchaseOrder
	scorecards map[string][]scoring.Scorecard
}

func NewMemory(ds domain.Dataset) *Memory {
	return &Memory{suppliers: ds.Suppliers, pos: ds.POs, scorecards: map[string][]scoring.Scorecard{}}
}

// LoadDatasetFile reads a domain.Dataset JSON file.
func LoadDatasetFile(path string) (domain.Dataset, error) {
	var ds domain.Dataset
	b, err := os.ReadFile(path)
	if err != nil {
		return ds, err
	}
	err = json.Unmarshal(b, &ds)
	return ds, err
}

func (m *Memory) ListSuppliers(context.Context) ([]domain.Supplier, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := append([]domain.Supplier(nil), m.suppliers...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) GetSupplier(_ context.Context, id string) (domain.Supplier, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.suppliers {
		if s.ID == id {
			return s, nil
		}
	}
	return domain.Supplier{}, ErrNotFound
}

func (m *Memory) LoadHistory(_ context.Context, since time.Time) ([]domain.PurchaseOrder, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.PurchaseOrder
	for _, p := range m.pos {
		if !p.OrderDate.Before(since) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *Memory) GetPO(_ context.Context, id string) (domain.PurchaseOrder, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.pos {
		if p.ID == id || p.Number == id {
			return p, nil
		}
	}
	return domain.PurchaseOrder{}, ErrNotFound
}

func (m *Memory) SaveScorecard(_ context.Context, sc scoring.Scorecard) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scorecards[sc.SupplierID] = append(m.scorecards[sc.SupplierID], sc)
	return nil
}

func (m *Memory) LatestScorecard(_ context.Context, supplierID string) (scoring.Scorecard, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h := m.scorecards[supplierID]
	if len(h) == 0 {
		return scoring.Scorecard{}, ErrNotFound
	}
	return h[len(h)-1], nil
}

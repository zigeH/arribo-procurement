package erp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/zigeH/arribo-procurement/internal/domain"
)

// odataPage is a Service Layer collection response (v1 and v2 next-link keys).
type odataPage[T any] struct {
	Value     []T    `json:"value"`
	NextLink  string `json:"odata.nextLink"`
	NextLink4 string `json:"@odata.nextLink"`
}

func (p odataPage[T]) next() string {
	if p.NextLink != "" {
		return p.NextLink
	}
	return p.NextLink4
}

// FixtureReader reads B1-shaped JSON exports from a directory:
// BusinessPartners.json, PurchaseOrders.json, PurchaseDeliveryNotes.json,
// each a Service Layer collection body ({"value": [...]}).
type FixtureReader struct{ Dir string }

func readPage[T any](path string) ([]T, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p odataPage[T]
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return p.Value, nil
}

func (f FixtureReader) Suppliers(context.Context) ([]Supplier, error) {
	bps, err := readPage[B1BusinessPartner](filepath.Join(f.Dir, "BusinessPartners.json"))
	if err != nil {
		return nil, err
	}
	return MapB1Suppliers(bps), nil
}

func (f FixtureReader) PurchaseOrders(_ context.Context, since time.Time) ([]domain.PurchaseOrder, error) {
	pos, err := readPage[B1Document](filepath.Join(f.Dir, "PurchaseOrders.json"))
	if err != nil {
		return nil, err
	}
	grs, err := readPage[B1Document](filepath.Join(f.Dir, "PurchaseDeliveryNotes.json"))
	if err != nil {
		return nil, err
	}
	all, err := MapB1(pos, grs)
	if err != nil {
		return nil, err
	}
	var out []domain.PurchaseOrder
	for _, p := range all {
		if !p.OrderDate.Before(since) {
			out = append(out, p)
		}
	}
	return out, nil
}

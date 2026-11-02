// Package erp reads purchasing data from a customer's ERP, read-only.
//
// The Reader interface is ERP-agnostic and returns normalized domain types.
// The first adapter is shaped like SAP Business One's Service Layer (OData):
// MLOptic running SAP B1 is an unconfirmed inference from a public implementer
// case page, so nothing outside this package may depend on B1 specifics.
//
// Stdlib only.
package erp

import (
	"context"
	"time"

	"github.com/zigeH/arribo-procurement/internal/domain"
)

// Supplier is a vendor as the ERP knows it, plus contact info used to
// recognize its emails.
type Supplier struct {
	domain.Supplier
	Emails []string // contact addresses on file (domains are used for matching)
}

// Reader reads purchasing data. Implementations must be read-only.
type Reader interface {
	// Suppliers returns vendors with contact emails.
	Suppliers(ctx context.Context) ([]Supplier, error)
	// PurchaseOrders returns POs dated on/after since, with lines and the
	// goods receipts posted against them merged in as line receipts.
	PurchaseOrders(ctx context.Context, since time.Time) ([]domain.PurchaseOrder, error)
}

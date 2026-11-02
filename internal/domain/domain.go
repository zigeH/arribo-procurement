// Package domain holds the core procurement entities. It has no external
// dependencies so every other package (scoring, store, workflows) can share it.
package domain

import "time"

// Supplier is a vendor the customer buys from. Names in non-production data
// are aliases (see docs/data-spec.md).
type Supplier struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Country string `json:"country,omitempty"`
}

// PurchaseOrder is one PO issued by the customer to a single supplier.
type PurchaseOrder struct {
	ID             string     `json:"id"`
	Number         string     `json:"number"`
	SupplierID     string     `json:"supplier_id"`
	OrderDate      time.Time  `json:"order_date"`
	SentAt         time.Time  `json:"sent_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	PaymentTerms   string     `json:"payment_terms,omitempty"` // e.g. "Net 30", "30% deposit / 70% before shipment"
	Incoterm       string     `json:"incoterm,omitempty"`      // e.g. "FOB", "EXW", "DDP"
	Currency       string     `json:"currency"`                // ISO 4217, e.g. "USD"
	Lines          []POLine   `json:"lines"`
}

// POLine is one item on a PO.
type POLine struct {
	ID           string    `json:"id"`
	LineNo       int       `json:"line_no"`
	PartNumber   string    `json:"part_number"`
	Category     string    `json:"category"` // e.g. "lens", "prism", "filter", "mount"
	Description  string    `json:"description,omitempty"`
	Qty          float64   `json:"qty"`
	UnitPrice    float64   `json:"unit_price"`
	PromisedDate time.Time `json:"promised_date"` // latest supplier-confirmed delivery date
	// OriginalPromisedDate is the first date the supplier confirmed, if the
	// promise was later moved. Nil means it never changed.
	OriginalPromisedDate *time.Time `json:"original_promised_date,omitempty"`
	Receipts             []Receipt  `json:"receipts,omitempty"`
}

// CommitDate is the first supplier-confirmed date: what on-time delivery and
// quoted lead time are measured against, so moving a date doesn't hide a slip.
func (l POLine) CommitDate() time.Time {
	if l.OriginalPromisedDate != nil {
		return *l.OriginalPromisedDate
	}
	return l.PromisedDate
}

// Receipt is a (partial) delivery against a PO line, including inspection result.
type Receipt struct {
	Date        time.Time `json:"date"`
	QtyReceived float64   `json:"qty_received"`
	QtyRejected float64   `json:"qty_rejected"`
}

// Dataset is the interchange format for sanitized customer exports and
// synthetic data (evals/data/*.json).
type Dataset struct {
	Customer  string          `json:"customer"`
	Generated time.Time       `json:"generated"`
	Note      string          `json:"note,omitempty"`
	Suppliers []Supplier      `json:"suppliers"`
	POs       []PurchaseOrder `json:"purchase_orders"`
}

// ReceivedQty returns total received quantity on the line.
func (l POLine) ReceivedQty() float64 {
	var q float64
	for _, r := range l.Receipts {
		q += r.QtyReceived
	}
	return q
}

// RejectedQty returns total rejected quantity on the line.
func (l POLine) RejectedQty() float64 {
	var q float64
	for _, r := range l.Receipts {
		q += r.QtyRejected
	}
	return q
}

// CompletedAt returns the date the full ordered quantity had arrived, if it
// has. Rejected units still count as delivered here: delivery timeliness and
// quality are scored separately, and replacement shipments for rejects are
// not modeled yet.
func (l POLine) CompletedAt() (time.Time, bool) {
	var cum float64
	for _, r := range l.Receipts {
		cum += r.QtyReceived
		if cum >= l.Qty {
			return r.Date, true
		}
	}
	return time.Time{}, false
}

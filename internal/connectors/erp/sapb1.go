package erp

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zigeH/arribo-procurement/internal/domain"
)

// SAP Business One Service Layer document shapes (subset we read).
// Field names follow the Service Layer JSON (OData) representation.

// B1BusinessPartner is a /BusinessPartners entity (CardType "cSupplier").
type B1BusinessPartner struct {
	CardCode     string `json:"CardCode"`
	CardName     string `json:"CardName"`
	CardType     string `json:"CardType"`
	EmailAddress string `json:"EmailAddress"`
	Country      string `json:"Country,omitempty"`
}

// B1DocLine is a DocumentLines entry.
type B1DocLine struct {
	LineNum         int     `json:"LineNum"`
	ItemCode        string  `json:"ItemCode"`
	ItemDescription string  `json:"ItemDescription"`
	Quantity        float64 `json:"Quantity"`
	Price           float64 `json:"Price"`
	Currency        string  `json:"Currency,omitempty"`
	ShipDate        string  `json:"ShipDate,omitempty"` // row delivery date
	BaseType        int     `json:"BaseType,omitempty"` // 22 = purchase order
	BaseEntry       int     `json:"BaseEntry,omitempty"`
	BaseLine        int     `json:"BaseLine"`
	ItemGroup       string  `json:"U_ItemGroup,omitempty"` // UDF: category (assumption)
}

// B1Document is a /PurchaseOrders or /PurchaseDeliveryNotes (goods receipt PO) entity.
type B1Document struct {
	DocEntry      int         `json:"DocEntry"`
	DocNum        int         `json:"DocNum"`
	DocDate       string      `json:"DocDate"`
	DocDueDate    string      `json:"DocDueDate,omitempty"`
	CardCode      string      `json:"CardCode"`
	CardName      string      `json:"CardName,omitempty"`
	DocCurrency   string      `json:"DocCurrency"`
	NumAtCard     string      `json:"NumAtCard,omitempty"`  // supplier reference
	PaymentTerms  string      `json:"U_PayTerms,omitempty"` // UDF (assumption)
	DocumentLines []B1DocLine `json:"DocumentLines"`
}

const b1BaseTypePO = 22

func parseB1Date(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{"2006-01-02", "2006-01-02T15:04:05Z", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad B1 date %q", s)
}

// POID is the normalized id for a B1 purchase order.
func POID(docEntry int) string { return "b1-po-" + strconv.Itoa(docEntry) }

// MapB1 converts B1 documents into normalized POs. Goods receipts are merged
// into the PO lines they reference (BaseType 22, BaseEntry/BaseLine).
func MapB1(pos, grpos []B1Document) ([]domain.PurchaseOrder, error) {
	out := make([]domain.PurchaseOrder, 0, len(pos))
	idx := map[int]int{}
	for _, d := range pos {
		od, err := parseB1Date(d.DocDate)
		if err != nil {
			return nil, err
		}
		po := domain.PurchaseOrder{
			ID:           POID(d.DocEntry),
			Number:       strconv.Itoa(d.DocNum),
			SupplierID:   d.CardCode,
			OrderDate:    od,
			SentAt:       od,
			Currency:     strings.ToUpper(d.DocCurrency),
			PaymentTerms: d.PaymentTerms,
		}
		for _, l := range d.DocumentLines {
			sd, err := parseB1Date(l.ShipDate)
			if err != nil {
				return nil, err
			}
			po.Lines = append(po.Lines, domain.POLine{
				ID:           fmt.Sprintf("%s-%d", po.ID, l.LineNum),
				LineNo:       l.LineNum + 1, // B1 LineNum is 0-based; buyers say "line 1"
				PartNumber:   l.ItemCode,
				Category:     l.ItemGroup,
				Description:  l.ItemDescription,
				Qty:          l.Quantity,
				UnitPrice:    l.Price,
				PromisedDate: sd,
			})
		}
		idx[d.DocEntry] = len(out)
		out = append(out, po)
	}
	for _, g := range grpos {
		gd, err := parseB1Date(g.DocDate)
		if err != nil {
			return nil, err
		}
		for _, gl := range g.DocumentLines {
			if gl.BaseType != b1BaseTypePO {
				continue
			}
			pi, ok := idx[gl.BaseEntry]
			if !ok {
				continue // receipt against a PO outside the window
			}
			for li := range out[pi].Lines {
				if out[pi].Lines[li].LineNo == gl.BaseLine+1 {
					out[pi].Lines[li].Receipts = append(out[pi].Lines[li].Receipts, domain.Receipt{Date: gd, QtyReceived: gl.Quantity})
				}
			}
		}
	}
	for i := range out {
		for li := range out[i].Lines {
			r := out[i].Lines[li].Receipts
			sort.Slice(r, func(a, b int) bool { return r[a].Date.Before(r[b].Date) })
		}
	}
	return out, nil
}

// MapB1Suppliers converts business partners of type supplier.
func MapB1Suppliers(bps []B1BusinessPartner) []Supplier {
	var out []Supplier
	for _, b := range bps {
		if b.CardType != "" && b.CardType != "cSupplier" {
			continue
		}
		s := Supplier{Supplier: domain.Supplier{ID: b.CardCode, Name: b.CardName, Country: b.Country}}
		for _, e := range strings.FieldsFunc(b.EmailAddress, func(r rune) bool { return r == ';' || r == ',' || r == ' ' }) {
			s.Emails = append(s.Emails, strings.ToLower(e))
		}
		out = append(out, s)
	}
	return out
}

// Package timeline merges ERP facts and supplier messages into one timeline
// per PO, and detects where they disagree (the daily "对不上清单").
//
// Every entry and discrepancy cites its sources (ERP document or message id).
// Everything is computed "as of" a time: nothing after it is visible.
//
// Stdlib only.
package timeline

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/zigeH/arribo-procurement/internal/domain"
	"github.com/zigeH/arribo-procurement/internal/inbox"
	"github.com/zigeH/arribo-procurement/internal/match"
)

// Item is a supplier message event resolved to the ERP.
type Item struct {
	MessageID string      `json:"message_id"`
	Date      time.Time   `json:"date"`
	From      string      `json:"from"`
	Subject   string      `json:"subject"`
	Match     match.Match `json:"match"`
}

// Source cites where a fact came from.
type Source struct {
	Kind  string `json:"kind"` // erp | email
	Ref   string `json:"ref"`  // ERP document id or message id
	Quote string `json:"quote,omitempty"`
}

// Entry is one event on a PO timeline.
type Entry struct {
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"` // po_created | goods_receipt | supplier_ack | supplier_date_change | ...
	LineID     string    `json:"line_id,omitempty"`
	Summary    string    `json:"summary"`
	Source     Source    `json:"source"`
	Confidence float64   `json:"confidence,omitempty"`
}

// LineState is the merged view of one PO line.
type LineState struct {
	LineID        string     `json:"line_id"`
	LineNo        int        `json:"line_no"`
	PartNumber    string     `json:"part_number"`
	Qty           float64    `json:"qty"`
	Received      float64    `json:"received"`
	ERPPromised   time.Time  `json:"erp_promised"`
	SupplierDate  *time.Time `json:"supplier_date,omitempty"`
	ERPPrice      float64    `json:"erp_price"`
	SupplierPrice *float64   `json:"supplier_price,omitempty"`
	Status        string     `json:"status"` // open | partial | received | overdue
}

// Timeline is the merged view of one PO.
type Timeline struct {
	POID       string      `json:"po_id"`
	PONumber   string      `json:"po_number"`
	SupplierID string      `json:"supplier_id"`
	OrderDate  time.Time   `json:"order_date"`
	Currency   string      `json:"currency"`
	AsOf       time.Time   `json:"as_of"`
	Lines      []LineState `json:"lines"`
	Entries    []Entry     `json:"entries"`
}

// Discrepancy is something the ERP and the supplier disagree on, or a gap
// nobody has followed up on.
type Discrepancy struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`     // date_mismatch | price_mismatch | shipped_not_received | overdue_no_update | unacknowledged | quality_issue
	Severity      string    `json:"severity"` // alert | warn | info
	POID          string    `json:"po_id"`
	PONumber      string    `json:"po_number"`
	LineID        string    `json:"line_id,omitempty"`
	PartNumber    string    `json:"part_number,omitempty"`
	SupplierID    string    `json:"supplier_id"`
	Message       string    `json:"message"`
	ERPValue      string    `json:"erp_value,omitempty"`
	SupplierValue string    `json:"supplier_value,omitempty"`
	Since         time.Time `json:"since"`
	Sources       []Source  `json:"sources"`
}

// Config for discrepancy detection.
type Config struct {
	DateToleranceDays  int // supplier vs ERP date difference to ignore (default 0: any calendar day differs)
	AlertSlipDays      int // supplier date later than ERP by more than this → alert (default 2)
	PriceTolerance     float64
	ShipToReceiveDays  int // shipped per supplier but not received after this many days
	OverdueGraceDays   int
	AckDays            int // PO with no supplier response after this many days
	AckLookbackDays    int // only check POs this recent
	QualityVisibleDays int
}

func DefaultConfig() Config {
	return Config{AlertSlipDays: 2, PriceTolerance: 0.005, ShipToReceiveDays: 7, OverdueGraceDays: 2, AckDays: 3, AckLookbackDays: 30, QualityVisibleDays: 14}
}

func day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func received(l domain.POLine, asOf time.Time) float64 {
	var q float64
	for _, r := range l.Receipts {
		if !r.Date.After(asOf) {
			q += r.QtyReceived
		}
	}
	return q
}

func visible(items []Item, poID string, asOf time.Time) []Item {
	var out []Item
	for _, it := range items {
		if it.Match.POID == poID && !it.Date.After(asOf) && !it.Match.NeedsReview() {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

func src(it Item) Source {
	return Source{Kind: "email", Ref: it.MessageID, Quote: it.Match.Event.Quote}
}

// Build returns the timeline of one PO as of asOf.
func Build(po domain.PurchaseOrder, items []Item, asOf time.Time) Timeline {
	tl := Timeline{POID: po.ID, PONumber: po.Number, SupplierID: po.SupplierID, OrderDate: po.OrderDate, Currency: po.Currency, AsOf: asOf}
	tl.Entries = append(tl.Entries, Entry{At: po.OrderDate, Kind: "po_created", Summary: fmt.Sprintf("PO %s issued (%d lines)", po.Number, len(po.Lines)), Source: Source{Kind: "erp", Ref: po.ID}})
	its := visible(items, po.ID, asOf)
	for _, l := range po.Lines {
		ls := LineState{LineID: l.ID, LineNo: l.LineNo, PartNumber: l.PartNumber, Qty: l.Qty, ERPPromised: l.PromisedDate, ERPPrice: l.UnitPrice, Received: received(l, asOf)}
		for _, r := range l.Receipts {
			if r.Date.After(asOf) {
				continue
			}
			tl.Entries = append(tl.Entries, Entry{At: r.Date, Kind: "goods_receipt", LineID: l.ID, Summary: fmt.Sprintf("line %d: received %.0f of %.0f", l.LineNo, r.QtyReceived, l.Qty), Source: Source{Kind: "erp", Ref: "goods receipt " + r.Date.Format("2006-01-02")}})
		}
		for _, it := range its {
			if it.Match.LineID != l.ID {
				continue
			}
			e := it.Match.Event
			switch {
			case e.Date != nil && e.Type == inbox.DateChange:
				d := *e.Date
				ls.SupplierDate = &d
			case e.Price != nil && e.Type == inbox.PriceChange:
				p := *e.Price
				ls.SupplierPrice = &p
			}
		}
		switch {
		case ls.Received >= l.Qty:
			ls.Status = "received"
		case asOf.After(effectiveDue(ls).AddDate(0, 0, 2)):
			ls.Status = "overdue"
		case ls.Received > 0:
			ls.Status = "partial"
		default:
			ls.Status = "open"
		}
		tl.Lines = append(tl.Lines, ls)
	}
	for _, it := range its {
		e := it.Match.Event
		en := Entry{At: it.Date, LineID: it.Match.LineID, Source: src(it), Confidence: it.Match.Confidence}
		switch e.Type {
		case inbox.Ack:
			en.Kind, en.Summary = "supplier_ack", "supplier confirmed the PO"
		case inbox.DateChange:
			en.Kind, en.Summary = "supplier_date_change", "supplier gives new date "+fmtDate(e.Date)
		case inbox.PartialShip:
			en.Kind, en.Summary = "supplier_partial_ship", fmt.Sprintf("supplier shipped %s", fmtQty(e.Qty))
		case inbox.PriceChange:
			en.Kind, en.Summary = "supplier_price_change", fmt.Sprintf("supplier changes unit price to %s %s", fmtQty(e.Price), e.Currency)
		case inbox.QualityIssue:
			en.Kind, en.Summary = "supplier_quality_issue", fmt.Sprintf("supplier confirms %s rejected", fmtQty(e.Qty))
		default:
			continue
		}
		tl.Entries = append(tl.Entries, en)
	}
	sort.SliceStable(tl.Entries, func(i, j int) bool { return tl.Entries[i].At.Before(tl.Entries[j].At) })
	return tl
}

func effectiveDue(ls LineState) time.Time {
	if ls.SupplierDate != nil && ls.SupplierDate.After(ls.ERPPromised) {
		return *ls.SupplierDate
	}
	return ls.ERPPromised
}

func fmtDate(t *time.Time) string {
	if t == nil {
		return "?"
	}
	return t.Format("2006-01-02")
}

func fmtQty(f *float64) string {
	if f == nil {
		return "?"
	}
	if f2 := math.Round(*f); f2 == *f {
		return fmt.Sprintf("%.0f", *f)
	}
	return fmt.Sprintf("%.2f", *f)
}

// Detect returns the discrepancy list as of asOf, most severe first.
func Detect(pos []domain.PurchaseOrder, items []Item, asOf time.Time, cfg Config) []Discrepancy {
	var out []Discrepancy
	for _, po := range pos {
		if po.OrderDate.After(asOf) {
			continue
		}
		its := visible(items, po.ID, asOf)
		anyReply := len(its) > 0
		anyReceipt := false
		for _, l := range po.Lines {
			if received(l, asOf) > 0 {
				anyReceipt = true
			}
		}
		base := Discrepancy{POID: po.ID, PONumber: po.Number, SupplierID: po.SupplierID}

		if !anyReply && !anyReceipt && asOf.Sub(po.OrderDate) > time.Duration(cfg.AckDays)*24*time.Hour && asOf.Sub(po.OrderDate) <= time.Duration(cfg.AckLookbackDays)*24*time.Hour {
			d := base
			d.Kind, d.Severity, d.ID = "unacknowledged", "warn", "unacknowledged:"+po.ID
			d.Message = fmt.Sprintf("no supplier confirmation %d days after PO was issued", int(asOf.Sub(po.OrderDate).Hours()/24))
			d.Since = po.OrderDate.AddDate(0, 0, cfg.AckDays)
			d.Sources = []Source{{Kind: "erp", Ref: po.ID}}
			out = append(out, d)
		}

		for _, l := range po.Lines {
			got := received(l, asOf)
			open := got < l.Qty
			lb := base
			lb.LineID, lb.PartNumber = l.ID, l.PartNumber
			var dates, prices, ships, quals []Item
			for _, it := range its {
				if it.Match.LineID != l.ID {
					continue
				}
				switch it.Match.Event.Type {
				case inbox.DateChange:
					if it.Match.Event.Date != nil {
						dates = append(dates, it)
					}
				case inbox.PriceChange:
					if it.Match.Event.Price != nil {
						prices = append(prices, it)
					}
				case inbox.PartialShip:
					ships = append(ships, it)
				case inbox.QualityIssue:
					quals = append(quals, it)
				}
			}

			if open && len(dates) > 0 {
				last := dates[len(dates)-1]
				sd := day(*last.Match.Event.Date)
				ed := day(l.PromisedDate)
				diff := int(sd.Sub(ed).Hours() / 24)
				if abs(diff) > cfg.DateToleranceDays {
					d := lb
					d.Kind, d.ID = "date_mismatch", "date_mismatch:"+l.ID
					d.Severity = "warn"
					if diff > cfg.AlertSlipDays {
						d.Severity = "alert"
					}
					d.ERPValue, d.SupplierValue = ed.Format("2006-01-02"), sd.Format("2006-01-02")
					d.Message = fmt.Sprintf("supplier says %s, ERP still has %s (%+d days)", d.SupplierValue, d.ERPValue, diff)
					if len(dates) > 1 {
						d.Message += fmt.Sprintf("; supplier changed the date %d times", len(dates))
					}
					d.Since = last.Date
					for _, it := range dates {
						d.Sources = append(d.Sources, src(it))
					}
					d.Sources = append(d.Sources, Source{Kind: "erp", Ref: po.ID})
					out = append(out, d)
				}
			}

			if len(prices) > 0 {
				last := prices[len(prices)-1]
				p := *last.Match.Event.Price
				if l.UnitPrice > 0 && math.Abs(p-l.UnitPrice)/l.UnitPrice > cfg.PriceTolerance {
					d := lb
					d.Kind, d.Severity, d.ID = "price_mismatch", "warn", "price_mismatch:"+l.ID
					d.ERPValue, d.SupplierValue = fmt.Sprintf("%.2f", l.UnitPrice), fmt.Sprintf("%.2f", p)
					d.Message = fmt.Sprintf("supplier price %.2f vs ERP %.2f %s (%+.1f%%); invoice will not match", p, l.UnitPrice, po.Currency, (p/l.UnitPrice-1)*100)
					d.Since = last.Date
					d.Sources = []Source{src(last), {Kind: "erp", Ref: po.ID}}
					out = append(out, d)
				}
			}

			if open && len(ships) > 0 {
				var shipped float64
				for _, it := range ships {
					if it.Match.Event.Qty != nil {
						shipped += *it.Match.Event.Qty
					}
				}
				last := ships[len(ships)-1]
				if shipped > got && asOf.Sub(last.Date) > time.Duration(cfg.ShipToReceiveDays)*24*time.Hour {
					d := lb
					d.Kind, d.Severity, d.ID = "shipped_not_received", "warn", "shipped_not_received:"+l.ID
					d.ERPValue, d.SupplierValue = fmt.Sprintf("%.0f received", got), fmt.Sprintf("%.0f shipped", shipped)
					d.Message = fmt.Sprintf("supplier shipped %.0f on %s but ERP shows %.0f received", shipped, last.Date.Format("2006-01-02"), got)
					d.Since = last.Date.AddDate(0, 0, cfg.ShipToReceiveDays)
					d.Sources = []Source{src(last), {Kind: "erp", Ref: po.ID}}
					out = append(out, d)
				}
			}

			if open && asOf.After(day(l.PromisedDate).AddDate(0, 0, cfg.OverdueGraceDays+1)) {
				latestSupplier := time.Time{}
				if len(dates) > 0 {
					latestSupplier = day(*dates[len(dates)-1].Match.Event.Date)
				}
				if !latestSupplier.After(asOf) {
					d := lb
					d.Kind, d.Severity, d.ID = "overdue_no_update", "alert", "overdue_no_update:"+l.ID
					d.ERPValue = day(l.PromisedDate).Format("2006-01-02")
					d.Message = fmt.Sprintf("%.0f of %.0f still open, %d days past the ERP date, no newer date from the supplier", l.Qty-got, l.Qty, int(asOf.Sub(day(l.PromisedDate)).Hours()/24))
					d.Since = day(l.PromisedDate).AddDate(0, 0, cfg.OverdueGraceDays+1)
					d.Sources = []Source{{Kind: "erp", Ref: po.ID}}
					out = append(out, d)
				}
			}

			for _, it := range quals {
				if asOf.Sub(it.Date) <= time.Duration(cfg.QualityVisibleDays)*24*time.Hour {
					d := lb
					d.Kind, d.Severity, d.ID = "quality_issue", "info", "quality_issue:"+l.ID+":"+it.MessageID
					d.SupplierValue = fmtQty(it.Match.Event.Qty) + " rejected"
					d.Message = "supplier confirmed " + d.SupplierValue + "; check replacement is scheduled"
					d.Since = it.Date
					d.Sources = []Source{src(it)}
					out = append(out, d)
				}
			}
		}
	}
	rank := map[string]int{"alert": 0, "warn": 1, "info": 2}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Severity] != rank[out[j].Severity] {
			return rank[out[i].Severity] < rank[out[j].Severity]
		}
		return out[i].Since.Before(out[j].Since)
	})
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

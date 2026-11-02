package timeline

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/zigeH/arribo-procurement/internal/connectors/erp"
	"github.com/zigeH/arribo-procurement/internal/domain"
	"github.com/zigeH/arribo-procurement/internal/extract"
	"github.com/zigeH/arribo-procurement/internal/inbox"
	"github.com/zigeH/arribo-procurement/internal/match"
)

// ErrNotFound is returned for unknown POs.
var ErrNotFound = errors.New("not found")

// Hub is the PO data hub: ERP + supplier messages, resolved and merged.
type Hub struct {
	Extractor string
	Suppliers []erp.Supplier
	POs       []domain.PurchaseOrder
	Items     []Item // resolved, confident
	Review    []Item // need a human
	Ignored   int    // messages not about an order
	Cfg       Config
	byID      map[string]int
}

// Load reads the ERP and processes messages.
func Load(ctx context.Context, r erp.Reader, msgs []inbox.Message, ex extract.Extractor) (*Hub, error) {
	sups, err := r.Suppliers(ctx)
	if err != nil {
		return nil, err
	}
	pos, err := r.PurchaseOrders(ctx, time.Time{})
	if err != nil {
		return nil, err
	}
	h := &Hub{Extractor: ex.Name(), Suppliers: sups, POs: pos, Cfg: DefaultConfig(), byID: map[string]int{}}
	for i, p := range pos {
		h.byID[p.ID] = i
	}
	m := match.New(sups, pos)
	for _, msg := range msgs {
		evs, err := ex.Extract(ctx, msg)
		if err != nil {
			// One bad message must not stop the hub; it goes to review.
			h.Review = append(h.Review, Item{MessageID: msg.ID, Date: msg.Date, From: msg.From, Subject: msg.Subject,
				Match: match.Match{Event: extract.Event{MessageID: msg.ID}, Reasons: []string{"extraction failed: " + err.Error()}}})
			continue
		}
		for _, e := range evs {
			e.MessageID = msg.ID
			mt := m.Resolve(msg, e)
			it := Item{MessageID: msg.ID, Date: msg.Date, From: msg.From, Subject: msg.Subject, Match: mt}
			switch {
			case e.Type == inbox.Other:
				h.Ignored++
			case mt.NeedsReview():
				h.Review = append(h.Review, it)
			default:
				h.Items = append(h.Items, it)
			}
		}
	}
	return h, nil
}

// Timeline of one PO (by id or number).
func (h *Hub) Timeline(id string, asOf time.Time) (Timeline, error) {
	i, ok := h.byID[id]
	if !ok {
		for j, p := range h.POs {
			if p.Number == id {
				i, ok = j, true
			}
		}
	}
	if !ok || h.POs[i].OrderDate.After(asOf) {
		return Timeline{}, ErrNotFound // a PO issued after asOf did not exist yet
	}
	return Build(h.POs[i], h.Items, asOf, h.Cfg), nil
}

// Discrepancies as of asOf.
func (h *Hub) Discrepancies(asOf time.Time) []Discrepancy {
	return Detect(h.POs, h.Items, asOf, h.Cfg)
}

// ReviewQueue: items received by asOf that need a human.
func (h *Hub) ReviewQueue(asOf time.Time) []Item {
	var out []Item
	for _, it := range h.Review {
		if !it.Date.After(asOf) {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	return out
}

// EnrichedDataset is the ERP data with supplier facts folded in, for scoring:
// a supplier date change sets OriginalPromisedDate (ERP date) and PromisedDate
// (latest supplier date); the first acknowledgment sets AcknowledgedAt.
// Only the last date change per line is kept, so an as-of replay between an
// earlier and the last change shows no move at all: it under-reports, it
// never shows a move before it happened.
func (h *Hub) EnrichedDataset(customer string, generated time.Time) domain.Dataset {
	ds := domain.Dataset{Customer: customer, Generated: generated, Note: "ERP data enriched with supplier messages by the PO data hub"}
	for _, s := range h.Suppliers {
		ds.Suppliers = append(ds.Suppliers, s.Supplier)
	}
	ack := map[string]time.Time{}
	lastDate := map[string]time.Time{}
	changedAt := map[string]time.Time{}
	items := append([]Item(nil), h.Items...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Date.Before(items[j].Date) })
	for _, it := range items {
		e := it.Match.Event
		switch e.Type {
		case inbox.Ack:
			if _, ok := ack[it.Match.POID]; !ok {
				ack[it.Match.POID] = it.Date
			}
		case inbox.DateChange:
			if e.Date != nil && it.Match.LineID != "" {
				lastDate[it.Match.LineID] = *e.Date
				changedAt[it.Match.LineID] = it.Date
			}
		}
	}
	for _, p := range h.POs {
		np := p
		if t, ok := ack[p.ID]; ok {
			tt := t
			np.AcknowledgedAt = &tt
		}
		np.Lines = append([]domain.POLine(nil), p.Lines...)
		for i, l := range np.Lines {
			if d, ok := lastDate[l.ID]; ok && !d.Equal(l.PromisedDate) {
				orig := l.PromisedDate
				if l.OriginalPromisedDate != nil {
					orig = *l.OriginalPromisedDate
				}
				np.Lines[i].OriginalPromisedDate = &orig
				np.Lines[i].PromisedDate = d
				ca := changedAt[l.ID] // when the supplier told us: keeps as-of replay honest
				np.Lines[i].PromiseChangedAt = &ca
			}
		}
		ds.POs = append(ds.POs, np)
	}
	return ds
}

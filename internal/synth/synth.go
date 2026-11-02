// Package synth generates a deterministic, MLOptic-shaped synthetic dataset
// (precision optics: lenses, prisms, filters, mounts, coatings) so we can
// develop and evaluate before real sanitized data arrives.
//
// Each supplier has a known "persona"; evals assert the scorer recovers it.
package synth

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/zigeH/arribo-procurement/internal/domain"
)

// Persona is the ground truth behavior of a synthetic supplier.
type Persona struct {
	ID          string
	Name        string
	Country     string
	Key         string   // stable persona key used by evals
	Categories  []string // what they make
	PriceFactor float64  // vs part base price
	OnTimeRate  float64
	LateDaysMax int
	LeadFactor  float64 // quoted lead time vs part base
	RejectRate  float64
	AckHours    float64 // typical acknowledgment time
	NoAckRate   float64
	POsPerMonth float64
	Terms       string
	Incoterm    string
}

// Personas are the suppliers in the synthetic dataset.
var Personas = []Persona{
	{ID: "SUP-01", Name: "Supplier Alpha (alias)", Country: "CN", Key: "reliable_premium", Categories: []string{"lens", "prism", "filter"}, PriceFactor: 1.08, OnTimeRate: 0.96, LateDaysMax: 4, LeadFactor: 1.0, RejectRate: 0.004, AckHours: 8, POsPerMonth: 2.0, Terms: "Net 30", Incoterm: "FOB"},
	{ID: "SUP-02", Name: "Supplier Bravo (alias)", Country: "CN", Key: "cheap_late", Categories: []string{"lens", "mount"}, PriceFactor: 0.88, OnTimeRate: 0.50, LateDaysMax: 25, LeadFactor: 0.95, RejectRate: 0.02, AckHours: 40, POsPerMonth: 1.8, Terms: "30% deposit / 70% before shipment", Incoterm: "EXW"},
	{ID: "SUP-03", Name: "Supplier Charlie (alias)", Country: "US", Key: "balanced", Categories: []string{"lens", "prism", "mount", "coating"}, PriceFactor: 1.00, OnTimeRate: 0.88, LateDaysMax: 6, LeadFactor: 1.0, RejectRate: 0.008, AckHours: 18, POsPerMonth: 2.2, Terms: "Net 45", Incoterm: "DDP"},
	{ID: "SUP-04", Name: "Supplier Delta (alias)", Country: "CN", Key: "quality_problem", Categories: []string{"filter", "coating", "lens"}, PriceFactor: 0.95, OnTimeRate: 0.82, LateDaysMax: 8, LeadFactor: 1.05, RejectRate: 0.065, AckHours: 26, POsPerMonth: 1.5, Terms: "Net 30", Incoterm: "FOB"},
	{ID: "SUP-05", Name: "Supplier Echo (alias)", Country: "TW", Key: "slow_responder", Categories: []string{"prism", "filter"}, PriceFactor: 1.02, OnTimeRate: 0.78, LateDaysMax: 10, LeadFactor: 1.45, RejectRate: 0.01, AckHours: 130, NoAckRate: 0.15, POsPerMonth: 1.2, Terms: "Net 30", Incoterm: "FOB"},
	{ID: "SUP-06", Name: "Supplier Foxtrot (alias)", Country: "DE", Key: "overpriced", Categories: []string{"lens", "prism", "coating"}, PriceFactor: 1.32, OnTimeRate: 0.92, LateDaysMax: 5, LeadFactor: 1.0, RejectRate: 0.005, AckHours: 12, POsPerMonth: 1.0, Terms: "Net 30", Incoterm: "DAP"},
	{ID: "SUP-07", Name: "Supplier Golf (alias)", Country: "CN", Key: "new_supplier", Categories: []string{"mount"}, PriceFactor: 0.97, OnTimeRate: 0.9, LateDaysMax: 5, LeadFactor: 1.0, RejectRate: 0.01, AckHours: 20, POsPerMonth: 0, Terms: "Net 30", Incoterm: "FOB"},
}

type part struct {
	number   string
	category string
	desc     string
	price    float64
	leadDays float64
}

func catalog() []part {
	specs := []struct {
		cat, prefix, desc string
		price, lead       float64
		n                 int
	}{
		{"lens", "LNS", "aspheric lens", 42, 35, 8},
		{"prism", "PRS", "right-angle prism", 65, 42, 5},
		{"filter", "FLT", "bandpass filter", 28, 30, 5},
		{"mount", "MNT", "precision lens mount", 18, 21, 5},
		{"coating", "CTG", "AR coating service (per pc)", 6, 14, 4},
	}
	var out []part
	for _, s := range specs {
		for i := 1; i <= s.n; i++ {
			// spread prices so parts differ but stay deterministic
			f := 0.6 + 0.15*float64(i)
			out = append(out, part{
				number:   fmt.Sprintf("%s-%03d", s.prefix, i*7),
				category: s.cat,
				desc:     fmt.Sprintf("%s variant %d", s.desc, i),
				price:    math.Round(s.price*f*100) / 100,
				leadDays: s.lead,
			})
		}
	}
	return out
}

// Generate builds the dataset with order dates in the 18 months before end.
func Generate(seed int64, end time.Time) domain.Dataset {
	rng := rand.New(rand.NewSource(seed))
	parts := catalog()
	byCat := map[string][]part{}
	for _, p := range parts {
		byCat[p.category] = append(byCat[p.category], p)
	}

	ds := domain.Dataset{
		Customer:  "mloptic-synthetic",
		Generated: end,
		Note:      "Synthetic data shaped like a sanitized MLOptic export. Supplier names are aliases. Not real data.",
	}
	for _, p := range Personas {
		ds.Suppliers = append(ds.Suppliers, domain.Supplier{ID: p.ID, Name: p.Name, Country: p.Country})
	}

	start := end.AddDate(0, -18, 0)
	seq := 0
	for _, p := range Personas {
		var orderDates []time.Time
		if p.Key == "new_supplier" {
			orderDates = []time.Time{end.AddDate(0, 0, -50), end.AddDate(0, 0, -20)}
		} else {
			for m := start; m.Before(end); m = m.AddDate(0, 1, 0) {
				n := int(p.POsPerMonth)
				if rng.Float64() < p.POsPerMonth-float64(n) {
					n++
				}
				for i := 0; i < n; i++ {
					orderDates = append(orderDates, m.AddDate(0, 0, rng.Intn(28)).Add(time.Duration(8+rng.Intn(9))*time.Hour))
				}
			}
		}
		for _, od := range orderDates {
			if od.After(end) {
				continue
			}
			seq++
			po := domain.PurchaseOrder{
				ID:           fmt.Sprintf("po-%04d", seq),
				Number:       fmt.Sprintf("MLO-PO-%05d", 24000+seq),
				SupplierID:   p.ID,
				OrderDate:    od,
				SentAt:       od.Add(time.Duration(rng.Intn(4)) * time.Hour),
				Currency:     "USD",
				PaymentTerms: p.Terms,
				Incoterm:     p.Incoterm,
			}
			if rng.Float64() >= p.NoAckRate {
				h := math.Max(1, p.AckHours*(0.5+rng.Float64()))
				ack := po.SentAt.Add(time.Duration(h * float64(time.Hour)))
				po.AcknowledgedAt = &ack
			}
			nLines := 1 + rng.Intn(3)
			for li := 1; li <= nLines; li++ {
				cat := p.Categories[rng.Intn(len(p.Categories))]
				pt := byCat[cat][rng.Intn(len(byCat[cat]))]
				qty := float64((5 + rng.Intn(40)) * 10)
				price := pt.price * p.PriceFactor * (0.97 + 0.06*rng.Float64())
				lead := pt.leadDays * p.LeadFactor * (0.9 + 0.2*rng.Float64())
				promised := truncDay(od).AddDate(0, 0, int(math.Round(lead)))
				line := domain.POLine{
					ID:           fmt.Sprintf("%s-%d", po.ID, li),
					LineNo:       li,
					PartNumber:   pt.number,
					Category:     pt.category,
					Description:  pt.desc,
					Qty:          qty,
					UnitPrice:    math.Round(price*100) / 100,
					PromisedDate: promised,
				}
				// Delivery outcome
				late := 0
				if rng.Float64() > p.OnTimeRate {
					late = 3 + rng.Intn(max(1, p.LateDaysMax-2))
				} else {
					late = rng.Intn(3) - 1 // -1..+1 day jitter
				}
				delivered := promised.AddDate(0, 0, late)
				if !delivered.After(end) {
					rej := math.Round(qty * p.RejectRate * (0.5 + rng.Float64()))
					// split into two receipts sometimes
					if qty >= 200 && rng.Float64() < 0.3 {
						first := delivered.AddDate(0, 0, -5)
						if first.Before(od) {
							first = od.AddDate(0, 0, 1)
						}
						line.Receipts = []domain.Receipt{
							{Date: first, QtyReceived: qty / 2, QtyRejected: math.Floor(rej / 2)},
							{Date: delivered, QtyReceived: qty / 2, QtyRejected: rej - math.Floor(rej/2)},
						}
					} else {
						line.Receipts = []domain.Receipt{{Date: delivered, QtyReceived: qty, QtyRejected: rej}}
					}
				}
				po.Lines = append(po.Lines, line)
			}
			ds.POs = append(ds.POs, po)
		}
	}
	return ds
}

func truncDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

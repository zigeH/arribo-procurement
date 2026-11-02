package scoring

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/zigeH/arribo-procurement/internal/domain"
)

// observation is one historical data point for a part.
type observation struct {
	poID         string
	lineID       string
	supplierID   string
	unitPrice    float64
	leadDays     float64
	firstArticle bool // first time this supplier supplied this part
}

// Baseline indexes history by part number + currency.
//
// Two rules keep comparisons fair (docs/scoring-methodology.md):
//   - A supplier is benchmarked only against OTHER suppliers' purchases of the
//     same part. Comparing a supplier with its own history always looks
//     "normal" and hides a premium. Single-sourced parts get no benchmark.
//   - Lead time compares repeat orders only. First orders of a part (first
//     article) legitimately take longer (new tooling, first-article inspection).
type Baseline struct {
	byPart       map[string][]observation
	firstArticle map[string]bool // line id -> first article
}

func partKey(part, currency string) string {
	return strings.ToUpper(strings.TrimSpace(part)) + "|" + strings.ToUpper(strings.TrimSpace(currency))
}

// BuildBaseline indexes all lines with an order date in [from, to]. First
// article is determined from the full history given (not only the window), so
// a part first bought before the window is not mistaken for new.
func BuildBaseline(history []domain.PurchaseOrder, from, to time.Time) *Baseline {
	b := &Baseline{byPart: map[string][]observation{}, firstArticle: map[string]bool{}}

	type key struct{ sup, part string }
	first := map[key]time.Time{}
	firstLine := map[key]string{}
	for _, po := range history {
		for _, l := range po.Lines {
			k := key{po.SupplierID, partKey(l.PartNumber, "")}
			if t, ok := first[k]; !ok || po.OrderDate.Before(t) || (po.OrderDate.Equal(t) && l.ID < firstLine[k]) {
				first[k], firstLine[k] = po.OrderDate, l.ID
			}
		}
	}
	for _, id := range firstLine {
		b.firstArticle[id] = true
	}

	for _, po := range history {
		if po.OrderDate.Before(from) || po.OrderDate.After(to) {
			continue
		}
		for _, l := range po.Lines {
			if l.UnitPrice <= 0 || l.Qty <= 0 {
				continue
			}
			k := partKey(l.PartNumber, po.Currency)
			b.byPart[k] = append(b.byPart[k], observation{
				poID:         po.ID,
				lineID:       l.ID,
				supplierID:   po.SupplierID,
				unitPrice:    l.UnitPrice,
				leadDays:     leadDays(po, l),
				firstArticle: b.firstArticle[l.ID],
			})
		}
	}
	return b
}

// IsFirstArticle reports whether the line was the supplier's first order of that part.
func (b *Baseline) IsFirstArticle(lineID string) bool { return b.firstArticle[lineID] }

// PriceFor returns the median unit price paid to OTHER suppliers for the same
// part and currency, and the sample count.
func (b *Baseline) PriceFor(part, currency, supplierID string) (float64, int) {
	var xs []float64
	for _, o := range b.byPart[partKey(part, currency)] {
		if o.supplierID == supplierID {
			continue
		}
		xs = append(xs, o.unitPrice)
	}
	return median(xs), len(xs)
}

// LeadFor returns the median quoted lead time (days) of OTHER suppliers'
// repeat (non-first-article) orders of the same part.
func (b *Baseline) LeadFor(part, currency, supplierID string) (float64, int) {
	var xs []float64
	for _, o := range b.byPart[partKey(part, currency)] {
		if o.supplierID == supplierID || o.leadDays <= 0 || o.firstArticle {
			continue
		}
		xs = append(xs, o.leadDays)
	}
	return median(xs), len(xs)
}

func leadDays(po domain.PurchaseOrder, l domain.POLine) float64 {
	c := l.CommitDate()
	if c.IsZero() || po.OrderDate.IsZero() {
		return 0
	}
	return c.Sub(po.OrderDate).Hours() / 24
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func clamp(x, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, x))
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

func ptr(x float64) *float64 { return &x }

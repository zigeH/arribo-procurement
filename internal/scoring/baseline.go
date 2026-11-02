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
	poID      string
	lineID    string
	unitPrice float64
	leadDays  float64
}

// Baseline indexes history by part number + currency so any line can be
// compared to the *other* lines for the same part.
type Baseline struct {
	byPart map[string][]observation
}

func partKey(part, currency string) string {
	return strings.ToUpper(strings.TrimSpace(part)) + "|" + strings.ToUpper(strings.TrimSpace(currency))
}

// BuildBaseline indexes all lines with an order date in [from, to].
func BuildBaseline(history []domain.PurchaseOrder, from, to time.Time) *Baseline {
	b := &Baseline{byPart: map[string][]observation{}}
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
				poID:      po.ID,
				lineID:    l.ID,
				unitPrice: l.UnitPrice,
				leadDays:  leadDays(po, l),
			})
		}
	}
	return b
}

// PriceFor returns the median unit price of comparable lines (same part and
// currency, excluding lines on excludePOID) and the sample count.
func (b *Baseline) PriceFor(part, currency, excludePOID string) (float64, int) {
	var xs []float64
	for _, o := range b.byPart[partKey(part, currency)] {
		if o.poID == excludePOID {
			continue
		}
		xs = append(xs, o.unitPrice)
	}
	return median(xs), len(xs)
}

// LeadFor returns the median quoted lead time (days) of comparable lines.
func (b *Baseline) LeadFor(part, currency, excludePOID string) (float64, int) {
	var xs []float64
	for _, o := range b.byPart[partKey(part, currency)] {
		if o.poID == excludePOID || o.leadDays <= 0 {
			continue
		}
		xs = append(xs, o.leadDays)
	}
	return median(xs), len(xs)
}

func leadDays(po domain.PurchaseOrder, l domain.POLine) float64 {
	if l.PromisedDate.IsZero() || po.OrderDate.IsZero() {
		return 0
	}
	return l.PromisedDate.Sub(po.OrderDate).Hours() / 24
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

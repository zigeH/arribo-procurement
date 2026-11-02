package scoring

import (
	"fmt"
	"sort"
	"time"

	"github.com/zigeH/arribo-procurement/internal/domain"
)

const maxEvidence = 20

// asOfView returns a copy of the PO as it would have looked at asOf: receipts
// and acknowledgments after asOf are dropped. This is what makes historical
// replay honest (no peeking into the future).
func asOfView(po domain.PurchaseOrder, asOf time.Time) domain.PurchaseOrder {
	out := po
	if po.AcknowledgedAt != nil && po.AcknowledgedAt.After(asOf) {
		out.AcknowledgedAt = nil
	}
	out.Lines = make([]domain.POLine, len(po.Lines))
	for i, l := range po.Lines {
		nl := l
		nl.Receipts = nil
		for _, r := range l.Receipts {
			if !r.Date.After(asOf) {
				nl.Receipts = append(nl.Receipts, r)
			}
		}
		out.Lines[i] = nl
	}
	return out
}

// windowed returns POs ordered within [from, asOf], viewed as of asOf.
func windowed(history []domain.PurchaseOrder, from, asOf time.Time) []domain.PurchaseOrder {
	var out []domain.PurchaseOrder
	for _, po := range history {
		if po.OrderDate.Before(from) || po.OrderDate.After(asOf) {
			continue
		}
		out = append(out, asOfView(po, asOf))
	}
	return out
}

// ScoreSupplier builds a scorecard for one supplier from the full history
// (all suppliers are needed for price and lead-time baselines).
func ScoreSupplier(supplierID string, history []domain.PurchaseOrder, asOf time.Time, cfg Config) Scorecard {
	from := asOf.AddDate(0, -cfg.WindowMonths, 0)
	all := windowed(history, from, asOf)
	base := BuildBaseline(all, from, asOf)

	var mine []domain.PurchaseOrder
	lineCount := 0
	for _, po := range all {
		if po.SupplierID == supplierID {
			mine = append(mine, po)
			lineCount += len(po.Lines)
		}
	}

	sc := Scorecard{
		SupplierID:  supplierID,
		AsOf:        asOf,
		WindowStart: from,
		POCount:     len(mine),
		LineCount:   lineCount,
		Confidence:  confidence(lineCount),
	}

	dims := map[Dimension]DimensionScore{
		DimDelivery:       scoreDelivery(mine, asOf, cfg, &sc.Flags),
		DimPrice:          scorePrice(mine, base, cfg, &sc.Flags),
		DimLeadTime:       scoreLead(mine, base, cfg),
		DimQuality:        scoreQuality(mine, &sc.Flags),
		DimResponsiveness: scoreResponsiveness(mine, asOf, &sc.Flags),
	}

	var wsum, total float64
	for _, d := range AllDimensions {
		ds := dims[d]
		ds.Weight = cfg.Weights[d]
		if ds.Score != nil && ds.Weight > 0 {
			wsum += ds.Weight
			total += ds.Weight * *ds.Score
		}
		sc.Dimensions = append(sc.Dimensions, ds)
	}
	if wsum > 0 {
		o := round1(total / wsum)
		sc.Overall = &o
		sc.Grade = grade(o)
		capGrade(&sc)
	} else {
		sc.Grade = "N/A"
	}
	return sc
}

func finish(d Dimension, scores []float64, weights []float64, ev []Evidence, summary string) DimensionScore {
	ds := DimensionScore{Dimension: d, Samples: len(scores), Summary: summary}
	var wsum, total float64
	for i, s := range scores {
		w := 1.0
		if weights != nil {
			w = weights[i]
		}
		wsum += w
		total += w * s
	}
	if wsum > 0 {
		ds.Score = ptr(round1(total / wsum))
	}
	// Worst evidence first: that is what a buyer wants to look at.
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].Score < ev[j].Score })
	if len(ev) > maxEvidence {
		ev = ev[:maxEvidence]
	}
	ds.Evidence = ev
	return ds
}

func scoreDelivery(pos []domain.PurchaseOrder, asOf time.Time, cfg Config, flags *[]Flag) DimensionScore {
	grace := time.Duration(cfg.OnTimeGraceDays) * 24 * time.Hour
	var scores []float64
	var ev []Evidence
	onTime, late, overdueOpen := 0, 0, 0
	var lateDaysTotal float64
	for _, po := range pos {
		for _, l := range po.Lines {
			if l.PromisedDate.IsZero() {
				continue
			}
			due := l.PromisedDate.Add(grace)
			done, ok := l.CompletedAt()
			switch {
			case ok:
				days := done.Sub(l.PromisedDate).Hours() / 24
				if !done.After(due) {
					onTime++
					scores = append(scores, 100)
					ev = append(ev, Evidence{POID: po.ID, PONumber: po.Number, LineID: l.ID, Detail: "delivered on time", Value: round1(days), Score: 100})
				} else {
					late++
					lateDaysTotal += days
					scores = append(scores, 0)
					ev = append(ev, Evidence{POID: po.ID, PONumber: po.Number, LineID: l.ID, Detail: fmt.Sprintf("delivered %.0f days late", days), Value: round1(days), Score: 0})
				}
			case asOf.After(due):
				late++
				overdueOpen++
				days := asOf.Sub(l.PromisedDate).Hours() / 24
				lateDaysTotal += days
				scores = append(scores, 0)
				ev = append(ev, Evidence{POID: po.ID, PONumber: po.Number, LineID: l.ID, Detail: fmt.Sprintf("open and %.0f days past promised date", days), Value: round1(days), Score: 0})
			}
		}
	}
	summary := "no lines due yet"
	if n := onTime + late; n > 0 {
		summary = fmt.Sprintf("%d/%d lines on time (%.0f%%)", onTime, n, 100*float64(onTime)/float64(n))
		if late > 0 {
			summary += fmt.Sprintf("; late lines average %.1f days", lateDaysTotal/float64(late))
		}
	}
	if overdueOpen > 0 {
		*flags = append(*flags, Flag{Severity: "alert", Code: "overdue_open_lines", Message: fmt.Sprintf("%d open line(s) are past the promised date", overdueOpen)})
	}
	return finish(DimDelivery, scores, nil, ev, summary)
}

func scorePrice(pos []domain.PurchaseOrder, base *Baseline, cfg Config, flags *[]Flag) DimensionScore {
	var scores, weights []float64
	var ev []Evidence
	noBaseline := 0
	for _, po := range pos {
		for _, l := range po.Lines {
			med, n := base.PriceFor(l.PartNumber, po.Currency, po.ID)
			if n < cfg.MinComparables || med <= 0 {
				noBaseline++
				continue
			}
			r := l.UnitPrice / med
			s := priceScore(r)
			scores = append(scores, s)
			weights = append(weights, l.UnitPrice*l.Qty) // spend-weighted
			ev = append(ev, Evidence{POID: po.ID, PONumber: po.Number, LineID: l.ID,
				Detail: fmt.Sprintf("%s at %.2f vs median %.2f (%+.0f%%, n=%d)", l.PartNumber, l.UnitPrice, med, (r-1)*100, n),
				Value:  l.UnitPrice, Baseline: round1(med*100) / 100, Score: round1(s)})
		}
	}
	summary := fmt.Sprintf("%d line(s) compared to same-part median", len(scores))
	if noBaseline > 0 {
		summary += fmt.Sprintf("; %d line(s) had fewer than %d comparables and were not scored", noBaseline, cfg.MinComparables)
		if len(scores) == 0 {
			*flags = append(*flags, Flag{Severity: "info", Code: "price_no_baseline", Message: "not enough comparable purchases to benchmark price"})
		}
	}
	return finish(DimPrice, scores, weights, ev, summary)
}

func scoreLead(pos []domain.PurchaseOrder, base *Baseline, cfg Config) DimensionScore {
	var scores []float64
	var ev []Evidence
	for _, po := range pos {
		for _, l := range po.Lines {
			ld := leadDays(po, l)
			if ld <= 0 {
				continue
			}
			med, n := base.LeadFor(l.PartNumber, po.Currency, po.ID)
			if n < cfg.MinComparables || med <= 0 {
				continue
			}
			r := ld / med
			s := leadScore(r)
			scores = append(scores, s)
			ev = append(ev, Evidence{POID: po.ID, PONumber: po.Number, LineID: l.ID,
				Detail: fmt.Sprintf("%s quoted %.0f days vs median %.0f (n=%d)", l.PartNumber, ld, med, n),
				Value:  round1(ld), Baseline: round1(med), Score: round1(s)})
		}
	}
	return finish(DimLeadTime, scores, nil, ev, fmt.Sprintf("%d line(s) compared to same-part median lead time", len(scores)))
}

func scoreQuality(pos []domain.PurchaseOrder, flags *[]Flag) DimensionScore {
	var received, rejected float64
	var ev []Evidence
	for _, po := range pos {
		for _, l := range po.Lines {
			rc, rj := l.ReceivedQty(), l.RejectedQty()
			if rc <= 0 {
				continue
			}
			received += rc
			rejected += rj
			if rj > 0 {
				rate := rj / rc
				ev = append(ev, Evidence{POID: po.ID, PONumber: po.Number, LineID: l.ID,
					Detail: fmt.Sprintf("%.0f of %.0f rejected", rj, rc), Value: round1(rate * 100), Score: round1(qualityScore(rate))})
			}
		}
	}
	if received == 0 {
		return DimensionScore{Dimension: DimQuality, Summary: "no receipts yet"}
	}
	rate := rejected / received
	if rate > 0.03 {
		*flags = append(*flags, Flag{Severity: "warn", Code: "high_reject_rate", Message: fmt.Sprintf("reject rate %.1f%% over the window", rate*100)})
	}
	ds := finish(DimQuality, []float64{qualityScore(rate)}, nil, ev, fmt.Sprintf("%.1f%% rejected (%.0f of %.0f units)", rate*100, rejected, received))
	ds.Samples = len(ev)
	if ds.Samples == 0 {
		ds.Samples = 1
	}
	return ds
}

func scoreResponsiveness(pos []domain.PurchaseOrder, asOf time.Time, flags *[]Flag) DimensionScore {
	var scores []float64
	var ev []Evidence
	unacked := 0
	for _, po := range pos {
		if po.SentAt.IsZero() {
			continue
		}
		if po.AcknowledgedAt == nil {
			if asOf.Sub(po.SentAt) > 72*time.Hour {
				unacked++
				scores = append(scores, 0)
				ev = append(ev, Evidence{POID: po.ID, PONumber: po.Number, Detail: "PO never acknowledged", Score: 0})
			}
			continue
		}
		h := po.AcknowledgedAt.Sub(po.SentAt).Hours()
		s := ackScore(h)
		scores = append(scores, s)
		ev = append(ev, Evidence{POID: po.ID, PONumber: po.Number, Detail: fmt.Sprintf("acknowledged in %.0fh", h), Value: round1(h), Score: round1(s)})
	}
	if unacked > 0 {
		*flags = append(*flags, Flag{Severity: "warn", Code: "unacknowledged_pos", Message: fmt.Sprintf("%d PO(s) not acknowledged after 72h", unacked)})
	}
	return finish(DimResponsiveness, scores, nil, ev, fmt.Sprintf("%d PO acknowledgment(s) measured", len(scores)))
}

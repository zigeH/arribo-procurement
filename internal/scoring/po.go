package scoring

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zigeH/arribo-procurement/internal/domain"
)

// PO score blend. Supplier reliability is history; price/lead/terms are this PO.
var poWeights = struct{ price, lead, terms, supplier float64 }{0.40, 0.20, 0.10, 0.30}

// AssessPO checks one PO against the baseline built from every *other* PO in
// the window, plus the supplier's historical scorecard (also excluding this PO).
func AssessPO(po domain.PurchaseOrder, history []domain.PurchaseOrder, asOf time.Time, cfg Config) POAssessment {
	from := asOf.AddDate(0, -cfg.WindowMonths, 0)
	var others []domain.PurchaseOrder
	for _, h := range history {
		if h.ID != po.ID {
			others = append(others, h)
		}
	}
	// The baseline includes this PO only so first article is decided for its
	// lines with the same rule as the scorecard; price and lead benchmarks skip
	// the PO's own supplier, so its lines never benchmark themselves.
	base := BuildBaseline(append(pastOf(others, asOf), po), from, asOf)
	sup := ScoreSupplier(po.SupplierID, others, asOf, cfg)

	a := POAssessment{
		POID:            po.ID,
		PONumber:        po.Number,
		SupplierID:      po.SupplierID,
		SupplierOverall: sup.Overall,
		SupplierGrade:   sup.Grade,
	}

	var priceTotal, priceSpend, leadTotal float64
	var leadN int
	escalate, review := false, false

	for _, l := range po.Lines {
		lc := LineCheck{LineID: l.ID, PartNumber: l.PartNumber, UnitPrice: l.UnitPrice, LeadDays: round1(leadDays(po, l))}

		med, n := base.PriceFor(l.PartNumber, po.Currency, po.SupplierID)
		lc.PriceSamples = n
		if n >= cfg.MinComparables && med > 0 && l.UnitPrice > 0 {
			r := l.UnitPrice / med
			lc.PriceBaseline, lc.PriceRatio = ptr(round1(med*100)/100), ptr(round1(r*1000)/1000)
			switch {
			case r < 1-cfg.PriceInRangePct:
				lc.PriceVerdict = "below"
			case r <= 1+cfg.PriceInRangePct:
				lc.PriceVerdict = "in_range"
			case r <= 1+2*cfg.PriceInRangePct:
				lc.PriceVerdict, review = "above", true
			default:
				lc.PriceVerdict, escalate = "far_above", true
			}
			spend := l.UnitPrice * l.Qty
			priceTotal += priceScore(r) * spend
			priceSpend += spend
		} else {
			lc.PriceVerdict = "no_baseline"
		}

		lmed, ln := base.LeadFor(l.PartNumber, po.Currency, po.SupplierID)
		lc.LeadSamples = ln
		firstArticle := base.IsFirstArticle(l.ID)
		if ld := leadDays(po, l); !firstArticle && ln >= cfg.MinComparables && lmed > 0 && ld > 0 {
			r := ld / lmed
			lc.LeadBaseline, lc.LeadRatio = ptr(round1(lmed)), ptr(round1(r*1000)/1000)
			switch {
			case r < 1-cfg.LeadInRangePct:
				lc.LeadVerdict = "faster"
			case r <= 1+cfg.LeadInRangePct:
				lc.LeadVerdict = "in_range"
			case r <= 1+2*cfg.LeadInRangePct:
				lc.LeadVerdict, review = "slower", true
			default:
				lc.LeadVerdict, escalate = "much_slower", true
			}
			leadTotal += leadScore(r)
			leadN++
		} else if firstArticle {
			lc.LeadVerdict = "first_order"
		} else {
			lc.LeadVerdict = "no_baseline"
		}
		a.Lines = append(a.Lines, lc)
	}

	a.Terms = CheckTerms(po, cfg.Policy)
	passed := 0
	for _, t := range a.Terms {
		if t.Passed {
			passed++
		} else {
			escalate = true
		}
	}

	var wsum, total float64
	add := func(w, s float64) { wsum += w; total += w * s }
	if priceSpend > 0 {
		add(poWeights.price, priceTotal/priceSpend)
	}
	if leadN > 0 {
		add(poWeights.lead, leadTotal/float64(leadN))
	}
	if len(a.Terms) > 0 {
		add(poWeights.terms, 100*float64(passed)/float64(len(a.Terms)))
	}
	if sup.Overall != nil {
		add(poWeights.supplier, *sup.Overall)
		switch sup.Grade {
		case "D":
			escalate = true
		case "C":
			review = true
		}
	}
	if wsum > 0 {
		a.Score = ptr(round1(total / wsum))
	}
	if priceSpend == 0 {
		review = true
		a.Flags = append(a.Flags, Flag{Severity: "info", Code: "price_no_baseline", Message: "no line on this PO has enough comparable purchases to benchmark price"})
	}

	switch {
	case escalate:
		a.Verdict = "escalate"
	case review:
		a.Verdict = "review"
	default:
		a.Verdict = "ok"
	}
	a.Flags = append(a.Flags, sup.Flags...)
	return a
}

var (
	reNet     = regexp.MustCompile(`(?i)(?:net\s*|月结\s*)(\d+)`)
	reDeposit = regexp.MustCompile(`(\d+)\s*%\s*(deposit|advance|prepay)`)
)

// CheckTerms evaluates PO terms against customer policy. Rules with no value
// configured are skipped; every result cites the policy source.
func CheckTerms(po domain.PurchaseOrder, p Policy) []TermCheck {
	src := p.Source
	if src == "" {
		src = "customer policy (unconfirmed)"
	}
	var out []TermCheck
	if len(p.AllowedIncoterms) > 0 {
		ok := false
		for _, t := range p.AllowedIncoterms {
			if strings.EqualFold(strings.TrimSpace(po.Incoterm), t) {
				ok = true
			}
		}
		out = append(out, TermCheck{Rule: "allowed_incoterms", Passed: ok, Source: src,
			Message: fmt.Sprintf("incoterm %q; allowed %v", po.Incoterm, p.AllowedIncoterms)})
	}
	if p.MinPaymentNetDays > 0 {
		m := reNet.FindStringSubmatch(po.PaymentTerms)
		ok, msg := false, fmt.Sprintf("payment terms %q do not state Net days; policy requires Net %d+", po.PaymentTerms, p.MinPaymentNetDays)
		if m != nil {
			d, _ := strconv.Atoi(m[1])
			ok = d >= p.MinPaymentNetDays
			msg = fmt.Sprintf("Net %d; policy requires Net %d+", d, p.MinPaymentNetDays)
		}
		out = append(out, TermCheck{Rule: "min_payment_net_days", Passed: ok, Message: msg, Source: src})
	}
	if p.MaxDepositPct > 0 {
		m := reDeposit.FindStringSubmatch(po.PaymentTerms)
		ok, msg := true, "no deposit stated"
		if m != nil {
			d, _ := strconv.Atoi(m[1])
			ok = d <= p.MaxDepositPct
			msg = fmt.Sprintf("%d%% deposit; policy max %d%%", d, p.MaxDepositPct)
		}
		out = append(out, TermCheck{Rule: "max_deposit_pct", Passed: ok, Message: msg, Source: src})
	}
	if len(p.RequiredCurrencies) > 0 {
		ok := false
		for _, c := range p.RequiredCurrencies {
			if strings.EqualFold(po.Currency, c) {
				ok = true
			}
		}
		out = append(out, TermCheck{Rule: "currency", Passed: ok, Source: src,
			Message: fmt.Sprintf("currency %s; allowed %v", po.Currency, p.RequiredCurrencies)})
	}
	return out
}

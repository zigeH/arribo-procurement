package scoring

import (
	"testing"
	"time"

	"github.com/zigeH/arribo-procurement/internal/domain"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func day(n int) time.Time       { return t0.AddDate(0, 0, n) }
func tp(t time.Time) *time.Time { return &t }

// po builds a one-line PO. lateDays < 0 means "not delivered".
func po(id, sup string, orderDay int, part string, price float64, leadDays, lateDays int, rejected float64) domain.PurchaseOrder {
	line := domain.POLine{ID: id + "-1", LineNo: 1, PartNumber: part, Category: "lens", Qty: 100, UnitPrice: price, PromisedDate: day(orderDay + leadDays)}
	if lateDays >= 0 {
		line.Receipts = []domain.Receipt{{Date: day(orderDay + leadDays + lateDays), QtyReceived: 100, QtyRejected: rejected}}
	}
	return domain.PurchaseOrder{
		ID: id, Number: "PO-" + id, SupplierID: sup, OrderDate: day(orderDay),
		SentAt: day(orderDay), AcknowledgedAt: tp(day(orderDay).Add(12 * time.Hour)),
		Currency: "USD", PaymentTerms: "Net 30", Incoterm: "FOB", Lines: []domain.POLine{line},
	}
}

func TestPriceCurve(t *testing.T) {
	cases := map[float64]float64{0.8: 100, 1.0: 100, 1.1: 75, 1.2: 50, 1.4: 0, 2.0: 0}
	for r, want := range cases {
		if got := priceScore(r); got-want > 1e-9 || want-got > 1e-9 {
			t.Errorf("priceScore(%v)=%v want %v", r, got, want)
		}
	}
}

func TestAckCurve(t *testing.T) {
	if ackScore(10) != 100 || ackScore(72) != 70 || ackScore(168) != 20 || ackScore(500) != 10 {
		t.Fatal("ack curve endpoints changed; update docs/scoring-methodology.md too")
	}
}

func TestInsufficientDataIsNilNotGuess(t *testing.T) {
	h := []domain.PurchaseOrder{po("a", "S1", 0, "LENS-1", 10, 30, 0, 0)}
	sc := ScoreSupplier("S1", h, day(200), DefaultConfig())
	for _, d := range sc.Dimensions {
		if d.Dimension == DimPrice && d.Score != nil {
			t.Fatalf("price scored with no comparables: %v", *d.Score)
		}
	}
}

func TestReliableBeatsLateAndCheapBeatsExpensive(t *testing.T) {
	var h []domain.PurchaseOrder
	for i := 0; i < 6; i++ {
		d := i * 20
		h = append(h,
			po("good"+string(rune('a'+i)), "GOOD", d, "LENS-1", 10, 30, 0, 0),
			po("late"+string(rune('a'+i)), "LATE", d, "LENS-1", 10, 30, 15, 0),
			po("pricy"+string(rune('a'+i)), "PRICY", d, "LENS-1", 14, 30, 0, 0),
		)
	}
	asOf := day(300)
	cfg := DefaultConfig()
	good := ScoreSupplier("GOOD", h, asOf, cfg)
	late := ScoreSupplier("LATE", h, asOf, cfg)
	pricy := ScoreSupplier("PRICY", h, asOf, cfg)

	dim := func(sc Scorecard, d Dimension) float64 {
		for _, x := range sc.Dimensions {
			if x.Dimension == d && x.Score != nil {
				return *x.Score
			}
		}
		t.Fatalf("%s missing %s", sc.SupplierID, d)
		return 0
	}
	if dim(good, DimDelivery) != 100 || dim(late, DimDelivery) != 0 {
		t.Fatalf("delivery: good=%v late=%v", dim(good, DimDelivery), dim(late, DimDelivery))
	}
	if dim(pricy, DimPrice) >= dim(good, DimPrice) {
		t.Fatalf("price: pricy=%v should be below good=%v", dim(pricy, DimPrice), dim(good, DimPrice))
	}
	if *good.Overall <= *late.Overall || *good.Overall <= *pricy.Overall {
		t.Fatalf("overall ordering wrong: good=%v late=%v pricy=%v", *good.Overall, *late.Overall, *pricy.Overall)
	}
	if good.Grade != "A" {
		t.Fatalf("good supplier grade = %s", good.Grade)
	}
}

func TestReplayIgnoresFuture(t *testing.T) {
	// Delivered on day 40 (10 days late). As of day 35 it is simply overdue-open;
	// as of day 25 it is not yet due and must not count at all.
	h := []domain.PurchaseOrder{po("a", "S1", 0, "P", 10, 30, 10, 0)}
	sc := ScoreSupplier("S1", h, day(25), DefaultConfig())
	if sc.Dimensions[0].Samples != 0 {
		t.Fatalf("not-yet-due line was scored: %+v", sc.Dimensions[0])
	}
	sc = ScoreSupplier("S1", h, day(35), DefaultConfig())
	if len(sc.Flags) == 0 || sc.Flags[0].Code != "overdue_open_lines" {
		t.Fatalf("expected overdue flag, got %+v", sc.Flags)
	}
}

func TestAssessPOFlagsFarAbovePrice(t *testing.T) {
	var h []domain.PurchaseOrder
	for i := 0; i < 5; i++ {
		h = append(h, po("base"+string(rune('a'+i)), "S1", i*10, "PRISM-9", 50, 40, 0, 0))
	}
	target := po("new", "S2", 100, "PRISM-9", 75, 40, -1, 0)
	h = append(h, target)
	a := AssessPO(target, h, day(100), DefaultConfig())
	if a.Lines[0].PriceVerdict != "far_above" || a.Verdict != "escalate" {
		t.Fatalf("got verdict=%s price=%s", a.Verdict, a.Lines[0].PriceVerdict)
	}
}

func TestTermsNeedPolicyAndCiteSource(t *testing.T) {
	p := po("a", "S1", 0, "P", 10, 30, 0, 0)
	p.PaymentTerms = "50% deposit, balance Net 15"
	if len(CheckTerms(p, Policy{})) != 0 {
		t.Fatal("empty policy must produce no term checks")
	}
	res := CheckTerms(p, Policy{Source: "test", MinPaymentNetDays: 30, MaxDepositPct: 30})
	if len(res) != 2 || res[0].Passed || res[1].Passed || res[0].Source != "test" {
		t.Fatalf("unexpected: %+v", res)
	}
}

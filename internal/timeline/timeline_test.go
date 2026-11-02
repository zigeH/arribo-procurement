package timeline

import (
	"context"
	"testing"
	"time"

	"github.com/zigeH/arribo-procurement/internal/connectors/erp"
	"github.com/zigeH/arribo-procurement/internal/domain"
	"github.com/zigeH/arribo-procurement/internal/extract"
	"github.com/zigeH/arribo-procurement/internal/inbox"
	"github.com/zigeH/arribo-procurement/internal/match"
)

func d(s string) time.Time      { t, _ := time.Parse("2006-01-02", s); return t }
func fp(f float64) *float64     { return &f }
func tp(t time.Time) *time.Time { return &t }

func fixture() (domain.PurchaseOrder, []Item) {
	po := domain.PurchaseOrder{ID: "b1-po-1", Number: "250001", SupplierID: "S1", OrderDate: d("2026-10-01"), Currency: "CNY",
		Lines: []domain.POLine{
			{ID: "b1-po-1-0", LineNo: 1, PartNumber: "LNS-00001-A", Qty: 100, UnitPrice: 10, PromisedDate: d("2026-10-20")},
			{ID: "b1-po-1-1", LineNo: 2, PartNumber: "PRS-00002-A", Qty: 50, UnitPrice: 20, PromisedDate: d("2026-10-10"),
				Receipts: []domain.Receipt{{Date: d("2026-10-09"), QtyReceived: 20}}},
		}}
	mk := func(id, date string, line string, e extract.Event) Item {
		e.MessageID = id
		return Item{MessageID: id, Date: d(date), Match: match.Match{Event: e, POID: "b1-po-1", LineID: line, Confidence: 0.9}}
	}
	items := []Item{
		mk("m1", "2026-10-02", "", extract.Event{Type: inbox.Ack}),
		mk("m2", "2026-10-15", "b1-po-1-0", extract.Event{Type: inbox.DateChange, Date: tp(d("2026-11-05")), Quote: "交期调整为11月5日"}),
		mk("m3", "2026-10-03", "b1-po-1-0", extract.Event{Type: inbox.PriceChange, Price: fp(10.8)}),
		mk("m4", "2026-10-05", "b1-po-1-1", extract.Event{Type: inbox.PartialShip, Qty: fp(40)}),
	}
	return po, items
}

func kinds(ds []Discrepancy) map[string]string {
	out := map[string]string{}
	for _, x := range ds {
		out[x.Kind+":"+x.LineID] = x.Severity
	}
	return out
}

func TestDetect(t *testing.T) {
	po, items := fixture()
	got := kinds(Detect([]domain.PurchaseOrder{po}, items, d("2026-10-25"), DefaultConfig()))
	for k, sev := range map[string]string{
		"date_mismatch:b1-po-1-0":        "alert", // supplier +16 days, ERP not updated
		"price_mismatch:b1-po-1-0":       "warn",
		"shipped_not_received:b1-po-1-1": "warn",  // 40 shipped, 20 received, >7 days
		"overdue_no_update:b1-po-1-1":    "alert", // 30 open, past ERP date, no newer supplier date
	} {
		if got[k] != sev {
			t.Errorf("%s: got %q want %q (all: %v)", k, got[k], sev, got)
		}
	}
	if _, ok := got["overdue_no_update:b1-po-1-0"]; ok {
		t.Error("line 1 has a newer supplier date; must not be overdue_no_update")
	}
	if _, ok := got["unacknowledged:"]; ok {
		t.Error("PO was acknowledged")
	}
	// Before the date change email, no date mismatch.
	if _, ok := kinds(Detect([]domain.PurchaseOrder{po}, items, d("2026-10-12"), DefaultConfig()))["date_mismatch:b1-po-1-0"]; ok {
		t.Error("as-of view leaked a future email")
	}
	// Unacknowledged when nothing came back.
	if _, ok := kinds(Detect([]domain.PurchaseOrder{po}, nil, d("2026-10-06"), DefaultConfig()))["unacknowledged:"]; !ok {
		t.Error("expected unacknowledged")
	}
}

func TestBuildTimeline(t *testing.T) {
	po, items := fixture()
	tl := Build(po, items, d("2026-10-25"))
	if len(tl.Entries) != 6 || tl.Entries[0].Kind != "po_created" {
		t.Fatalf("entries: %+v", tl.Entries)
	}
	l0 := tl.Lines[0]
	if l0.SupplierDate == nil || !l0.SupplierDate.Equal(d("2026-11-05")) || *l0.SupplierPrice != 10.8 || l0.Status != "open" {
		t.Fatalf("line0: %+v", l0)
	}
	if tl.Lines[1].Status != "overdue" || tl.Lines[1].Received != 20 {
		t.Fatalf("line1: %+v", tl.Lines[1])
	}
	for _, e := range tl.Entries {
		if e.Source.Ref == "" {
			t.Fatalf("entry without source: %+v", e)
		}
	}
}

type memReader struct {
	sups []erp.Supplier
	pos  []domain.PurchaseOrder
}

func (m memReader) Suppliers(context.Context) ([]erp.Supplier, error) { return m.sups, nil }
func (m memReader) PurchaseOrders(context.Context, time.Time) ([]domain.PurchaseOrder, error) {
	return m.pos, nil
}

func TestHubEnrichesForScoring(t *testing.T) {
	po, _ := fixture()
	r := memReader{sups: []erp.Supplier{{Supplier: domain.Supplier{ID: "S1", Name: "Optics A"}, Emails: []string{"sales@a.example.cn"}}}, pos: []domain.PurchaseOrder{po}}
	msgs := []inbox.Message{
		{ID: "e1", From: "sales@a.example.cn", Subject: "Re: PO 250001", Body: "贵司PO 250001已收到，我司确认按订单交期安排生产。", Date: d("2026-10-02")},
		{ID: "e2", From: "sales@a.example.cn", Subject: "关于PO 250001交期调整", Body: "贵司PO 250001第1项（LNS-00001-A）交期由10月20日调整为11月5日。", Date: d("2026-10-15")},
		{ID: "e3", From: "nobody@qq.com", Subject: "PO 250001", Body: "第2项 推迟到11月9日", Date: d("2026-10-16")},
	}
	h, err := Load(context.Background(), r, msgs, extract.Rules{})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Items) != 2 {
		t.Fatalf("items %d review %d", len(h.Items), len(h.Review))
	}
	ds := h.EnrichedDataset("x", d("2026-10-25"))
	l := ds.POs[0].Lines[0]
	if l.OriginalPromisedDate == nil || !l.OriginalPromisedDate.Equal(d("2026-10-20")) || !l.PromisedDate.Equal(d("2026-11-05")) {
		t.Fatalf("not enriched: %+v", l)
	}
	if l.PromiseChangedAt == nil || !l.PromiseChangedAt.Equal(d("2026-10-15")) {
		t.Fatalf("promise change time not set from email: %+v", l.PromiseChangedAt)
	}
	if ds.POs[0].AcknowledgedAt == nil {
		t.Fatal("ack not applied")
	}
}

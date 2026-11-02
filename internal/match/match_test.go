package match

import (
	"testing"
	"time"

	"github.com/zigeH/arribo-procurement/internal/connectors/erp"
	"github.com/zigeH/arribo-procurement/internal/domain"
	"github.com/zigeH/arribo-procurement/internal/extract"
	"github.com/zigeH/arribo-procurement/internal/inbox"
)

func d(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

func setup() *Matcher {
	sups := []erp.Supplier{
		{Supplier: domain.Supplier{ID: "A", Name: "Supplier Alpha (alias)"}, Emails: []string{"sales@alpha.example.cn"}},
		{Supplier: domain.Supplier{ID: "B", Name: "Optics shop Hubei (alias)"}, Emails: []string{"po@hubei.example.cn"}},
	}
	pos := []domain.PurchaseOrder{
		{ID: "p1", Number: "250001", SupplierID: "A", OrderDate: d("2026-10-01"), Lines: []domain.POLine{
			{ID: "p1-0", LineNo: 1, PartNumber: "LNS-00001-A", Qty: 10, PromisedDate: d("2026-11-01")},
			{ID: "p1-1", LineNo: 2, PartNumber: "PRS-00002-B", Qty: 10, PromisedDate: d("2026-11-02")}}},
		{ID: "p2", Number: "250002", SupplierID: "B", OrderDate: d("2026-10-02"), Lines: []domain.POLine{
			{ID: "p2-0", LineNo: 1, PartNumber: "PLT-00003-A", Qty: 10, PromisedDate: d("2026-11-03")}}},
	}
	return New(sups, pos)
}

func TestResolve(t *testing.T) {
	m := setup()
	at := d("2026-10-10")
	cases := []struct {
		name   string
		msg    inbox.Message
		ev     extract.Event
		line   string
		review bool
	}{
		{"domain+po+line", inbox.Message{From: "sales@alpha.example.cn", Date: at}, extract.Event{Type: inbox.DateChange, PONumber: "250001", LineNo: 2, Confidence: 0.9}, "p1-1", false},
		{"line by part", inbox.Message{From: "sales@alpha.example.cn", Date: at}, extract.Event{Type: inbox.DateChange, PONumber: "250001", PartNumber: "LNS-00001-A", Confidence: 0.9}, "p1-0", false},
		{"no PO, supplier+part", inbox.Message{From: "sales@alpha.example.cn", Date: at}, extract.Event{Type: inbox.PartialShip, PartNumber: "PRS-00002-B", Confidence: 0.8}, "p1-1", false},
		{"signature alias", inbox.Message{From: "123@qq.com", Body: "刘\nOptics shop Hubei 销售部", Date: at}, extract.Event{Type: inbox.Ack, PONumber: "250002", Confidence: 0.9}, "", false},
		{"unknown sender -> review", inbox.Message{From: "x@qq.com", Date: at}, extract.Event{Type: inbox.DateChange, PONumber: "250001", LineNo: 1, Confidence: 0.9}, "p1-0", true},
		{"other supplier's PO -> review", inbox.Message{From: "po@hubei.example.cn", Date: at}, extract.Event{Type: inbox.DateChange, PONumber: "250001", LineNo: 1, Confidence: 0.9}, "", true},
		{"ambiguous line -> review", inbox.Message{From: "sales@alpha.example.cn", Date: at}, extract.Event{Type: inbox.DateChange, PONumber: "250001", Confidence: 0.9}, "", true},
		{"unknown PO -> review", inbox.Message{From: "sales@alpha.example.cn", Date: at}, extract.Event{Type: inbox.DateChange, PONumber: "999999", Confidence: 0.9}, "", true},
	}
	for _, c := range cases {
		r := m.Resolve(c.msg, c.ev)
		if r.LineID != c.line || r.NeedsReview() != c.review {
			t.Errorf("%s: line %q review %v (%v)", c.name, r.LineID, r.NeedsReview(), r.Reasons)
		}
	}
}

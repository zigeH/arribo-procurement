package extract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zigeH/arribo-procurement/internal/inbox"
)

var ref = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

func one(t *testing.T, subj, body string) Event {
	t.Helper()
	evs, err := Rules{}.Extract(context.Background(), inbox.Message{ID: "m", Subject: subj, Body: body, Date: ref})
	if err != nil || len(evs) != 1 {
		t.Fatalf("%v %v", err, evs)
	}
	return evs[0]
}

func TestRulesDateChangeZh(t *testing.T) {
	e := one(t, "关于PO 250123交期调整", "您好，因镀膜返工，贵司PO 250123第2项（lns81807a）交期由10月30日调整为11月20日，敬请谅解。")
	if e.Type != inbox.DateChange || e.PONumber != "250123" || e.LineNo != 2 || e.PartNumber != "LNS-81807-A" {
		t.Fatalf("%+v", e)
	}
	if e.Date == nil || e.Date.Format("2006-01-02") != "2026-11-20" {
		t.Fatalf("date %v", e.Date)
	}
}

func TestRulesYearRollover(t *testing.T) {
	e := one(t, "延期通知", "您好，订单号250123的PRS-31352-B要推迟到1月8日交货，抱歉。")
	if e.Date == nil || e.Date.Format("2006-01-02") != "2027-01-08" {
		t.Fatalf("date %v", e.Date)
	}
}

func TestRulesPartialAndPriceEn(t *testing.T) {
	e := one(t, "Partial shipment - PO#250200", "Hi, we shipped 40 pcs of MEC-12345 (line 1) today under PO#250200; the remaining 60 pcs will follow next week.")
	if e.Type != inbox.PartialShip || e.Qty == nil || *e.Qty != 40 {
		t.Fatalf("%+v", e)
	}
	p := one(t, "Price adjustment order 250201", "Hello, the unit price for BLK-STD-003 (order 250201, line 2) changes from USD 120.00 to USD 131.50.")
	if p.Type != inbox.PriceChange || p.Price == nil || *p.Price != 131.5 || p.Currency != "USD" || p.PartNumber != "BLK-STD-003" {
		t.Fatalf("%+v", p)
	}
}

func TestRulesNoiseIsOther(t *testing.T) {
	if e := one(t, "春节放假通知", "我司2027年春节放假时间为2月5日至2月16日，节前订单请提前安排。"); e.Type != inbox.Other {
		t.Fatalf("%+v", e)
	}
}

func TestLLMParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" || r.URL.Path != "/v1/messages" {
			w.WriteHeader(400)
			return
		}
		out := `Here you go: [{"type":"date_change","po_number":"MLO-PO-250123","line_no":2,"part_number":"lns-81807 rev A","date":"2026-11-20","confidence":0.9,"quote":"交期调整为11月20日"}]`
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []map[string]string{{"type": "text", "text": out}}})
	}))
	defer srv.Close()
	l := &LLM{APIKey: "k", Model: "m", BaseURL: srv.URL}
	evs, err := l.Extract(context.Background(), inbox.Message{ID: "x", Date: ref})
	if err != nil || len(evs) != 1 {
		t.Fatalf("%v %v", err, evs)
	}
	e := evs[0]
	if e.PONumber != "250123" || e.PartNumber != "LNS-81807-A" || e.Date.Format("2006-01-02") != "2026-11-20" {
		t.Fatalf("%+v", e)
	}
}

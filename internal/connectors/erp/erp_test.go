package erp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeServiceLayer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var expireOnce int32 = 1
	logins := new(int32)
	mux := http.NewServeMux()
	mux.HandleFunc("/b1s/v1/Login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["UserName"] != "reader" || body["CompanyDB"] != "SBODEMO" {
			w.WriteHeader(401)
			return
		}
		atomic.AddInt32(logins, 1)
		http.SetCookie(w, &http.Cookie{Name: "B1SESSION", Value: "s1", Path: "/"})
		_, _ = w.Write([]byte(`{"SessionId":"s1"}`))
	})
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if c, err := r.Cookie("B1SESSION"); err != nil || c.Value != "s1" {
			w.WriteHeader(401)
			return false
		}
		return true
	}
	mux.HandleFunc("/b1s/v1/BusinessPartners", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = w.Write([]byte(`{"value":[{"CardCode":"V001","CardName":"Jiangsu Optics","CardType":"cSupplier","EmailAddress":"sales@js-optics.cn; po@js-optics.cn"}]}`))
	})
	mux.HandleFunc("/b1s/v1/PurchaseOrders", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		// Simulate a session timeout on the first page request.
		if atomic.CompareAndSwapInt32(&expireOnce, 1, 0) {
			w.WriteHeader(401)
			return
		}
		if r.URL.Query().Get("$skip") == "" && !strings.Contains(r.URL.Query().Get("$filter"), "DocDate ge '2026-01-01'") {
			t.Errorf("filter not passed: %q", r.URL.RawQuery)
		}
		if r.URL.Query().Get("$skip") == "" {
			_, _ = w.Write([]byte(`{"value":[{"DocEntry":10,"DocNum":250010,"DocDate":"2026-03-01","CardCode":"V001","DocCurrency":"CNY","DocumentLines":[{"LineNum":0,"ItemCode":"LNS-1","Quantity":100,"Price":12.5,"ShipDate":"2026-04-10"}]}],"odata.nextLink":"PurchaseOrders?$skip=1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"value":[{"DocEntry":11,"DocNum":250011,"DocDate":"2026-03-02","CardCode":"V001","DocCurrency":"CNY","DocumentLines":[{"LineNum":0,"ItemCode":"PRS-2","Quantity":5,"Price":300,"ShipDate":"2026-04-20"},{"LineNum":1,"ItemCode":"PRS-3","Quantity":5,"Price":320,"ShipDate":"2026-04-22"}]}]}`))
	})
	mux.HandleFunc("/b1s/v1/PurchaseDeliveryNotes", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = w.Write([]byte(`{"value":[{"DocEntry":90,"DocNum":900,"DocDate":"2026-04-21","CardCode":"V001","DocCurrency":"CNY","DocumentLines":[{"LineNum":0,"ItemCode":"PRS-3","Quantity":3,"BaseType":22,"BaseEntry":11,"BaseLine":1}]}]}`))
	})
	return httptest.NewServer(mux), logins
}

func TestServiceLayerReadsPagesAndRelogs(t *testing.T) {
	srv, logins := fakeServiceLayer(t)
	defer srv.Close()
	sl := &ServiceLayer{BaseURL: srv.URL + "/b1s/v1", CompanyDB: "SBODEMO", UserName: "reader", Password: "x"}
	ctx := context.Background()

	sups, err := sl.Suppliers(ctx)
	if err != nil || len(sups) != 1 || len(sups[0].Emails) != 2 {
		t.Fatalf("suppliers: %v %+v", err, sups)
	}
	pos, err := sl.PurchaseOrders(ctx, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(pos) != 2 {
		t.Fatalf("pages not followed: %d POs", len(pos))
	}
	if atomic.LoadInt32(logins) != 2 {
		t.Fatalf("expected re-login after 401, logins=%d", *logins)
	}
	l := pos[1].Lines[1]
	if pos[1].ID != "b1-po-11" || pos[1].Number != "250011" || l.LineNo != 2 || len(l.Receipts) != 1 || l.Receipts[0].QtyReceived != 3 {
		t.Fatalf("mapping wrong: %+v", pos[1])
	}
}

func TestFixtureReader(t *testing.T) {
	r := FixtureReader{Dir: "../../../evals/data/b1"}
	sups, err := r.Suppliers(context.Background())
	if err != nil || len(sups) == 0 {
		t.Fatalf("%v %d", err, len(sups))
	}
	pos, err := r.PurchaseOrders(context.Background(), time.Time{})
	if err != nil || len(pos) == 0 {
		t.Fatalf("%v %d", err, len(pos))
	}
}

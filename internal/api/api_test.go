package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zigeH/arribo-procurement/internal/connectors/erp"
	"github.com/zigeH/arribo-procurement/internal/extract"
	"github.com/zigeH/arribo-procurement/internal/inbox"
	"github.com/zigeH/arribo-procurement/internal/scoring"
	"github.com/zigeH/arribo-procurement/internal/service"
	"github.com/zigeH/arribo-procurement/internal/store"
	"github.com/zigeH/arribo-procurement/internal/timeline"
)

var (
	firstPO    string
	nSuppliers int
)

func newServer(t *testing.T) *httptest.Server {
	ds, err := store.LoadDatasetFile("../../evals/data/synthetic_mloptic.json")
	if err != nil {
		t.Fatal(err)
	}
	firstPO, nSuppliers = ds.POs[0].ID, len(ds.Suppliers)
	s := &Server{
		Svc: &service.Service{Store: store.NewMemory(ds), Cfg: scoring.DefaultConfig()},
		Now: func() time.Time { return ds.Generated },
	}
	return httptest.NewServer(s.Handler())
}

func get(t *testing.T, url string, want int, into any) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != want {
		t.Fatalf("GET %s = %d, want %d", url, res.StatusCode, want)
	}
	if into != nil {
		if err := json.NewDecoder(res.Body).Decode(into); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEndpoints(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()

	var list struct{ Suppliers []supplierRow }
	get(t, srv.URL+"/v1/suppliers", 200, &list)
	if len(list.Suppliers) != nSuppliers || nSuppliers != 76 {
		t.Fatalf("suppliers = %d", len(list.Suppliers))
	}
	var sc scoring.Scorecard
	get(t, srv.URL+"/v1/suppliers/SUP-02/scorecard?as_of=2026-06-30", 200, &sc)
	if sc.Overall == nil || len(sc.Dimensions) != 5 {
		t.Fatalf("bad scorecard %+v", sc)
	}
	var a scoring.POAssessment
	get(t, srv.URL+"/v1/pos/"+firstPO+"/assessment", 200, &a)
	get(t, srv.URL+"/v1/suppliers/NOPE/scorecard", 404, nil)
	get(t, srv.URL+"/v1/suppliers/SUP-01/scorecard?as_of=bad", 400, nil)

	res, _ := http.Post(srv.URL+"/v1/scoring/runs", "application/json", nil)
	if res.StatusCode != 503 {
		t.Fatalf("run without temporal = %d, want 503", res.StatusCode)
	}
}

func TestHubEndpoints(t *testing.T) {
	c, err := inbox.LoadCorpus("../../evals/data/emails.json")
	if err != nil {
		t.Fatal(err)
	}
	h, err := timeline.Load(context.Background(), erp.FixtureReader{Dir: "../../evals/data/b1"}, c.Messages, extract.Rules{})
	if err != nil {
		t.Fatal(err)
	}
	asOf := c.Messages[len(c.Messages)-1].Date
	s := &Server{Svc: &service.Service{Store: store.NewMemory(h.EnrichedDataset("t", asOf)), Cfg: scoring.DefaultConfig()}, Hub: h, Now: func() time.Time { return asOf }}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	var dl struct {
		Count int
		Items []timeline.Discrepancy
	}
	get(t, srv.URL+"/v1/discrepancies", 200, &dl)
	if dl.Count == 0 || len(dl.Items[0].Sources) == 0 {
		t.Fatalf("discrepancies: %+v", dl)
	}
	var tl timeline.Timeline
	get(t, srv.URL+"/v1/pos/"+dl.Items[0].POID+"/timeline", 200, &tl)
	if len(tl.Entries) == 0 {
		t.Fatal("empty timeline")
	}
	get(t, srv.URL+"/v1/pos/"+tl.PONumber+"/timeline", 200, nil) // by PO number too
	get(t, srv.URL+"/v1/pos/nope/timeline", 404, nil)
	get(t, srv.URL+"/v1/review-queue", 200, nil)

	noHub := httptest.NewServer((&Server{Svc: s.Svc}).Handler())
	defer noHub.Close()
	get(t, noHub.URL+"/v1/discrepancies", 503, nil)
}

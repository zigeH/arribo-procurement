package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zigeH/arribo-procurement/internal/scoring"
	"github.com/zigeH/arribo-procurement/internal/service"
	"github.com/zigeH/arribo-procurement/internal/store"
)

func newServer(t *testing.T) *httptest.Server {
	ds, err := store.LoadDatasetFile("../../evals/data/synthetic_mloptic.json")
	if err != nil {
		t.Fatal(err)
	}
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
	if len(list.Suppliers) != 7 {
		t.Fatalf("suppliers = %d", len(list.Suppliers))
	}
	var sc scoring.Scorecard
	get(t, srv.URL+"/v1/suppliers/SUP-02/scorecard?as_of=2026-06-30", 200, &sc)
	if sc.Overall == nil || len(sc.Dimensions) != 5 {
		t.Fatalf("bad scorecard %+v", sc)
	}
	var a scoring.POAssessment
	get(t, srv.URL+"/v1/pos/po-0001/assessment", 200, &a)
	get(t, srv.URL+"/v1/suppliers/NOPE/scorecard", 404, nil)
	get(t, srv.URL+"/v1/suppliers/SUP-01/scorecard?as_of=bad", 400, nil)

	res, _ := http.Post(srv.URL+"/v1/scoring/runs", "application/json", nil)
	if res.StatusCode != 503 {
		t.Fatalf("run without temporal = %d, want 503", res.StatusCode)
	}
}

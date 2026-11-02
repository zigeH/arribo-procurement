// Package api serves the HTTP API. Stdlib-only; Temporal is injected through
// the RunStarter interface so the API runs (read-only) without Temporal.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/zigeH/arribo-procurement/internal/service"
	"github.com/zigeH/arribo-procurement/internal/store"
	"github.com/zigeH/arribo-procurement/internal/timeline"
)

// RunStarter starts a batch scoring run (a Temporal workflow in production).
type RunStarter interface {
	StartScoringRun(ctx context.Context, asOf time.Time) (runID string, err error)
}

type Server struct {
	Svc         *service.Service
	Runs        RunStarter    // may be nil
	Hub         *timeline.Hub // PO data hub; may be nil
	AllowOrigin string        // CORS origin for the console; "" = none
	Now         func() time.Time
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /v1/suppliers", s.listSuppliers)
	mux.HandleFunc("GET /v1/suppliers/{id}/scorecard", s.scorecard)
	mux.HandleFunc("GET /v1/pos/{id}/assessment", s.assessPO)
	mux.HandleFunc("POST /v1/scoring/runs", s.startRun)
	mux.HandleFunc("GET /v1/pos/{id}/timeline", s.poTimeline)
	mux.HandleFunc("GET /v1/discrepancies", s.discrepancies)
	mux.HandleFunc("GET /v1/review-queue", s.reviewQueue)
	return s.cors(mux)
}

func (s *Server) asOf(r *http.Request) (time.Time, error) {
	if v := r.URL.Query().Get("as_of"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return time.Time{}, errors.New("as_of must be YYYY-MM-DD")
		}
		return t.Add(24*time.Hour - time.Second), nil // end of that day
	}
	if s.Now != nil {
		return s.Now(), nil
	}
	return time.Now().UTC(), nil
}

type supplierRow struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Country    string   `json:"country,omitempty"`
	Overall    *float64 `json:"overall"`
	Grade      string   `json:"grade"`
	Confidence string   `json:"confidence"`
	POCount    int      `json:"po_count"`
	Alerts     int      `json:"alerts"`
}

func (s *Server) listSuppliers(w http.ResponseWriter, r *http.Request) {
	asOf, err := s.asOf(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	sups, err := s.Svc.Store.ListSuppliers(r.Context())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	rows := make([]supplierRow, 0, len(sups))
	for _, sp := range sups {
		sc, err := s.Svc.Scorecard(r.Context(), sp.ID, asOf)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		alerts := 0
		for _, f := range sc.Flags {
			if f.Severity == "alert" || f.Severity == "warn" {
				alerts++
			}
		}
		rows = append(rows, supplierRow{ID: sp.ID, Name: sp.Name, Country: sp.Country, Overall: sc.Overall, Grade: sc.Grade, Confidence: sc.Confidence, POCount: sc.POCount, Alerts: alerts})
	}
	writeJSON(w, 200, map[string]any{"as_of": asOf, "suppliers": rows})
}

func (s *Server) scorecard(w http.ResponseWriter, r *http.Request) {
	asOf, err := s.asOf(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	sc, err := s.Svc.Scorecard(r.Context(), r.PathValue("id"), asOf)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, err)
		return
	} else if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, sc)
}

func (s *Server) assessPO(w http.ResponseWriter, r *http.Request) {
	asOf, err := s.asOf(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	a, err := s.Svc.AssessPO(r.Context(), r.PathValue("id"), asOf)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, err)
		return
	} else if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, a)
}

func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	if s.Runs == nil {
		writeErr(w, 503, errors.New("batch scoring requires Temporal; set TEMPORAL_ADDRESS"))
		return
	}
	asOf, err := s.asOf(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	id, err := s.Runs.StartScoringRun(r.Context(), asOf)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 202, map[string]string{"run_id": id})
}

func (s *Server) hubOr503(w http.ResponseWriter) bool {
	if s.Hub == nil {
		writeErr(w, 503, errors.New("PO data hub not configured (ERP source + messages)"))
		return false
	}
	return true
}

func (s *Server) poTimeline(w http.ResponseWriter, r *http.Request) {
	if !s.hubOr503(w) {
		return
	}
	asOf, err := s.asOf(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	tl, err := s.Hub.Timeline(r.PathValue("id"), asOf)
	if errors.Is(err, timeline.ErrNotFound) {
		writeErr(w, 404, err)
		return
	}
	writeJSON(w, 200, tl)
}

func (s *Server) discrepancies(w http.ResponseWriter, r *http.Request) {
	if !s.hubOr503(w) {
		return
	}
	asOf, err := s.asOf(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	ds := s.Hub.Discrepancies(asOf)
	if k := r.URL.Query().Get("kind"); k != "" {
		var f []timeline.Discrepancy
		for _, d := range ds {
			if d.Kind == k {
				f = append(f, d)
			}
		}
		ds = f
	}
	byKind := map[string]int{}
	for _, d := range ds {
		byKind[d.Kind]++
	}
	if ds == nil {
		ds = []timeline.Discrepancy{}
	}
	writeJSON(w, 200, map[string]any{"as_of": asOf, "count": len(ds), "by_kind": byKind, "items": ds})
}

func (s *Server) reviewQueue(w http.ResponseWriter, r *http.Request) {
	if !s.hubOr503(w) {
		return
	}
	asOf, err := s.asOf(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	q := s.Hub.ReviewQueue(asOf)
	if q == nil {
		q = []timeline.Item{}
	}
	writeJSON(w, 200, map[string]any{"as_of": asOf, "count": len(q), "items": q})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.AllowOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", s.AllowOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == http.MethodOptions {
				w.WriteHeader(204)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "err", err)
	}
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

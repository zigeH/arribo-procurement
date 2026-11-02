// Package service is the thin application layer shared by the API (live
// reads) and Temporal activities (batch runs). Stdlib-only.
package service

import (
	"context"
	"time"

	"github.com/zigeH/arribo-procurement/internal/scoring"
	"github.com/zigeH/arribo-procurement/internal/store"
)

type Service struct {
	Store store.Store
	Cfg   scoring.Config
}

func (s *Service) since(asOf time.Time) time.Time {
	// one extra month so baselines at the window edge have context
	return asOf.AddDate(0, -s.Cfg.WindowMonths-1, 0)
}

// Scorecard computes a supplier scorecard as of asOf.
func (s *Service) Scorecard(ctx context.Context, supplierID string, asOf time.Time) (scoring.Scorecard, error) {
	if _, err := s.Store.GetSupplier(ctx, supplierID); err != nil {
		return scoring.Scorecard{}, err
	}
	h, err := s.Store.LoadHistory(ctx, s.since(asOf))
	if err != nil {
		return scoring.Scorecard{}, err
	}
	return scoring.ScoreSupplier(supplierID, h, asOf, s.Cfg), nil
}

// AssessPO rates one PO as of asOf.
func (s *Service) AssessPO(ctx context.Context, poID string, asOf time.Time) (scoring.POAssessment, error) {
	po, err := s.Store.GetPO(ctx, poID)
	if err != nil {
		return scoring.POAssessment{}, err
	}
	h, err := s.Store.LoadHistory(ctx, s.since(asOf))
	if err != nil {
		return scoring.POAssessment{}, err
	}
	return scoring.AssessPO(po, h, asOf, s.Cfg), nil
}

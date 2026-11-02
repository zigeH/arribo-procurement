package service

import (
	"context"
	"testing"

	"github.com/zigeH/arribo-procurement/internal/scoring"
	"github.com/zigeH/arribo-procurement/internal/store"
)

func TestServiceOnSyntheticData(t *testing.T) {
	ds, err := store.LoadDatasetFile("../../evals/data/synthetic_mloptic.json")
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: store.NewMemory(ds), Cfg: scoring.DefaultConfig()}
	ctx := context.Background()

	sc, err := svc.Scorecard(ctx, "SUP-01", ds.Generated)
	if err != nil || sc.Overall == nil {
		t.Fatalf("scorecard: %v %+v", err, sc)
	}
	a, err := svc.AssessPO(ctx, ds.POs[len(ds.POs)-1].ID, ds.Generated)
	if err != nil || a.Verdict == "" {
		t.Fatalf("assess: %v %+v", err, a)
	}
	if _, err := svc.Scorecard(ctx, "nope", ds.Generated); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// Package bootstrap wires config from env for the binaries.
//
// Env:
//
//	DATABASE_URL  Postgres URL. If empty, an in-memory store is loaded from DATASET.
//	DATASET       JSON dataset for the in-memory store (default evals/data/synthetic_mloptic.json)
//	TENANT_ID     tenant for the Postgres store (default "mloptic")
package bootstrap

import (
	"context"
	"log/slog"
	"os"

	"github.com/zigeH/arribo-procurement/internal/scoring"
	"github.com/zigeH/arribo-procurement/internal/service"
	"github.com/zigeH/arribo-procurement/internal/store"
	"github.com/zigeH/arribo-procurement/internal/store/postgres"
)

func Env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Service builds the service with the configured store.
func Service(ctx context.Context) (*service.Service, func(), error) {
	cfg := scoring.DefaultConfig()
	if url := os.Getenv("DATABASE_URL"); url != "" {
		pg, err := postgres.Open(ctx, url, Env("TENANT_ID", "mloptic"))
		if err != nil {
			return nil, nil, err
		}
		if err := pg.Migrate(ctx); err != nil {
			return nil, nil, err
		}
		slog.Info("store: postgres", "tenant", pg.Tenant)
		return &service.Service{Store: pg, Cfg: cfg}, pg.Pool.Close, nil
	}
	path := Env("DATASET", "evals/data/synthetic_mloptic.json")
	ds, err := store.LoadDatasetFile(path)
	if err != nil {
		return nil, nil, err
	}
	slog.Info("store: in-memory", "dataset", path, "pos", len(ds.POs))
	return &service.Service{Store: store.NewMemory(ds), Cfg: cfg}, func() {}, nil
}

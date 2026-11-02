// seed imports a dataset JSON into Postgres. Usage:
//
//	DATABASE_URL=postgres://... go run ./cmd/seed [-file evals/data/synthetic_mloptic.json] [-tenant mloptic]
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/zigeH/arribo-procurement/internal/store"
	"github.com/zigeH/arribo-procurement/internal/store/postgres"
)

func main() {
	file := flag.String("file", "evals/data/synthetic_mloptic.json", "dataset JSON")
	tenant := flag.String("tenant", "mloptic", "tenant id")
	flag.Parse()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	ctx := context.Background()
	pg, err := postgres.Open(ctx, url, *tenant)
	if err != nil {
		slog.Error("open", "err", err)
		os.Exit(1)
	}
	defer pg.Pool.Close()
	if err := pg.Migrate(ctx); err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}
	ds, err := store.LoadDatasetFile(*file)
	if err != nil {
		slog.Error("load", "err", err)
		os.Exit(1)
	}
	if err := pg.ImportDataset(ctx, ds); err != nil {
		slog.Error("import", "err", err)
		os.Exit(1)
	}
	slog.Info("seeded", "tenant", *tenant, "suppliers", len(ds.Suppliers), "pos", len(ds.POs))
}

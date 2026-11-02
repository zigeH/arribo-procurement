package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/zigeH/arribo-procurement/internal/store"
)

// Integration test: runs only when DATABASE_URL is set (CI provides one).
func TestRoundTrip(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, url, "test-roundtrip")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ds, err := store.LoadDatasetFile("../../../evals/data/synthetic_mloptic.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ImportDataset(ctx, ds); err != nil {
		t.Fatal(err)
	}
	// Idempotent re-import.
	if err := s.ImportDataset(ctx, ds); err != nil {
		t.Fatal(err)
	}

	mem := store.NewMemory(ds)
	want, _ := mem.LoadHistory(ctx, ds.Generated.AddDate(-5, 0, 0))
	got, err := s.LoadHistory(ctx, ds.Generated.AddDate(-5, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("POs: got %d want %d", len(got), len(want))
	}
	lines, receipts := 0, 0
	for _, p := range got {
		lines += len(p.Lines)
		for _, l := range p.Lines {
			receipts += len(l.Receipts)
		}
	}
	wl, wr := 0, 0
	for _, p := range want {
		wl += len(p.Lines)
		for _, l := range p.Lines {
			wr += len(l.Receipts)
		}
	}
	if lines != wl || receipts != wr {
		t.Fatalf("lines %d/%d receipts %d/%d", lines, wl, receipts, wr)
	}
}

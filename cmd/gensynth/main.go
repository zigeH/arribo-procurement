// gensynth writes the deterministic synthetic data used by evals and the
// local demo:
//
//	evals/data/synthetic_mloptic.json   purchase history (normalized)
//	evals/data/b1/*.json                same history as an SAP B1 Service Layer export
//	evals/data/emails.json              supplier email corpus + golden labels
//
// Run: go run ./cmd/gensynth   (CI runs -check to verify committed files are fresh)
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zigeH/arribo-procurement/internal/synth"
)

func main() {
	dir := flag.String("dir", "evals/data", "output directory")
	seed := flag.Int64("seed", 42, "random seed")
	check := flag.Bool("check", false, "fail if files on disk differ from a fresh generation")
	flag.Parse()

	end := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	ds := synth.Generate(*seed, end)
	b1, corpus := synth.GenerateHub(*seed, ds)

	type page struct {
		Value any `json:"value"`
	}
	files := map[string]any{
		"synthetic_mloptic.json":        ds,
		"b1/BusinessPartners.json":      page{b1.BusinessPartners},
		"b1/PurchaseOrders.json":        page{b1.PurchaseOrders},
		"b1/PurchaseDeliveryNotes.json": page{b1.PurchaseDeliveryNotes},
		"emails.json":                   corpus,
	}
	stale := false
	for name, v := range files {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			panic(err)
		}
		b = append(b, '\n')
		path := filepath.Join(*dir, name)
		if *check {
			cur, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(cur, b) {
				fmt.Fprintf(os.Stderr, "%s is stale; run `go run ./cmd/gensynth`\n", path)
				stale = true
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			panic(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			panic(err)
		}
	}
	if stale {
		os.Exit(1)
	}
	if !*check {
		fmt.Printf("wrote %s: %d suppliers, %d POs, %d goods receipts, %d emails\n", *dir, len(ds.Suppliers), len(ds.POs), len(b1.PurchaseDeliveryNotes), len(corpus.Messages))
	}
}

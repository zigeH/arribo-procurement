// gensynth writes the deterministic synthetic dataset used by evals and the
// local demo. Run: go run ./cmd/gensynth  (use -check in CI to verify the
// committed file is up to date).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/zigeH/arribo-procurement/internal/synth"
)

func main() {
	out := flag.String("out", "evals/data/synthetic_mloptic.json", "output path")
	seed := flag.Int64("seed", 42, "random seed")
	check := flag.Bool("check", false, "fail if the file on disk differs from a fresh generation")
	flag.Parse()

	end := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	ds := synth.Generate(*seed, end)
	b, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		panic(err)
	}
	b = append(b, '\n')

	if *check {
		cur, err := os.ReadFile(*out)
		if err != nil || !bytes.Equal(cur, b) {
			fmt.Fprintf(os.Stderr, "%s is stale; run `go run ./cmd/gensynth`\n", *out)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %s: %d suppliers, %d POs\n", *out, len(ds.Suppliers), len(ds.POs))
}

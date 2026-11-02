// replay prints supplier scorecards for a dataset as of a date: the quickest
// way to sanity-check scoring changes or to produce a historical replay report.
//
//	go run ./cmd/replay [-file evals/data/synthetic_mloptic.json] [-as-of 2026-06-30] [-supplier SUP-02] [-json]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/zigeH/arribo-procurement/internal/scoring"
	"github.com/zigeH/arribo-procurement/internal/store"
)

func main() {
	file := flag.String("file", "evals/data/synthetic_mloptic.json", "dataset JSON")
	asOfS := flag.String("as-of", "", "YYYY-MM-DD (default: dataset generation date)")
	only := flag.String("supplier", "", "show full detail for one supplier")
	asJSON := flag.Bool("json", false, "print JSON")
	flag.Parse()

	ds, err := store.LoadDatasetFile(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	asOf := ds.Generated
	if *asOfS != "" {
		t, err := time.Parse("2006-01-02", *asOfS)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad -as-of:", err)
			os.Exit(1)
		}
		asOf = t.Add(24*time.Hour - time.Second)
	}
	cfg := scoring.DefaultConfig()

	var cards []scoring.Scorecard
	names := map[string]string{}
	for _, s := range ds.Suppliers {
		names[s.ID] = s.Name
		if *only != "" && s.ID != *only {
			continue
		}
		cards = append(cards, scoring.ScoreSupplier(s.ID, ds.POs, asOf, cfg))
	}
	sort.Slice(cards, func(i, j int) bool { return val(cards[i].Overall) > val(cards[j].Overall) })

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(cards)
		return
	}

	fmt.Printf("Supplier scorecards as of %s (%s)\n\n", asOf.Format("2006-01-02"), ds.Customer)
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SUPPLIER\tNAME\tOVERALL\tGRADE\tCONF\tDELIVERY\tPRICE\tLEAD\tQUALITY\tRESP\tFLAGS")
	for _, c := range cards {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s", c.SupplierID, names[c.SupplierID], f(c.Overall), c.Grade, c.Confidence)
		for _, d := range c.Dimensions {
			fmt.Fprintf(tw, "\t%s", f(d.Score))
		}
		fmt.Fprintf(tw, "\t%d\n", len(c.Flags))
	}
	tw.Flush()

	if *only != "" && len(cards) == 1 {
		c := cards[0]
		fmt.Println()
		for _, fl := range c.Flags {
			fmt.Printf("[%s] %s\n", fl.Severity, fl.Message)
		}
		for _, d := range c.Dimensions {
			fmt.Printf("\n%s: %s  (%s)\n", d.Dimension, f(d.Score), d.Summary)
			for i, e := range d.Evidence {
				if i == 5 {
					break
				}
				fmt.Printf("  - %s %s: %s\n", e.PONumber, e.LineID, e.Detail)
			}
		}
	}
}

func val(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}

func f(p *float64) string {
	if p == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.1f", *p)
}

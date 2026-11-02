// Package evals replays a dataset through the scorer and checks the results
// against known ground truth. Synthetic personas today; sanitized customer
// exports with buyer-labeled expectations later (see docs/data-spec.md).
//
// Any change to scoring must keep these passing. If a change intentionally
// alters behavior, update the expectation in the same PR and say why.
package evals

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/zigeH/arribo-procurement/internal/domain"
	"github.com/zigeH/arribo-procurement/internal/scoring"
	"github.com/zigeH/arribo-procurement/internal/synth"
)

func load(t *testing.T) domain.Dataset {
	t.Helper()
	b, err := os.ReadFile("data/synthetic_mloptic.json")
	if err != nil {
		t.Fatal(err)
	}
	var ds domain.Dataset
	if err := json.Unmarshal(b, &ds); err != nil {
		t.Fatal(err)
	}
	return ds
}

func scorecards(t *testing.T) map[string]scoring.Scorecard {
	ds := load(t)
	out := map[string]scoring.Scorecard{}
	for _, p := range synth.Personas {
		out[p.Key] = scoring.ScoreSupplier(p.ID, ds.POs, ds.Generated, scoring.DefaultConfig())
	}
	return out
}

func dim(t *testing.T, sc scoring.Scorecard, d scoring.Dimension) float64 {
	t.Helper()
	for _, x := range sc.Dimensions {
		if x.Dimension == d {
			if x.Score == nil {
				t.Fatalf("%s: %s has no score (%s)", sc.SupplierID, d, x.Summary)
			}
			return *x.Score
		}
	}
	t.Fatalf("%s: missing %s", sc.SupplierID, d)
	return 0
}

func hasFlag(sc scoring.Scorecard, code string) bool {
	for _, f := range sc.Flags {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestPersonaRecovery(t *testing.T) {
	sc := scorecards(t)
	for k, s := range sc {
		ov := "nil"
		if s.Overall != nil {
			ov = formatF(*s.Overall)
		}
		t.Logf("%-17s overall=%-5s grade=%s conf=%s lines=%d", k, ov, s.Grade, s.Confidence, s.LineCount)
	}

	type cmp struct {
		name    string
		d       scoring.Dimension
		low, hi string
	}
	for _, c := range []cmp{
		{"late supplier has worse delivery than reliable one", scoring.DimDelivery, "cheap_late", "reliable_premium"},
		{"late supplier has worse delivery than balanced one", scoring.DimDelivery, "cheap_late", "balanced"},
		{"overpriced supplier scores lower on price than balanced", scoring.DimPrice, "overpriced", "balanced"},
		{"premium supplier scores lower on price than cheap one", scoring.DimPrice, "reliable_premium", "cheap_late"},
		{"quality problem supplier scores lower on quality", scoring.DimQuality, "quality_problem", "balanced"},
		{"slow responder scores lower on responsiveness", scoring.DimResponsiveness, "slow_responder", "balanced"},
		{"slow responder quotes longer lead times", scoring.DimLeadTime, "slow_responder", "balanced"},
	} {
		lo, hi := dim(t, sc[c.low], c.d), dim(t, sc[c.hi], c.d)
		if lo >= hi {
			t.Errorf("%s: %s=%.1f should be < %s=%.1f", c.name, c.low, lo, c.hi, hi)
		}
	}

	if !hasFlag(sc["quality_problem"], "high_reject_rate") {
		t.Error("quality_problem should carry high_reject_rate flag")
	}
	// A critical weakness must show in the grade, not just in one dimension.
	for _, bad := range []string{"cheap_late", "quality_problem", "slow_responder", "overpriced"} {
		if g := sc[bad].Grade; g == "A" || g == "B" {
			t.Errorf("%s graded %s; a supplier with a critical weakness should be C or D", bad, g)
		}
	}
	if sc["new_supplier"].Confidence != "low" {
		t.Errorf("new_supplier confidence = %s, want low", sc["new_supplier"].Confidence)
	}
	for _, good := range []string{"reliable_premium", "balanced"} {
		for _, bad := range []string{"cheap_late", "quality_problem", "slow_responder"} {
			if *sc[good].Overall <= *sc[bad].Overall {
				t.Errorf("overall: %s (%.1f) should beat %s (%.1f)", good, *sc[good].Overall, bad, *sc[bad].Overall)
			}
		}
	}
}

func TestEveryScoreHasEvidence(t *testing.T) {
	for k, s := range scorecards(t) {
		for _, d := range s.Dimensions {
			if d.Score != nil && len(d.Evidence) == 0 && d.Dimension != scoring.DimQuality {
				t.Errorf("%s/%s scored without evidence", k, d.Dimension)
			}
		}
	}
}

func formatF(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

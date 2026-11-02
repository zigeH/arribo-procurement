// Package scoring rates suppliers and individual POs against a baseline built
// from the customer's own purchase history.
//
// Ground rules (see docs/scoring-methodology.md):
//   - Never invent a benchmark. A dimension with too few comparable data
//     points returns Score == nil ("insufficient data"), not a guess.
//   - Every score carries Evidence pointing at the PO lines it came from.
//   - Pure functions only: no I/O, no clock. Callers pass AsOf.
package scoring

import "time"

// Dimension is one axis of a supplier scorecard.
type Dimension string

const (
	DimDelivery       Dimension = "delivery"       // on-time delivery vs supplier-confirmed date
	DimPrice          Dimension = "price"          // unit price vs median for the same part
	DimLeadTime       Dimension = "lead_time"      // quoted lead time vs median for the same part
	DimQuality        Dimension = "quality"        // rejected / received quantity
	DimResponsiveness Dimension = "responsiveness" // hours from PO sent to supplier acknowledgment
)

// AllDimensions in display order.
var AllDimensions = []Dimension{DimDelivery, DimPrice, DimLeadTime, DimQuality, DimResponsiveness}

// Evidence ties a number back to the data it came from.
type Evidence struct {
	POID     string  `json:"po_id"`
	PONumber string  `json:"po_number,omitempty"`
	LineID   string  `json:"line_id,omitempty"`
	Detail   string  `json:"detail"`
	Value    float64 `json:"value"`
	Baseline float64 `json:"baseline,omitempty"`
	Score    float64 `json:"score"`
}

// DimensionScore is the result for one dimension. Score is nil when there is
// not enough data to score honestly.
type DimensionScore struct {
	Dimension Dimension  `json:"dimension"`
	Score     *float64   `json:"score"`
	Weight    float64    `json:"weight"`
	Samples   int        `json:"samples"`
	Summary   string     `json:"summary"`
	Evidence  []Evidence `json:"evidence,omitempty"`
}

// Flag is a notable finding that should be surfaced to a human.
type Flag struct {
	Severity string `json:"severity"` // "info" | "warn" | "alert"
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// Scorecard is a supplier-level rating over a time window.
type Scorecard struct {
	SupplierID  string           `json:"supplier_id"`
	AsOf        time.Time        `json:"as_of"`
	WindowStart time.Time        `json:"window_start"`
	Overall     *float64         `json:"overall"`
	Grade       string           `json:"grade"`      // A/B/C/D or "N/A"
	Confidence  string           `json:"confidence"` // low/medium/high, by sample size
	POCount     int              `json:"po_count"`
	LineCount   int              `json:"line_count"`
	Dimensions  []DimensionScore `json:"dimensions"`
	Flags       []Flag           `json:"flags,omitempty"`
}

// LineCheck compares one PO line against the baseline.
type LineCheck struct {
	LineID        string   `json:"line_id"`
	PartNumber    string   `json:"part_number"`
	UnitPrice     float64  `json:"unit_price"`
	PriceBaseline *float64 `json:"price_baseline"` // median of comparable lines; nil if too few
	PriceRatio    *float64 `json:"price_ratio"`    // unit price / baseline
	PriceVerdict  string   `json:"price_verdict"`  // below | in_range | above | far_above | no_baseline
	PriceSamples  int      `json:"price_samples"`
	LeadDays      float64  `json:"lead_days"`
	LeadBaseline  *float64 `json:"lead_baseline"`
	LeadRatio     *float64 `json:"lead_ratio"`
	LeadVerdict   string   `json:"lead_verdict"` // faster | in_range | slower | much_slower | no_baseline
	LeadSamples   int      `json:"lead_samples"`
}

// TermCheck is the result of checking a PO term against customer policy.
type TermCheck struct {
	Rule    string `json:"rule"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
	Source  string `json:"source"` // where the rule comes from; required
}

// POAssessment rates a single PO: are its prices, lead times and terms in
// line with the baseline, and how reliable is this supplier historically.
type POAssessment struct {
	POID            string      `json:"po_id"`
	PONumber        string      `json:"po_number"`
	SupplierID      string      `json:"supplier_id"`
	Score           *float64    `json:"score"`
	Verdict         string      `json:"verdict"` // ok | review | escalate
	Lines           []LineCheck `json:"lines"`
	Terms           []TermCheck `json:"terms"`
	SupplierOverall *float64    `json:"supplier_overall"`
	SupplierGrade   string      `json:"supplier_grade"`
	Flags           []Flag      `json:"flags,omitempty"`
}

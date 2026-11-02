package scoring

// Config controls scoring. Defaults are deliberately conservative; per-customer
// overrides should live in customer config, not in code.
type Config struct {
	// WindowMonths is how much history counts toward a supplier scorecard.
	WindowMonths int
	// Weights per dimension. Missing dimensions are dropped and the remaining
	// weights renormalized.
	Weights map[Dimension]float64
	// OnTimeGraceDays: a line delivered up to this many days after the
	// promised date still counts as on time.
	OnTimeGraceDays int
	// MinComparables: minimum number of other lines for the same part needed
	// before we publish a price or lead-time baseline.
	MinComparables int
	// PriceInRangePct: within +/- this fraction of baseline is "in_range".
	PriceInRangePct float64
	// LeadInRangePct: same for lead time.
	LeadInRangePct float64
	// Policy holds customer purchasing rules used for term checks.
	Policy Policy
}

// Policy is the customer's purchasing policy. Every rule must name a Source.
// Rules with an empty value are skipped.
type Policy struct {
	Source             string   // e.g. "MLOptic purchasing policy (confirmed by <name>, <date>)"
	AllowedIncoterms   []string // empty = no check
	MinPaymentNetDays  int      // 0 = no check; parses "Net N"
	MaxDepositPct      int      // 0 = no check; parses "N% deposit"
	RequiredCurrencies []string // empty = no check
}

// DefaultConfig returns the starting configuration. The policy is empty on
// purpose: term rules must come from the customer, not from us.
func DefaultConfig() Config {
	return Config{
		WindowMonths: 18,
		Weights: map[Dimension]float64{
			DimDelivery:       0.35,
			DimPrice:          0.25,
			DimQuality:        0.20,
			DimLeadTime:       0.10,
			DimResponsiveness: 0.10,
		},
		OnTimeGraceDays: 2,
		MinComparables:  3,
		PriceInRangePct: 0.10,
		LeadInRangePct:  0.20,
	}
}

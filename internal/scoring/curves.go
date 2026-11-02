package scoring

import "fmt"

// Score curves map a raw measurement to 0..100. They are documented in
// docs/scoring-methodology.md; change both together.

// priceScore: at or below baseline = 100; every 1% above baseline costs 2.5
// points (20% above = 50, 40% above = 0).
func priceScore(ratio float64) float64 {
	if ratio <= 1 {
		return 100
	}
	return clamp(100-(ratio-1)*250, 0, 100)
}

// leadScore: at or below baseline = 100; every 1% slower costs 1.5 points
// (quoted lead time ~67% longer than usual = 0).
func leadScore(ratio float64) float64 {
	if ratio <= 1 {
		return 100
	}
	return clamp(100-(ratio-1)*150, 0, 100)
}

// qualityScore: every 0.1% rejected costs 1 point (1% = 90, 5% = 50, 10% = 0).
func qualityScore(rejectRate float64) float64 {
	return clamp(100-rejectRate*1000, 0, 100)
}

// ackScore: hours from PO sent to supplier acknowledgment.
// <=24h 100; 24-72h falls to 70; 72-168h falls to 20; >168h = 10.
func ackScore(hours float64) float64 {
	switch {
	case hours <= 24:
		return 100
	case hours <= 72:
		return 100 - (hours-24)/48*30
	case hours <= 168:
		return 70 - (hours-72)/96*50
	default:
		return 10
	}
}

func grade(overall float64) string {
	switch {
	case overall >= 85:
		return "A"
	case overall >= 70:
		return "B"
	case overall >= 55:
		return "C"
	default:
		return "D"
	}
}

// capGrade stops a weighted average from hiding a critical weakness: any
// weighted dimension below 60 caps the grade at C, below 30 at D. The overall
// number is left as computed; the cap is explained in a flag.
func capGrade(sc *Scorecard) {
	worst, worstDim := 101.0, Dimension("")
	for _, d := range sc.Dimensions {
		if d.Score != nil && d.Weight > 0 && *d.Score < worst {
			worst, worstDim = *d.Score, d.Dimension
		}
	}
	limit := ""
	switch {
	case worst < 30:
		limit = "D"
	case worst < 60:
		limit = "C"
	}
	if limit != "" && sc.Grade < limit { // "A" < "B" < "C" < "D"
		sc.Flags = append(sc.Flags, Flag{Severity: "warn", Code: "grade_capped",
			Message: fmt.Sprintf("grade capped at %s (was %s): %s score is %.1f", limit, sc.Grade, worstDim, worst)})
		sc.Grade = limit
	}
}

func confidence(lines int) string {
	switch {
	case lines < 10:
		return "low"
	case lines < 30:
		return "medium"
	default:
		return "high"
	}
}

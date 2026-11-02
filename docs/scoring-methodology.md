# Supplier & PO scoring methodology (v0)

Purpose: give buyers a consistent, explainable way to judge suppliers and
individual POs, replacing ad-hoc judgment. Everything is computed from the
customer's own purchase history; we do not use external "industry averages"
unless a source is configured and cited.

## Baseline

For a PO line, the **price baseline** is the median unit price paid to **other
suppliers** for the same part number and currency, ordered within the window
(default 18 months). A supplier is never benchmarked against its own history
(that always looks "normal" and hides a premium). With fewer than
`MinComparables` (default 3) such purchases, the line is "no_baseline" and is
not price-scored: typical for single-sourced custom parts.

The **lead-time baseline** is the median quoted lead time (first confirmed date
− order date) of other suppliers' **repeat** orders of the same part. A
supplier's first order of a part (first article) is excluded on both sides,
since new parts legitimately take longer.

**On-time delivery** is measured against the **first** date the supplier
confirmed (`original_promised_date` when the date was later moved). Moved dates
are shown in evidence and flagged when frequent.

All scoring is "as of" a date: receipts and acknowledgments after that date
are ignored, so historical replays show what was knowable at the time.

## Supplier scorecard dimensions

| Dimension | Measure | Curve (0–100) | Weight |
|---|---|---|---|
| Delivery | Line delivered (full qty received) by first confirmed date + 2 days grace; open lines past due count as late | 100 on time, 0 late; averaged over lines | 0.35 |
| Price | unit price / other suppliers' median for the same part | ≤1.0 → 100; −2.5 pts per 1% above (1.2 → 50, 1.4 → 0); spend-weighted | 0.25 |
| Quality | rejected / received units | 100 − 1 pt per 0.1% (1% → 90, 5% → 50) | 0.20 |
| Lead time | quoted days (first confirmed − order date) / other suppliers' repeat-order median; first orders excluded | ≤1.0 → 100; −1.5 pts per 1% slower | 0.10 |
| Responsiveness | hours from PO sent to acknowledgment | ≤24h 100 → 72h 70 → 168h 20; >168h 10; unacked >72h 0 | 0.10 |

**Overall** = weighted mean of dimensions that have a score (weights renormalized).

**Grade**: A ≥ 85, B ≥ 70, C ≥ 55, else D. **Cap rule**: any weighted dimension
below 60 caps the grade at C, below 30 at D (flagged `grade_capped`), so a
weighted average cannot hide a critical weakness.

**Confidence** by lines in window: < 10 low, < 30 medium, else high.

Flags: `overdue_open_lines` (alert), `high_reject_rate` (>3%, warn),
`unacknowledged_pos` (warn), `price_no_baseline` (info), `grade_capped` (warn),
`frequent_date_changes` (warn, >20% of due lines had the date moved).

## PO assessment

Per line: price and lead-time verdicts vs baseline
- price: `below` (< −10%), `in_range` (±10%), `above` (+10–20%), `far_above` (> +20%)
- lead: `faster` (< −20%), `in_range` (±20%), `slower` (+20–40%), `much_slower` (> +40%),
  `first_order` (supplier's first order of this part: not compared)

Terms are checked only against rules in the customer's `Policy`, each citing
its source. Payment terms parse both "Net N" and "月结N天". With no policy configured, no term checks run.

PO score = weighted mean of: price (0.40), lead time (0.20), terms (0.10),
supplier historical overall (0.30), over the parts that are available.

Verdict: `escalate` if any line is far_above / much_slower, any term fails, or
the supplier grade is D; `review` if any line is above / slower, supplier grade
is C, or no line could be price-benchmarked; else `ok`.

## Open calibration questions (for MLOptic buyers)

- Grace days for "on time" (2 days today)? Different for domestic vs overseas?
- Is +10% the right "in range" band for optics parts? For coatings/services?
- Which failures matter most: late delivery or quality? (sets weights)
- Purchasing policy: allowed Incoterms, minimum Net days, maximum deposit.
- Single-sourced custom parts can't be price-benchmarked against other
  suppliers. Should we compare by spec (material, size, tolerance, coating),
  track each part's own price trend, or use a should-cost model?
- Is "first confirmed date" the right on-time reference for MLOptic, or do
  they accept supplier date changes they approved?

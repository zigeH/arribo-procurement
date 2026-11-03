import type { SupplierRow } from "./api";

/** Formats a 0–100 score. Null means "not enough data" — never 0. */
export function formatScore(score: number | null): string {
  return score === null ? "Not enough data" : score.toFixed(0);
}

/** Sorts by overall score descending; suppliers without a score go last, by name. */
export function sortByOverallDesc(rows: SupplierRow[]): SupplierRow[] {
  return [...rows].sort((a, b) => {
    if (a.overall === null && b.overall === null) return a.name.localeCompare(b.name);
    if (a.overall === null) return 1;
    if (b.overall === null) return -1;
    return b.overall - a.overall || a.name.localeCompare(b.name);
  });
}

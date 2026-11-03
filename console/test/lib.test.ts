// Unit tests for the pure helpers. Run with `npm test` (node --test, built-in type stripping).
import assert from "node:assert/strict";
import { test } from "node:test";
import type { SupplierRow } from "../lib/api.ts";
import { parseAsOf, withAsOf } from "../lib/asof.ts";
import { formatScore, sortByOverallDesc } from "../lib/format.ts";

test("parseAsOf accepts only real YYYY-MM-DD dates", () => {
  assert.equal(parseAsOf("2026-01-15"), "2026-01-15");
  assert.equal(parseAsOf("2024-02-29"), "2024-02-29");
  for (const bad of [null, undefined, "", "2026-1-15", "2026-02-30", "2025-02-29", "15/01/2026", "2026-01-15T00:00:00Z"]) {
    assert.equal(parseAsOf(bad), null, String(bad));
  }
});

test("withAsOf appends the param only when set", () => {
  assert.equal(withAsOf("/suppliers", "2026-01-15"), "/suppliers?as_of=2026-01-15");
  assert.equal(withAsOf("/suppliers", null), "/suppliers");
});

test("formatScore never renders a missing score as 0", () => {
  assert.equal(formatScore(null), "Not enough data");
  assert.equal(formatScore(0), "0");
  assert.equal(formatScore(87.6), "88");
});

const row = (name: string, overall: number | null): SupplierRow => ({
  id: name,
  name,
  overall,
  grade: overall === null ? "N/A" : "B",
  confidence: "medium",
  po_count: 1,
  alerts: 0,
});

test("sortByOverallDesc: highest first, unscored last by name, input untouched", () => {
  const input = [row("Zulu", null), row("Bravo", 70), row("Alpha", null), row("Charlie", 90), row("Delta", 70), row("Echo", 0)];
  const sorted = sortByOverallDesc(input).map((r) => r.name);
  assert.deepEqual(sorted, ["Charlie", "Bravo", "Delta", "Echo", "Alpha", "Zulu"]);
  assert.equal(input[0].name, "Zulu");
});

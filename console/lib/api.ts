// Typed client for the Arribo Go API.
//
// Types mirror the Go JSON shapes in internal/scoring/types.go and
// internal/api/api.go. Nullable scores stay `number | null`: a null score
// means "not enough data" and must never be shown as 0 (CLAUDE.md hard rule 1).

export const API_URL = (process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080").replace(/\/+$/, "");

/** ISO date string, YYYY-MM-DD. */
export type ISODate = string;
/** RFC 3339 timestamp as encoded by Go's time.Time. */
export type Timestamp = string;

export type Severity = "info" | "warn" | "alert";
export type Grade = "A" | "B" | "C" | "D" | "N/A";
export type Confidence = "low" | "medium" | "high" | (string & {});

export type Dimension = "delivery" | "price" | "lead_time" | "quality" | "responsiveness";

export interface Evidence {
  po_id: string;
  po_number?: string;
  line_id?: string;
  detail: string;
  value: number;
  baseline?: number;
  score: number;
}

export interface DimensionScore {
  dimension: Dimension;
  score: number | null;
  weight: number;
  samples: number;
  summary: string;
  evidence?: Evidence[];
}

export interface Flag {
  severity: Severity;
  code: string;
  message: string;
}

export interface Scorecard {
  supplier_id: string;
  as_of: Timestamp;
  window_start: Timestamp;
  overall: number | null;
  grade: Grade | (string & {});
  confidence: Confidence;
  po_count: number;
  line_count: number;
  dimensions: DimensionScore[];
  flags?: Flag[];
}

export type PriceVerdict = "below" | "in_range" | "above" | "far_above" | "no_baseline";
export type LeadVerdict = "faster" | "in_range" | "slower" | "much_slower" | "no_baseline";

export interface LineCheck {
  line_id: string;
  part_number: string;
  unit_price: number;
  price_baseline: number | null;
  price_ratio: number | null;
  price_verdict: PriceVerdict;
  price_samples: number;
  lead_days: number;
  lead_baseline: number | null;
  lead_ratio: number | null;
  lead_verdict: LeadVerdict;
  lead_samples: number;
}

export interface TermCheck {
  rule: string;
  passed: boolean;
  message: string;
  source: string;
}

export interface POAssessment {
  po_id: string;
  po_number: string;
  supplier_id: string;
  score: number | null;
  verdict: "ok" | "review" | "escalate";
  lines: LineCheck[];
  terms: TermCheck[] | null;
  supplier_overall: number | null;
  supplier_grade: string;
  flags?: Flag[];
}

/** One row of GET /v1/suppliers (api.supplierRow). */
export interface SupplierRow {
  id: string;
  name: string;
  country?: string;
  overall: number | null;
  grade: Grade | (string & {});
  confidence: Confidence;
  po_count: number;
  /** Count of warn + alert flags on the supplier's scorecard. */
  alerts: number;
}

export interface SupplierList {
  as_of: Timestamp;
  suppliers: SupplierRow[];
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number | null, // null = network error / API unreachable
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function get<T>(path: string, asOf?: ISODate | null): Promise<T> {
  const url = new URL(API_URL + path);
  if (asOf) url.searchParams.set("as_of", asOf);
  let res: Response;
  try {
    res = await fetch(url, { cache: "no-store", headers: { Accept: "application/json" } });
  } catch {
    throw new ApiError(`Cannot reach the API at ${API_URL}`, null);
  }
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) msg = body.error;
    } catch {
      // non-JSON error body; keep status text
    }
    throw new ApiError(msg, res.status);
  }
  return (await res.json()) as T;
}

export const api = {
  health: () => get<{ status: string }>("/healthz"),
  suppliers: (asOf?: ISODate | null) => get<SupplierList>("/v1/suppliers", asOf),
  scorecard: (supplierId: string, asOf?: ISODate | null) =>
    get<Scorecard>(`/v1/suppliers/${encodeURIComponent(supplierId)}/scorecard`, asOf),
  poAssessment: (poId: string, asOf?: ISODate | null) =>
    get<POAssessment>(`/v1/pos/${encodeURIComponent(poId)}/assessment`, asOf),
};

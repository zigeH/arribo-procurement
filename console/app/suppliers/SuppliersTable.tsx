"use client";

import { useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { GradeBadge } from "@/components/GradeBadge";
import { ApiError, api, type SupplierRow } from "@/lib/api";
import { parseAsOf } from "@/lib/asof";
import { formatScore, sortByOverallDesc } from "@/lib/format";

type State =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ok"; asOf: string; rows: SupplierRow[] };

export function SuppliersTable() {
  const asOf = parseAsOf(useSearchParams().get("as_of"));
  const [state, setState] = useState<State>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    api.suppliers(asOf).then(
      (res) => !cancelled && setState({ kind: "ok", asOf: res.as_of, rows: sortByOverallDesc(res.suppliers) }),
      (err: unknown) =>
        !cancelled && setState({ kind: "error", message: err instanceof ApiError ? err.message : String(err) }),
    );
    return () => {
      cancelled = true;
    };
  }, [asOf]);

  if (state.kind === "loading") return <p className="text-sm text-zinc-500">Loading…</p>;
  if (state.kind === "error") return <p className="text-sm text-red-700">Could not load suppliers: {state.message}</p>;
  if (state.rows.length === 0) return <p className="text-sm text-zinc-500">No suppliers yet.</p>;

  return (
    <>
      <p className="mb-3 text-xs text-zinc-400">As of {state.asOf.slice(0, 10)}</p>
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-zinc-200 text-left text-xs uppercase tracking-wide text-zinc-500">
            <th className="py-2 pr-4 font-medium">Supplier</th>
            <th className="py-2 pr-4 text-right font-medium">Overall</th>
            <th className="py-2 pr-4 font-medium">Grade</th>
            <th className="py-2 pr-4 font-medium">Confidence</th>
            <th className="py-2 pr-4 text-right font-medium">POs</th>
            <th className="py-2 text-right font-medium">Alerts</th>
          </tr>
        </thead>
        <tbody>
          {state.rows.map((s) => (
            <tr key={s.id} className="border-b border-zinc-100">
              <td className="py-3 pr-4">
                <div className="font-medium text-zinc-900">{s.name}</div>
                {s.country && <div className="text-xs text-zinc-400">{s.country}</div>}
              </td>
              <td className={`py-3 pr-4 text-right tabular-nums ${s.overall === null ? "text-xs text-zinc-400" : ""}`}>
                {formatScore(s.overall)}
              </td>
              <td className="py-3 pr-4">
                <GradeBadge grade={s.grade} />
              </td>
              <td className="py-3 pr-4 capitalize text-zinc-600">{s.confidence}</td>
              <td className="py-3 pr-4 text-right tabular-nums">{s.po_count}</td>
              <td className={`py-3 text-right tabular-nums ${s.alerts > 0 ? "font-medium text-red-700" : "text-zinc-400"}`}>
                {s.alerts}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  );
}

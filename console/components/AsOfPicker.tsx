"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { parseAsOf } from "@/lib/asof";

/** Shared "As of" date, stored in the URL as ?as_of=YYYY-MM-DD. Empty = today (API default). */
export function AsOfPicker() {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const asOf = parseAsOf(params.get("as_of"));

  function update(value: string) {
    const next = new URLSearchParams(params.toString());
    if (parseAsOf(value)) next.set("as_of", value);
    else next.delete("as_of");
    const qs = next.toString();
    router.replace(qs ? `${pathname}?${qs}` : pathname);
  }

  return (
    <label className="flex items-center gap-2 text-sm text-zinc-600">
      As of
      <input
        type="date"
        value={asOf ?? ""}
        onChange={(e) => update(e.target.value)}
        className="rounded-md border border-zinc-300 px-2 py-1 text-zinc-900 focus:border-zinc-500 focus:outline-none"
      />
      {asOf && (
        <button type="button" onClick={() => update("")} className="text-xs text-zinc-500 hover:text-zinc-900">
          Today
        </button>
      )}
    </label>
  );
}

"use client";

import { useEffect, useState } from "react";
import { API_URL, api } from "@/lib/api";

const POLL_MS = 15_000;

/** Shows a banner while GET /healthz fails, so empty pages are not mistaken for "no data". */
export function ApiStatusBanner() {
  const [down, setDown] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const check = () =>
      api.health().then(
        () => !cancelled && setDown(false),
        () => !cancelled && setDown(true),
      );
    check();
    const id = setInterval(check, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);

  if (!down) return null;
  return (
    <div role="alert" className="border-b border-red-200 bg-red-50 px-8 py-2 text-sm text-red-800">
      API unreachable at <code className="font-mono">{API_URL}</code>. Start it with <code className="font-mono">make api</code>{" "}
      (and set <code className="font-mono">CORS_ORIGIN</code> to this console&apos;s origin).
    </div>
  );
}

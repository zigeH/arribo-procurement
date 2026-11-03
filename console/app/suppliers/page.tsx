import { Suspense } from "react";
import { SuppliersTable } from "./SuppliersTable";

export default function SuppliersPage() {
  return (
    <div className="max-w-5xl">
      <h1 className="text-xl font-semibold tracking-tight">Suppliers</h1>
      <p className="mt-1 text-sm text-zinc-500">Scored against your own purchase history.</p>
      <div className="mt-6">
        <Suspense fallback={<p className="text-sm text-zinc-500">Loading…</p>}>
          <SuppliersTable />
        </Suspense>
      </div>
    </div>
  );
}

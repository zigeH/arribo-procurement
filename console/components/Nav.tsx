"use client";

import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { parseAsOf, withAsOf } from "@/lib/asof";

const ITEMS = [
  { href: "/", label: "对不上清单", hint: "Discrepancies" },
  { href: "/suppliers", label: "Suppliers" },
];

export function Nav() {
  const pathname = usePathname();
  const asOf = parseAsOf(useSearchParams().get("as_of"));
  return (
    <nav className="flex flex-col gap-1 text-sm">
      {ITEMS.map((item) => {
        const active = item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
        return (
          <Link
            key={item.href}
            href={withAsOf(item.href, asOf)}
            className={`rounded-md px-2 py-1.5 ${
              active ? "bg-white font-medium text-zinc-900 shadow-sm ring-1 ring-zinc-200" : "text-zinc-600 hover:bg-zinc-100"
            }`}
          >
            {item.label}
            {item.hint && <span className="ml-2 text-xs text-zinc-400">{item.hint}</span>}
          </Link>
        );
      })}
    </nav>
  );
}

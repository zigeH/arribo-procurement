const STYLES: Record<string, string> = {
  A: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  B: "bg-sky-50 text-sky-700 ring-sky-200",
  C: "bg-amber-50 text-amber-700 ring-amber-200",
  D: "bg-red-50 text-red-700 ring-red-200",
};

export function GradeBadge({ grade }: { grade: string }) {
  const style = STYLES[grade] ?? "bg-zinc-50 text-zinc-500 ring-zinc-200";
  return (
    <span className={`inline-flex min-w-8 justify-center rounded-md px-2 py-0.5 text-xs font-medium ring-1 ring-inset ${style}`}>
      {grade || "N/A"}
    </span>
  );
}

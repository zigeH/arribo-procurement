// The "as of" date lives in the URL (?as_of=YYYY-MM-DD) so views are shareable
// and every page sees the same date. Absent = API default (now).

const ISO_DATE = /^\d{4}-\d{2}-\d{2}$/;

/** Returns the as_of param if it is a well-formed YYYY-MM-DD date, else null. */
export function parseAsOf(value: string | null | undefined): string | null {
  if (!value || !ISO_DATE.test(value)) return null;
  const d = new Date(value + "T00:00:00Z");
  return Number.isNaN(d.getTime()) || d.toISOString().slice(0, 10) !== value ? null : value;
}

/** Appends ?as_of= to an internal href when set. */
export function withAsOf(href: string, asOf: string | null): string {
  return asOf ? `${href}?as_of=${asOf}` : href;
}

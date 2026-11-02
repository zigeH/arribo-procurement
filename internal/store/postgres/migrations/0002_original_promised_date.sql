-- 0002: first supplier-confirmed date, kept when the promise is moved, and
-- when it was moved (so as-of replay only sees moves that had happened).
ALTER TABLE po_lines ADD COLUMN IF NOT EXISTS original_promised_date timestamptz;
ALTER TABLE po_lines ADD COLUMN IF NOT EXISTS promise_changed_at timestamptz;

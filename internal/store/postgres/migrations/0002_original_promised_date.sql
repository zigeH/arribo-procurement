-- 0002: first supplier-confirmed date, kept when the promise is moved.
ALTER TABLE po_lines ADD COLUMN IF NOT EXISTS original_promised_date timestamptz;

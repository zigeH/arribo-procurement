# Sanitized PO export: data spec for MLOptic

This is exactly what Arribo needs for the supplier/PO scoring demo, and what
can be removed or masked. It answers "which documents, which fields, how
sanitized".

## What we need

A CSV/Excel export (or several) covering the last **12–18 months** of
purchase orders. One row per **PO line** is easiest.

| Field | Required | Notes / how to sanitize |
|---|---|---|
| po_number | yes | Can be replaced by any consistent token (e.g. hash) |
| supplier_id | yes | Replace names with consistent aliases (Supplier A, B, …). Same supplier must keep the same alias |
| supplier_country | optional | Country only |
| order_date | yes | Date PO was issued |
| sent_date | optional | When PO was sent to supplier |
| acknowledged_date | optional | When supplier confirmed (enables responsiveness score) |
| line_no | yes | |
| part_number | yes | Internal part numbers can be hashed consistently; **the same part must keep the same token** so prices can be compared |
| category | optional | e.g. lens / prism / filter / mount / coating |
| quantity | yes | |
| unit_price | yes | May be multiplied by a constant secret factor across the whole file; we only use ratios |
| currency | yes | |
| promised_date | yes | Supplier-confirmed delivery date (latest) |
| original_promised_date | helpful | First date the supplier confirmed, if it was later changed (or a change log). Without it, a supplier that keeps moving dates looks punctual |
| spec fields | helpful | Material, diameter/size, tolerance, coating: lets us compare prices of single-sourced custom parts by spec |
| payment_terms | optional | e.g. "Net 30", "30% deposit" |
| incoterm | optional | e.g. FOB, EXW, DDP |

Receipts / inspection (separate sheet is fine), one row per delivery:

| Field | Required | Notes |
|---|---|---|
| po_number + line_no | yes | Same tokens as above |
| received_date | yes | |
| qty_received | yes | |
| qty_rejected | optional | Enables the quality score |

## What we do not need

Customer (end-buyer) names, product drawings, specifications, engineering
notes, contact names/emails, bank details, contract text.

## Handling

- Stored outside source control, encrypted at rest, access limited to the two
  founders; deleted on request.
- Never sent to third-party services other than the LLM provider under a
  no-training agreement, and only if needed (scoring itself uses no LLM).

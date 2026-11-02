// Package postgres implements store.Store on Postgres (pgx).
package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zigeH/arribo-procurement/internal/domain"
	"github.com/zigeH/arribo-procurement/internal/scoring"
	"github.com/zigeH/arribo-procurement/internal/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	Pool   *pgxpool.Pool
	Tenant string
}

var _ store.Store = (*Store)(nil)

func Open(ctx context.Context, url, tenant string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return &Store{Pool: pool, Tenant: tenant}, nil
}

// Migrate applies embedded migrations in filename order, once each.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, n := range names {
		var exists bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, n).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sql, err := migrations.ReadFile(n)
		if err != nil {
			return err
		}
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", n, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, n); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListSuppliers(ctx context.Context) ([]domain.Supplier, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, name, country FROM suppliers WHERE tenant_id=$1 ORDER BY id`, s.Tenant)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Supplier, error) {
		var sp domain.Supplier
		err := r.Scan(&sp.ID, &sp.Name, &sp.Country)
		return sp, err
	})
}

func (s *Store) GetSupplier(ctx context.Context, id string) (domain.Supplier, error) {
	var sp domain.Supplier
	err := s.Pool.QueryRow(ctx, `SELECT id, name, country FROM suppliers WHERE tenant_id=$1 AND id=$2`, s.Tenant, id).Scan(&sp.ID, &sp.Name, &sp.Country)
	if errors.Is(err, pgx.ErrNoRows) {
		return sp, store.ErrNotFound
	}
	return sp, err
}

func (s *Store) LoadHistory(ctx context.Context, since time.Time) ([]domain.PurchaseOrder, error) {
	return s.loadPOs(ctx, `po.order_date >= $2`, since)
}

func (s *Store) GetPO(ctx context.Context, id string) (domain.PurchaseOrder, error) {
	pos, err := s.loadPOs(ctx, `(po.id = $2 OR po.number = $2)`, id)
	if err != nil {
		return domain.PurchaseOrder{}, err
	}
	if len(pos) == 0 {
		return domain.PurchaseOrder{}, store.ErrNotFound
	}
	return pos[0], nil
}

// loadPOs loads POs matching cond ($1 = tenant, $2 = arg) with lines and receipts.
func (s *Store) loadPOs(ctx context.Context, cond string, arg any) ([]domain.PurchaseOrder, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT po.id, po.number, po.supplier_id, po.order_date, po.sent_at, po.acknowledged_at,
		       po.payment_terms, po.incoterm, po.currency
		FROM purchase_orders po
		WHERE po.tenant_id = $1 AND `+cond+`
		ORDER BY po.order_date, po.id`, s.Tenant, arg)
	if err != nil {
		return nil, err
	}
	var pos []domain.PurchaseOrder
	idx := map[string]int{}
	for rows.Next() {
		var p domain.PurchaseOrder
		var sent *time.Time
		if err := rows.Scan(&p.ID, &p.Number, &p.SupplierID, &p.OrderDate, &sent, &p.AcknowledgedAt, &p.PaymentTerms, &p.Incoterm, &p.Currency); err != nil {
			rows.Close()
			return nil, err
		}
		if sent != nil {
			p.SentAt = *sent
		}
		idx[p.ID] = len(pos)
		pos = append(pos, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(pos) == 0 {
		return pos, err
	}
	ids := make([]string, len(pos))
	for i, p := range pos {
		ids[i] = p.ID
	}

	lrows, err := s.Pool.Query(ctx, `
		SELECT l.id, l.po_id, l.line_no, l.part_number, l.category, l.description,
		       l.qty::float8, l.unit_price::float8, l.promised_date,
		       r.received_on, r.qty_received::float8, r.qty_rejected::float8
		FROM po_lines l
		LEFT JOIN receipts r ON r.tenant_id = l.tenant_id AND r.line_id = l.id
		WHERE l.tenant_id = $1 AND l.po_id = ANY($2)
		ORDER BY l.po_id, l.line_no, r.seq`, s.Tenant, ids)
	if err != nil {
		return nil, err
	}
	defer lrows.Close()
	lineIdx := map[string][2]int{} // line id -> (po index, line index)
	for lrows.Next() {
		var l domain.POLine
		var poID string
		var promised, recvOn *time.Time
		var qr, qj *float64
		if err := lrows.Scan(&l.ID, &poID, &l.LineNo, &l.PartNumber, &l.Category, &l.Description, &l.Qty, &l.UnitPrice, &promised, &recvOn, &qr, &qj); err != nil {
			return nil, err
		}
		pi := idx[poID]
		pos_ := &pos[pi]
		li, seen := lineIdx[l.ID]
		if !seen {
			if promised != nil {
				l.PromisedDate = *promised
			}
			pos_.Lines = append(pos_.Lines, l)
			li = [2]int{pi, len(pos_.Lines) - 1}
			lineIdx[l.ID] = li
		}
		if recvOn != nil {
			rc := domain.Receipt{Date: *recvOn}
			if qr != nil {
				rc.QtyReceived = *qr
			}
			if qj != nil {
				rc.QtyRejected = *qj
			}
			pos[li[0]].Lines[li[1]].Receipts = append(pos[li[0]].Lines[li[1]].Receipts, rc)
		}
	}
	return pos, lrows.Err()
}

func (s *Store) SaveScorecard(ctx context.Context, sc scoring.Scorecard) error {
	body, err := json.Marshal(sc)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO scorecards (tenant_id, supplier_id, as_of, overall, grade, body)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (tenant_id, supplier_id, as_of) DO UPDATE SET overall=EXCLUDED.overall, grade=EXCLUDED.grade, body=EXCLUDED.body`,
		s.Tenant, sc.SupplierID, sc.AsOf, sc.Overall, sc.Grade, body)
	return err
}

func (s *Store) LatestScorecard(ctx context.Context, supplierID string) (scoring.Scorecard, error) {
	var body []byte
	err := s.Pool.QueryRow(ctx, `SELECT body FROM scorecards WHERE tenant_id=$1 AND supplier_id=$2 ORDER BY as_of DESC LIMIT 1`, s.Tenant, supplierID).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return scoring.Scorecard{}, store.ErrNotFound
	}
	if err != nil {
		return scoring.Scorecard{}, err
	}
	var sc scoring.Scorecard
	err = json.Unmarshal(body, &sc)
	return sc, err
}

// ImportDataset upserts a dataset (used by cmd/seed and future customer imports).
func (s *Store) ImportDataset(ctx context.Context, ds domain.Dataset) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for _, sp := range ds.Suppliers {
		if _, err := tx.Exec(ctx, `INSERT INTO suppliers (tenant_id,id,name,country) VALUES ($1,$2,$3,$4)
			ON CONFLICT (tenant_id,id) DO UPDATE SET name=EXCLUDED.name, country=EXCLUDED.country`,
			s.Tenant, sp.ID, sp.Name, sp.Country); err != nil {
			return err
		}
	}
	for _, p := range ds.POs {
		var sent *time.Time
		if !p.SentAt.IsZero() {
			sent = &p.SentAt
		}
		if _, err := tx.Exec(ctx, `INSERT INTO purchase_orders (tenant_id,id,number,supplier_id,order_date,sent_at,acknowledged_at,payment_terms,incoterm,currency)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (tenant_id,id) DO UPDATE SET number=EXCLUDED.number, supplier_id=EXCLUDED.supplier_id, order_date=EXCLUDED.order_date,
			  sent_at=EXCLUDED.sent_at, acknowledged_at=EXCLUDED.acknowledged_at, payment_terms=EXCLUDED.payment_terms,
			  incoterm=EXCLUDED.incoterm, currency=EXCLUDED.currency`,
			s.Tenant, p.ID, p.Number, p.SupplierID, p.OrderDate, sent, p.AcknowledgedAt, p.PaymentTerms, p.Incoterm, p.Currency); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM po_lines WHERE tenant_id=$1 AND po_id=$2`, s.Tenant, p.ID); err != nil {
			return err
		}
		for _, l := range p.Lines {
			var promised *time.Time
			if !l.PromisedDate.IsZero() {
				promised = &l.PromisedDate
			}
			if _, err := tx.Exec(ctx, `INSERT INTO po_lines (tenant_id,id,po_id,line_no,part_number,category,description,qty,unit_price,promised_date)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
				s.Tenant, l.ID, p.ID, l.LineNo, l.PartNumber, l.Category, l.Description, l.Qty, l.UnitPrice, promised); err != nil {
				return err
			}
			for i, r := range l.Receipts {
				if _, err := tx.Exec(ctx, `INSERT INTO receipts (tenant_id,line_id,seq,received_on,qty_received,qty_rejected) VALUES ($1,$2,$3,$4,$5,$6)`,
					s.Tenant, l.ID, i+1, r.Date, r.QtyReceived, r.QtyRejected); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit(ctx)
}

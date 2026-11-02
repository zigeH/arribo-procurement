// Package workflows holds Temporal workflows and activities.
//
// Rules (also in CLAUDE.md):
//   - Workflow code is deterministic: no time.Now, no I/O, no maps iterated for
//     ordering, no goroutines. Use workflow.Now / workflow.Go / activities.
//   - All I/O and all scoring happen in activities.
//   - Changing a workflow's command sequence requires workflow.GetVersion.
package workflows

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/zigeH/arribo-procurement/internal/service"
)

const TaskQueue = "arribo-scoring"

// ScoreAllSuppliersInput: AsOf zero means "workflow start time".
type ScoreAllSuppliersInput struct {
	AsOf time.Time
}

type SupplierResult struct {
	SupplierID string
	Overall    *float64
	Grade      string
	Flags      int
}

type ScoreAllSuppliersResult struct {
	AsOf    time.Time
	Results []SupplierResult
	Failed  []string
}

// ScoreAllSuppliersWorkflow snapshots a scorecard for every supplier.
// Fan-out is bounded so a big supplier base doesn't flood the store.
func ScoreAllSuppliersWorkflow(ctx workflow.Context, in ScoreAllSuppliersInput) (ScoreAllSuppliersResult, error) {
	asOf := in.AsOf
	if asOf.IsZero() {
		asOf = workflow.Now(ctx)
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    5,
		},
	})

	var a *Activities
	var ids []string
	if err := workflow.ExecuteActivity(ctx, a.ListSupplierIDs).Get(ctx, &ids); err != nil {
		return ScoreAllSuppliersResult{}, err
	}

	const maxParallel = 8
	out := ScoreAllSuppliersResult{AsOf: asOf}
	for start := 0; start < len(ids); start += maxParallel {
		end := min(start+maxParallel, len(ids))
		futures := make([]workflow.Future, 0, end-start)
		for _, id := range ids[start:end] {
			futures = append(futures, workflow.ExecuteActivity(ctx, a.ScoreAndSaveSupplier, id, asOf))
		}
		for i, f := range futures {
			var r SupplierResult
			if err := f.Get(ctx, &r); err != nil {
				workflow.GetLogger(ctx).Error("supplier scoring failed", "supplier", ids[start+i], "err", err)
				out.Failed = append(out.Failed, ids[start+i])
				continue
			}
			out.Results = append(out.Results, r)
		}
	}
	return out, nil
}

// Activities wraps the service layer.
type Activities struct {
	Svc *service.Service
}

func (a *Activities) ListSupplierIDs(ctx context.Context) ([]string, error) {
	sups, err := a.Svc.Store.ListSuppliers(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(sups))
	for i, s := range sups {
		ids[i] = s.ID
	}
	return ids, nil
}

func (a *Activities) ScoreAndSaveSupplier(ctx context.Context, supplierID string, asOf time.Time) (SupplierResult, error) {
	sc, err := a.Svc.Scorecard(ctx, supplierID, asOf)
	if err != nil {
		return SupplierResult{}, fmt.Errorf("score %s: %w", supplierID, err)
	}
	if err := a.Svc.Store.SaveScorecard(ctx, sc); err != nil {
		return SupplierResult{}, fmt.Errorf("save %s: %w", supplierID, err)
	}
	return SupplierResult{SupplierID: supplierID, Overall: sc.Overall, Grade: sc.Grade, Flags: len(sc.Flags)}, nil
}

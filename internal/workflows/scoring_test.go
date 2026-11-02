package workflows

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/zigeH/arribo-procurement/internal/scoring"
	"github.com/zigeH/arribo-procurement/internal/service"
	"github.com/zigeH/arribo-procurement/internal/store"
)

func TestScoreAllSuppliersWorkflow(t *testing.T) {
	ds, err := store.LoadDatasetFile("../../evals/data/synthetic_mloptic.json")
	require.NoError(t, err)
	mem := store.NewMemory(ds)
	acts := &Activities{Svc: &service.Service{Store: mem, Cfg: scoring.DefaultConfig()}}

	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivity(acts)
	env.ExecuteWorkflow(ScoreAllSuppliersWorkflow, ScoreAllSuppliersInput{AsOf: ds.Generated})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var res ScoreAllSuppliersResult
	require.NoError(t, env.GetWorkflowResult(&res))
	require.Len(t, res.Results, len(ds.Suppliers))
	require.Empty(t, res.Failed)

	sc, err := mem.LatestScorecard(t.Context(), "SUP-01")
	require.NoError(t, err)
	require.WithinDuration(t, ds.Generated, sc.AsOf, time.Second)
}

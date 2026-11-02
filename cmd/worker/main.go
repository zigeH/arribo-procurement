// worker runs Temporal workflows and activities. Requires TEMPORAL_ADDRESS.
package main

import (
	"context"
	"log/slog"
	"os"

	"go.temporal.io/sdk/worker"

	"github.com/zigeH/arribo-procurement/internal/bootstrap"
	"github.com/zigeH/arribo-procurement/internal/temporalx"
	"github.com/zigeH/arribo-procurement/internal/workflows"
)

func main() {
	if !temporalx.Enabled() {
		slog.Error("TEMPORAL_ADDRESS is required (local: `make temporal` then TEMPORAL_ADDRESS=localhost:7233)")
		os.Exit(1)
	}
	ctx := context.Background()
	svc, closeStore, err := bootstrap.Service(ctx)
	if err != nil {
		slog.Error("init store", "err", err)
		os.Exit(1)
	}
	defer closeStore()

	c, err := temporalx.Dial()
	if err != nil {
		slog.Error("temporal dial", "err", err)
		os.Exit(1)
	}
	defer c.Close()

	w := worker.New(c, workflows.TaskQueue, worker.Options{})
	w.RegisterWorkflow(workflows.ScoreAllSuppliersWorkflow)
	w.RegisterActivity(&workflows.Activities{Svc: svc})

	slog.Info("worker started", "task_queue", workflows.TaskQueue)
	if err := w.Run(worker.InterruptCh()); err != nil {
		slog.Error("worker", "err", err)
		os.Exit(1)
	}
}

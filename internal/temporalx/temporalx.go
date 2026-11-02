// Package temporalx builds Temporal clients from env and adapts them to the
// interfaces other packages use.
//
// Env:
//
//	TEMPORAL_ADDRESS    host:port (local dev: localhost:7233; Cloud: <ns>.<acct>.tmprl.cloud:7233)
//	TEMPORAL_NAMESPACE  default "default"
//	TEMPORAL_API_KEY    set for Temporal Cloud (enables TLS + API key auth)
package temporalx

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/zigeH/arribo-procurement/internal/workflows"
)

// Enabled reports whether Temporal is configured.
func Enabled() bool { return os.Getenv("TEMPORAL_ADDRESS") != "" }

// Dial connects using env configuration.
func Dial() (client.Client, error) {
	opts := client.Options{
		HostPort:  os.Getenv("TEMPORAL_ADDRESS"),
		Namespace: envOr("TEMPORAL_NAMESPACE", "default"),
	}
	if key := os.Getenv("TEMPORAL_API_KEY"); key != "" {
		opts.Credentials = client.NewAPIKeyStaticCredentials(key)
		opts.ConnectionOptions = client.ConnectionOptions{TLS: &tls.Config{}}
	}
	return client.Dial(opts)
}

// Starter adapts a Temporal client to api.RunStarter.
type Starter struct{ C client.Client }

func (s Starter) StartScoringRun(ctx context.Context, asOf time.Time) (string, error) {
	id := fmt.Sprintf("score-all-%s", asOf.UTC().Format("20060102-150405"))
	run, err := s.C.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        id,
		TaskQueue: workflows.TaskQueue,
	}, workflows.ScoreAllSuppliersWorkflow, workflows.ScoreAllSuppliersInput{AsOf: asOf})
	if err != nil {
		return "", err
	}
	return run.GetID() + "/" + run.GetRunID(), nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

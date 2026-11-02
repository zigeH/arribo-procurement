// Package bootstrap wires config from env for the binaries.
//
// Env:
//
//	DATABASE_URL  Postgres URL. If empty, an in-memory store is loaded from DATASET.
//	DATASET       JSON dataset for the in-memory store (default evals/data/synthetic_mloptic.json)
//	TENANT_ID     tenant for the Postgres store (default "mloptic")
package bootstrap

import (
	"context"
	"log/slog"
	"os"

	"github.com/zigeH/arribo-procurement/internal/connectors/erp"
	"github.com/zigeH/arribo-procurement/internal/extract"
	"github.com/zigeH/arribo-procurement/internal/inbox"
	"github.com/zigeH/arribo-procurement/internal/timeline"

	"github.com/zigeH/arribo-procurement/internal/scoring"
	"github.com/zigeH/arribo-procurement/internal/service"
	"github.com/zigeH/arribo-procurement/internal/store"
	"github.com/zigeH/arribo-procurement/internal/store/postgres"
)

func Env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Service builds the service with the configured store.
func Service(ctx context.Context) (*service.Service, func(), error) {
	cfg := scoring.DefaultConfig()
	if url := os.Getenv("DATABASE_URL"); url != "" {
		pg, err := postgres.Open(ctx, url, Env("TENANT_ID", "mloptic"))
		if err != nil {
			return nil, nil, err
		}
		if err := pg.Migrate(ctx); err != nil {
			return nil, nil, err
		}
		slog.Info("store: postgres", "tenant", pg.Tenant)
		return &service.Service{Store: pg, Cfg: cfg}, pg.Pool.Close, nil
	}
	path := Env("DATASET", "evals/data/synthetic_mloptic.json")
	ds, err := store.LoadDatasetFile(path)
	if err != nil {
		return nil, nil, err
	}
	slog.Info("store: in-memory", "dataset", path, "pos", len(ds.POs))
	return &service.Service{Store: store.NewMemory(ds), Cfg: cfg}, func() {}, nil
}

// Hub builds the PO data hub from env, or returns nil if not configured.
//
//	HUB_ERP         fixtures (default when HUB_B1_DIR exists) | servicelayer | off
//	HUB_B1_DIR      B1-shaped JSON export dir (default evals/data/b1)
//	HUB_MESSAGES    message corpus JSON (default evals/data/emails.json)
//	SAPB1_URL, SAPB1_COMPANY, SAPB1_USER, SAPB1_PASSWORD   for servicelayer (read-only user)
//	ARRIBO_EXTRACTOR rules (default) | llm (needs ANTHROPIC_API_KEY and ARRIBO_LLM_APPROVED)
func Hub(ctx context.Context) (*timeline.Hub, error) {
	var r erp.Reader
	switch mode := Env("HUB_ERP", "fixtures"); mode {
	case "off":
		return nil, nil
	case "servicelayer":
		r = &erp.ServiceLayer{BaseURL: os.Getenv("SAPB1_URL"), CompanyDB: os.Getenv("SAPB1_COMPANY"),
			UserName: os.Getenv("SAPB1_USER"), Password: os.Getenv("SAPB1_PASSWORD")}
	default:
		dir := Env("HUB_B1_DIR", "evals/data/b1")
		if _, err := os.Stat(dir); err != nil {
			slog.Warn("PO data hub disabled: no ERP fixtures", "dir", dir)
			return nil, nil
		}
		r = erp.FixtureReader{Dir: dir}
	}
	var msgs []inbox.Message
	defMsgs := ""
	if _, ok := r.(erp.FixtureReader); ok {
		defMsgs = "evals/data/emails.json" // synthetic corpus only goes with synthetic ERP data
	}
	if p := Env("HUB_MESSAGES", defMsgs); p != "" {
		c, err := inbox.LoadCorpus(p)
		if err != nil {
			return nil, err
		}
		msgs = c.Messages
	}
	var ex extract.Extractor = extract.Rules{}
	if os.Getenv("ARRIBO_EXTRACTOR") == "llm" {
		l, err := extract.NewLLMFromEnv()
		if err != nil {
			slog.Warn("ARRIBO_EXTRACTOR=llm refused; using rules", "reason", err)
		} else {
			slog.Info("LLM extraction enabled", "approval", os.Getenv("ARRIBO_LLM_APPROVED"))
			ex = l
		}
	}
	h, err := timeline.Load(ctx, r, msgs, ex)
	if err != nil {
		return nil, err
	}
	slog.Info("PO data hub loaded", "pos", len(h.POs), "messages", len(msgs), "resolved", len(h.Items), "review", len(h.Review), "extractor", h.Extractor)
	return h, nil
}

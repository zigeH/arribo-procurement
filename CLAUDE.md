# Arribo Procurement: agent operating manual

Read this before touching code. It applies to humans and to every coding agent
(including the scheduled overnight agents).

## What we are building

Arribo is an AI-native procurement execution system for companies that make
physical products (first customer: MLOptic, precision optics). It reads the
customer's POs and supplier communication and keeps orders moving: PO
confirmation, delivery tracking, exception handling, supplier analytics.

**Current milestone (week of 2026-11-02): MLOptic demo.**
Feature 1 is **supplier & PO scoring against the customer's own history**:
- Supplier scorecard: delivery, price, lead time, quality, responsiveness.
- PO assessment: are this PO's prices / lead times / terms in line with baseline?

## Hard rules

1. **Never invent a benchmark.** Baselines come from the customer's own data
   (or from a cited source in `scoring.Policy`). Not enough data → `Score: nil`
   and say so. Do not fill gaps with "industry averages" from memory.
2. **Every score has evidence** (PO number + line + the numbers used).
3. **No real customer data in git, in prompts to third parties, or in cloud
   agent sandboxes.** Use `evals/data/synthetic_mloptic.json`. Real sanitized
   exports live outside the repo (`evals/data/customer/` is gitignored).
4. **No production credentials** in code, tests, CI or agent sessions.
5. **Replay must not peek into the future.** Anything scored "as of" a date
   only sees receipts/acks on or before that date (`asOfView`).
6. **Temporal workflow code is deterministic**: no `time.Now`, I/O, goroutines,
   random, or map-order dependence in workflow functions. Use `workflow.Now`,
   activities, `workflow.Go`. Changing an existing workflow's sequence of
   commands requires `workflow.GetVersion`.
7. Scoring curves are documented in `docs/scoring-methodology.md`. Change the
   code and the doc in the same PR.

## Architecture (decided; don't relitigate in a PR)

| Area | Choice |
|---|---|
| API, worker, scoring, connectors | **Go** (this repo) |
| Console (UI) | **TypeScript** in `console/` (not started) |
| Voice service | Python, separate repo/service (owner: Vincent) |
| Durable execution | **Temporal** (Temporal Cloud in prod; `temporal server start-dev` locally) |
| Database | **Postgres** (RDS in prod), multi-tenant via `tenant_id` |
| Hosting | AWS |
| Rules / policy evaluation | in **activities**, never inside workflow code |

```
cmd/api        HTTP API (net/http). Runs with in-memory store + no Temporal for demos
cmd/worker     Temporal worker
cmd/seed       load a dataset JSON into Postgres
cmd/gensynth   regenerate synthetic data (deterministic)
cmd/replay     print scorecards for a dataset as of a date
internal/domain     entities (stdlib only)
internal/scoring    pure scoring functions (stdlib only, no I/O, no clock)
internal/service    app layer shared by API + activities (stdlib only)
internal/store      Store interface + in-memory impl (stdlib only)
internal/store/postgres   pgx impl + embedded migrations
internal/workflows  Temporal workflows + activities
internal/temporalx  Temporal client from env
internal/api        HTTP handlers (stdlib only)
evals/              replay evals; must stay green
docs/               methodology + customer data spec
deploy/             AWS deployment notes (Dockerfile at repo root; no IaC yet)
```

Keep `domain`, `scoring`, `service`, `store` (not `store/postgres`) and `api`
free of third-party imports so they build anywhere.

## Commands

```
make test     # go test ./...
make eval     # persona replay evals (verbose)
make replay   # scorecard table for synthetic data
make api      # API on :8080 using synthetic data in memory
make infra    # local Postgres + Temporal (docker compose)
make seed     # load synthetic data into Postgres
```

API: `GET /v1/suppliers`, `GET /v1/suppliers/{id}/scorecard?as_of=YYYY-MM-DD`,
`GET /v1/pos/{id}/assessment?as_of=`, `POST /v1/scoring/runs`.

## How agents pick up and ship work

1. First, if any open PR of yours is labeled `changes-requested`, address
   that feedback before taking new work.
   Otherwise take the oldest open issue labeled `agent-ready` that is not
   labeled `in-progress` and whose "Depends on #N" issues are all closed.
   Add `in-progress` immediately so parallel agents skip it.
2. Branch `agent/<issue-number>-<short-slug>` from latest `main`.
3. Keep the change small (one issue). If the issue is too big, split it into
   new `agent-ready` issues and do only the first part.
4. Before opening a PR: `gofmt -w .`, `go vet ./...`, `go test ./...` all clean.
   Add/adjust tests for every behavior change; scoring changes need an eval.
5. Open a PR titled `<issue title> (#<n>)`, body: what changed, how verified,
   anything uncertain. Label `needs-review`. **Never merge to `main` yourself;
   never push to `main` directly.** Humans merge.
6. If blocked (missing info, product decision), comment on the issue with the
   exact question, label it `needs-human`, remove `in-progress`, and move on.

GitHub from agent sessions: GraphQL is unavailable, so `gh issue create`,
`gh pr create` etc. fail. Use the REST API via `gh api repos/zigeH/arribo-procurement/...`
(e.g. `POST .../pulls`, `POST .../issues/{n}/labels`, `POST .../pulls/{n}/reviews`).

## Known gaps / bootstrapping notes

- `go.sum` is not committed yet (first commit was made from an environment
  without module proxy access). CI runs `go mod tidy` first. Committing
  `go.sum` and tightening CI is an open issue.
- Score curves and grade thresholds are first guesses; calibrate with MLOptic
  buyers (issue: calibration).

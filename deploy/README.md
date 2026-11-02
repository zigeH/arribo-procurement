# Deployment (AWS): target layout

Notes only. No Terraform/CDK and no real resources yet; nothing here contains
credentials. Secrets come from AWS Secrets Manager at runtime.

## Image

One image (`Dockerfile` at the repo root) holds both processes:

| Process | Command | Notes |
|---|---|---|
| API | `/app/api` (default) | HTTP on `$PORT` (8080), health check `GET /healthz` |
| Worker | `/app/worker` | Temporal worker; needs `TEMPORAL_ADDRESS` |

Distroless static base, runs as non-root (uid 65532), no shell. CI builds the
image on every PR (no push). Pushing to ECR is a later step.

## Layout

```
                 ALB (HTTPS, ACM cert)
                        │  /healthz
                ┌───────▼────────┐        ┌──────────────────────┐
                │ ECS Fargate    │        │ ECS Fargate          │
                │ service "api"  │        │ service "worker"     │
                │ 2+ tasks       │        │ 2 tasks min          │
                └───┬────────┬───┘        └────┬────────────┬────┘
                    │        │ start runs      │ poll        │
                    │        └────────► Temporal Cloud ◄─────┘
                    │                  (namespace per env, API key)
                    ▼                                   │
             RDS Postgres  ◄────────────────────────────┘
          (private subnets, Multi-AZ in prod)

  Logs: CloudWatch Logs (awslogs driver), one group per service.
  Secrets: Secrets Manager → ECS task "secrets".
```

- Both services run in private subnets; only the ALB is public. Egress via NAT
  (Temporal Cloud, later the mailbox/ERP connectors).
- The worker has no inbound port. Scale it on Temporal task-queue backlog later;
  start with a fixed 2 tasks.
- Migrations run on startup of both api and worker (`bootstrap.Service` →
  embedded migrations, applied once each; see `internal/store/postgres`).
  There is no lock: if several tasks start at once against a fresh database,
  one may fail on the `schema_migrations` primary key and be restarted by ECS.
  Before prod, add a Postgres advisory lock or run migrations as a one-off task.

## Environment variables

| Var | Used by | Source | Notes |
|---|---|---|---|
| `PORT` | api | task def | default 8080 |
| `DATABASE_URL` | api, worker | Secrets Manager | RDS URL with `sslmode=require`. Unset → in-memory synthetic data (demo only) |
| `TENANT_ID` | api, worker | task def | default `mloptic` |
| `TEMPORAL_ADDRESS` | api, worker | task def | `<ns>.<acct>.tmprl.cloud:7233`. Unset on api → `POST /v1/scoring/runs` disabled |
| `TEMPORAL_NAMESPACE` | api, worker | task def | default `default` |
| `TEMPORAL_API_KEY` | api, worker | Secrets Manager | enables TLS + API-key auth for Temporal Cloud |
| `CORS_ORIGIN` | api | task def | console origin, e.g. `https://app.arribo.ai` |
| `DATASET` | api, worker | task def | demo only: path of a JSON dataset for the in-memory store |

When PR #12 (PO data hub) lands, add its variables here: `HUB_ERP`,
`SAPB1_URL`/`SAPB1_COMPANY`/`SAPB1_USER` (task def) and `SAPB1_PASSWORD`
(Secrets Manager), `ARRIBO_EXTRACTOR`, `ARRIBO_LLM_APPROVED`, and
`ANTHROPIC_API_KEY` (Secrets Manager). Also stop baking the synthetic hub
fixtures into production images.

## IAM (minimum)

- Task execution role: pull from ECR, write CloudWatch Logs, read the specific
  Secrets Manager ARNs above.
- Task role: none needed today (no AWS API calls from the app).

## Open questions

- Region (latency to MLOptic in Nanjing vs the US office; China data rules if
  their ERP/mail data must stay in-country).
- How the worker reaches MLOptic's ERP (VPN / IP allowlist / on-prem agent):
  this decides whether we need a fixed egress IP (NAT EIP).

# arribo-procurement

Arribo's procurement execution backend. First feature: supplier & PO scoring
against the customer's own purchase history (MLOptic demo).

Quick start (no Docker, no Temporal; synthetic data in memory):

```
make replay        # scorecard table
make api           # http://localhost:8080/v1/suppliers
```

Full local stack: `make infra && make seed`, then run `make worker` and
`make api` with the env from `.env.example`.

- Agent/human operating manual: [CLAUDE.md](CLAUDE.md)
- Scoring methodology: [docs/scoring-methodology.md](docs/scoring-methodology.md)
- Customer data spec: [docs/data-spec.md](docs/data-spec.md)

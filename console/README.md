# console

TypeScript web console for the MLOptic demo: Next.js (App Router) + Tailwind.
Talks to the Go API (`/v1/...`, see `CLAUDE.md`).

## Run locally

```sh
# terminal 1: Go API on :8080 with synthetic data, allowing the console origin
CORS_ORIGIN=http://localhost:3000 make api

# terminal 2: console on :3000
cd console
npm ci
npm run dev
```

Open http://localhost:3000/suppliers. The "As of" picker in the header is stored
in the URL (`?as_of=YYYY-MM-DD`) and carried across nav links; empty means today.

`NEXT_PUBLIC_API_URL` sets the API base URL (default `http://localhost:8080`).
It is inlined at build time.

## Layout

```
app/layout.tsx           shell: left nav, as-of picker, API-unreachable banner
app/page.tsx             / = 对不上清单 (placeholder until #3)
app/suppliers/           supplier list (GET /v1/suppliers)
components/              Nav, AsOfPicker, ApiStatusBanner, GradeBadge
lib/api.ts               typed API client; types mirror internal/scoring/types.go and internal/api/api.go
lib/asof.ts, lib/format.ts   pure helpers (unit tests in test/, `npm test`)
```

Rules carried over from the backend: a `null` score means "not enough data"
and is shown as such, never as 0.

## Checks (CI runs these)

```sh
npm ci && npm run lint && npm test && npm run build
```

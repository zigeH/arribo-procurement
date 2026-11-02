
.PHONY: hub-eval
hub-eval:        ## PO data hub evals (extraction, matching, discrepancies)
	go test ./evals -run Hub -v

.PHONY: test eval replay api worker infra infra-down seed synth fmt

test:            ## unit + workflow tests (+ Postgres integration if DATABASE_URL is set)
	go test ./...

eval:            ## replay evals against synthetic personas
	go test ./evals -v

replay:          ## print supplier scorecards for the synthetic dataset
	go run ./cmd/replay

api:             ## API on :8080 (in-memory store unless DATABASE_URL is set)
	go run ./cmd/api

worker:          ## Temporal worker (needs TEMPORAL_ADDRESS)
	go run ./cmd/worker

infra:           ## local Postgres + Temporal dev server
	docker compose up -d

infra-down:
	docker compose down

seed:            ## load synthetic data into Postgres
	go run ./cmd/seed

synth:           ## regenerate synthetic data
	go run ./cmd/gensynth

fmt:
	gofmt -w .

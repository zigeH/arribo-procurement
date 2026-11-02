# One image with both binaries: /app/api (default) and /app/worker.
# Pick the process with the container command, e.g. `docker run IMAGE /app/worker`
# or the ECS task definition's "command".
#
#   docker build -t arribo .
#   docker run -p 8080:8080 arribo                 # API, in-memory synthetic data
#   docker run -e TEMPORAL_ADDRESS=... arribo /app/worker

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
COPY . .
# go.sum is not committed yet (issue #1): resolve modules like CI does.
# Dependencies float to latest until then and may need a newer Go than this
# image, so allow the toolchain download (setup-go in CI does the same).
# Once #1 lands, drop GOTOOLCHAIN and use `go mod download` before `COPY . .`.
ENV GOTOOLCHAIN=auto
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

# Distroless static, non-root (uid 65532), no shell.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/api /out/worker /app/
# Synthetic demo data only (used when DATABASE_URL is unset). Real customer
# exports are excluded by .dockerignore and must never be baked into an image.
COPY --from=build /src/evals/data/synthetic_mloptic.json /app/evals/data/synthetic_mloptic.json
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT []
CMD ["/app/api"]

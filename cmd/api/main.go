// api serves the HTTP API. Runs without Postgres (in-memory dataset) and
// without Temporal (batch runs disabled) for local demos.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zigeH/arribo-procurement/internal/api"
	"github.com/zigeH/arribo-procurement/internal/bootstrap"
	"github.com/zigeH/arribo-procurement/internal/temporalx"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc, closeStore, err := bootstrap.Service(ctx)
	if err != nil {
		slog.Error("init store", "err", err)
		os.Exit(1)
	}
	defer closeStore()

	srv := &api.Server{Svc: svc, AllowOrigin: os.Getenv("CORS_ORIGIN")}
	if temporalx.Enabled() {
		c, err := temporalx.Dial()
		if err != nil {
			slog.Error("temporal dial", "err", err)
			os.Exit(1)
		}
		defer c.Close()
		srv.Runs = temporalx.Starter{C: c}
	} else {
		slog.Warn("TEMPORAL_ADDRESS not set: batch scoring runs disabled")
	}

	addr := ":" + bootstrap.Env("PORT", "8080")
	hs := &http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	slog.Info("api listening", "addr", addr)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}

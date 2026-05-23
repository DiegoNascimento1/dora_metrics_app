// Command mcp-server expõe as métricas DORA via MCP (Model Context
// Protocol) sobre HTTP/JSON-RPC para que LLMs / agentes possam consultar
// as métricas sem hardcode de SQL ou conhecimento da camada REST.
//
// Endpoints:
//
//	POST /mcp        — JSON-RPC dispatcher
//	GET  /healthz    — health check
//
// Auth: Bearer estático via env MCP_SERVER_TOKEN. Se vazio em produção,
// o servidor recusa subir (fail-fast).
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/config"
	"github.com/dora-metrics-app/backend/internal/llm"
	mcpserver "github.com/dora-metrics-app/backend/internal/mcp/server"
	"github.com/dora-metrics-app/backend/internal/observability"
	"github.com/dora-metrics-app/backend/internal/storage"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339Nano

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("load config")
	}
	zerolog.SetGlobalLevel(cfg.LogLevel())
	log.Logger = log.Output(os.Stdout).With().Str("service", "mcp-server").Logger()

	token := os.Getenv("MCP_SERVER_TOKEN")
	if token == "" && os.Getenv("MCP_ALLOW_INSECURE") != "true" {
		log.Fatal().Msg("MCP_SERVER_TOKEN obrigatório (defina MCP_ALLOW_INSECURE=true para dev)")
	}

	addr := os.Getenv("MCP_SERVER_ADDR")
	if addr == "" {
		addr = ":8090"
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownOtel, err := observability.InitTracing(rootCtx, "mcp-server")
	if err != nil {
		log.Warn().Err(err).Msg("init tracing falhou")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownOtel(ctx)
	}()

	db, err := storage.NewPool(rootCtx, cfg.Database)
	if err != nil {
		log.Fatal().Err(err).Msg("connect database")
	}
	defer db.Close()

	// LLM client — nil quando ANTHROPIC_API_KEY não está configurado;
	// o servidor usa o template determinístico como fallback.
	llmClient := llm.New(cfg.AnthropicAPIKey)
	if llmClient != nil {
		log.Info().Msg("LLM (Claude) habilitado para explainTrend")
	}

	srv := mcpserver.NewWithLLM(db, token, llmClient)

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Info().Str("addr", addr).Msg("mcp-server listening")
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("http server")
		}
	}()

	<-rootCtx.Done()
	log.Info().Msg("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown")
	}
	log.Info().Msg("mcp-server stopped")
}

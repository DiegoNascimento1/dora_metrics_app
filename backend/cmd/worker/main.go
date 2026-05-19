// Command worker é o processador assíncrono da plataforma DORA Metrics.
// Consome jobs da fila asynq (Redis) e processa eventos de coleta/cálculo.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/config"
	"github.com/dora-metrics-app/backend/internal/storage"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339Nano

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("load config")
	}

	zerolog.SetGlobalLevel(cfg.LogLevel())
	log.Logger = log.Output(os.Stdout).With().Str("service", "worker").Logger()

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := storage.NewPool(rootCtx, cfg.Database)
	if err != nil {
		log.Fatal().Err(err).Msg("connect database")
	}
	defer db.Close()

	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.Worker.RedisAddr},
		asynq.Config{
			Concurrency: cfg.Worker.Concurrency,
			Queues: map[string]int{
				"collect":  6,
				"compute":  3,
				"default":  1,
			},
			Logger: asynqLogger{},
		},
	)

	mux := asynq.NewServeMux()
	// TODO Fase 1: registrar handlers
	// mux.HandleFunc("collect:gitlab:deployments", h.HandleCollectGitlabDeployments)
	// mux.HandleFunc("compute:metric:project_window", h.HandleComputeMetricWindow)

	go func() {
		log.Info().Int("concurrency", cfg.Worker.Concurrency).Msg("worker starting")
		if err := srv.Run(mux); err != nil {
			log.Fatal().Err(err).Msg("worker run")
		}
	}()

	<-rootCtx.Done()
	log.Info().Msg("shutdown signal received")
	srv.Shutdown()
	log.Info().Msg("worker stopped")

	_ = db // segurar referência (linter); usado pelos handlers reais
}

// asynqLogger adapta zerolog para a interface asynq.Logger.
type asynqLogger struct{}

func (asynqLogger) Debug(args ...any) { log.Debug().Msgf("%v", args) }
func (asynqLogger) Info(args ...any)  { log.Info().Msgf("%v", args) }
func (asynqLogger) Warn(args ...any)  { log.Warn().Msgf("%v", args) }
func (asynqLogger) Error(args ...any) { log.Error().Msgf("%v", args) }
func (asynqLogger) Fatal(args ...any) { log.Fatal().Msgf("%v", args) }

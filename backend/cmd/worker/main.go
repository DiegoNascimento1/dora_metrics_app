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

	"github.com/dora-metrics-app/backend/internal/collector"
	"github.com/dora-metrics-app/backend/internal/config"
	"github.com/dora-metrics-app/backend/internal/secret"
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

	secretProvider, err := secret.New(cfg.SecretProvider)
	if err != nil {
		log.Fatal().Err(err).Msg("init secret provider")
	}

	redisOpt := asynq.RedisClientOpt{Addr: cfg.Worker.RedisAddr}

	asynqClient := asynq.NewClient(redisOpt)
	defer asynqClient.Close()

	handlers := &collector.Handlers{
		DB:      db,
		Secret:  secretProvider,
		Asynq:   asynqClient,
		Windows: []int{7, 30, 90},
	}

	srv := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: cfg.Worker.Concurrency,
			Queues: map[string]int{
				collector.QueueCollect: 6,
				collector.QueueCompute: 3,
				collector.QueueDefault: 1,
			},
			Logger: asynqLogger{},
		},
	)

	mux := asynq.NewServeMux()
	handlers.Register(mux)

	scheduler, err := buildScheduler(redisOpt)
	if err != nil {
		log.Fatal().Err(err).Msg("build scheduler")
	}

	go func() {
		log.Info().Int("concurrency", cfg.Worker.Concurrency).Msg("worker starting")
		if err := srv.Run(mux); err != nil {
			log.Fatal().Err(err).Msg("worker run")
		}
	}()

	go func() {
		log.Info().Msg("periodic scheduler starting")
		if err := scheduler.Run(); err != nil {
			log.Fatal().Err(err).Msg("scheduler run")
		}
	}()

	<-rootCtx.Done()
	log.Info().Msg("shutdown signal received")
	scheduler.Shutdown()
	srv.Shutdown()
	log.Info().Msg("worker stopped")
}

// buildScheduler configura o asynq.Scheduler com:
//   - scan:active_projects        a cada 5 minutos (refresh incremental)
//   - reconcile:projects          às 03:00 UTC diariamente (backfill 7d)
//
// Para múltiplas réplicas do worker no futuro, migrar para
// asynq.PeriodicTaskManager (faz fencing entre instâncias).
func buildScheduler(redisOpt asynq.RedisClientOpt) (*asynq.Scheduler, error) {
	s := asynq.NewScheduler(redisOpt, &asynq.SchedulerOpts{
		Logger: asynqLogger{},
	})

	scanTask := asynq.NewTask(
		collector.TaskScanActiveProjects,
		nil,
		asynq.Queue(collector.QueueDefault),
		asynq.MaxRetry(0),
	)
	if _, err := s.Register("*/5 * * * *", scanTask); err != nil {
		return nil, err
	}

	reconcileTask := asynq.NewTask(
		collector.TaskReconcileAllProjects,
		nil,
		asynq.Queue(collector.QueueDefault),
		asynq.MaxRetry(0),
	)
	if _, err := s.Register("0 3 * * *", reconcileTask); err != nil {
		return nil, err
	}

	return s, nil
}

// asynqLogger adapta zerolog para a interface asynq.Logger.
type asynqLogger struct{}

func (asynqLogger) Debug(args ...any) { log.Debug().Msgf("%v", args) }
func (asynqLogger) Info(args ...any)  { log.Info().Msgf("%v", args) }
func (asynqLogger) Warn(args ...any)  { log.Warn().Msgf("%v", args) }
func (asynqLogger) Error(args ...any) { log.Error().Msgf("%v", args) }
func (asynqLogger) Fatal(args ...any) { log.Fatal().Msgf("%v", args) }

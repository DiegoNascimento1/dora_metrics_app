// Command worker é o processador assíncrono da plataforma DORA Metrics.
// Consome jobs da fila asynq (Redis) e processa eventos de coleta/cálculo.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/collector"
	"github.com/dora-metrics-app/backend/internal/config"
	"github.com/dora-metrics-app/backend/internal/integrations/atlassian"
	"github.com/dora-metrics-app/backend/internal/observability"
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

	otelShutdown, err := observability.InitTracing(rootCtx, "worker")
	if err != nil {
		log.Warn().Err(err).Msg("init tracing falhou — seguindo sem tracing")
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = otelShutdown(shutdownCtx)
	}()

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

	// Redis client separado para pub/sub SSE (asynq não expõe o cliente raw).
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Worker.RedisAddr})
	defer rdb.Close()

	handlers := &collector.Handlers{
		DB:           db,
		Secret:       secretProvider,
		Asynq:        asynqClient,
		Redis:        rdb,
		Windows:      []int{7, 30, 90},
		JiraMCPURL:   cfg.Jira.MCPURL,
		JiraMCPToken: cfg.Jira.MCPToken,
	}

	// Atlassian OAuth 3LO opcional — quando configurado, o coletor
	// prefere usar tokens por-tenant renovados automaticamente via UI.
	if cfg.AtlassianOAuth.ClientID != "" && cfg.AtlassianOAuth.ClientSecret != "" {
		cipher, err := atlassian.NewCipherFromEnv()
		if err != nil {
			log.Warn().Err(err).Msg("atlassian OAuth: OAUTH_ENCRYPTION_KEY ausente — usando só env JIRA_MCP_TOKEN")
		} else {
			oauthCfg := &atlassian.OAuthConfig{
				ClientID:     cfg.AtlassianOAuth.ClientID,
				ClientSecret: cfg.AtlassianOAuth.ClientSecret,
				RedirectURI:  cfg.AtlassianOAuth.RedirectURI,
				Scopes:       []string{"read:jira-work", "read:jira-user", "offline_access"},
			}
			handlers.AtlassianOAuth = atlassian.NewService(db, cipher, oauthCfg)
			log.Info().Msg("atlassian OAuth 3LO ativo no coletor (tokens por tenant)")
		}
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

	observability.Register()

	mux := asynq.NewServeMux()
	mux.Use(observability.AsynqMiddleware)
	handlers.Register(mux)

	scheduler, err := buildScheduler(redisOpt)
	if err != nil {
		log.Fatal().Err(err).Msg("build scheduler")
	}

	// Servidor HTTP só pra expor /metrics no worker (Prometheus scrape).
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", observability.Handler())
	metricsMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	metricsSrv := &http.Server{
		Addr:              cfg.Worker.MetricsAddr,
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
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

	go func() {
		log.Info().Str("addr", cfg.Worker.MetricsAddr).Msg("worker /metrics listening")
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("metrics server")
		}
	}()

	<-rootCtx.Done()
	log.Info().Msg("shutdown signal received")
	scheduler.Shutdown()
	srv.Shutdown()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		log.Warn().Err(err).Msg("metrics server shutdown")
	}
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

	// Snapshot mensal — 1º dia do mês 00:00 UTC. Captura o mês que acabou.
	snapshotTask := asynq.NewTask(
		collector.TaskSnapshotMonthly,
		nil,
		asynq.Queue(collector.QueueDefault),
		asynq.MaxRetry(2),
	)
	if _, err := s.Register("0 0 1 * *", snapshotTask); err != nil {
		return nil, err
	}

	// Digest semanal — segunda 09:00 UTC. Calcula a foto da semana
	// anterior (deploys, incidents, delta de tier).
	digestTask := asynq.NewTask(
		collector.TaskDigestWeekly,
		nil,
		asynq.Queue(collector.QueueDefault),
		asynq.MaxRetry(2),
	)
	if _, err := s.Register("0 9 * * 1", digestTask); err != nil {
		return nil, err
	}

	// Predict semanal — segunda 10:00 UTC (1h depois do digest).
	// Regressão linear sobre o histórico de tier rank; dispara
	// alert_event para rules kind="predicted_regression" se a tendência
	// for degrading com confidence >= medium.
	predictTask := asynq.NewTask(
		collector.TaskPredictWeekly,
		nil,
		asynq.Queue(collector.QueueDefault),
		asynq.MaxRetry(2),
	)
	if _, err := s.Register("0 10 * * 1", predictTask); err != nil {
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

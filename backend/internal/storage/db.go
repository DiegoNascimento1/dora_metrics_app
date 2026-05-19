// Package storage encapsula o acesso ao Postgres via pgx/v5.
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/config"
)

// Pool é o wrapper sobre pgxpool.Pool usado no resto da aplicação.
type Pool struct {
	*pgxpool.Pool
}

// NewPool cria o pool de conexões a partir de config.DatabaseConfig
// e valida com um ping inicial.
func NewPool(ctx context.Context, cfg config.DatabaseConfig) (*Pool, error) {
	pgxCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse pgxpool config: %w", err)
	}

	pgxCfg.MaxConns = cfg.MaxConns
	pgxCfg.MinConns = cfg.MinConns
	pgxCfg.MaxConnLifetime = 30 * time.Minute
	pgxCfg.MaxConnIdleTime = 5 * time.Minute
	pgxCfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("create pgxpool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	log.Info().
		Str("host", cfg.Host).
		Int("port", cfg.Port).
		Str("database", cfg.Database).
		Int32("max_conns", cfg.MaxConns).
		Msg("postgres connected")

	return &Pool{Pool: pool}, nil
}

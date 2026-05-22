// Package config carrega e expõe a configuração da aplicação.
// Usa viper com fonte primária em variáveis de ambiente (.env carregado no docker compose).
package config

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

// Config é a configuração raiz da aplicação.
type Config struct {
	Env      string
	LogLevel_ string

	API      APIConfig
	Worker   WorkerConfig
	Database DatabaseConfig
	GitLab   GitLabConfig
	Jira     JiraConfig

	// SecretProvider escolhe a implementação de secret.Provider.
	SecretProvider string

	// ReliabilityProvider escolhe o backend de SLOs externos.
	// Aceita: "" / "none" / "datadog" / "sentry" / "prometheus" / "yaml".
	ReliabilityProvider string

	// AtlassianOAuth carrega config do app OAuth 3LO usado para conectar
	// tenants ao Jira via UI. Vazio = feature desativada (UI esconde
	// botão "Conectar Jira"; coletor cai pro REST com env tradicional).
	AtlassianOAuth AtlassianOAuthConfig
}

// AtlassianOAuthConfig contém as credenciais do app OAuth (do
// Developer Console — uma por instalação, NÃO por tenant).
type AtlassianOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

// APIConfig descreve o servidor HTTP.
type APIConfig struct {
	HTTPAddr string
}

// WorkerConfig descreve o processador assíncrono.
type WorkerConfig struct {
	Concurrency int
	RedisAddr   string
	MetricsAddr string // endereço HTTP onde o /metrics do worker é exposto
}

// DatabaseConfig descreve a conexão Postgres.
type DatabaseConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
	SSLMode  string
	MaxConns int32
	MinConns int32
}

// DSN devolve a connection string libpq-style.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.Database, d.SSLMode,
	)
}

// GitLabConfig descreve credenciais e endpoint GitLab.
type GitLabConfig struct {
	BaseURL       string
	Token         string
	WebhookToken  string
}

// JiraConfig descreve credenciais e endpoints Jira (REST + MCP).
type JiraConfig struct {
	BaseURL       string
	Email         string
	APIToken      string
	MCPURL        string
	MCPAuthKind   string // "api_token" | "oauth"
	MCPToken      string // Bearer token p/ chamar mcp.atlassian.com (env JIRA_MCP_TOKEN)
	WebhookSecret string
}

// LogLevel resolve o nível de log a partir do campo configurado.
func (c Config) LogLevel() zerolog.Level {
	lvl, err := zerolog.ParseLevel(strings.ToLower(c.LogLevel_))
	if err != nil || lvl == zerolog.NoLevel {
		return zerolog.InfoLevel
	}
	return lvl
}

// Load carrega configuração de variáveis de ambiente.
func Load() (Config, error) {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("API_HTTP_ADDR", ":8080")
	v.SetDefault("API_LOG_LEVEL", "info")
	v.SetDefault("API_ENV", "development")

	v.SetDefault("POSTGRES_HOST", "localhost")
	v.SetDefault("POSTGRES_PORT", 5432)
	v.SetDefault("POSTGRES_DB", "dora")
	v.SetDefault("POSTGRES_USER", "dora")
	v.SetDefault("POSTGRES_PASSWORD", "dora")
	v.SetDefault("POSTGRES_SSLMODE", "disable")
	v.SetDefault("DB_MAX_CONNS", 20)
	v.SetDefault("DB_MIN_CONNS", 2)

	v.SetDefault("WORKER_CONCURRENCY", 10)
	v.SetDefault("WORKER_REDIS_ADDR", "localhost:6379")
	v.SetDefault("WORKER_METRICS_ADDR", ":9090")

	v.SetDefault("SECRET_PROVIDER", "env")
	v.SetDefault("JIRA_MCP_URL", "https://mcp.atlassian.com/v1/mcp")
	v.SetDefault("JIRA_MCP_AUTH_KIND", "api_token")
	v.SetDefault("GITLAB_BASE_URL", "https://gitlab.com")

	cfg := Config{
		Env:                 v.GetString("API_ENV"),
		LogLevel_:           v.GetString("API_LOG_LEVEL"),
		SecretProvider:      v.GetString("SECRET_PROVIDER"),
		ReliabilityProvider: v.GetString("RELIABILITY_PROVIDER"),
		API: APIConfig{
			HTTPAddr: v.GetString("API_HTTP_ADDR"),
		},
		Worker: WorkerConfig{
			Concurrency: v.GetInt("WORKER_CONCURRENCY"),
			RedisAddr:   v.GetString("WORKER_REDIS_ADDR"),
			MetricsAddr: v.GetString("WORKER_METRICS_ADDR"),
		},
		Database: DatabaseConfig{
			Host:     v.GetString("POSTGRES_HOST"),
			Port:     v.GetInt("POSTGRES_PORT"),
			Database: v.GetString("POSTGRES_DB"),
			User:     v.GetString("POSTGRES_USER"),
			Password: v.GetString("POSTGRES_PASSWORD"),
			SSLMode:  v.GetString("POSTGRES_SSLMODE"),
			MaxConns: int32(v.GetInt("DB_MAX_CONNS")),
			MinConns: int32(v.GetInt("DB_MIN_CONNS")),
		},
		GitLab: GitLabConfig{
			BaseURL:      v.GetString("GITLAB_BASE_URL"),
			Token:        v.GetString("GITLAB_TOKEN"),
			WebhookToken: v.GetString("GITLAB_WEBHOOK_TOKEN"),
		},
		Jira: JiraConfig{
			BaseURL:       v.GetString("JIRA_BASE_URL"),
			Email:         v.GetString("JIRA_EMAIL"),
			APIToken:      v.GetString("JIRA_API_TOKEN"),
			MCPURL:        v.GetString("JIRA_MCP_URL"),
			MCPAuthKind:   v.GetString("JIRA_MCP_AUTH_KIND"),
			MCPToken:      v.GetString("JIRA_MCP_TOKEN"),
			WebhookSecret: v.GetString("JIRA_WEBHOOK_SECRET"),
		},
		AtlassianOAuth: AtlassianOAuthConfig{
			ClientID:     v.GetString("ATLASSIAN_OAUTH_CLIENT_ID"),
			ClientSecret: v.GetString("ATLASSIAN_OAUTH_CLIENT_SECRET"),
			RedirectURI:  v.GetString("ATLASSIAN_OAUTH_REDIRECT_URI"),
		},
	}

	return cfg, nil
}

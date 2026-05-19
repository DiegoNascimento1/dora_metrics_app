// Package jira implementa a coleta de dados Jira via MCP (primary)
// e REST API v3 (fallback).
//
// Documentação:
//   - ../../../../docs/04-jira-integration.md
//   - ../../../../docs/02-mcp-protocol.md
package jira

import (
	"context"
	"time"
)

// Issue é uma projeção leve de uma issue Jira que importa para nossas métricas.
type Issue struct {
	Key            string
	ProjectKey     string
	Summary        string
	IssueType      string
	Status         string
	StatusCategory string
	Priority       string
	Labels         []string
	Created        time.Time
	Resolved       *time.Time
}

// Source é a interface implementada pelo coletor Jira.
// Tem duas implementações: MCPSource e RESTSource.
type Source interface {
	// SearchIssues executa uma busca JQL e devolve issues normalizadas.
	SearchIssues(ctx context.Context, jql string, limit int) ([]Issue, error)

	// GetIssue busca uma issue específica por key (ex: "PAY-1234").
	GetIssue(ctx context.Context, key string) (*Issue, error)

	// Name identifica a origem dos dados (para logging/métricas).
	Name() string
}

// MCPSource consome o Atlassian Rovo MCP Server (https://mcp.atlassian.com/v1/mcp).
// TODO Fase 3: implementar via mark3labs/mcp-go ou cliente JSON-RPC custom.
type MCPSource struct {
	endpoint string
	auth     string
}

// NewMCPSource constrói um cliente MCP do Atlassian.
func NewMCPSource(endpoint, auth string) *MCPSource {
	return &MCPSource{endpoint: endpoint, auth: auth}
}

// Name implementa Source.
func (s *MCPSource) Name() string { return "atlassian-mcp" }

// SearchIssues — stub. Fase 3.
func (s *MCPSource) SearchIssues(_ context.Context, _ string, _ int) ([]Issue, error) {
	return nil, ErrNotImplemented
}

// GetIssue — stub. Fase 3.
func (s *MCPSource) GetIssue(_ context.Context, _ string) (*Issue, error) {
	return nil, ErrNotImplemented
}

// RESTSource consome a Jira Cloud REST API v3 diretamente.
// Implementação primária da Fase 2; serve também como fallback do MCP.
type RESTSource struct {
	baseURL  string
	email    string
	apiToken string
}

// NewRESTSource constrói um cliente Jira REST.
func NewRESTSource(baseURL, email, apiToken string) *RESTSource {
	return &RESTSource{baseURL: baseURL, email: email, apiToken: apiToken}
}

// Name implementa Source.
func (s *RESTSource) Name() string { return "jira-rest" }

// SearchIssues — stub. Fase 2.
func (s *RESTSource) SearchIssues(_ context.Context, _ string, _ int) ([]Issue, error) {
	return nil, ErrNotImplemented
}

// GetIssue — stub. Fase 2.
func (s *RESTSource) GetIssue(_ context.Context, _ string) (*Issue, error) {
	return nil, ErrNotImplemented
}

// ErrNotImplemented é o sentinel devolvido por stubs até a implementação real.
var ErrNotImplemented = errNotImplemented{}

type errNotImplemented struct{}

func (errNotImplemented) Error() string { return "jira source: not implemented yet" }

// Package server implementa um servidor MCP (Model Context Protocol)
// próprio expondo as métricas DORA como tools/resources.
//
// Decisão de stack: implementação custom JSON-RPC 2.0 sobre HTTP POST em
// vez de adotar o `modelcontextprotocol/go-sdk` (ainda pré-1.0, churn de
// API alto). A spec MCP que importa para o MVP cabe em ~250 LOC:
// `initialize`, `tools/list`, `tools/call`, `resources/list`, `resources/read`.
// Ver ADR 0004 para o trade-off.
//
// Auth: Bearer estático via env `MCP_SERVER_TOKEN`. OAuth 2.1 fica para
// iteração futura.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/storage"
	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

const protocolVersion = "2025-06-18"

// Server é o handler HTTP do MCP. Implementa http.Handler.
type Server struct {
	db       *storage.Pool
	token    string
	tools    []Tool
	handlers map[string]toolHandler
	now      func() time.Time // injetável p/ testes

	// OAuth opcional. Quando Enabled(), o servidor aceita Bearer
	// emitido pelo /oauth/token em vez do token estático. O token
	// estático continua aceito como fallback (operadores podem
	// começar com Bearer estático e migrar).
	oauth *OAuthServer
	mux   *http.ServeMux
}

// Tool é a definição declarativa exposta no `tools/list`.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// toolHandler executa uma tool. Recebe argumentos crus (json.RawMessage).
type toolHandler func(ctx context.Context, args json.RawMessage) (any, error)

// New constrói um Server. Token vazio = sem auth (apenas para testes).
//
// Se MCP_OAUTH_CLIENTS estiver configurado, o servidor ativa também o
// fluxo OAuth 2.1 PKCE — clientes podem obter Bearer tokens dinâmicos
// pelo endpoint /oauth/token. O token estático continua válido como
// fallback (decisão pragmática: facilita rollout).
func New(db *storage.Pool, token string) *Server {
	s := &Server{
		db:       db,
		token:    token,
		handlers: map[string]toolHandler{},
		now:      time.Now,
		mux:      http.NewServeMux(),
	}
	s.registerTools()

	if oauth := NewOAuthServer(); oauth.Enabled() {
		s.oauth = oauth
		oauth.RegisterRoutes(s.mux)
	}
	return s
}

// ServeHTTP roteia POST /mcp para o dispatcher JSON-RPC. Outros endpoints
// devolvem 404 — exceto /oauth/* quando OAuth está habilitado (delegados
// ao OAuthServer via s.mux).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	if strings.HasPrefix(r.URL.Path, "/oauth/") {
		s.mux.ServeHTTP(w, r)
		return
	}
	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, nil, errParseError, "parse error", nil)
		return
	}
	resp := s.dispatch(r.Context(), req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) checkAuth(r *http.Request) bool {
	got := r.Header.Get("Authorization")

	// 1. Token estático sempre aceito quando configurado (Bearer fixo).
	if s.token != "" {
		want := "Bearer " + s.token
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
			return true
		}
	}

	// 2. OAuth 2.1 — Bearer emitido pelo /oauth/token.
	if s.oauth != nil {
		if tok, ok := strings.CutPrefix(got, "Bearer "); ok {
			if _, _, valid := s.oauth.ValidateToken(tok); valid {
				return true
			}
		}
	}

	// Token estático vazio E sem OAuth → modo "open" (testes).
	return s.token == "" && s.oauth == nil
}

// dispatch resolve o método JSON-RPC pedido. Devolve sempre uma rpcResponse
// (com result ou error). Notifications (id=null) não são suportadas no MVP.
func (s *Server) dispatch(ctx context.Context, req rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourcesRead(ctx, req)
	case "ping":
		return rpcResponse{Jsonrpc: "2.0", ID: req.ID, Result: map[string]any{}}
	}
	return errorResponse(req.ID, errMethodNotFound, "method not found: "+req.Method, nil)
}

func (s *Server) handleInitialize(req rpcRequest) rpcResponse {
	return rpcResponse{
		Jsonrpc: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools":     map[string]any{"listChanged": false},
				"resources": map[string]any{"listChanged": false, "subscribe": false},
			},
			"serverInfo": map[string]any{
				"name":    "dora-metrics-mcp",
				"version": "0.1.0",
			},
		},
	}
}

func (s *Server) handleToolsList(req rpcRequest) rpcResponse {
	return rpcResponse{
		Jsonrpc: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"tools": s.tools},
	}
}

func (s *Server) handleToolsCall(ctx context.Context, req rpcRequest) rpcResponse {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, errInvalidParams, "invalid params", err.Error())
	}
	h, ok := s.handlers[p.Name]
	if !ok {
		return errorResponse(req.ID, errMethodNotFound, "unknown tool: "+p.Name, nil)
	}
	out, err := h(ctx, p.Arguments)
	if err != nil {
		// Tools devolvem erro como conteúdo isError=true (conforme MCP),
		// não como erro JSON-RPC — assim o cliente vê a stack trace
		// textual em vez de falhar a chamada.
		return rpcResponse{
			Jsonrpc: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"isError": true,
				"content": []map[string]any{
					{"type": "text", "text": err.Error()},
				},
			},
		}
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return errorResponse(req.ID, errInternalError, "marshal result", err.Error())
	}
	return rpcResponse{
		Jsonrpc: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": string(payload)},
			},
		},
	}
}

func (s *Server) handleResourcesList(req rpcRequest) rpcResponse {
	// Lista vazia no MVP — resources são "lidas dinamicamente" via
	// resources/read pela URI; descobrir manualmente é OK para LLMs.
	return rpcResponse{
		Jsonrpc: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"resources": []map[string]any{
				{
					"uri":         "dora://schema",
					"name":        "DORA tier definitions",
					"description": "Definições dos tiers Elite/High/Medium/Low conforme DORA Report 2024.",
					"mimeType":    "application/json",
				},
				{
					"uri":         "dora://benchmarks",
					"name":        "Industry benchmarks (DORA Report 2024)",
					"description": "Percentis p50/p75/p90 da indústria para comparação anônima.",
					"mimeType":    "application/json",
				},
			},
		},
	}
}

func (s *Server) handleResourcesRead(ctx context.Context, req rpcRequest) rpcResponse {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, errInvalidParams, "invalid params", err.Error())
	}

	switch p.URI {
	case "dora://schema":
		return rpcResponse{
			Jsonrpc: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"contents": []map[string]any{
					{
						"uri":      p.URI,
						"mimeType": "application/json",
						"text":     `{"thresholds":{"df":{"elite":1.0,"high":0.143,"medium":0.033},"lt_seconds":{"elite":3600,"high":604800,"medium":2592000},"cfr":{"elite":0.05,"high":0.10,"medium":0.20},"mttr_seconds":{"elite":3600,"high":86400,"medium":604800}}}`,
					},
				},
			},
		}
	case "dora://benchmarks":
		// Snapshot do DORA Report 2024 (mesmo conteúdo do endpoint REST
		// /api/v1/benchmarks). Hardcoded aqui para que o MCP server não
		// dependa de import circular do api/.
		return rpcResponse{
			Jsonrpc: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"contents": []map[string]any{
					{
						"uri":      p.URI,
						"mimeType": "application/json",
						"text":     `{"source":"DORA Report 2024","industryAverages":{"p50":{"deploymentFrequencyPerDay":0.5,"leadTimeForChangesSeconds":259200,"changeFailureRate":0.12,"mttrSeconds":43200},"p75":{"deploymentFrequencyPerDay":2.5,"leadTimeForChangesSeconds":86400,"changeFailureRate":0.08,"mttrSeconds":14400},"p90":{"deploymentFrequencyPerDay":8.0,"leadTimeForChangesSeconds":14400,"changeFailureRate":0.04,"mttrSeconds":3600}}}`,
					},
				},
			},
		}
	default:
		// project/{id}/dora-metrics e team/{id}/dora-metrics
		kind, id, ok := parseDoraURI(p.URI)
		if !ok {
			return errorResponse(req.ID, errInvalidParams, "uri inválida: "+p.URI, nil)
		}
		data, err := s.fetchMetricsByID(ctx, kind, id, 30)
		if err != nil {
			return errorResponse(req.ID, errInternalError, "fetch metrics", err.Error())
		}
		payload, _ := json.Marshal(data)
		return rpcResponse{
			Jsonrpc: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"contents": []map[string]any{
					{
						"uri":      p.URI,
						"mimeType": "application/json",
						"text":     string(payload),
					},
				},
			},
		}
	}
}

// ---- tools ----

func (s *Server) registerTools() {
	s.tools = []Tool{
		{
			Name:        "getDoraMetrics",
			Description: "Devolve as 4 métricas DORA (DF, LT, CFR, MTTR) + tier combinado para um project_id OU team_id na janela escolhida.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project_id":  map[string]any{"type": "string", "description": "UUID do projeto. Excludente com team_id."},
					"team_id":     map[string]any{"type": "string", "description": "UUID do time. Excludente com project_id."},
					"window_days": map[string]any{"type": "integer", "enum": []int{7, 30, 90}, "default": 30},
				},
			},
		},
		{
			Name:        "getDeployments",
			Description: "Lista deployments do projeto numa janela (default 30 dias).",
			InputSchema: map[string]any{
				"type": "object",
				"required": []string{"project_id"},
				"properties": map[string]any{
					"project_id":  map[string]any{"type": "string"},
					"window_days": map[string]any{"type": "integer", "enum": []int{7, 30, 90}, "default": 30},
				},
			},
		},
		{
			Name:        "compareTeams",
			Description: "Comparativo lado-a-lado das métricas DORA de 2-4 times.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"team_ids"},
				"properties": map[string]any{
					"team_ids":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 2, "maxItems": 4},
					"window_days": map[string]any{"type": "integer", "enum": []int{7, 30, 90}, "default": 30},
				},
			},
		},
		{
			Name:        "explainTrend",
			Description: "Narrativa textual (template, sem LLM) descrevendo a evolução das métricas vs janela anterior. Hook para LLM no futuro.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project_id":  map[string]any{"type": "string"},
					"team_id":     map[string]any{"type": "string"},
					"window_days": map[string]any{"type": "integer", "enum": []int{7, 30, 90}, "default": 30},
				},
			},
		},
	}
	s.handlers["getDoraMetrics"] = s.toolGetDoraMetrics
	s.handlers["getDeployments"] = s.toolGetDeployments
	s.handlers["compareTeams"] = s.toolCompareTeams
	s.handlers["explainTrend"] = s.toolExplainTrend
}

// ---- shapes JSON-RPC ----

type rpcRequest struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSON-RPC 2.0 standard codes
const (
	errParseError     = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternalError  = -32603
)

func errorResponse(id json.RawMessage, code int, msg string, data any) rpcResponse {
	return rpcResponse{
		Jsonrpc: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg, Data: data},
	}
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, msg string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(errorResponse(id, code, msg, data))
}

// ---- helpers de queries ----

// fetchMetricsByID resolve o tenant a partir do projeto/time e busca a
// metric_window mais recente. `kind` ∈ {project, team}.
func (s *Server) fetchMetricsByID(ctx context.Context, kind string, id uuid.UUID, windowDays int) (map[string]any, error) {
	q := queries.New(s.db.Pool)
	var tenantID uuid.UUID
	switch kind {
	case "project":
		p, err := q.GetProject(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("project %s not found", id)
			}
			return nil, err
		}
		tenantID = p.TenantID
	case "team":
		t, err := q.GetTeam(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("team %s not found", id)
			}
			return nil, err
		}
		tenantID = t.TenantID
	default:
		return nil, fmt.Errorf("kind inválido: %s", kind)
	}

	row, err := q.GetLatestMetricWindow(ctx, queries.GetLatestMetricWindowParams{
		TenantID:   tenantID,
		ScopeKind:  kind,
		ScopeID:    id,
		WindowDays: int32(windowDays),
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	out := map[string]any{
		"scope_kind":     kind,
		"scope_id":       id.String(),
		"window_days":    windowDays,
		"classification": "insufficient_data",
		"computed_at":    s.now().UTC().Format(time.RFC3339),
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}

	out["computed_at"] = row.ComputedAt.UTC().Format(time.RFC3339)
	out["sample_size"] = row.SampleSize
	out["lead_time_median_seconds"] = row.LeadTimeMedianS
	out["mttr_mean_seconds"] = row.MttrMeanS
	if row.Classification != nil {
		out["classification"] = *row.Classification
	}
	if row.DeploymentFrequency.Valid {
		f, _ := row.DeploymentFrequency.Float64Value()
		out["deployment_frequency"] = f.Float64
	}
	if row.ChangeFailureRate.Valid {
		f, _ := row.ChangeFailureRate.Float64Value()
		out["change_failure_rate"] = f.Float64
	}
	return out, nil
}

// parseDoraURI aceita "dora://project/{id}/dora-metrics" ou "dora://team/{id}/dora-metrics".
func parseDoraURI(uri string) (kind string, id uuid.UUID, ok bool) {
	const prefix = "dora://"
	if len(uri) < len(prefix)+8 || uri[:len(prefix)] != prefix {
		return "", uuid.Nil, false
	}
	rest := uri[len(prefix):]
	// "project/{id}/dora-metrics"
	for _, k := range []string{"project", "team"} {
		marker := k + "/"
		if len(rest) > len(marker) && rest[:len(marker)] == marker {
			tail := rest[len(marker):]
			// id é tudo até a próxima "/"
			for i := 0; i < len(tail); i++ {
				if tail[i] == '/' {
					parsed, err := uuid.Parse(tail[:i])
					if err != nil {
						return "", uuid.Nil, false
					}
					return k, parsed, true
				}
			}
			parsed, err := uuid.Parse(tail)
			if err != nil {
				return "", uuid.Nil, false
			}
			return k, parsed, true
		}
	}
	return "", uuid.Nil, false
}

// ---- log helper ----
//
//nolint:unused // keep for future debug
func (s *Server) logCall(method string, dur time.Duration) {
	log.Debug().Str("method", method).Dur("dur", dur).Msg("mcp call")
}

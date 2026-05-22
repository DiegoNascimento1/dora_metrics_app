// Package jira implementa a coleta de dados Jira via MCP (primary, futuro)
// e REST API v3 (atual, suficiente para a Fase 2).
//
// Documentação:
//   - ../../../../docs/04-jira-integration.md
//   - ../../../../docs/02-mcp-protocol.md
package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	mcpclient "github.com/dora-metrics-app/backend/internal/mcp/client"
)

// Issue é a projeção mínima de uma issue Jira que importa para CFR/MTTR.
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
	Raw            []byte
}

// Source é a interface implementada pelo coletor Jira.
// Duas implementações: MCPSource (Fase 3) e RESTSource (atual).
type Source interface {
	SearchIssues(ctx context.Context, jql string, limit int) ([]Issue, error)
	Name() string
}

// MCPSource consome o Atlassian Rovo MCP Server
// (`https://mcp.atlassian.com/v1/mcp`).
//
// O cliente faz handshake `initialize` lazy (apenas na primeira chamada) e
// invoca a tool `searchJiraIssuesUsingJql` para listar issues. Em caso de
// erro do MCP, automaticamente cai para o RESTSource fallback (se
// configurado em `WithFallback`).
//
// Auth: Bearer estático (MVP). OAuth 2.1 fica para próxima iteração.
type MCPSource struct {
	client      *mcpclient.AtlassianClient
	fallback    Source
	initialized atomic.Bool
}

// NewMCPSource constrói um cliente MCP do Atlassian.
func NewMCPSource(endpoint, token string) *MCPSource {
	return &MCPSource{client: mcpclient.NewAtlassianClient(endpoint, token)}
}

// WithFallback adiciona uma fonte alternativa usada se o MCP falhar
// (tipicamente um RESTSource). Devolve o próprio MCPSource pra chaining.
func (s *MCPSource) WithFallback(fb Source) *MCPSource {
	s.fallback = fb
	return s
}

// Name implementa Source.
func (s *MCPSource) Name() string { return "atlassian-mcp" }

// SearchIssues chama a tool searchJiraIssuesUsingJql do MCP Atlassian.
// Cai para o fallback (se houver) se o MCP devolver erro de qualquer tipo.
func (s *MCPSource) SearchIssues(ctx context.Context, jql string, limit int) ([]Issue, error) {
	if !s.initialized.Load() {
		if _, err := s.client.Initialize(ctx); err != nil {
			return s.useFallback(ctx, jql, limit, err)
		}
		s.initialized.Store(true)
	}

	args := map[string]any{
		"jql": jql,
	}
	if limit > 0 {
		args["limit"] = limit
	}

	rawResult, err := s.client.CallTool(ctx, "searchJiraIssuesUsingJql", args)
	if err != nil {
		return s.useFallback(ctx, jql, limit, err)
	}

	// O Atlassian MCP devolve um JSON com array `issues` (mesma shape do REST v3).
	var resp struct {
		Issues []json.RawMessage `json:"issues"`
	}
	if err := json.Unmarshal(rawResult, &resp); err != nil {
		return s.useFallback(ctx, jql, limit, fmt.Errorf("decode mcp result: %w", err))
	}

	out := make([]Issue, 0, len(resp.Issues))
	for _, raw := range resp.Issues {
		issue, err := decodeIssue(raw)
		if err != nil {
			continue
		}
		out = append(out, issue)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// useFallback registra o erro e delega ao RESTSource fallback (se houver).
// Devolve o erro original se nada estiver configurado.
func (s *MCPSource) useFallback(ctx context.Context, jql string, limit int, mcpErr error) ([]Issue, error) {
	if s.fallback == nil {
		return nil, fmt.Errorf("mcp falhou e fallback não configurado: %w", mcpErr)
	}
	return s.fallback.SearchIssues(ctx, jql, limit)
}

// RESTSource consome a Jira Cloud REST API v3 com Basic auth.
type RESTSource struct {
	baseURL    string
	authHeader string
	httpClient *http.Client
}

// NewRESTSource constrói um cliente Jira REST.
func NewRESTSource(baseURL, email, apiToken string) *RESTSource {
	cred := base64.StdEncoding.EncodeToString([]byte(email + ":" + apiToken))
	return &RESTSource{
		baseURL:    strings.TrimRight(baseURL, "/"),
		authHeader: "Basic " + cred,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name implementa Source.
func (s *RESTSource) Name() string { return "jira-rest" }

// SearchIssues executa POST /rest/api/3/search/jql (enhanced search) com
// paginação por nextPageToken.
//
// limit <= 0 = sem limite.
func (s *RESTSource) SearchIssues(ctx context.Context, jql string, limit int) ([]Issue, error) {
	const pageSize = 100
	out := make([]Issue, 0, 64)
	var nextToken string

	for {
		body := searchRequest{
			JQL:           jql,
			NextPageToken: nextToken,
			MaxResults:    pageSize,
			Fields: []string{
				"summary", "status", "priority", "issuetype",
				"labels", "created", "resolutiondate", "project",
			},
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal search request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			s.baseURL+"/rest/api/3/search/jql", strings.NewReader(string(payload)))
		if err != nil {
			return nil, fmt.Errorf("build search request: %w", err)
		}
		req.Header.Set("Authorization", s.authHeader)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("jira request: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
			resp.Body.Close()
			return nil, &APIError{StatusCode: resp.StatusCode, Message: string(respBody)}
		}

		var page searchResponse
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode search response: %w", err)
		}
		resp.Body.Close()

		for _, raw := range page.Issues {
			issue, err := decodeIssue(raw)
			if err != nil {
				return nil, fmt.Errorf("decode issue: %w", err)
			}
			out = append(out, issue)
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}

		if page.NextPageToken == "" || page.IsLast || len(page.Issues) == 0 {
			break
		}
		nextToken = page.NextPageToken
	}
	return out, nil
}

// ---- internal shapes ----

type searchRequest struct {
	JQL           string   `json:"jql"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
	MaxResults    int      `json:"maxResults,omitempty"`
	Fields        []string `json:"fields,omitempty"`
}

type searchResponse struct {
	NextPageToken string            `json:"nextPageToken"`
	IsLast        bool              `json:"isLast"`
	Issues        []json.RawMessage `json:"issues"`
}

func decodeIssue(raw json.RawMessage) (Issue, error) {
	var shape struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
			Project struct {
				Key string `json:"key"`
			} `json:"project"`
			IssueType struct {
				Name string `json:"name"`
			} `json:"issuetype"`
			Status struct {
				Name           string `json:"name"`
				StatusCategory struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"status"`
			Priority struct {
				Name string `json:"name"`
			} `json:"priority"`
			Labels         []string `json:"labels"`
			Created        string   `json:"created"`
			ResolutionDate *string  `json:"resolutiondate"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return Issue{}, err
	}

	created, err := parseJiraTime(shape.Fields.Created)
	if err != nil {
		return Issue{}, fmt.Errorf("parsing time %q: %w", shape.Fields.Created, err)
	}

	var resolved *time.Time
	if shape.Fields.ResolutionDate != nil && *shape.Fields.ResolutionDate != "" {
		t, err := parseJiraTime(*shape.Fields.ResolutionDate)
		if err == nil {
			resolved = &t
		}
	}

	labels := shape.Fields.Labels
	if labels == nil {
		labels = []string{}
	}

	return Issue{
		Key:            shape.Key,
		ProjectKey:     shape.Fields.Project.Key,
		Summary:        shape.Fields.Summary,
		IssueType:      shape.Fields.IssueType.Name,
		Status:         shape.Fields.Status.Name,
		StatusCategory: shape.Fields.Status.StatusCategory.Key,
		Priority:       shape.Fields.Priority.Name,
		Labels:         labels,
		Created:        created,
		Resolved:       resolved,
		Raw:            raw,
	}, nil
}

// parseJiraTime aceita os dois formatos retornados pela Jira Cloud:
//   - "2026-05-19T12:34:56.000-0300" (offset sem dois pontos — Jira REST v3)
//   - "2026-05-19T12:34:56Z" / "2026-05-19T12:34:56-03:00" (RFC3339, alguns clientes/webhooks)
//
// Devolve o time.Time parseado ou erro do último layout tentado.
func parseJiraTime(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02T15:04:05.000-0700", s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// User é a projeção de um Jira Cloud user para alimentar
// `platform.person_identity` (kind=jira). Apenas os campos que casam com
// a heurística do `internal/identities`: accountId (external_id),
// displayName, emailAddress.
type User struct {
	AccountID    string `json:"accountId"`
	AccountType  string `json:"accountType"` // "atlassian" | "app" | "customer"
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
	Active       bool   `json:"active"`
}

// SearchUsers chama GET /rest/api/3/users/search com pagination via
// startAt + maxResults. Devolve apenas usuários `accountType=atlassian`
// e `active=true` (filtra fora bots e contas desativadas) — Jira não
// suporta filtro server-side desses campos.
//
// `query` filtra por substring de displayName/email (passado como ?query=).
// Vazio = todos.
//
// Fonte: https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-user-search/
func (s *RESTSource) SearchUsers(ctx context.Context, query string, limit int) ([]User, error) {
	const pageSize = 100
	out := make([]User, 0, 64)
	startAt := 0

	for {
		q := url.Values{}
		q.Set("startAt", strconv.Itoa(startAt))
		q.Set("maxResults", strconv.Itoa(pageSize))
		if query != "" {
			q.Set("query", query)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			s.baseURL+"/rest/api/3/users/search?"+q.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("build users request: %w", err)
		}
		req.Header.Set("Authorization", s.authHeader)
		req.Header.Set("Accept", "application/json")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("jira request: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
			resp.Body.Close()
			return nil, &APIError{StatusCode: resp.StatusCode, Message: string(body)}
		}

		var batch []User
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode users response: %w", err)
		}
		resp.Body.Close()

		for _, u := range batch {
			if u.AccountType != "" && u.AccountType != "atlassian" {
				continue
			}
			if !u.Active {
				continue
			}
			out = append(out, u)
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}

		// Paginação Jira: se a página voltou menos que maxResults, acabou.
		if len(batch) < pageSize {
			break
		}
		startAt += pageSize
	}
	return out, nil
}

// APIError representa um erro retornado pela API Jira.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("jira api: %d %s", e.StatusCode, e.Message)
}

// ErrNotImplemented é devolvido pelos stubs (MCPSource até a Fase 3).
var ErrNotImplemented = errors.New("jira source: not implemented yet")

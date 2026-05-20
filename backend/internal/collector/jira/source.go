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
	"strings"
	"time"
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

// MCPSource consome o Atlassian Rovo MCP Server.
// Stub para futura implementação na Fase 3.
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
			Labels         []string  `json:"labels"`
			Created        time.Time `json:"created"`
			ResolutionDate *string   `json:"resolutiondate"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return Issue{}, err
	}

	var resolved *time.Time
	if shape.Fields.ResolutionDate != nil && *shape.Fields.ResolutionDate != "" {
		// Jira retorna "2026-05-19T12:34:56.000-0300" (offset sem dois pontos).
		t, err := time.Parse("2006-01-02T15:04:05.000-0700", *shape.Fields.ResolutionDate)
		if err != nil {
			t, err = time.Parse(time.RFC3339, *shape.Fields.ResolutionDate)
		}
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
		Created:        shape.Fields.Created,
		Resolved:       resolved,
		Raw:            raw,
	}, nil
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

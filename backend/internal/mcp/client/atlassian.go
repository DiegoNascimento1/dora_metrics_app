// Package client implementa um cliente MCP (Model Context Protocol)
// genérico sobre HTTP+JSON-RPC, especializado para o Atlassian Rovo MCP
// Server (`https://mcp.atlassian.com/v1/mcp`).
//
// O cliente expõe apenas as duas chamadas que o coletor Jira precisa
// hoje: `initialize` + `tools/call` para a tool `searchJiraIssuesUsingJql`.
//
// Auth: Bearer estático no MVP (env `JIRA_MCP_TOKEN`). OAuth 2.1
// completo fica para a próxima iteração — registrado em ADR 0004.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// AtlassianClient é um cliente MCP HTTP minimalista.
type AtlassianClient struct {
	endpoint string
	token    string
	http     *http.Client
	// nextID gera IDs JSON-RPC monotonicamente.
	nextID atomic.Int64
}

// NewAtlassianClient constrói um cliente. Endpoint vazio = default oficial.
func NewAtlassianClient(endpoint, token string) *AtlassianClient {
	if endpoint == "" {
		endpoint = "https://mcp.atlassian.com/v1/mcp"
	}
	return &AtlassianClient{
		endpoint: strings.TrimRight(endpoint, "/"),
		token:    token,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Initialize negocia o handshake MCP. Devolve a versão protocolar
// retornada pelo servidor. Em geral, basta chamar uma vez por sessão.
func (c *AtlassianClient) Initialize(ctx context.Context) (string, error) {
	resp, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"clientInfo": map[string]any{
			"name":    "dora-metrics-jira-collector",
			"version": "0.1.0",
		},
	})
	if err != nil {
		return "", err
	}
	var r struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		return "", err
	}
	return r.ProtocolVersion, nil
}

// CallTool invoca `tools/call` com nome + argumentos. Devolve o conteúdo
// textual (o servidor MCP serializa o payload da tool em content[0].text).
// Esse text é tipicamente um JSON com o resultado.
func (c *AtlassianClient) CallTool(ctx context.Context, name string, args any) (json.RawMessage, error) {
	resp, err := c.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}
	var r struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("decode tool result: %w", err)
	}
	if len(r.Content) == 0 {
		return nil, fmt.Errorf("tool %s retornou content vazio", name)
	}
	if r.IsError {
		return nil, fmt.Errorf("tool %s: %s", name, r.Content[0].Text)
	}
	return json.RawMessage(r.Content[0].Text), nil
}

// call dispara uma requisição JSON-RPC e devolve o campo `result` cru.
func (c *AtlassianClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("mcp error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	return envelope.Result, nil
}

// HTTPError representa falha HTTP do servidor MCP.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("mcp http %d: %s", e.StatusCode, e.Body)
}

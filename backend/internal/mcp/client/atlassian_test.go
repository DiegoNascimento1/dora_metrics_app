package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mcpServer constrói um servidor HTTP que responde JSON-RPC 2.0.
// `handler` recebe o método e devolve o `result` (qualquer json-serializable).
func mcpServer(t *testing.T, handler func(method string, params json.RawMessage) (any, error)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result, err := handler(req.Method, req.Params)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]any{"code": -32603, "message": err.Error()},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
}

func TestInitialize(t *testing.T) {
	srv := mcpServer(t, func(method string, _ json.RawMessage) (any, error) {
		if method != "initialize" {
			t.Errorf("method = %s", method)
		}
		return map[string]any{"protocolVersion": "2025-06-18"}, nil
	})
	defer srv.Close()

	c := NewAtlassianClient(srv.URL, "t")
	v, err := c.Initialize(context.Background())
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if v != "2025-06-18" {
		t.Errorf("version = %s", v)
	}
}

func TestCallTool_Success(t *testing.T) {
	srv := mcpServer(t, func(method string, params json.RawMessage) (any, error) {
		if method != "tools/call" {
			t.Errorf("method = %s", method)
		}
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(params, &p)
		if p.Name != "searchJiraIssues" {
			t.Errorf("tool name = %s", p.Name)
		}
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": `{"issues":[{"key":"ACME-1"}]}`},
			},
		}, nil
	})
	defer srv.Close()

	c := NewAtlassianClient(srv.URL, "t")
	out, err := c.CallTool(context.Background(), "searchJiraIssues", map[string]any{"jql": "x"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !strings.Contains(string(out), "ACME-1") {
		t.Errorf("got %s, want contains ACME-1", out)
	}
}

func TestCallTool_IsError(t *testing.T) {
	srv := mcpServer(t, func(_ string, _ json.RawMessage) (any, error) {
		return map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": "rate limited"}},
		}, nil
	})
	defer srv.Close()

	c := NewAtlassianClient(srv.URL, "t")
	_, err := c.CallTool(context.Background(), "foo", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("err = %v", err)
	}
}

func TestCallTool_JSONRPCError(t *testing.T) {
	srv := mcpServer(t, func(_ string, _ json.RawMessage) (any, error) {
		return nil, errors.New("internal boom")
	})
	defer srv.Close()

	c := NewAtlassianClient(srv.URL, "t")
	_, err := c.CallTool(context.Background(), "foo", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v", err)
	}
}

func TestCallTool_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer srv.Close()

	c := NewAtlassianClient(srv.URL, "t")
	_, err := c.CallTool(context.Background(), "foo", nil)
	var hErr *HTTPError
	if !errors.As(err, &hErr) {
		t.Fatalf("expected HTTPError, got %T (%v)", err, err)
	}
	if hErr.StatusCode != 502 {
		t.Errorf("status = %d", hErr.StatusCode)
	}
}

func TestCallTool_EmptyContent(t *testing.T) {
	srv := mcpServer(t, func(_ string, _ json.RawMessage) (any, error) {
		return map[string]any{"content": []any{}}, nil
	})
	defer srv.Close()

	c := NewAtlassianClient(srv.URL, "t")
	_, err := c.CallTool(context.Background(), "foo", nil)
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestNewAtlassianClient_DefaultEndpoint(t *testing.T) {
	c := NewAtlassianClient("", "x")
	if c.endpoint != "https://mcp.atlassian.com/v1/mcp" {
		t.Errorf("endpoint = %s", c.endpoint)
	}
}

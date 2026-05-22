package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rpcCall faz POST /mcp e devolve a resposta JSON-RPC parseada.
func rpcCall(t *testing.T, srv *Server, token, method string, params any) rpcResponse {
	t.Helper()
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(b))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp rpcResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	return resp
}

func TestServer_HealthCheck(t *testing.T) {
	s := New(nil, "")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestServer_Unauthorized(t *testing.T) {
	s := New(nil, "secret")
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	// sem header Authorization
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestServer_AuthorizedWithToken(t *testing.T) {
	s := New(nil, "sek")
	resp := rpcCall(t, s, "sek", "initialize", map[string]any{})
	if resp.Error != nil {
		t.Fatalf("error = %+v, want nil", resp.Error)
	}
}

func TestServer_Initialize(t *testing.T) {
	s := New(nil, "")
	resp := rpcCall(t, s, "", "initialize", map[string]any{})
	if resp.Error != nil {
		t.Fatalf("error = %+v", resp.Error)
	}
	r, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not map: %T", resp.Result)
	}
	if r["protocolVersion"] == nil {
		t.Errorf("missing protocolVersion")
	}
	caps, ok := r["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities is not map")
	}
	if _, ok := caps["tools"]; !ok {
		t.Errorf("missing tools capability")
	}
	srvInfo, _ := r["serverInfo"].(map[string]any)
	if srvInfo["name"] != "dora-metrics-mcp" {
		t.Errorf("serverInfo.name = %v", srvInfo["name"])
	}
}

func TestServer_ToolsList(t *testing.T) {
	s := New(nil, "")
	resp := rpcCall(t, s, "", "tools/list", map[string]any{})
	if resp.Error != nil {
		t.Fatalf("error = %+v", resp.Error)
	}
	r, _ := resp.Result.(map[string]any)
	tools, ok := r["tools"].([]any)
	if !ok {
		t.Fatalf("tools is not list")
	}
	if len(tools) < 4 {
		t.Errorf("got %d tools, want >= 4", len(tools))
	}

	// Sanity: nomes esperados estão na lista.
	names := map[string]bool{}
	for _, raw := range tools {
		t := raw.(map[string]any)
		names[t["name"].(string)] = true
	}
	for _, want := range []string{"getDoraMetrics", "getDeployments", "compareTeams", "explainTrend"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
}

func TestServer_MethodNotFound(t *testing.T) {
	s := New(nil, "")
	resp := rpcCall(t, s, "", "nonexistent/method", map[string]any{})
	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != errMethodNotFound {
		t.Errorf("code = %d, want %d", resp.Error.Code, errMethodNotFound)
	}
}

func TestServer_ToolCall_UnknownTool(t *testing.T) {
	s := New(nil, "")
	resp := rpcCall(t, s, "", "tools/call", map[string]any{
		"name":      "ghost",
		"arguments": map[string]any{},
	})
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if !strings.Contains(resp.Error.Message, "ghost") {
		t.Errorf("error message = %q, expected to contain 'ghost'", resp.Error.Message)
	}
}

func TestServer_ToolCall_InvalidArguments(t *testing.T) {
	s := New(nil, "")
	// chamando getDoraMetrics sem project_id nem team_id
	resp := rpcCall(t, s, "", "tools/call", map[string]any{
		"name":      "getDoraMetrics",
		"arguments": map[string]any{},
	})
	if resp.Error != nil {
		t.Fatalf("expected isError=true content, got JSON-RPC error: %+v", resp.Error)
	}
	r, _ := resp.Result.(map[string]any)
	if r["isError"] != true {
		t.Errorf("expected isError=true, got %+v", r)
	}
}

func TestServer_Ping(t *testing.T) {
	s := New(nil, "")
	resp := rpcCall(t, s, "", "ping", nil)
	if resp.Error != nil {
		t.Fatalf("error = %+v", resp.Error)
	}
}

func TestServer_ResourcesList(t *testing.T) {
	s := New(nil, "")
	resp := rpcCall(t, s, "", "resources/list", map[string]any{})
	if resp.Error != nil {
		t.Fatalf("error = %+v", resp.Error)
	}
	r, _ := resp.Result.(map[string]any)
	res, _ := r["resources"].([]any)
	if len(res) == 0 {
		t.Errorf("expected at least one resource, got 0")
	}
}

func TestServer_ResourcesRead_Schema(t *testing.T) {
	s := New(nil, "")
	resp := rpcCall(t, s, "", "resources/read", map[string]any{"uri": "dora://schema"})
	if resp.Error != nil {
		t.Fatalf("error = %+v", resp.Error)
	}
	r, _ := resp.Result.(map[string]any)
	contents, _ := r["contents"].([]any)
	if len(contents) == 0 {
		t.Fatal("expected contents")
	}
	c0 := contents[0].(map[string]any)
	if text, _ := c0["text"].(string); !strings.Contains(text, "elite") {
		t.Errorf("schema does not mention elite: %s", text)
	}
}

func TestServer_ParseError(t *testing.T) {
	s := New(nil, "")
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestServer_404(t *testing.T) {
	s := New(nil, "")
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestServer_MethodNotAllowed(t *testing.T) {
	s := New(nil, "")
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestParseDoraURI(t *testing.T) {
	cases := []struct {
		uri      string
		wantKind string
		wantOK   bool
	}{
		{"dora://project/00000000-0000-0000-0000-000000000001/dora-metrics", "project", true},
		{"dora://team/00000000-0000-0000-0000-000000000002/dora-metrics", "team", true},
		{"dora://team/00000000-0000-0000-0000-000000000002", "team", true},
		{"http://nope", "", false},
		{"dora://project/not-a-uuid/x", "", false},
	}
	for _, c := range cases {
		t.Run(c.uri, func(t *testing.T) {
			kind, _, ok := parseDoraURI(c.uri)
			if ok != c.wantOK {
				t.Errorf("ok = %v, want %v", ok, c.wantOK)
			}
			if c.wantOK && kind != c.wantKind {
				t.Errorf("kind = %s, want %s", kind, c.wantKind)
			}
		})
	}
}

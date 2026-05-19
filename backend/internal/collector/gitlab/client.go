// Package gitlab implementa a coleta de dados a partir da API REST do GitLab.
//
// Documentação: ../../../../docs/03-gitlab-integration.md
package gitlab

import (
	"context"
	"net/http"
	"time"
)

// Client é o cliente HTTP da API GitLab.
// TODO Fase 1: substituir por xanzy/go-gitlab para evitar reimplementar
// paginação, rate limit handling e auth.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient constrói o cliente.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Ping checa conectividade com a API GitLab.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v4/version", nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &APIError{StatusCode: resp.StatusCode}
	}
	return nil
}

// APIError representa um erro retornado pela API GitLab.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return http.StatusText(e.StatusCode)
}

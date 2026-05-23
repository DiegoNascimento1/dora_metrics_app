// Package github implementa a coleta de dados a partir da API REST do GitHub v3.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.github.com"

// Client é o cliente HTTP da API GitHub REST v3.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient constrói o cliente. token pode ser PAT ou GitHub App token.
func NewClient(token string) *Client {
	return &Client{
		baseURL: defaultBaseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClientWithBaseURL constrói o cliente com base URL customizada (útil
// para GitHub Enterprise e testes com httptest.Server).
func NewClientWithBaseURL(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ---- Tipos de domínio ----

// Deployment representa um deployment do GitHub.
type Deployment struct {
	ID          int64     `json:"id"`
	Ref         string    `json:"ref"`
	SHA         string    `json:"sha"`
	Environment string    `json:"environment"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Creator     struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	} `json:"creator"`
}

// DeploymentStatus representa um status de deployment do GitHub.
type DeploymentStatus struct {
	ID          int64     `json:"id"`
	State       string    `json:"state"` // "success", "failure", "error", "pending", "in_progress"
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Description string    `json:"description"`
	LogURL      string    `json:"log_url"`
}

// PullRequest representa um pull request do GitHub.
type PullRequest struct {
	Number         int        `json:"number"`
	Title          string     `json:"title"`
	State          string     `json:"state"`
	MergedAt       *time.Time `json:"merged_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	MergeCommitSHA *string    `json:"merge_commit_sha"`
	User           struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	} `json:"user"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	Base      struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

// Commit representa um commit do GitHub.
type Commit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author struct {
			Name  string    `json:"name"`
			Email string    `json:"email"`
			Date  time.Time `json:"date"`
		} `json:"author"`
		Committer struct {
			Name  string    `json:"name"`
			Email string    `json:"email"`
			Date  time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
	Author *struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	} `json:"author"`
}

// ---- Métodos de coleta ----

// ListDeployments lista os deployments de um repositório GitHub desde `since`.
// Pagina usando o header Link até não haver mais páginas.
func (c *Client) ListDeployments(ctx context.Context, owner, repo string, since time.Time) ([]Deployment, error) {
	q := url.Values{}
	q.Set("per_page", "100")
	if !since.IsZero() {
		// GitHub não filtra por data nativamente — filtramos no client.
		// Mas ordenamos por created desc para sair cedo assim que passarmos da janela.
	}

	var all []Deployment
	nextURL := fmt.Sprintf("%s/repos/%s/%s/deployments?%s",
		c.baseURL, url.PathEscape(owner), url.PathEscape(repo), q.Encode())

	for nextURL != "" {
		var batch []Deployment
		var next string
		var err error
		next, err = c.doGetJSONLink(ctx, nextURL, &batch)
		if err != nil {
			return nil, err
		}

		// Filtra pelo `since` e para de paginar se todos os itens forem mais antigos.
		passedWindow := false
		for _, d := range batch {
			if d.CreatedAt.Before(since) {
				passedWindow = true
				continue
			}
			all = append(all, d)
		}
		if passedWindow && len(batch) > 0 {
			break
		}
		nextURL = next
	}
	return all, nil
}

// GetDeploymentStatuses devolve os statuses de um deployment específico.
func (c *Client) GetDeploymentStatuses(ctx context.Context, owner, repo string, deploymentID int64) ([]DeploymentStatus, error) {
	q := url.Values{}
	q.Set("per_page", "100")

	var all []DeploymentStatus
	nextURL := fmt.Sprintf("%s/repos/%s/%s/deployments/%d/statuses?%s",
		c.baseURL, url.PathEscape(owner), url.PathEscape(repo), deploymentID, q.Encode())

	for nextURL != "" {
		var batch []DeploymentStatus
		next, err := c.doGetJSONLink(ctx, nextURL, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		nextURL = next
	}
	return all, nil
}

// ListPullRequests lista os PRs fechados (merged) de um repositório GitHub
// atualizados após `since`.
func (c *Client) ListPullRequests(ctx context.Context, owner, repo string, since time.Time) ([]PullRequest, error) {
	q := url.Values{}
	q.Set("state", "closed")
	q.Set("sort", "updated")
	q.Set("direction", "desc")
	q.Set("per_page", "100")

	var all []PullRequest
	nextURL := fmt.Sprintf("%s/repos/%s/%s/pulls?%s",
		c.baseURL, url.PathEscape(owner), url.PathEscape(repo), q.Encode())

	for nextURL != "" {
		var batch []PullRequest
		next, err := c.doGetJSONLink(ctx, nextURL, &batch)
		if err != nil {
			return nil, err
		}

		passedWindow := false
		for _, pr := range batch {
			// Só nos interessam PRs merged.
			if pr.MergedAt == nil {
				continue
			}
			if pr.UpdatedAt.Before(since) {
				passedWindow = true
				continue
			}
			all = append(all, pr)
		}
		if passedWindow {
			break
		}
		nextURL = next
	}
	return all, nil
}

// GetCommit devolve os detalhes de um commit específico (inclui data do author/committer).
func (c *Client) GetCommit(ctx context.Context, owner, repo, sha string) (Commit, error) {
	path := fmt.Sprintf("%s/repos/%s/%s/commits/%s",
		c.baseURL, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha))

	var commit Commit
	_, err := c.doGetJSONLink(ctx, path, &commit)
	return commit, err
}

// Ping verifica a conectividade com a API GitHub.
func (c *Client) Ping(ctx context.Context) error {
	var result struct{}
	_, err := c.doGetJSONLink(ctx, c.baseURL+"/user", &result)
	return err
}

// ---- helpers HTTP ----

// doGetJSONLink executa GET na URL completa e devolve o próximo URL de
// paginação (extraído do header Link: <url>; rel="next") ou "" se última
// página.
func (c *Client) doGetJSONLink(ctx context.Context, fullURL string, out any) (nextURL string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1*1024))
		return "", &APIError{StatusCode: resp.StatusCode, Message: string(body)}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", &APIError{StatusCode: resp.StatusCode, Message: "unauthorized — verifique GITHUB_TOKEN"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return "", &APIError{StatusCode: resp.StatusCode, Message: string(body)}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return "", fmt.Errorf("decode response: %w", err)
		}
	}

	// Extrai rel="next" do header Link.
	nextURL = parseLinkNext(resp.Header.Get("Link"))
	return nextURL, nil
}

// parseLinkNext extrai a URL com rel="next" do header Link do GitHub.
// Formato: <https://...>; rel="next", <https://...>; rel="last"
func parseLinkNext(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.Split(header, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		segments := strings.Split(part, ";")
		if len(segments) < 2 {
			continue
		}
		rel := strings.TrimSpace(segments[1])
		if rel != `rel="next"` {
			continue
		}
		// Extrai a URL entre < e >.
		link := strings.TrimSpace(segments[0])
		if len(link) > 2 && link[0] == '<' && link[len(link)-1] == '>' {
			return link[1 : len(link)-1]
		}
	}
	return ""
}

// APIError representa um erro retornado pela API GitHub.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("github api: %d %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("github api: %d %s", e.StatusCode, http.StatusText(e.StatusCode))
}

// ---- helpers para teste ----

// extractPageFromURL extrai o parâmetro ?page= de uma URL (útil para mocks).
func extractPageFromURL(rawURL string) int {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 1
	}
	n, _ := strconv.Atoi(u.Query().Get("page"))
	if n < 1 {
		return 1
	}
	return n
}

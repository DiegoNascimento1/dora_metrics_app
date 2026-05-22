// Package gitlab implementa a coleta de dados a partir da API REST do GitLab.
//
// Documentação: ../../../../docs/03-gitlab-integration.md
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client é o cliente HTTP da API GitLab v4.
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

// Environment é a projeção mínima de um environment GitLab.
type Environment struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// Deployment é a projeção mínima de um deployment GitLab.
type Deployment struct {
	ID          int         `json:"id"`
	IID         int         `json:"iid"`
	Ref         string      `json:"ref"`
	SHA         string      `json:"sha"`
	Status      string      `json:"status"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	FinishedAt  *time.Time  `json:"finished_at"`
	Environment Environment `json:"environment"`
	Deployable  *Deployable `json:"deployable"`
	User        *User       `json:"user"`
}

// Deployable representa o job CI que produziu o deployment.
type Deployable struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	User       *User      `json:"user"`
}

// User identifica um usuário GitLab.
type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// ListEnvironments devolve os ambientes do projeto.
// GitLab limita a 200 por padrão; suficiente para a Fase 1.
func (c *Client) ListEnvironments(ctx context.Context, projectID string) ([]Environment, error) {
	path := fmt.Sprintf("/api/v4/projects/%s/environments?per_page=100", url.PathEscape(projectID))

	var envs []Environment
	if err := c.doGetJSON(ctx, path, &envs); err != nil {
		return nil, err
	}
	return envs, nil
}

// Member é a projeção de um membro de projeto/grupo GitLab usada para
// alimentar `platform.person_identity` (kind=gitlab).
//
// Apenas os campos que casam com `internal/identities` heurística:
// id (external_id), username, name, public_email.
type Member struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	Name        string `json:"name"`
	PublicEmail string `json:"public_email"`
	WebURL      string `json:"web_url"`
}

// ListProjectMembers percorre as páginas e devolve todos os membros do
// projeto (inclui herdados do grupo). Esquerda do pipeline da Fase 3.5:
// alimenta person_identity para o auto-match heurístico.
//
// Endpoint: GET /api/v4/projects/:id/members/all?per_page=100&page=N
// (`/all` inclui herdados do grupo; `/members` direto seria só os
// adicionados explicitamente no projeto).
func (c *Client) ListProjectMembers(ctx context.Context, projectID string) ([]Member, error) {
	var all []Member
	page := 1
	for {
		path := fmt.Sprintf(
			"/api/v4/projects/%s/members/all?per_page=100&page=%d",
			url.PathEscape(projectID), page,
		)
		var batch []Member
		nextPage, err := c.doGetJSONPaged(ctx, path, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if nextPage == 0 {
			break
		}
		page = nextPage
	}
	return all, nil
}

// ListDeploymentsOpts agrupa os filtros suportados.
type ListDeploymentsOpts struct {
	Environment  string
	Status       string
	UpdatedAfter time.Time // zero = sem filtro
	PerPage      int
}

// ListDeployments percorre as páginas e devolve todos os deployments
// que casam com os filtros, em ordem crescente de updated_at.
func (c *Client) ListDeployments(ctx context.Context, projectID string, opts ListDeploymentsOpts) ([]Deployment, error) {
	if opts.PerPage <= 0 || opts.PerPage > 100 {
		opts.PerPage = 100
	}

	q := url.Values{}
	q.Set("per_page", strconv.Itoa(opts.PerPage))
	q.Set("order_by", "updated_at")
	q.Set("sort", "asc")
	if opts.Environment != "" {
		q.Set("environment", opts.Environment)
	}
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if !opts.UpdatedAfter.IsZero() {
		q.Set("updated_after", opts.UpdatedAfter.UTC().Format(time.RFC3339))
	}

	var all []Deployment
	page := 1
	for {
		q.Set("page", strconv.Itoa(page))
		path := fmt.Sprintf("/api/v4/projects/%s/deployments?%s", url.PathEscape(projectID), q.Encode())

		var batch []Deployment
		nextPage, err := c.doGetJSONPaged(ctx, path, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if nextPage == 0 {
			break
		}
		page = nextPage
	}
	return all, nil
}

// MergeRequest é a projeção mínima de um MR GitLab.
type MergeRequest struct {
	ID             int        `json:"id"`
	IID            int        `json:"iid"`
	Title          string     `json:"title"`
	State          string     `json:"state"`
	TargetBranch   string     `json:"target_branch"`
	SourceBranch   string     `json:"source_branch"`
	MergedAt       *time.Time `json:"merged_at"`
	MergeCommitSHA *string    `json:"merge_commit_sha"`
	SquashCommitSHA *string   `json:"squash_commit_sha"`
	WebURL         string     `json:"web_url"`
	Labels         []string   `json:"labels"`
	Author         *User      `json:"author"`
	Additions      *int       `json:"user_additions"`
	Deletions      *int       `json:"user_deletions"`
}

// Commit é a projeção mínima de um commit (usado para `first_commit_at`).
type Commit struct {
	ID            string    `json:"id"`
	ShortID       string    `json:"short_id"`
	AuthoredDate  time.Time `json:"authored_date"`
	CommittedDate time.Time `json:"committed_date"`
	AuthorName    string    `json:"author_name"`
}

// ListMergeRequestsOpts agrupa filtros do listing de MRs.
type ListMergeRequestsOpts struct {
	State        string // "merged", "opened", "closed", "all"
	TargetBranch string
	UpdatedAfter time.Time
	PerPage      int
}

// ListMergeRequests percorre páginas e devolve os MRs que casam com os filtros,
// ordenados por updated_at ASC.
func (c *Client) ListMergeRequests(ctx context.Context, projectID string, opts ListMergeRequestsOpts) ([]MergeRequest, error) {
	if opts.PerPage <= 0 || opts.PerPage > 100 {
		opts.PerPage = 100
	}

	q := url.Values{}
	q.Set("per_page", strconv.Itoa(opts.PerPage))
	q.Set("order_by", "updated_at")
	q.Set("sort", "asc")
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	if opts.TargetBranch != "" {
		q.Set("target_branch", opts.TargetBranch)
	}
	if !opts.UpdatedAfter.IsZero() {
		q.Set("updated_after", opts.UpdatedAfter.UTC().Format(time.RFC3339))
	}

	var all []MergeRequest
	page := 1
	for {
		q.Set("page", strconv.Itoa(page))
		path := fmt.Sprintf("/api/v4/projects/%s/merge_requests?%s", url.PathEscape(projectID), q.Encode())

		var batch []MergeRequest
		nextPage, err := c.doGetJSONPaged(ctx, path, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if nextPage == 0 {
			break
		}
		page = nextPage
	}
	return all, nil
}

// ListMRCommits devolve os commits associados a um MR (todos os commits da
// branch, na ordem em que aparecem no MR). O primeiro commit corresponde ao
// `first_commit_at` usado no cálculo de Lead Time.
func (c *Client) ListMRCommits(ctx context.Context, projectID string, mrIID int) ([]Commit, error) {
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/commits?per_page=100",
		url.PathEscape(projectID), mrIID)

	var all []Commit
	page := 1
	for {
		pagedPath := fmt.Sprintf("%s&page=%d", path, page)
		var batch []Commit
		nextPage, err := c.doGetJSONPaged(ctx, pagedPath, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if nextPage == 0 {
			break
		}
		page = nextPage
	}
	return all, nil
}

// Ping checa conectividade.
func (c *Client) Ping(ctx context.Context) error {
	return c.doGetJSON(ctx, "/api/v4/version", &struct{}{})
}

func (c *Client) doGetJSON(ctx context.Context, path string, out any) error {
	_, err := c.doGetJSONPaged(ctx, path, out)
	return err
}

// doGetJSONPaged executa GET e devolve, além do erro, o próximo número de página
// (0 se for a última). Lê o header X-Next-Page do GitLab.
func (c *Client) doGetJSONPaged(ctx context.Context, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("gitlab request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return 0, &APIError{StatusCode: resp.StatusCode, Message: string(body)}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return 0, fmt.Errorf("decode response: %w", err)
		}
	}

	nextPage := 0
	if v := resp.Header.Get("X-Next-Page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			nextPage = n
		}
	}
	return nextPage, nil
}

// APIError representa um erro retornado pela API GitLab.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("gitlab api: %d %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("gitlab api: %d %s", e.StatusCode, http.StatusText(e.StatusCode))
}

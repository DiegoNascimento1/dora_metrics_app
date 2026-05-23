// Package llm fornece integração com o Anthropic Claude para geração de
// narrativas sobre métricas DORA. Usa prompt caching para reduzir custo
// com o system prompt estático (definições DORA + thresholds).
package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Modelo padrão para geração de narrativas.
const defaultModel = "claude-sonnet-4-6"

// systemPrompt define as métricas DORA e os thresholds por tier.
// Fica em cache (CacheControl ephemeral) para evitar re-processamento.
const systemPrompt = `Você é um especialista em DevOps e DORA Metrics (DevOps Research and Assessment).

# Definições DORA

As 4 métricas DORA medem a performance de entrega de software:

1. **Deployment Frequency (DF)** — Com que frequência a organização implanta código em produção.
2. **Lead Time for Changes (LT)** — Tempo desde o primeiro commit até chegar em produção.
3. **Change Failure Rate (CFR)** — Porcentagem de deployments que causam falha em produção.
4. **Mean Time to Restore (MTTR)** — Tempo médio para restaurar o serviço após uma falha.

# Classificação por Tier

| Tier   | DF                | LT            | CFR     | MTTR       |
|--------|-------------------|---------------|---------|------------|
| Elite  | múltiplas/dia     | < 1 hora      | < 5%    | < 1 hora   |
| High   | 1/semana a 1/dia  | 1 dia a 1 sem | 5-10%   | < 1 dia    |
| Medium | 1/mês a 1/semana  | 1 sem a 1 mês | 10-20%  | 1 dia a 1 sem |
| Low    | < 1/mês           | > 1 mês       | > 20%   | > 1 semana |

# Thresholds numéricos

- DF Elite: >= 1.0 deploys/dia | High: >= 0.143/dia | Medium: >= 0.033/dia
- LT Elite: < 3600s | High: < 604800s | Medium: < 2592000s
- CFR Elite: < 5% | High: < 10% | Medium: < 20%
- MTTR Elite: < 3600s | High: < 86400s | Medium: < 604800s

# Sua função

Analise os dados fornecidos e gere uma narrativa concisa (3-5 parágrafos) em português brasileiro que:
- Descreva o estado atual das métricas e o tier combinado
- Compare com o período anterior quando disponível
- Destaque pontos de atenção (métricas piores) e conquistas (métricas melhores)
- Sugira 1-2 ações concretas para melhorar as métricas mais fracas
- Seja direto e objetivo, sem repetir os dados brutos desnecessariamente

Responda APENAS com a narrativa textual, sem markdown ou formatação especial.`

// Client é o cliente LLM. Nil é válido — todos os métodos retornam
// valores vazios sem erro quando c == nil (graceful no-op).
type Client struct {
	inner *anthropic.Client
}

// New constrói o Client. Retorna nil se apiKey for vazio — os callers
// devem usar o template de fallback nesse caso.
func New(apiKey string) *Client {
	if apiKey == "" {
		return nil
	}
	c := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &Client{inner: &c}
}

// MetricSnapshot representa o estado das 4 métricas em uma janela.
type MetricSnapshot struct {
	LeadTimeHours     float64 `json:"lead_time_hours"`
	DeployFreqPerDay  float64 `json:"deploy_freq_per_day"`
	ChangeFailureRate float64 `json:"change_failure_rate"`
	MTTRHours         float64 `json:"mttr_hours"`
	Tier              string  `json:"tier"`
}

// DeploymentSummary é um resumo de deployment para contexto LLM.
type DeploymentSummary struct {
	Ref        string `json:"ref"`
	Status     string `json:"status"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// IncidentSummary é um resumo de incident para contexto LLM.
type IncidentSummary struct {
	Summary    string `json:"summary"`
	Priority   string `json:"priority,omitempty"`
	ResolvedAt string `json:"resolved_at,omitempty"`
}

// ExplainTrendInput agrega os dados enviados ao LLM para geração da narrativa.
type ExplainTrendInput struct {
	ProjectName    string              `json:"project_name"`
	Window         string              `json:"window"`
	Current        MetricSnapshot      `json:"current"`
	Previous       MetricSnapshot      `json:"previous"`
	TopDeployments []DeploymentSummary `json:"top_deployments,omitempty"`
	TopIncidents   []IncidentSummary   `json:"top_incidents,omitempty"`
}

// ExplainTrend gera uma narrativa textual sobre a tendência das métricas
// usando o Claude. Se c == nil ou se ocorrer erro, retorna "" para que o
// caller use o template determinístico como fallback.
func (c *Client) ExplainTrend(ctx context.Context, input ExplainTrendInput) (string, error) {
	if c == nil {
		return "", nil
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal input: %w", err)
	}

	// System prompt com cache ephemeral — evita re-processar os ~700 tokens
	// estáticos de definições DORA a cada chamada.
	sysBlock := anthropic.TextBlockParam{
		Text:         systemPrompt,
		CacheControl: anthropic.NewCacheControlEphemeralParam(),
	}

	msg, err := c.inner.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(defaultModel),
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{sysBlock},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(string(inputJSON))),
		},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic messages.new: %w", err)
	}

	if len(msg.Content) == 0 {
		return "", nil
	}

	// Extrai o texto da primeira resposta.
	for _, block := range msg.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}
	return "", nil
}

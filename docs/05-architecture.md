# 05 — Arquitetura

## Visão de alto nível

```
                    ┌───────────────────────────────────────────────┐
                    │              PLATAFORMA DORA                  │
                    │                                               │
   GitLab ─┐        │  ┌──────────────┐    ┌─────────────────────┐  │
           ├──────► │  │   Coletor    │    │   Calculadora       │  │
   Jira  ──┘        │  │  (workers)   │ ─► │  (rules engine)     │  │
   (via              │  │              │    │                     │  │
   MCP)             │  │  • Webhook   │    │  • LTC              │  │
                    │  │    receivers │    │  • DF               │  │
                    │  │  • Pollers   │    │  • CFR              │  │
                    │  │  • Reconcile │    │  • MTTR             │  │
                    │  └──────┬───────┘    └─────────┬───────────┘  │
                    │         │                      │              │
                    │         ▼                      ▼              │
                    │  ┌─────────────────────────────────────────┐  │
                    │  │            Armazenamento                │  │
                    │  │   (eventos brutos + agregações)         │  │
                    │  └─────────────────────────────────────────┘  │
                    │                       │                       │
                    │       ┌───────────────┼───────────────┐       │
                    │       ▼               ▼               ▼       │
                    │  ┌─────────┐   ┌─────────────┐   ┌─────────┐  │
                    │  │   API   │   │   MCP       │   │ Alertas │  │
                    │  │  REST/  │   │  Server     │   │ engine  │  │
                    │  │  GraphQL│   │ (próprio)   │   │         │  │
                    │  └────┬────┘   └─────────────┘   └────┬────┘  │
                    │       │                               │       │
                    └───────┼───────────────────────────────┼───────┘
                            ▼                               ▼
                    ┌──────────────┐               ┌──────────────┐
                    │  Dashboard   │               │ Webhooks /   │
                    │  Web (UI)    │               │ Email / Slack│
                    └──────────────┘               └──────────────┘
```

## Componentes

### 1. Coletor

Responsável por trazer dados externos para dentro do nosso armazenamento. Três modos:

- **Webhook receivers:** endpoints HTTPS que recebem eventos do GitLab e do Jira em tempo real. Validam assinatura, normalizam payload, gravam em uma `inbox` de eventos brutos e disparam processamento.
- **Pollers:** workers agendados que fazem polling em endpoints REST (GitLab) ou tools MCP (Jira). Usados para descoberta inicial (backfill) e reconciliação.
- **Reconciliação:** job diário que compara o estado externo dos últimos N dias com o que temos no nosso armazenamento e preenche o que falta (cobertura para webhooks perdidos).

### 2. Calculadora

Lê eventos brutos e produz métricas. Estratégia híbrida:

- **Incremental (event-driven):** ao chegar um evento novo (ex: `deployment success`), recalcula apenas o que aquela mudança afeta (Lead Time daquele deploy, contagem de DF do dia).
- **Janela rolante:** job recorrente (a cada 1h) recalcula janelas rolantes de 7/30/90 dias por time/projeto.
- **Snapshot mensal:** job no fim do mês congela valores em uma tabela de histórico imutável.

A calculadora **não acessa GitLab/Jira diretamente** — ela opera só sobre eventos no nosso storage. Isso garante reprodutibilidade (recalcular o passado dá o mesmo resultado).

### 3. Armazenamento

Duas camadas lógicas (independente de qual banco escolher):

- **Eventos brutos:** uma linha por evento externo (deployment, MR, incident). Preserva o payload original (JSON) + colunas extraídas para indexação.
- **Agregações:** tabelas materializadas por `(projeto, time, janela_tempo, métrica)`. Lidas pelos dashboards.

Modelo detalhado em [06-data-model.md](06-data-model.md).

### 4. API

Expõe agregações para consumo:

- **REST**: endpoints simples — `/metrics/{team}/dora?from=...&to=...`.
- **GraphQL** (opcional, fase 2): queries com profundidade variável — dashboard pode pedir métricas + drill-down em uma chamada.

**Autenticação:** OIDC com IdP da organização (Okta, Keycloak, Azure AD).

### 5. Servidor MCP próprio

Em Fase 3, exposição das nossas métricas via MCP para consumo por agentes/LLMs. Ver [02-mcp-protocol.md § Expor nosso próprio servidor MCP](02-mcp-protocol.md#expor-nosso-pr%C3%B3prio-servidor-mcp-futuro).

### 6. Engine de alertas

Regras configuráveis ("se CFR > 20% em janela 7d, notificar #squad-payments"). Webhook out / email / Slack. Baseada em **mudanças de estado** das agregações para evitar spam.

### 7. Dashboard Web

Frontend que consome a API. Visualizações:

- 4 tiles principais (uma por métrica DORA) com classificação Elite/High/Medium/Low colorida.
- Séries temporais (linha) com janela ajustável.
- Drill-down: clicar em um valor anômalo abre lista dos deploys/incidentes que compõem.
- Comparação multi-time (até 4 times lado a lado).
- Comparação histórica (este mês vs mês passado).

## Decisão D1 — Stack

A escolha da stack precisa equilibrar 4 critérios:

1. **Maturidade do MCP SDK** (cliente, porque consumimos Atlassian)
2. **Familiaridade do time** (não temos info ainda)
3. **Ecossistema de jobs/queues** (workers, scheduling)
4. **Ferramental de dashboard** (componentes de gráfico)

### Opção A — Python (FastAPI + Celery/Arq)

**Prós:**
- MCP Python SDK é o mais maduro.
- `python-gitlab` e bibliotecas Jira estabelecidas.
- Pandas para cálculos pontuais em backfill.
- Frontend separado em React/Vue — Python só backend.

**Contras:**
- Dois codebases (backend Python + frontend JS).
- Performance de cálculos massivos requer Pandas/NumPy, mais um stack.

### Opção B — Node/TypeScript (Fastify + BullMQ + Next.js)

**Prós:**
- Um único codebase TS para backend + frontend.
- MCP TS SDK maduro.
- `@gitbeaker/node` para GitLab, `jira.js` para Jira REST.
- Tipagem compartilhada entre backend e frontend.

**Contras:**
- Cálculos de séries temporais menos confortáveis que em Python.
- Modelo de dados/migrations menos padronizado.

### Opção C — Go

**Prós:**
- Excelente para workers concorrentes (coletor).
- Single binary deploy.
- Performance excelente para reconciliação em larga escala.

**Contras:**
- MCP SDK comunitário, menos polido.
- Frontend obriga stack separada.
- Time pequeno geralmente não escolhe Go primeiro a menos que já saiba.

### Recomendação

**Opção A (Python)** se o time vai construir só backend e o frontend será separado por outra pessoa/equipe.

**Opção B (Node/TS)** se o mesmo time vai construir tudo end-to-end. Codebase único acelera iteração.

**Opção C (Go)** apenas se houver razão específica (volume gigante de projetos GitLab > 10k).

A decisão fica em aberto. Registrar em `docs/adr/0001-stack.md` quando tomada.

## Fluxos principais

### Fluxo 1 — Deploy chega via webhook

```
GitLab → POST /webhooks/gitlab
    1. Validar X-Gitlab-Token
    2. Persistir evento bruto em raw_events
    3. Enfileirar job "process_deployment"
    4. Responder 200 (rápido — não bloquear o GitLab)

Worker pega job:
    1. Ler raw_event
    2. Inserir/atualizar deployments
    3. Resolver MRs ancestrais ao SHA (ver 03-gitlab-integration.md)
    4. Calcular Lead Time para cada MR resolvido
    5. Atualizar agregações de DF e LTC
    6. Notificar alert engine se mudança de estado em métrica
```

### Fluxo 2 — Reconciliação noturna

```
Cron 03:00 UTC:
    Para cada projeto monitorado:
        - GET deployments updated_after=ontem-48h
        - GET merge_requests updated_after=ontem-48h
        - MCP searchJiraIssues created >= ontem-48h AND issuetype=Incident
        - Upsert tudo (idempotente)
        - Recalcular agregações afetadas
```

### Fluxo 3 — Dashboard pede métricas

```
GET /api/metrics/squad-payments/dora?window=30d
    1. Hit em tabela agregada (não recalcula on-the-fly)
    2. Retorna {lead_time_median, deploy_freq, cfr, mttr_mean, classification}
    3. Cache HTTP 60s (agregações mudam no máximo a cada minuto)
```

## Considerações de produção

- **Idempotência:** todo upsert chaveado pelo ID externo (gitlab_deployment_id, jira_issue_key). Webhook duplicado não deve dobrar contagem.
- **Backpressure:** workers com concurrency limit configurável; fila com max length para não acumular durante incidentes.
- **Observabilidade:** logs estruturados (JSON), métricas Prometheus, traces OpenTelemetry. Coleta de dados externos é exatamente o tipo de coisa que falha silenciosamente — instrumentar bem.
- **Segurança:** tokens em vault (não em env var em texto claro em produção). Webhook secrets rotacionáveis.
- **Multi-tenant (futuro):** isolar por `tenant_id` em todas as tabelas desde o começo, mesmo que comecemos single-tenant. Custa pouco agora; custa muito depois.

## Não-objetivos

Para não inflar o produto, estas são explicitamente coisas que **não** vamos fazer no escopo inicial:

- Substituir o Jira/GitLab para gestão (não somos uma ferramenta de issue tracking ou git).
- Análise de qualidade de código (CodeClimate, SonarQube fazem isso).
- APM/observabilidade de runtime (Datadog, New Relic).
- Predição de incidentes via ML (depois, quando tivermos histórico).
- Substituir o DORA Quick Check oficial (faremos algo similar mas focado em métricas, não em práticas).

## Fontes

- Doc DORA dentro do projeto: [01-dora-metrics.md](01-dora-metrics.md)
- Doc MCP: [02-mcp-protocol.md](02-mcp-protocol.md)
- Doc GitLab: [03-gitlab-integration.md](03-gitlab-integration.md)
- Doc Jira: [04-jira-integration.md](04-jira-integration.md)

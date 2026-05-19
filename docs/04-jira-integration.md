# 04 — Integração Jira

O Jira fornece os sinais que **não vivem no GitLab**: incidentes de produção, releases, sprints, e o vínculo entre código e demanda de negócio. Para nosso produto, o Jira é a **fonte primária de Change Failure Rate (CFR)** e **MTTR**.

## Estratégia: MCP primary, REST fallback

Vamos consumir o Jira preferencialmente via **Atlassian Rovo MCP Server** (ver [02-mcp-protocol.md](02-mcp-protocol.md)). Quando uma operação não estiver coberta pelas tools MCP, **fallback explícito** para Jira Cloud REST API v3.

```
┌─────────────────────────┐
│  Coletor Jira           │
│                         │   1ª tentativa: MCP
│   ┌──────────────────┐  │ ─────────────────────► Atlassian Rovo MCP
│   │ JiraClient       │  │
│   │  .search()       │  │   fallback p/ operações
│   │  .getIssue()     │  │   não cobertas:
│   │  .listVersions() │  │ ─────────────────────► Jira Cloud REST v3
│   └──────────────────┘  │
└─────────────────────────┘
```

A camada `JiraClient` abstrai isso. O resto do código não precisa saber qual transporte foi usado.

## Ferramentas do Atlassian Rovo MCP (Jira)

O servidor expõe um conjunto de tools que evolui. Lista representativa relevante para DORA (validar via `tools/list` na inicialização):

| Tool                             | Uso para DORA                                                       |
| -------------------------------- | ------------------------------------------------------------------- |
| `getAccessibleAtlassianResources`| Listar `cloudId` dos sites Atlassian acessíveis                     |
| `searchJiraIssuesUsingJql`       | Buscar incidentes, bugs, releases via JQL                           |
| `getJiraIssue`                   | Detalhe de uma issue específica (campos, transições, histórico)     |
| `getJiraIssueRemoteIssueLinks`   | Links externos (ex: link com MR do GitLab via Smart Commits)        |
| `getJiraIssueWorklog`            | Histórico de trabalho/tempo                                         |
| `getTransitionsForJiraIssue`     | Estados possíveis (úteis para inferir status de incidente)          |
| `lookupJiraAccountId`            | Mapear usuário → accountId                                          |

> A lista exata muda; tratar como capability discovery na inicialização. Logar warning quando uma tool esperada não estiver disponível.

## JQL — a linguagem-chave

JQL (Jira Query Language) é como filtrar issues. Toda a coleta de DORA via Jira parte de queries JQL.

### Incidentes (CFR/MTTR)

```
project in ("PAY", "AUTH", "WEB")
AND issuetype = "Incident"
AND created >= "2026-01-01"
ORDER BY created DESC
```

### Bugs de produção (alternativa quando não há issuetype Incident)

```
project = "PAY"
AND issuetype = "Bug"
AND priority in ("Critical", "Highest")
AND labels = "production"
AND created >= -30d
```

### Issues vinculadas a um deploy (via fix version)

```
project = "PAY"
AND fixVersion = "v2.45.0"
```

### Issues mencionadas em commits (Smart Commits)

Quando o time usa Smart Commits do GitLab, o Jira mantém `remote links` automáticos para os commits/MRs. Buscar essas issues via `getJiraIssueRemoteIssueLinks` permite correlacionar issue ↔ deploy.

## Identificação de incidentes

**[Decisão D4]** — qual issue Jira é um "incidente de produção" para fins de CFR/MTTR?

Opções, ordenadas da mais robusta:

1. **`issuetype = "Incident"`** (Jira Service Management). ✅ Recomendado. Tipos nativos são padronizados e auditáveis.
2. **`issuetype = "Bug" AND labels = "production-incident"`** quando JSM não estiver em uso.
3. **`priority = "Highest" AND created within 24h of deploy`** — frágil; depende de SLA do time.
4. **Integração externa (PagerDuty, Opsgenie)** que cria/sincroniza issues. Bom em organizações maduras.

**Recomendação:** começar com (1) configurável por projeto. Permitir definir uma **JQL customizada por projeto** no catálogo da plataforma — assim cada time pode usar a convenção que já tem.

## Cálculo de MTTR a partir do Jira

Para cada incident:

```
mttr_seconds = resolution_datetime - created_datetime
```

Campos:

- **Início:** `created`. Idealmente, o time registra o incidente o mais cedo possível (quando o usuário foi impactado). Alguns times preferem usar um custom field `impact_started_at` se ele existir — configurável.
- **Fim:** `resolutiondate`. Se ausente, fallback para a transição mais recente para status de categoria `Done`.

**Issues reabertas:** considerar apenas o último ciclo open→closed para o cálculo (ou somar os ciclos, configurável).

## Cálculo de CFR a partir do Jira

Numerador: deployments associados a um incidente. Estratégias de associação:

1. **Janela temporal:** incidentes criados entre `deploy.finished_at` e `deploy.finished_at + N horas` (N configurável, default 24h). Atribui ao deploy mais recente em produção daquele projeto.
2. **Vínculo explícito:** issue tem `fixVersion` ou link remoto apontando para o release/MR/commit do deploy.
3. **Tag/label:** incident tem label `caused-by-deploy-<sha>` definida manualmente pelo time.

**Recomendação:** usar (1) como default e enriquecer com (2) quando disponível. Permitir que o time corrija manualmente associações no UI (importante para credibilidade do CFR).

## Releases (fix versions)

Quando o time usa `fixVersion` consistente, ganhamos um sinal valioso para Lead Time:

```
GET /rest/api/3/project/{projectIdOrKey}/versions
```

Cada version tem `releaseDate`. Issues marcadas com aquela version compõem o "conteúdo" da release. Útil para granular Lead Time por release em vez de por commit.

Não obrigatório no MVP — apenas times disciplinados usam fixVersion bem.

## Sprints (opcional)

Métricas DORA não usam sprint, mas para o dashboard final é útil cruzar:

- "Quantas issues fechamos por sprint" (throughput)
- "Quantos incidentes por sprint" (qualidade)

Acesso via API Agile:

```
GET /rest/agile/1.0/board/{boardId}/sprint
GET /rest/agile/1.0/sprint/{sprintId}/issue
```

Via MCP: nem todas as tools cobrem agile — provável fallback REST.

## Fallback REST: Jira Cloud API v3

Base URL: `https://<site>.atlassian.net/rest/api/3/`

Autenticação:
- **API token (basic):** `Authorization: Basic base64(email:api_token)` — simples, recomendado para MVP.
- **OAuth 2.0 (3LO):** para multi-tenant.

Endpoints principais (usar quando MCP não cobrir):

```
GET  /rest/api/3/search/jql?jql=...&fields=...&nextPageToken=...
GET  /rest/api/3/issue/{issueIdOrKey}?fields=...&expand=changelog
GET  /rest/api/3/issue/{issueIdOrKey}/changelog
GET  /rest/api/3/project/{projectIdOrKey}/versions
GET  /rest/api/3/field
```

**Atenção — migração v3 → v3 enhanced search (2024+):** a busca via JQL na API v3 passou a usar paginação por `nextPageToken` em vez de `startAt`/`maxResults` para resultados grandes. Implementar conforme a versão atual da doc oficial Atlassian.

**Custom fields:** o Jira de cada cliente tem custom fields com IDs únicos (`customfield_10042` etc). Não hard-codar — descobrir via `/rest/api/3/field` na inicialização e mapear por nome.

## Webhooks Jira

Cadastro via UI ou API:

```
POST /rest/api/3/webhook
{
  "webhooks": [
    {
      "events": ["jira:issue_created", "jira:issue_updated", "jira:issue_deleted"],
      "jqlFilter": "project in (PAY, AUTH, WEB) AND issuetype = Incident",
      "fieldIdsFilter": ["status", "resolution"]
    }
  ],
  "url": "https://nossa-plataforma/webhooks/jira"
}
```

**Limitação relevante:** webhooks Jira têm **TTL de 30 dias** — precisam ser renovados (`PUT /webhook/refresh`) periodicamente. Tratar isso no scheduler como job recorrente.

**Verificação:** Jira assina o payload com HMAC-SHA256 quando configurado com `secret`. Validar.

**Resiliência:** mesma estratégia do GitLab — webhooks + reconciliação periódica via JQL.

## Rate limits

**Jira Cloud:** budget por usuário, baseado em "cost" por endpoint (não em raw req/min). Endpoints leves (`GET /issue/{key}`) custam pouco; endpoints pesados (`search` com expand) custam muito.

**Headers de resposta:**
- `X-RateLimit-NearLimit: true` (warning)
- `Retry-After: <seconds>` em 429

**Estratégia:**
1. Backoff exponencial.
2. Preferir `search` paginado em vez de N chamadas individuais para `getIssue`.
3. Usar `fields` para pedir apenas o necessário (não fazer `?expand=*`).

## Mapeamento Jira → métricas DORA

| Métrica | Fontes Jira |
| ------- | ----------- |
| **Lead Time** | Não primário; útil só se cruzar issue→MR via Smart Commits (issue.created → deploy do MR vinculado) |
| **Deployment Frequency** | Não vem do Jira |
| **Change Failure Rate** | Numerador: contagem de incidents criados em janela pós-deploy |
| **MTTR** | Média de `(resolutiondate - created)` para incidents resolvidos na janela |

**Métricas auxiliares úteis (ainda do Jira):**

- Throughput de stories fechadas por sprint
- Tempo médio em status (`In Review`, `In QA`) — componentes do Lead Time amplo
- Backlog de bugs em aberto por prioridade

## Considerações operacionais

- **Multi-site:** uma organização pode ter múltiplos sites Atlassian (`empresa.atlassian.net`, `empresa-staging.atlassian.net`). Modelo de dados precisa de `site_id`.
- **Permissões:** o token/usuário precisa enxergar os projetos. Documentar permissões mínimas: **Browse Projects** + **View Development Tools** (para ver remote links com MRs).
- **Server/Data Center:** Atlassian descontinuou Server. Data Center ainda existe mas o MCP server é apenas para Cloud. Para clientes Data Center, vamos cair direto na REST v3 (algumas diferenças de payload).
- **Idiomas/timezone:** issues podem ter datas em timezone do site Atlassian, não UTC. Sempre normalizar para UTC na ingestão.

## Fontes

- Atlassian — [Atlassian MCP Server (GitHub)](https://github.com/atlassian/atlassian-mcp-server)
- Atlassian — [Use Atlassian Rovo MCP Server](https://support.atlassian.com/atlassian-rovo-mcp-server/docs/use-atlassian-rovo-mcp-server/)
- Jira Cloud REST API v3: `https://developer.atlassian.com/cloud/jira/platform/rest/v3/`
- Jira Webhooks: `https://developer.atlassian.com/cloud/jira/platform/webhooks/`
- JQL reference: `https://support.atlassian.com/jira-software-cloud/docs/advanced-search-reference-jql-fields/`

> Mantido com base em conhecimento de produto Atlassian Cloud + retorno de pesquisa sobre o servidor MCP. Antes de implementar, listar as tools efetivamente disponíveis via `tools/list` no endpoint `mcp.atlassian.com/v1/mcp`.

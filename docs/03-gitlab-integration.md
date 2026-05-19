# 03 — Integração GitLab

GitLab é a **fonte de verdade** para os eventos de código e deploy: commits, merge requests, pipelines, deployments e environments. Este documento mapeia o que precisamos do GitLab, com que API obter cada dado, e como ingerir de forma eficiente.

## Visão geral da escolha de API

GitLab oferece três interfaces principais:

| Interface     | Quando usar                                                                                   |
| ------------- | --------------------------------------------------------------------------------------------- |
| **REST v4**   | Operações point-in-time, polling agendado, paginação previsível. **Default da plataforma.**   |
| **GraphQL**   | Quando precisar de queries com profundidade variável (ex: MR + commits + reviewers + labels) sem N+1. Útil para backfill. |
| **Webhooks**  | Real-time. Necessário para registrar eventos no momento que ocorrem (push, MR merged, deployment success). |

**Estratégia para o produto:** webhooks para eventos novos + REST para backfill e reconciliação periódica + GraphQL para queries complexas pontuais. Detalhes em [05-architecture.md](05-architecture.md).

## Autenticação

- **Personal Access Token (PAT):** simples; bom para MVP. Scopes mínimos: `read_api`, `read_repository`.
- **Project Access Token / Group Access Token:** scoped por projeto/grupo; melhor para produção single-tenant.
- **OAuth 2.0:** para multi-tenant, login interativo do usuário. Não prioritário.

Headers:
```
PRIVATE-TOKEN: <token>             (PAT/PrAT/GrAT)
Authorization: Bearer <token>      (OAuth)
```

Hospedagem self-managed ou SaaS (`gitlab.com`): a base URL muda mas o path da API (`/api/v4/...`) não.

## Endpoints REST relevantes para DORA

A seção descreve apenas os endpoints que **alimentam diretamente** as métricas. GitLab tem muito mais — não consumir o que não usamos.

### Projetos

```
GET /api/v4/projects/:id
GET /api/v4/projects?membership=true&per_page=100
```

Catálogo inicial: lista de projetos que a plataforma monitora. Salvar `id`, `path_with_namespace`, `default_branch`, `created_at`.

### Merge Requests

```
GET /api/v4/projects/:id/merge_requests
    ?state=merged
    &updated_after=ISO8601
    &per_page=100
    &order_by=updated_at
```

Campos críticos para **Lead Time**:

| Campo                     | Uso                                                            |
| ------------------------- | -------------------------------------------------------------- |
| `id` / `iid`              | Chave do MR                                                    |
| `target_branch`           | Filtrar só MRs que mergeram em `main`/`master`/default         |
| `merged_at`               | Momento do merge                                               |
| `merge_commit_sha`        | SHA do merge — usado para correlacionar com deploy             |
| `squash_commit_sha`       | Se foi squash, este é o SHA único; senão `null`                |
| `author`                  | Filtrar bots                                                   |
| `labels`                  | Identificar `hotfix`, `bug`, etc                               |
| `web_url`                 | Link para o MR no UI                                           |

Para o primeiro commit (denominador do Lead Time):

```
GET /api/v4/projects/:id/merge_requests/:iid/commits
```

Pega o commit com menor `committed_date` (ou `authored_date` — decidir e documentar; preferência por `authored_date` por ser mais fiel ao "início do trabalho").

### Deployments

```
GET /api/v4/projects/:id/deployments
    ?environment=production
    &status=success
    &updated_after=ISO8601
    &per_page=100
```

Campos críticos para **Deployment Frequency** e correlação com MRs:

| Campo                                     | Uso                                                                   |
| ----------------------------------------- | --------------------------------------------------------------------- |
| `id`                                      | Chave do deployment                                                   |
| `status`                                  | Apenas `success` conta para DF                                        |
| `environment.name`                        | Filtrar `production*` — ver [Identificação de deployments](#identificação-de-deployments) |
| `sha`                                     | SHA do commit deployado                                               |
| `ref`                                     | Branch/tag da origem                                                  |
| `created_at` / `finished_at`              | `finished_at` é o momento real do deploy concluído                    |
| `deployable.user`                         | Quem disparou (CI bot ou humano)                                      |

### Environments

```
GET /api/v4/projects/:id/environments
```

Útil para construir o catálogo de ambientes por projeto (parte do nosso modelo de dados; ver [Identificação de deployments](#identificação-de-deployments)).

### Issues (incidentes, se gerenciados no GitLab)

Algumas organizações gerenciam incidentes no **GitLab Issues** com tipo `incident` (suporte nativo a issue type incident desde GitLab 14):

```
GET /api/v4/projects/:id/issues
    ?issue_type=incident
    &state=closed
    &updated_after=ISO8601
```

Mas no nosso caso o **Jira é a fonte primária de incidentes**. Issues GitLab tipo `incident` ficam como fonte secundária opcional.

### Commits (apenas para correlação)

```
GET /api/v4/projects/:id/repository/commits/:sha
```

Usado pontualmente para resolver um SHA em commit metadata quando precisamos correlacionar um deployment ao MR de origem.

## GraphQL — quando vale a pena

Endpoint único: `POST /api/graphql`

Exemplo de query útil para backfill (todos os MRs de um projeto com commits e labels em uma chamada):

```graphql
query ($projectPath: ID!, $after: String) {
  project(fullPath: $projectPath) {
    mergeRequests(state: merged, first: 50, after: $after, sort: MERGED_AT_DESC) {
      pageInfo { endCursor hasNextPage }
      nodes {
        iid
        mergedAt
        targetBranch
        author { username }
        labels { nodes { title } }
        commits(first: 1, sort: AUTHORED_ASC) {
          nodes { authoredDate sha }
        }
      }
    }
  }
}
```

**Cuidado:** GraphQL no GitLab tem limites de complexidade (`complexity score`). Queries muito aninhadas falham. Em backfill, paginar agressivamente.

## Webhooks

Cadastrar webhooks por projeto (ou no nível de grupo, propagando):

```
POST /api/v4/projects/:id/hooks
{
  "url": "https://nossa-plataforma/webhooks/gitlab",
  "token": "<segredo-compartilhado>",
  "push_events": false,
  "merge_requests_events": true,
  "deployment_events": true,
  "pipeline_events": false,
  "issues_events": true,
  "enable_ssl_verification": true
}
```

**Eventos que consumimos:**

- `Merge Request Hook` (`object_kind: "merge_request"`, `action: "merge"`): registra Lead Time quando o MR é mergeado, mas ainda não o deploy.
- `Deployment Hook` (`object_kind: "deployment"`, `status: "success"`): evento principal — dispara recálculo de Lead Time (junto com o merge associado) e Deployment Frequency.
- `Issue Hook`: opcional, só se usarmos GitLab issues como fonte de incidente.

**Verificação:** GitLab manda o header `X-Gitlab-Token` com o token configurado. Validar em constant-time.

**Resiliência:** webhooks **podem ser perdidos** (problemas de rede, downtime da nossa plataforma). Não confiar só neles — sempre rodar um job de **reconciliação periódica** (ex: diária) que faz `updated_after=ontem` em MRs e deployments e preenche o que faltou.

## Paginação

REST GitLab paginação por header:

```
HTTP/1.1 200 OK
X-Total: 1234
X-Next-Page: 2
Link: <...page=2>; rel="next", <...>; rel="last"
```

Para datasets grandes (> 10.000 itens), usar **keyset pagination** em endpoints que suportam:

```
GET /api/v4/projects/:id/merge_requests?pagination=keyset&per_page=100&order_by=updated_at&sort=desc
```

A vantagem do keyset é não degradar performance em páginas profundas (offset-based fica lento após page ~50).

## Rate limits

**GitLab.com SaaS:** limites publicados pela GitLab e atualizados periodicamente. Por usuário autenticado, situa-se na faixa de centenas a alguns milhares de requisições por minuto, com limites mais estritos para alguns endpoints (autenticação, raw files, pipelines).

**GitLab self-managed:** configurável pelo administrador da instância.

**Headers de resposta:** `RateLimit-Remaining`, `RateLimit-Reset`, `Retry-After`.

**Estratégia no coletor:**

1. Cliente HTTP com **backoff exponencial** ao receber `429`.
2. **Rate limiter local** (token bucket) configurado abaixo do limite oficial — por padrão 60% do limite para deixar margem.
3. **Concorrência limitada** por projeto (max 4 workers em paralelo) para não saturar o limite global.
4. Métrica `rate_limit_remaining` observada e exposta no nosso próprio dashboard de saúde.

## Identificação de deployments

**[Decisão D3]** — qual evento GitLab conta como "deploy em produção"?

Opções, ordenadas da mais robusta para a mais fraca:

1. **Recurso `Deployment` da API com `environment` configurado como production.** ✅ Recomendado. Requer que cada projeto registre o stage de deploy via `environment:` no `.gitlab-ci.yml` e nomeie o environment como `production`, `prod`, `prd` etc. Padronizar com regex `^prod(uction)?(-[a-z0-9-]+)?$`.

2. **Tag em `main` com pattern `v*` ou `release-*`.** Frágil — depende de cada projeto adotar a convenção e nem todos fazem release com tag.

3. **Job de CI com nome específico (ex: `deploy:prod`).** Funciona, mas exige varrer pipelines em vez de deployments, e jobs falham com falsos negativos.

4. **Webhook customizado disparado pelo próprio deploy.** Bom em organizações maduras; coloca o ônus no time de DevOps.

**Recomendação:** usar (1) como primário, expor configuração por projeto para overridar com (2) ou (3) quando o time tiver um setup atípico. Persistir essa configuração no `projects` table do nosso schema.

## Mapeamento GitLab → métricas DORA

| Métrica                  | Fontes GitLab                                                                                                                       |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------- |
| **Lead Time for Changes**| `merge_request.commits[0].authored_date` → `deployment.finished_at` (do primeiro deployment de produção que inclui esse merge SHA)  |
| **Deployment Frequency** | Contagem de `deployment.status == success && environment matches production` por janela                                              |
| **Change Failure Rate**  | Denominador: deployments de produção. Numerador: ver [04-jira-integration.md](04-jira-integration.md) (Jira) ou rollbacks GitLab    |
| **MTTR**                 | Não vem do GitLab primariamente — Jira é fonte. Pode usar GitLab issues type=`incident` como fallback                                |

**Correlação MR ↔ Deployment:** o deployment tem `sha` mas não tem ponteiro direto ao MR. Estratégia: dado um deployment com SHA `X`, achar todos os merge commits do default branch ancestrais de `X` e ainda não associados a deployment anterior. Esses MRs receberam Lead Time naquele deployment.

Implementação: no banco, manter `deployments(sha, finished_at)` e `merge_requests(merge_commit_sha, merged_at)`. Para cada novo deployment, varrer MRs com `merged_at` entre o deployment anterior do mesmo environment e o atual.

## Considerações operacionais

- **Self-hosted vs SaaS:** o coletor precisa ser configurável quanto a base URL e CA bundle (instâncias self-hosted às vezes têm CA interno).
- **Múltiplas instâncias:** uma organização pode ter um GitLab.com + um GitLab self-hosted. Modelo de dados precisa suportar `instance_id` na tabela de projetos.
- **Projetos privados:** o token precisa ter visibilidade. Documentar isso na onboarding.
- **GitLab Premium/Ultimate features:** algumas métricas adicionais (Value Stream Analytics, Code Review Time) só estão em tiers pagos. Não depender delas no MVP — calculamos manualmente.

## Fontes

- GitLab REST API docs: `https://docs.gitlab.com/ee/api/`
- GitLab GraphQL API docs: `https://docs.gitlab.com/ee/api/graphql/`
- GitLab Webhooks: `https://docs.gitlab.com/ee/user/project/integrations/webhook_events.html`
- GitLab DORA metrics nativo: `https://docs.gitlab.com/ee/user/analytics/dora_metrics.html` (referência de comparação; não consumimos esse endpoint diretamente, calculamos os nossos próprios)

> Os endpoints e limites foram descritos com base em conhecimento consolidado da REST API v4 do GitLab. Antes de implementar, validar contra a versão da instância em uso (ex: `GET /api/v4/version`).

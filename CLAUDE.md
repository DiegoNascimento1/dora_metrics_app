# DORA Metrics App — Contexto do Projeto

> Memória compartilhada do time. Toda sessão Claude Code neste repositório carrega este arquivo automaticamente. Mantenha-o conciso e atualizado.

## O que é

Plataforma completa de **DORA Metrics** (DevOps Research and Assessment) que:

1. **Coleta** dados de entrega de software a partir do **GitLab** (commits, merge requests, deployments, incidentes) e do **Jira** (issues, releases, sprints, bugs de produção).
2. **Calcula** as 4 métricas DORA (Lead Time for Changes, Deployment Frequency, Change Failure Rate, Mean Time to Restore) + métricas auxiliares.
3. **Visualiza** em dashboards com séries temporais, drill-down por time/produto e classificação Elite/High/Medium/Low.
4. **Alerta** em degradação de métricas via webhooks/email.
5. **Histórica** os dados em armazenamento próprio (não depende de queries ao vivo no GitLab/Jira para visualizações).

## Integrações

| Fonte   | Mecanismo principal                     | Fallback           |
| ------- | --------------------------------------- | ------------------ |
| Jira    | **MCP (Atlassian Rovo MCP Server)** via OAuth 2.1 | Jira Cloud REST API v3 |
| GitLab  | **GitLab REST API v4** + Webhooks       | GraphQL para queries complexas |

A escolha de MCP para o Jira segue dois objetivos: (a) padronizar o acesso a dados Atlassian via protocolo aberto que outras ferramentas/agentes do time também usem, (b) reaproveitar o servidor remoto oficial Atlassian (`https://mcp.atlassian.com/v1/mcp`) sem manter um cliente REST customizado.

## Stack

- **Backend:** Go **1.26** — `chi`, `pgx/v5`, `sqlc`, `asynq`, `zerolog`. Ver [ADR 0001](docs/adr/0001-stack-go-angular.md).
- **Frontend:** Angular **22** (standalone) — Angular Material, `ng2-charts`, `@ngrx/signals`. Node **24 LTS** no toolchain. Ver [ADR 0001](docs/adr/0001-stack-go-angular.md).
- **Banco:** PostgreSQL **18** puro (sem TimescaleDB no MVP). Ver [ADR 0002](docs/adr/0002-database-postgresql.md).
- **Contrato API:** OpenAPI; types do front gerados via `openapi-typescript`.
- **Layout do repo:** monorepo com `backend/` e `frontend/`; spec em `openapi.yaml` na raiz.

## Documentação de estudo

A pasta [docs/](docs/) contém o estudo profundo que fundamenta as decisões de produto e arquitetura. **Leia antes de começar a implementar.** Índice em [docs/README.md](docs/README.md).

## Convenções

- Idioma: **Português (BR)** para docs e comentários conceituais; identificadores de código em **inglês**.
- Datas em ISO 8601 (`YYYY-MM-DD`).
- Termos DORA mantidos em inglês (Lead Time, Deployment Frequency etc.) — ver [docs/08-glossario.md](docs/08-glossario.md).

## Para o agente (Claude Code)

- Antes de propor implementação, confirme que está alinhado com [docs/05-architecture.md](docs/05-architecture.md) e [docs/06-data-model.md](docs/06-data-model.md).
- Não introduza dependências de stack sem registrar a decisão em um ADR dentro de [docs/adr/](docs/adr/).
- Não invente endpoints do GitLab/Jira — sempre confira contra [docs/03-gitlab-integration.md](docs/03-gitlab-integration.md) e [docs/04-jira-integration.md](docs/04-jira-integration.md) ou a doc oficial.
- **Commits:** não incluir trailer `Co-Authored-By` apontando para Claude/IA. Os commits devem ficar em nome do autor humano apenas.

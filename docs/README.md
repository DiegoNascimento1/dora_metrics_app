# Documentação — DORA Metrics App

Estudo profundo que fundamenta o produto. Leia na ordem se for sua primeira vez.

## Índice

| #   | Documento                                                | Conteúdo                                                                                                   |
| --- | -------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| 01  | [DORA Metrics — fundamentos](01-dora-metrics.md)         | O que são as 4 métricas, como calcular, benchmarks Elite/High/Medium/Low, armadilhas comuns.               |
| 02  | [MCP (Model Context Protocol)](02-mcp-protocol.md)       | Arquitetura, transports (stdio / Streamable HTTP), OAuth 2.1, servidor Atlassian Rovo, padrões de consumo. |
| 03  | [Integração GitLab](03-gitlab-integration.md)            | Endpoints REST relevantes, GraphQL, webhooks, mapeamento GitLab → métricas DORA, paginação e rate limits.  |
| 04  | [Integração Jira (via MCP)](04-jira-integration.md)      | Uso do Atlassian MCP Server, ferramentas disponíveis, JQL, mapeamento Jira → métricas, fallback REST.      |
| 05  | [Arquitetura](05-architecture.md)                        | Visão de componentes da plataforma completa, fluxos de coleta, opções de stack, trade-offs.                |
| 06  | [Modelo de dados](06-data-model.md)                      | Entidades, eventos, agregações, esquema sugerido, particionamento temporal.                                |
| 07  | [Roadmap](07-roadmap.md)                                 | Fases de implementação (MVP → Plataforma), critérios de saída de cada fase.                                |
| 08  | [Glossário](08-glossario.md)                             | Termos DORA + termos do domínio do produto.                                                                |

## Decisões

Ver [docs/adr/](adr/) para o histórico completo de ADRs.

### Decididas

- **D1 — Stack:** Go (backend) + Angular (frontend). Ver [ADR 0001](adr/0001-stack-go-angular.md).
- **D2 — Banco:** PostgreSQL 16+ puro. Ver [ADR 0002](adr/0002-database-postgresql.md).

### Pendentes

- **D3** — Estratégia de identificação de "deployment" no GitLab (environment `production`, tags, jobs específicos). Discussão em [03-gitlab-integration.md](03-gitlab-integration.md#identifica%C3%A7%C3%A3o-de-deployments).
- **D4** — Estratégia de identificação de "incidente" (Jira issue type, label, integração externa tipo PagerDuty). Discussão em [04-jira-integration.md](04-jira-integration.md#identifica%C3%A7%C3%A3o-de-incidentes).
- **D5** — Estratégia de auth do servidor MCP em ambiente headless (OAuth 2.1 device flow vs API token). Discussão em [02-mcp-protocol.md](02-mcp-protocol.md#autentica%C3%A7%C3%A3o-em-ambiente-headless).

## Como contribuir com a documentação

- Cada doc tem uma seção "Fontes" no final listando o que foi usado como referência.
- Mudanças que afetem decisões de arquitetura devem virar um ADR em `docs/adr/NNNN-titulo.md`.
- Use Markdown puro (sem extensões específicas de plataforma) e linkagem relativa entre docs.

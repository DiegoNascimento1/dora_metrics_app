# 07 — Roadmap

Fases de construção da plataforma. Cada fase tem **critérios de saída** — só passa para a próxima quando todos os critérios estão verdes.

A intenção é ter algo útil **em produção interna** já na Fase 1, e ir adicionando valor sem grandes rewrites.

## Fase 0 — Fundação (1 sprint)

**Objetivo:** preparar o terreno para que a Fase 1 seja só código de produto.

- [ ] Decisão D1 (stack) registrada em ADR.
- [ ] Decisão D2 (banco) registrada em ADR.
- [ ] Repo inicializado com lint, formatter, CI básico.
- [ ] Esqueleto de migrations rodando contra Postgres local em Docker.
- [ ] Camada de auth/secret management definida (mesmo que minimal: env var + abstração para vault depois).
- [ ] Decisões D3 (deployment GitLab) e D4 (incidente Jira) com defaults configurados.

**Critério de saída:** `make up && make migrate && make test` funciona em qualquer máquina nova.

## Fase 1 — MVP de coleta (2–3 sprints)

**Objetivo:** ingerir dados suficientes para calcular **uma** métrica (Deployment Frequency) de **um** projeto.

- [ ] Cadastro manual de 1 `source_instance` GitLab + 1 `project`.
- [ ] Coletor GitLab via REST (polling 5min) que descobre deployments do projeto.
- [ ] Persistência em `raw_event` + `deployment` + `environment`.
- [ ] Recálculo da agregação `metric_window` para janela 30d com DF.
- [ ] CLI ou endpoint REST simples que retorna `{deployment_frequency: X}` para o projeto.

**Critério de saída:** com 1 projeto cadastrado, vejo o número correto de deploys/dia dos últimos 30 dias, e o número se atualiza automaticamente após novo deploy.

## Fase 2 — As 4 métricas (3–4 sprints)

**Objetivo:** calcular as 4 métricas DORA completas a partir de GitLab + Jira (REST direto, ainda sem MCP).

- [ ] Coletor de Merge Requests do GitLab (incluindo commits para `first_commit_at`).
- [ ] Cálculo de Lead Time com correlação MR ↔ deployment.
- [ ] Coletor Jira via REST API v3 (issues com `issuetype=Incident`).
- [ ] Cálculo de MTTR a partir de incidents.
- [ ] Cálculo de CFR com associação por janela temporal pós-deploy.
- [ ] Endpoint REST `/metrics/{project}/dora` retornando as 4 métricas.
- [ ] Webhooks GitLab e Jira recebendo eventos (substituindo polling onde possível).
- [ ] Job de reconciliação noturna.
- [ ] Classificação Elite/High/Medium/Low configurável.

**Critério de saída:** o endpoint retorna valores corretos validados manualmente contra 2–3 projetos reais. Mudanças em produção (novo deploy, novo incidente) aparecem em < 5 minutos.

## Fase 3 — Dashboard e MCP Jira (2–3 sprints)

**Objetivo:** apresentação visual + migrar coleta Jira para MCP.

- [ ] Frontend com os 4 tiles principais e séries temporais 90 dias.
- [ ] Drill-down: clicar em DF abre lista de deployments daquela janela.
- [ ] Autenticação OIDC.
- [ ] Refactor do coletor Jira para usar MCP Atlassian (`mcp.atlassian.com/v1/mcp`) com REST como fallback.
- [ ] Multi-projeto e multi-time (filtros e agrupamentos).

**Critério de saída:** stakeholders conseguem abrir o dashboard sozinhos, comparar 2 times, e identificar visualmente uma piora de métrica.

## Fase 4 — Alertas e múltiplos tenants (2 sprints)

**Objetivo:** operacionalizar como ferramenta de uso diário.

- [ ] Engine de alertas com regras configuráveis.
- [ ] Webhook out → Slack, email.
- [ ] Suporte a múltiplas `source_instance` simultâneas (ex: GitLab.com + GitLab self-hosted da mesma org).
- [ ] Suporte a múltiplos tenants reais (isolamento, billing-like — mesmo que internamente).
- [ ] Histórico mensal congelado (`metric_monthly_snapshot`).
- [ ] Exportação CSV/JSON.

**Critério de saída:** time recebe alerta no Slack quando CFR ultrapassa limiar, e o ruído está controlado (regra de "mudança de estado", não disparar todo dia).

## Fase 5 — Servidor MCP próprio + análise (2 sprints)

**Objetivo:** expor as métricas para consumo por agentes/LLMs e adicionar análise contextual.

- [ ] Servidor MCP próprio expondo tools: `getDoraMetrics`, `getDeployments`, `compareTeams`, `explainTrend`.
- [ ] Autenticação OAuth 2.1 do nosso MCP.
- [ ] Tool `explainTrend` que combina dados + LLM para produzir narrativa ("CFR subiu 8pp na última semana porque o deploy de 2026-04-18 causou 3 incidents...").
- [ ] Recursos: cada métrica acessível por URI MCP estável.

**Critério de saída:** um SRE pergunta ao Claude "como está nosso CFR?" via desktop e recebe resposta com dados reais da nossa plataforma.

## Fase 6 — Métricas auxiliares e refinamentos (contínuo)

A partir daqui, evolução contínua sem big bangs. Backlog:

- Code Review Time, Pickup Time (sub-componentes do Lead Time).
- DORA Reliability (v2): integração com SLOs externos.
- Predição: alertar antes de degradação, baseado em sinais antecedentes.
- Comparação com benchmarks de indústria (anonimizado).
- Integração com PagerDuty/Opsgenie para incidentes.

## Princípios para priorização

- **Valor antes de elegância:** Fase 1 pode ter código quadradinho, desde que mostre o número certo.
- **Não construir o que não vamos usar em < 1 sprint.** Multi-tenant existe no schema desde o dia 1, mas o UI multi-tenant só na Fase 4.
- **Cada fase deve poder ir a produção.** Não há fase "só refactor" — sempre entrega valor visível.
- **Mudar uma decisão é barato se ela está documentada.** ADRs por decisão importante.

## Riscos e mitigação

| Risco                                                                | Mitigação                                                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Webhooks GitLab/Jira pouco confiáveis                                | Reconciliação noturna como segurança; nunca depender só de webhook                                 |
| Definição de "produção" varia entre projetos                         | Configuração por projeto (`production_env_pattern` + `incident_jql`) desde o início                |
| Atlassian muda capabilities do MCP server                            | Discovery dinâmico de tools na inicialização; logging; manter fallback REST                        |
| Volume de dados explode em organizações grandes                      | Particionamento de `raw_event` por dia + retenção; agregações compactas; opção de migrar p/ Timescale |
| Time não confia nas métricas (data quality)                          | Cada métrica exibida com link de drill-down até os eventos brutos; auditável                       |
| Métrica gameada (deploy de MR vazio para inflar DF)                  | Reportar tamanho médio de mudança junto da DF; sinalizar anomalias                                 |

## Fontes

- Doc de métricas: [01-dora-metrics.md](01-dora-metrics.md)
- Doc de arquitetura: [05-architecture.md](05-architecture.md)
- Doc de modelo de dados: [06-data-model.md](06-data-model.md)

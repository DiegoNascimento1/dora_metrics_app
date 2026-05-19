# 08 — Glossário

Termos usados no produto e nos demais documentos. Quando um termo tem origem em uma especificação/relatório, mantemos o nome em inglês.

## DORA

**DORA (DevOps Research and Assessment)** — Programa de pesquisa do Google Cloud que publica anualmente o "Accelerate State of DevOps Report". Define as 4 métricas adotadas neste produto.

**4 Key Metrics** — As quatro métricas principais do DORA: Lead Time for Changes, Deployment Frequency, Change Failure Rate, MTTR. Detalhes em [01-dora-metrics.md](01-dora-metrics.md).

**Lead Time for Changes (LTC)** — Tempo entre o primeiro commit de uma mudança e seu deploy em produção. Reportado como mediana em uma janela de tempo.

**Deployment Frequency (DF)** — Quantidade de deploys de produção por unidade de tempo. Reportado como deploys/dia ou em categoria (on-demand, weekly, monthly).

**Change Failure Rate (CFR)** — Percentual de deploys em produção que resultaram em falha que precisou de remediação (rollback/hotfix/patch).

**Mean Time to Restore (MTTR)** — Tempo médio entre início e resolução de incidentes em produção. Também chamado "Mean Time to Recover".

**Elite / High / Medium / Low** — Quatro tiers de performance do DORA Report. Times Elite combinam alta frequência de deploy com alta estabilidade.

**Reliability** — Métrica adicional introduzida no DORA v2 (relatório 2021+) medindo disponibilidade percebida e satisfação operacional. Opcional no nosso produto.

## MCP

**MCP (Model Context Protocol)** — Protocolo aberto para padronizar consumo de contexto e ferramentas por aplicações de LLM. Mais em [02-mcp-protocol.md](02-mcp-protocol.md).

**Host MCP** — Aplicação cliente que consome servidores MCP (ex: Claude Desktop, IDE, nosso coletor).

**Servidor MCP** — Serviço que expõe ferramentas/recursos/prompts via protocolo MCP.

**stdio (transport)** — Transporte local — host inicia o servidor como processo filho e troca mensagens via stdin/stdout.

**Streamable HTTP (transport)** — Transporte remoto — endpoint HTTPS único que suporta requisições JSON-RPC e streaming via SSE.

**Tools, Resources, Prompts** — As três primitivas que um servidor MCP pode expor. Detalhe em [02-mcp-protocol.md § Primitivas](02-mcp-protocol.md#primitivas-do-protocolo).

**JSON-RPC 2.0** — Formato de mensageria do MCP (`{jsonrpc, id, method, params}`).

**Atlassian Rovo MCP Server** — Servidor MCP oficial da Atlassian em `https://mcp.atlassian.com/v1/mcp`. Cobre Jira, Confluence e Compass.

**OAuth 2.1 / PKCE** — Padrão de autenticação recomendado para servidores MCP remotos.

## GitLab

**Project** — Repositório no GitLab. Unidade primária de monitoramento da nossa plataforma.

**Merge Request (MR)** — Equivalente GitLab de "pull request". Pedido de incorporação de uma branch.

**Environment** — Recurso GitLab que representa um ambiente lógico de deploy (production, staging, etc).

**Deployment** — Recurso GitLab que representa um ato de deploy a um environment. Tem `status`, `sha`, `finished_at`.

**Pipeline** — Conjunto de jobs executados pelo GitLab CI. Pode (mas não precisa) gerar deployments.

**Default branch** — Branch principal do projeto (`main`/`master`/outro). Configurável por projeto.

**PAT (Personal Access Token)** — Token de autenticação pessoal. Tem scopes (`read_api`, `read_repository`).

**PrAT / GrAT** — Project / Group Access Token. Escopo limitado a um projeto ou grupo.

**Smart Commits** — Sintaxe em mensagem de commit (`PAY-123 #close`) que cria links automáticos com issues Atlassian.

## Jira

**Issue** — Unidade básica do Jira (story, task, bug, incident, etc).

**Issue type** — Tipo da issue. Para nossas métricas, `Incident` é o tipo central.

**Issue key** — Identificador externo (ex: `PAY-1234`).

**JQL (Jira Query Language)** — Linguagem de busca do Jira. Toda nossa coleta de incidentes parte de queries JQL.

**Status category** — Classificação geral do status: `new`, `indeterminate`, `done`. Independente do nome customizado do status.

**Fix Version** — Campo de release ao qual uma issue está atribuída. Útil para granular Lead Time por release.

**Cloud ID** — Identificador único de um site Atlassian Cloud. Necessário em chamadas via API e MCP.

**Custom field** — Campo customizado, identificado por `customfield_XXXXX`. Descobrir via `/rest/api/3/field`.

**JSM (Jira Service Management)** — Produto Atlassian que disponibiliza o issue type `Incident` nativo.

**Remote issue link** — Link entre uma issue Jira e um recurso externo (ex: MR do GitLab via Smart Commits).

## Plataforma (nosso produto)

**Tenant** — Cliente lógico da plataforma. Inicialmente um só; modelo de dados já preparado para múltiplos.

**Source instance** — Uma instância externa (um GitLab + um Jira). Um tenant pode ter várias.

**Team** — Time de engenharia. Vincula projetos para agregação.

**Project (na nossa base)** — Projeção interna de um GitLab project monitorado. Tem configuração de pattern de produção, JQL de incidentes, time vinculado etc.

**raw_event** — Tabela append-only com eventos brutos vindos de GitLab/Jira. Fonte de verdade reprocessável.

**metric_window** — Tabela de agregação por janela rolante (7/30/90d). Lida pelo dashboard.

**metric_monthly_snapshot** — Snapshot imutável mensal das métricas.

**Coletor** — Componente que traz dados externos para o nosso storage (webhook receivers + pollers + reconciliação).

**Calculadora** — Componente que deriva métricas dos eventos brutos.

**Reconciliação** — Job que compara estado externo com nosso estado e preenche o que falta (recupera webhooks perdidos).

**Classification threshold** — Limiares Elite/High/Medium/Low configuráveis por tenant.

## Operacional

**Idempotência** — Toda inserção chaveada por ID externo; webhook duplicado não duplica dados.

**Backoff exponencial** — Estratégia de retry com intervalos crescentes ao receber 429/5xx.

**Keyset pagination** — Paginação por cursor (não por offset). Necessária em datasets grandes para não degradar performance.

**Webhook secret** — Chave compartilhada usada para validar autenticidade de webhooks (`X-Gitlab-Token` ou HMAC Jira).

**ADR (Architecture Decision Record)** — Documento curto registrando uma decisão arquitetural e seu contexto. Vão em `docs/adr/NNNN-titulo.md`.

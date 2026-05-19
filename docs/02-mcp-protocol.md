# 02 — Model Context Protocol (MCP)

## O que é MCP

**Model Context Protocol** é um protocolo aberto, criado pela Anthropic em novembro de 2024, que padroniza como **aplicações hospedeiras de LLMs** (clientes) consomem contexto e ferramentas expostos por **serviços externos** (servidores). A analogia oficial é "USB-C para aplicações de IA": qualquer cliente compatível conecta a qualquer servidor compatível.

Antes de MCP, cada integração entre LLM e ferramenta externa (Jira, GitHub, banco de dados) precisava ser construída sob medida pelo provedor de IA. Com MCP, o **provedor da ferramenta** (ex: Atlassian) hospeda um servidor MCP padronizado, e **qualquer cliente de IA** (Claude Desktop, IDEs com agentes, plataformas internas) consome.

Para o nosso produto isso importa porque:

1. **Acesso ao Jira:** consumimos o servidor MCP oficial da Atlassian em vez de manter um cliente REST customizado.
2. **Exposição futura:** podemos, opcionalmente, **expor um servidor MCP próprio** que entrega nossas métricas DORA como ferramenta consumível por agentes (ex: "Claude, qual a Deployment Frequency do time de payments nos últimos 30 dias?").

## Arquitetura

```
┌─────────────────────┐                    ┌──────────────────────┐
│  Host de IA         │                    │  Servidor MCP        │
│  (cliente)          │   JSON-RPC 2.0     │  (ex: Atlassian)     │
│                     │ ◄─────────────────►│                      │
│  • Claude Desktop   │   stdio  ou        │  • Lista ferramentas │
│  • IDE com agente   │   Streamable HTTP  │  • Executa chamadas  │
│  • Nossa plataforma │                    │  • Fornece recursos  │
└─────────────────────┘                    └──────────────────────┘
```

**Mensageria:** JSON-RPC 2.0 — todas as requisições, respostas e notificações seguem o formato `{jsonrpc, id, method, params}`.

**Sessão:** stateful — após `initialize` o servidor mantém estado da sessão (capacidades negociadas, recursos subscritos).

**Inspiração:** o desenho de mensagens é fortemente inspirado em **LSP (Language Server Protocol)**. Quem já trabalhou com extensões de IDE reconhece o padrão.

## Transports

MCP define oficialmente **dois transports**. A spec roadmap 2026 confirmou que não haverá novos transports, apenas evolução dos existentes.

### 1. `stdio` — para servidores locais

O host **inicia o servidor MCP como processo filho** e troca mensagens via `stdin`/`stdout`. Cada mensagem JSON-RPC é uma linha (newline-delimited).

**Quando usar:** servidores que rodam na mesma máquina do cliente, sem rede. Útil para ferramentas que precisam de acesso ao filesystem local, ao git local, ao Docker local etc.

**Não é o nosso caso para Jira/GitLab** — esses são serviços remotos.

### 2. `Streamable HTTP` — para servidores remotos

O servidor expõe um endpoint HTTPS único. O cliente envia requisições POST com mensagens JSON-RPC. Para respostas longas ou streaming, o servidor responde com `Content-Type: text/event-stream` (SSE) sobre a mesma conexão HTTP. Suporta múltiplos clientes concorrentes e escala horizontalmente.

**Autenticação:** OAuth 2.1 com PKCE, ou bearer tokens estáticos (API tokens).

**Quando usar:** sempre que o servidor for hospedado em outra máquina/serviço. **É o caso do Atlassian Rovo MCP.**

> Nota histórica: até meados de 2025 existia também o transport **HTTP+SSE** (dois endpoints separados). Ele foi consolidado em **Streamable HTTP** (endpoint único). O endpoint legado SSE da Atlassian será **descontinuado após junho de 2026** — implementar direto contra Streamable HTTP.

## Primitivas do protocolo

Um servidor MCP pode expor três tipos de primitivas:

### Tools (ferramentas)

Operações que o LLM pode invocar — funções com parâmetros tipados (JSON Schema) e resultados estruturados. **Esta é a primitiva principal para nosso caso.**

Exemplo (servidor Atlassian Rovo):

```json
{
  "name": "getJiraIssue",
  "description": "Retrieve a Jira issue by key",
  "inputSchema": {
    "type": "object",
    "properties": {
      "cloudId": {"type": "string"},
      "issueKey": {"type": "string"}
    },
    "required": ["cloudId", "issueKey"]
  }
}
```

O cliente lista as ferramentas com `tools/list` e as invoca com `tools/call`.

### Resources (recursos)

Conteúdo somente-leitura referenciado por URI (ex: `jira://issue/PROJ-123`). O cliente subscreve a recursos e recebe notificações de mudança.

**Para nosso caso:** menos relevante no curto prazo. Útil futuramente se quisermos expor um issue Jira como contexto persistente em uma conversa.

### Prompts (templates)

Templates de prompt parametrizados que o servidor sugere. Permitem que o servidor "ensine" o cliente como usar suas ferramentas.

**Para nosso caso:** não usaremos no MVP.

## Fluxo de uma sessão (Streamable HTTP)

1. **Initialize:** cliente envia `initialize` com sua versão de protocolo e capacidades; servidor responde com suas capacidades.
2. **Discovery:** cliente chama `tools/list`, `resources/list`, `prompts/list` conforme interesse.
3. **Invocation:** cliente chama `tools/call` com `{name, arguments}`. Servidor retorna conteúdo estruturado.
4. **Notifications:** o servidor pode enviar notificações assíncronas (ex: lista de tools mudou) sobre o canal SSE.
5. **Shutdown:** cliente encerra a sessão.

## Autenticação

### OAuth 2.1 (recomendado para produção)

Fluxo `authorization_code` com PKCE. O servidor MCP **age como Resource Server** e tipicamente o **Authorization Server** é separado (ex: Atlassian Identity).

**Em ambientes interativos** (Claude Desktop, IDEs): o cliente abre o browser do usuário, este faz login no provedor (Atlassian), o callback retorna um `code` que o cliente troca por `access_token` + `refresh_token`.

**Em ambientes headless** (nosso backend coletor rodando em servidor): ver [Decisão D5 em docs/README.md](README.md#decis%C3%B5es-abertas). Opções:

- **Device authorization flow (RFC 8628):** o backend mostra um código que um humano valida em outro device. Bom para setup inicial, refresh automático depois.
- **API token estático:** Atlassian permite API tokens scoped que funcionam como bearer. Mais simples, mas o token é amplo (não user-scoped fino) e a rotação é manual.
- **Service account com OAuth Client Credentials:** preferível mas requer ser Atlassian Partner ou usar Forge.

### Recomendação inicial

Começar com **API token estático** (variável de ambiente, rotacionada manualmente) e migrar para **device flow + refresh token persistido** quando o produto for multi-tenant.

## Servidor Atlassian Rovo MCP

**Endpoint oficial:** `https://mcp.atlassian.com/v1/mcp`

**Hospedagem:** Cloudflare (gerenciado pela Atlassian, fora do nosso controle).

**Status (maio 2026):** GA desde fevereiro de 2026.

**Capacidades:** o servidor expõe ferramentas para Jira, Confluence e Compass. Para o nosso caso (DORA), as relevantes são as de Jira. Detalhamento em [04-jira-integration.md](04-jira-integration.md).

**Autenticação:** OAuth 2.1 ou API tokens. As ações executadas **respeitam as permissões do usuário autenticado** — se o token é de um usuário que não vê certos projetos, o MCP server vai retornar erro `permission_denied` para essas issues.

## Padrão de consumo no nosso backend

Embora MCP tenha sido projetado para LLMs serem clientes, **nada impede um backend determinístico** de ser cliente MCP. É exatamente isso que faremos para coletar dados do Jira:

```
┌──────────────────────┐
│  Coletor (workers)   │
│  ─ Scheduler         │   Streamable HTTP     ┌─────────────────────┐
│  ─ MCP Client SDK    │ ────────────────────► │ Atlassian Rovo MCP  │
│  ─ Storage adapter   │   JSON-RPC + OAuth 2.1│ mcp.atlassian.com   │
└──────────────────────┘                       └─────────────────────┘
```

O backend lista as tools disponíveis na inicialização, mapeia para um conjunto fixo de operações que precisamos (`searchJiraIssues`, `getJiraIssue`, etc) e invoca de forma agendada.

**Por que MCP em vez de bater na REST direto?**

| Aspecto              | MCP (via Atlassian)                          | REST direto (Jira Cloud API v3)                   |
| -------------------- | -------------------------------------------- | ------------------------------------------------- |
| Padronização         | Mesma interface se Atlassian mudar versão    | Quebra a cada major version                       |
| Auth                 | OAuth 2.1 + API token, ambos suportados      | Mesmo, mas configuração manual                    |
| Rate limiting        | Centralizado pelo MCP server                 | Por endpoint, mais difícil de orquestrar          |
| Cobertura            | Crescente, mas pode não ter todos endpoints  | 100% — qualquer endpoint REST                     |
| Latência             | Hop extra (MCP server intermedeia)           | Direto, mais rápido                               |
| Auditoria            | Logs centralizados no MCP                    | Distribuído                                       |
| Reuso interno        | Mesmo servidor MCP que o time já usa em IDEs | Cliente REST exclusivo do nosso produto           |

**Estratégia:** MCP como primary, REST como fallback explícito quando uma operação específica não estiver coberta. Detalhes em [04-jira-integration.md](04-jira-integration.md).

## SDKs cliente

A Anthropic mantém SDKs oficiais para vários runtimes. Para nosso caso, considerar:

- **Python:** `mcp` (pip) — cliente e servidor, maturidade alta.
- **TypeScript/Node:** `@modelcontextprotocol/sdk` — cliente e servidor, maturidade alta.
- **Go:** implementações comunitárias (ex: `mark3labs/mcp-go`), maturidade média.
- **Java/Kotlin:** SDK oficial em desenvolvimento, ainda menos maduro.

A escolha do SDK depende da [Decisão D1 — Stack](05-architecture.md#decis%C3%A3o-d1--stack). **MCP não deve ser o fator decisivo na escolha de stack**, mas Python e Node têm a integração mais polida hoje.

## Expor nosso próprio servidor MCP (futuro)

A partir da Fase 3 do roadmap (ver [07-roadmap.md](07-roadmap.md)), pretendemos expor **nossas métricas DORA** como servidor MCP próprio. Ferramentas previstas:

- `getDoraMetrics(team, period)` — retorna as 4 métricas para um time/período.
- `getDeployments(project, range)` — lista de deployments com metadata.
- `compareTeams(teams, period)` — comparação entre times.
- `explainTrend(team, metric, period)` — análise textual de tendência (combina dados + LLM).

Isso permite que um SRE pergunte ao Claude "como está nosso CFR esse mês?" e receba resposta direta, autorizada pelas mesmas permissões da plataforma.

## Autenticação em ambiente headless

Esta é a **Decisão D5** mencionada no README do docs/. Pendente. Resumo das opções:

| Opção                          | Setup            | Rotação       | Segurança          | Recomendado para                       |
| ------------------------------ | ---------------- | ------------- | ------------------ | -------------------------------------- |
| API token estático             | Manual no painel | Manual        | Token amplo        | MVP / desenvolvimento                  |
| OAuth 2.1 device flow          | 1 vez interativo | Refresh auto  | Scoped + revogável | Single-tenant produção                 |
| OAuth client credentials       | Forge/Partner    | Refresh auto  | Service account    | Multi-tenant (futuro)                  |

Próximo passo: implementar com API token e abstrair a camada de auth atrás de uma interface `AuthProvider` para trocar sem reescrever o cliente.

## Fontes

- Model Context Protocol — [Complete Guide 2026](https://www.essamamdani.com/blog/complete-guide-model-context-protocol-mcp-2026)
- Model Context Protocol Blog — [2026 MCP Roadmap](https://blog.modelcontextprotocol.io/posts/2026-mcp-roadmap/)
- Model Context Protocol Blog — [Future of MCP Transports](https://blog.modelcontextprotocol.io/posts/2025-12-19-mcp-transport-future/)
- Atlassian — [Introducing Atlassian's Remote MCP Server](https://www.atlassian.com/blog/announcements/remote-mcp-server)
- Atlassian — [Atlassian MCP Server (GitHub)](https://github.com/atlassian/atlassian-mcp-server)
- MindStudio — [Atlassian MCP Server GA](https://www.mindstudio.ai/blog/atlassian-mcp-server-ga-claude-reads-writes-jira-confluence-compass-oauth)
- DEV Community — [Complete Guide to MCP 2026](https://dev.to/x4nent/complete-guide-to-mcp-model-context-protocol-in-2026-architecture-implementation-and-4a11)

> Spec oficial em modelcontextprotocol.io. Próxima release da spec prevista para junho de 2026 — revisar este doc após publicação.

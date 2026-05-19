# DORA Metrics App

Plataforma de DORA Metrics integrando GitLab (API) e Jira (MCP).
Veja o estudo completo em [docs/](docs/README.md) e o contexto em [CLAUDE.md](CLAUDE.md).

## Stack (maio/2026)

- **Backend:** Go 1.26 — chi, pgx/v5, sqlc, asynq
- **Frontend:** Angular 22 (standalone) — Angular Material, ng2-charts, NgRx signals — Node 24 LTS
- **Banco:** PostgreSQL 18
- **Fila:** Redis (asynq)

## Requisitos

- Docker + Docker Compose
- Go 1.26+
- Node 24+ (npm 10+)
- `make`

Opcionais:
- `golangci-lint` 1.62+
- `sqlc` 1.27+
- `golang-migrate`
- `openapi-typescript`

## Quick start

```bash
cp .env.example .env
make up                    # sobe Postgres + Redis
make be-migrate            # aplica schema
make be-sqlc               # gera código de DB

# Em um terminal:
make be-run                # roda API em :8080

# Em outro:
make fe-install
make fe-run                # roda Angular em :4200
```

Para subir tudo em containers:

```bash
make up-full
```

## Layout

```
.
├── backend/        Go: api + worker + migrations
├── frontend/       Angular 22
├── docs/           Estudo profundo + ADRs
├── openapi.yaml    Contrato API (gera types pro front)
├── docker-compose.yml
└── Makefile
```

## Próximos passos

Ver [docs/07-roadmap.md](docs/07-roadmap.md). Estamos saindo da Fase 0; próximo: Fase 1 — MVP de coleta.

## Documentação

| Doc | Conteúdo |
|---|---|
| [CLAUDE.md](CLAUDE.md) | Contexto do projeto carregado pelas sessões Claude Code |
| [docs/](docs/README.md) | Estudo profundo (DORA, MCP, GitLab, Jira, arquitetura) |
| [docs/adr/](docs/adr/) | Decisões arquiteturais registradas |
